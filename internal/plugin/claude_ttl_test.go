package plugin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpa-helper-plugin/internal/policy"
	"cpa-helper-plugin/internal/snapshot"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const ttlSelectionID = "9ec3ecaa-6c7f-4f03-87e1-a05974d1142a"
const ttlExecutionID = "e229118e-a1be-4fa4-93ea-7b003d5b9761"

func enableTTLFixture(t *testing.T, a *App, g *ttlConfigGuard) {
	t.Helper()
	store, err := snapshot.OpenTTLMetadata(a.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	a.ttl.configure(g, store)
}

func ttlGuardFixture(t *testing.T) (*ttlConfigGuard, claudeTTLConfig) {
	t.Helper()
	cfg := claudeTTLConfig{Enabled: true, HostConfigPath: filepath.Join(t.TempDir(), "config.yaml"), DelegateBuiltin: "round-robin"}
	host := map[string]any{"payload": requiredTTLPayload(), "routing": map[string]any{"strategy": cfg.DelegateBuiltin}, "plugins": map[string]any{"configs": map[string]any{ID: map[string]any{"claude_cache_ttl": cfg}}}}
	raw, err := yaml.Marshal(host)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cfg.HostConfigPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	g, err := validateTTLConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return g, cfg
}

func TestClaudeTTLConfigurationAndDrift(t *testing.T) {
	g, cfg := ttlGuardFixture(t)
	if err := g.check(); err != nil {
		t.Fatal(err)
	}
	wrong := cfg
	wrong.DelegateBuiltin = "fill-first"
	if _, err := validateTTLConfig(wrong); err == nil {
		t.Fatal("accepted mismatched scheduler")
	}
	wrong = cfg
	wrong.HostConfigPath = "relative.yaml"
	if _, err := validateTTLConfig(wrong); err == nil {
		t.Fatal("accepted relative path")
	}
	raw, err := os.ReadFile(cfg.HostConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cfg.HostConfigPath, append(raw, []byte("\n# drift")...), 0600); err != nil {
		t.Fatal(err)
	}
	if g.check() == nil {
		t.Fatal("accepted configuration drift")
	}
	if err = os.WriteFile(cfg.HostConfigPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if g.check() == nil {
		t.Fatal("restoring the file bypassed required reload")
	}
	if err = os.WriteFile(cfg.HostConfigPath, bytes.ReplaceAll(raw, []byte("protocol: claude"), []byte("protocol: openai")), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateTTLConfig(cfg); err == nil {
		t.Fatal("accepted missing late Claude rules")
	}
}

func TestClaudeTTLRejectsIncompleteRootRules(t *testing.T) {
	for _, mutation := range []string{"missing-root", "unguarded-root"} {
		t.Run(mutation, func(t *testing.T) {
			_, cfg := ttlGuardFixture(t)
			payload := requiredTTLPayload()
			for _, kind := range []string{"override", "filter"} {
				rules := payload[kind].([]any)
				if mutation == "missing-root" {
					payload[kind] = rules[:1]
				} else {
					model := rules[1].(map[string]any)["models"].([]any)[0].(map[string]any)
					delete(model, "match")
				}
			}
			host := map[string]any{"payload": payload, "routing": map[string]any{"strategy": cfg.DelegateBuiltin}, "plugins": map[string]any{"configs": map[string]any{ID: map[string]any{"claude_cache_ttl": cfg}}}}
			raw, err := yaml.Marshal(host)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(cfg.HostConfigPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = validateTTLConfig(cfg); err == nil {
				t.Fatalf("accepted %s rules", mutation)
			}
		})
	}
}

func TestClaudeTTLFrozenSelectionAndDelayedUsage(t *testing.T) {
	a := configured(t)
	g, _ := ttlGuardFixture(t)
	enableTTLFixture(t, a, g)
	scope := policy.CallerScope("synthetic-key")
	key := policy.Key{ID: scope, Enabled: true, GroupIDs: []string{}, ClaudeCacheTTL: "1h"}
	req := pluginapi.RequestInterceptRequest{RequestID: "request-a", CallerScope: scope, Metadata: map[string]any{"caller_scope": scope}}
	before, err := a.ttl.begin(req, key, 7)
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{}
	for name, values := range before.Headers {
		headers.Set(name, values[0])
	}
	selection := pluginapi.SchedulerPickRequest{Options: pluginapi.SchedulerOptions{Headers: headers, Metadata: map[string]any{"caller_scope": scope, "selection_id": ttlSelectionID}}}
	delegate, err := a.ttl.selection(selection)
	if err != nil || delegate != "round-robin" {
		t.Fatalf("selection %q %v", delegate, err)
	}
	// A repeated before hook after policy update must retain the starting setting.
	key.ClaudeCacheTTL = "5m"
	if _, err = a.ttl.begin(req, key, 8); err != nil {
		t.Fatal(err)
	}
	req.ToFormat = "claude"
	req.Metadata["selection_id"] = ttlSelectionID
	after, err := a.ttl.after(req)
	if err != nil {
		t.Fatal(err)
	}
	if values := after.Headers[ttlHeader]; len(values) != 1 || values[0] != "1h" {
		t.Fatalf("lost frozen TTL: %#v", after)
	}
	a.ttl.complete(req.RequestID)
	if len(a.ttl.requests) != 0 || len(a.ttl.tokens) != 0 {
		t.Fatal("request lifecycle leaked")
	}
	// Completion can precede asynchronous usage. Multiple attempts may share a
	// selection ID; each usage RequestID remains an independent CPA execution.
	for _, id := range []string{ttlExecutionID, ttlSelectionID} {
		u := pluginapi.UsageRecord{RequestID: id, SelectionID: ttlSelectionID, Provider: "claude", Generate: true}
		if err = a.ttl.observe(u); err != nil {
			t.Fatal(err)
		}
		if err = a.ttl.observe(u); err != nil {
			t.Fatal(err)
		}
		m, err := a.ttl.store.Get(id)
		if err != nil || m == nil || m.TTL != "1h" || m.PolicyRevision != 7 {
			t.Fatalf("usage join: %+v %v", m, err)
		}
	}
	// Lost associations never consult the current policy.
	if err = a.ttl.observe(pluginapi.UsageRecord{RequestID: "49d13c68-51fc-4386-b355-b0ec11b14c8c", Provider: "claude", Generate: true}); err != nil {
		t.Fatal(err)
	}
	if a.ttl.unknown != 1 {
		t.Fatal("missing association was hidden")
	}
	a.ttl.shutdown()
	if len(a.ttl.selections) != 0 {
		t.Fatal("selection state leaked across shutdown")
	}
	m, err := a.ttl.store.Get(ttlExecutionID)
	if err != nil || m == nil {
		t.Fatal("shutdown erased durable metadata")
	}
}

func TestClaudeTTLIsolationAndMissingConfiguration(t *testing.T) {
	for _, provider := range []string{"claude", "openai"} {
		t.Run(provider, func(t *testing.T) {
			c := newClaudeTTL()
			scope := policy.CallerScope("fixture")
			req := pluginapi.RequestInterceptRequest{RequestID: "a", CallerScope: scope, ToFormat: provider, Metadata: map[string]any{"caller_scope": scope}}
			if _, err := c.begin(req, policy.Key{ID: scope, ClaudeCacheTTL: "1h"}, 1); err != nil {
				t.Fatal(err)
			}
			_, err := c.after(req)
			if (err != nil) != (provider == "claude") {
				t.Fatalf("unexpected enforcement error: %v", err)
			}
		})
	}
	for _, ttl := range []string{"", "1h"} {
		c := newClaudeTTL()
		scope := policy.CallerScope("fixture")
		req := pluginapi.RequestInterceptRequest{RequestID: "count", CallerScope: scope, ToFormat: "claude", Metadata: map[string]any{"caller_scope": scope, "generate": false}}
		if _, err := c.begin(req, policy.Key{ID: scope, ClaudeCacheTTL: ttl}, 1); err != nil {
			t.Fatal(err)
		}
		after, err := c.after(req)
		if err != nil || len(after.Headers) != 0 {
			t.Fatalf("count_tokens changed: %+v %v", after, err)
		}
	}
}

func TestClaudeTTLMetadataManagement(t *testing.T) {
	a := configured(t)
	for _, body := range []string{`{}`, `{"execution_ids":[]}`, `{"execution_ids":["../state"]}`, `{"execution_ids":["` + ttlExecutionID + `","` + ttlExecutionID + `"]}`, `{"execution_ids":["` + ttlExecutionID + `"],"unknown":true}`, `{"execution_ids":["` + ttlExecutionID + `"]} {}`} {
		w := httptest.NewRecorder()
		a.serveManagement(w, httptest.NewRequest("POST", BasePath+"/claude-ttl/executions", bytes.NewBufferString(body)), "")
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d", body, w.Code)
		}
	}
	w := httptest.NewRecorder()
	a.serveManagement(w, httptest.NewRequest("POST", BasePath+"/claude-ttl/executions", bytes.NewBufferString(`{"execution_ids":["`+ttlExecutionID+`"]}`)), "")
	var result struct {
		Records []snapshot.TTLMetadata `json:"records"`
		Missing []string               `json:"missing_execution_ids"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Records) != 0 || len(result.Missing) != 1 {
		t.Fatalf("unknown metadata: %d %s", w.Code, w.Body.String())
	}
}

func TestClaudeTTLConcurrentScopes(t *testing.T) {
	c := newClaudeTTL()
	g, _ := ttlGuardFixture(t)
	c.configure(g, nil)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := time.Unix(int64(i), 0).String()
			scope := policy.CallerScope(id)
			req := pluginapi.RequestInterceptRequest{RequestID: id, Metadata: map[string]any{"caller_scope": scope}}
			if _, err := c.begin(req, policy.Key{ID: scope, ClaudeCacheTTL: "5m"}, 1); err != nil {
				t.Error(err)
			}
			c.complete(id)
		}(i)
	}
	wg.Wait()
	if len(c.requests) != 0 || len(c.tokens) != 0 {
		t.Fatal("concurrent request cleanup leaked")
	}
}

func TestClaudeTTLUnappliedSelectionAndStorageFailure(t *testing.T) {
	a := configured(t)
	g, _ := ttlGuardFixture(t)
	enableTTLFixture(t, a, g)
	scope := policy.CallerScope("fixture")
	req := pluginapi.RequestInterceptRequest{RequestID: "attempt", CallerScope: scope, ToFormat: "claude", Metadata: map[string]any{"caller_scope": scope, "selection_id": ttlSelectionID}}
	before, err := a.ttl.begin(req, policy.Key{ID: scope, ClaudeCacheTTL: "1h"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{}
	for name, values := range before.Headers {
		headers.Set(name, values[0])
	}
	pick := pluginapi.SchedulerPickRequest{Options: pluginapi.SchedulerOptions{Headers: headers, Metadata: map[string]any{"caller_scope": scope, "selection_id": ttlSelectionID}}}
	if _, err = a.ttl.selection(pick); err != nil {
		t.Fatal(err)
	}
	usage := pluginapi.UsageRecord{RequestID: ttlExecutionID, SelectionID: ttlSelectionID, Provider: "claude", Generate: true}
	if err = a.ttl.observe(usage); err != nil {
		t.Fatal(err)
	}
	if m, err := a.ttl.store.Get(ttlExecutionID); err != nil || m != nil {
		t.Fatal("created fact before enforcement")
	}
	if _, err = a.ttl.after(req); err != nil {
		t.Fatal(err)
	}
	// A new selection must not inherit the first selection's applied flag.
	secondID := "26385a24-9998-4dc1-9997-20a5ae99f6e7"
	pick.Options.Metadata["selection_id"] = secondID
	if _, err = a.ttl.selection(pick); err != nil {
		t.Fatal(err)
	}
	usage.SelectionID = secondID
	if err = a.ttl.observe(usage); err != nil {
		t.Fatal(err)
	}
	if m, err := a.ttl.store.Get(ttlExecutionID); err != nil || m != nil {
		t.Fatal("new selection inherited enforcement")
	}
	req.Metadata["selection_id"] = secondID
	if _, err = a.ttl.after(req); err != nil {
		t.Fatal(err)
	}
	store := a.ttl.store
	a.ttl.store = nil
	if a.ttl.observe(usage) == nil || a.ttl.lastError == "" {
		t.Fatal("metadata failure was hidden")
	}
	a.ttl.store = store
	a.ttl.selections[secondID].request.created = time.Now().Add(-25 * time.Hour)
	a.ttl.lastSweep = time.Time{}
	a.ttl.sweep(time.Now())
	if len(a.ttl.selections) != 0 {
		t.Fatal("expired selections leaked")
	}
}

func TestDisabledTTLDoesNotCreateMetadataStorage(t *testing.T) {
	a := configured(t)
	if _, err := os.Stat(filepath.Join(a.stateDir, "ttl-metadata")); !os.IsNotExist(err) {
		t.Fatalf("disabled feature created metadata storage: %v", err)
	}
}
