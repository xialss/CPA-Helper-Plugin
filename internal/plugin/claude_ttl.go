package plugin

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"cpa-helper-plugin/internal/policy"
	"cpa-helper-plugin/internal/snapshot"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type frozenTTL struct {
	scope    string
	ttl      string
	revision uint64
	token    string
	guard    *ttlConfigGuard
	created  time.Time
}

type ttlSelection struct {
	request *frozenTTL
	applied bool
}

type claudeTTL struct {
	mu         sync.Mutex
	guard      *ttlConfigGuard
	store      *snapshot.TTLMetadataStore
	requests   map[string]*frozenTTL
	tokens     map[string]*frozenTTL
	selections map[string]*ttlSelection
	lastSweep  time.Time
	lastError  string
	unknown    uint64
}

func newClaudeTTL() *claudeTTL {
	return &claudeTTL{requests: map[string]*frozenTTL{}, tokens: map[string]*frozenTTL{}, selections: map[string]*ttlSelection{}}
}

func (c *claudeTTL) configure(guard *ttlConfigGuard, store *snapshot.TTLMetadataStore) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.guard, c.store, c.lastError = guard, store, ""
}

// Freeze even an inherited TTL, so a hot update cannot enable it mid-request.
func (c *claudeTTL) begin(req pluginapi.RequestInterceptRequest, key policy.Key, revision uint64) (pluginapi.RequestInterceptResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	response := pluginapi.RequestInterceptResponse{ClearHeaders: []string{ttlHeader, ttlRequestHeader}}
	if state := c.requests[req.RequestID]; state != nil {
		if state.scope != key.ID {
			return response, errors.New("Claude TTL request identity changed")
		}
		response.Headers = http.Header{ttlRequestHeader: {state.token}}
		return response, nil
	}
	ttl := key.ClaudeCacheTTL
	if generate, exists := req.Metadata["generate"].(bool); exists && !generate {
		ttl = ""
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return response, errors.New("cannot create Claude TTL request identity")
	}
	state := &frozenTTL{scope: key.ID, ttl: ttl, revision: revision, token: hex.EncodeToString(token[:]), guard: c.guard, created: time.Now().UTC()}
	c.requests[req.RequestID], c.tokens[state.token] = state, state
	response.Headers = http.Header{ttlRequestHeader: {state.token}}
	return response, nil
}

func (c *claudeTTL) after(req pluginapi.RequestInterceptRequest) (pluginapi.RequestInterceptResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	response := pluginapi.RequestInterceptResponse{ClearHeaders: []string{ttlHeader, ttlRequestHeader}}
	if !strings.EqualFold(req.ToFormat, "claude") || internalRequest(req.Metadata) {
		return response, nil
	}
	state := c.requests[req.RequestID]
	if state == nil {
		return response, errors.New("Claude TTL frozen request identity is unavailable")
	}
	if state.ttl == "" {
		return response, nil
	}
	if state.scope != req.CallerScope && state.scope != text(req.Metadata, pluginapi.CallerScopeMetadataKey) {
		return response, errors.New("Claude TTL caller identity changed")
	}
	if err := state.guard.check(); err != nil {
		c.lastError = err.Error()
		return response, err
	}
	selectionID := text(req.Metadata, pluginapi.SelectionIDMetadataKey)
	selection := c.selections[selectionID]
	if selectionID == "" || selection == nil || selection.request != state {
		return response, errors.New("Claude TTL scheduler selection is unavailable")
	}
	selection.applied = true
	response.Headers = http.Header{ttlHeader: {state.ttl}}
	return response, nil
}

// Selection IDs are retained by CPA only for handled decisions. Explicitly
// delegate unrestricted selections to the configured CPA builtin, preserving its
// scheduling semantics; restricted selections continue through the existing picker.
func (c *claudeTTL) selection(req pluginapi.SchedulerPickRequest) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.tokens[http.Header(req.Options.Headers).Get(ttlRequestHeader)]
	if state == nil || state.ttl == "" {
		return "", nil
	}
	if state.scope != text(req.Options.Metadata, pluginapi.CallerScopeMetadataKey) {
		return "", errors.New("Claude TTL scheduler identity changed")
	}
	// Disabled/invalid enforcement is rejected in the after hook only for Claude;
	// it must not block other providers on a Key with a Claude-only setting.
	if state.guard == nil {
		return "", nil
	}
	id := text(req.Options.Metadata, pluginapi.SelectionIDMetadataKey)
	if !snapshot.ValidExecutionID(id) {
		return "", errors.New("Claude TTL scheduler selection ID is unavailable")
	}
	c.sweep(time.Now().UTC())
	if previous := c.selections[id]; previous != nil && previous.request != state {
		return "", errors.New("Claude TTL scheduler selection conflict")
	}
	if c.selections[id] == nil {
		c.selections[id] = &ttlSelection{request: state}
	}
	return state.guard.delegate, nil
}

func (c *claudeTTL) complete(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if state := c.requests[id]; state != nil {
		delete(c.tokens, state.token)
		delete(c.requests, id)
	}
	c.sweep(time.Now().UTC())
}

func (c *claudeTTL) sweep(now time.Time) {
	if now.Sub(c.lastSweep) < time.Minute {
		return
	}
	for id, selection := range c.selections {
		if now.Sub(selection.request.created) >= snapshot.TTLMetadataRetention {
			delete(c.selections, id)
		}
	}
	c.lastSweep = now
}

func (c *claudeTTL) observe(req pluginapi.UsageRecord) error {
	if !strings.EqualFold(req.Provider, "claude") || !req.Generate {
		return nil
	}
	c.mu.Lock()
	selection := c.selections[req.SelectionID]
	if selection == nil || !selection.applied || time.Since(selection.request.created) >= snapshot.TTLMetadataRetention {
		if c.guard != nil {
			c.unknown++
		}
		c.mu.Unlock()
		return nil
	}
	state := selection.request
	if c.store == nil {
		c.lastError = "Claude TTL metadata storage is unavailable"
		c.mu.Unlock()
		return errors.New("Claude TTL metadata storage is unavailable")
	}
	store := c.store
	metadata := snapshot.TTLMetadata{ExecutionID: req.RequestID, CallerScope: state.scope, TTL: state.ttl, Source: "enforced_request", PolicyRevision: state.revision, ObservedAt: time.Now().UTC()}
	c.mu.Unlock()
	if err := store.Put(metadata); err != nil {
		c.mu.Lock()
		c.lastError = err.Error()
		c.mu.Unlock()
		return err
	}
	return nil
}

func (c *claudeTTL) health() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	status := "disabled"
	if c.guard != nil {
		status = "configured"
		if err := c.guard.check(); err != nil {
			status = "unavailable"
			c.lastError = err.Error()
		}
	}
	return map[string]any{"status": status, "ttl_source": "enforced_request", "metadata_retention_hours": 24, "active_requests": len(c.requests), "retained_selections": len(c.selections), "unknown_usage": c.unknown, "last_error": c.lastError, "configuration_validation": "pinned_file_requires_reload"}
}

func (c *claudeTTL) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests, c.tokens, c.selections = map[string]*frozenTTL{}, map[string]*frozenTTL{}, map[string]*ttlSelection{}
}
