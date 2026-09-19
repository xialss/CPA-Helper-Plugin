// Package snapshot owns durable, atomic policy transactions.
package snapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"cpa-helper-plugin/internal/policy"
)

// ErrConflict signals a stale revision or a reused idempotency key.
var ErrConflict = errors.New("revision or idempotency conflict")

// ErrPersistenceUncertain means replacement completed but its directory entry
// could not be confirmed durable. The store must stay fail-closed until reopened.
var ErrPersistenceUncertain = errors.New("policy persistence durability uncertain")

type receipt struct {
	Digest   string `json:"digest"`
	Revision uint64 `json:"revision"`
}
type diskState struct {
	Format     int                `json:"format"`
	Current    policy.Snapshot    `json:"current"`
	Previous   *policy.Snapshot   `json:"previous,omitempty"`
	Receipts   map[string]receipt `json:"receipts"`
	AcceptedAt time.Time          `json:"accepted_at"`
}

// Store publishes immutable policy engines only after durable writes succeed.
type Store struct {
	mu        sync.RWMutex
	dir       string
	state     diskState
	engine    *policy.Engine
	lastError string
	write     func(string, []byte) error
}

// Health describes policy availability independently of management availability.
type Health struct {
	Status           string    `json:"status"`
	Revision         uint64    `json:"policy_revision"`
	ContractVersion  string    `json:"contract_version"`
	PluginVersion    string    `json:"plugin_version"`
	GeneratedAt      time.Time `json:"generated_at"`
	AcceptedAt       time.Time `json:"accepted_at"`
	PolicyAgeSeconds int64     `json:"policy_age_seconds"`
	PreviousRevision uint64    `json:"previous_revision"`
	LastError        string    `json:"last_error,omitempty"`
}

// Open initializes only a newly created directory. An existing directory without
// valid state stays unavailable; deleting state must never silently remove policy.
func Open(dir string) *Store {
	s := &Store{dir: dir, write: atomicWrite}
	err := os.Mkdir(dir, 0700)
	if err == nil {
		s.state = diskState{Format: 1, Current: policy.Initial(), Receipts: map[string]receipt{}, AcceptedAt: time.Now().UTC()}
		raw, marshalErr := json.Marshal(s.state)
		if marshalErr != nil {
			s.lastError = "cannot encode initial policy"
			return s
		}
		if err = s.write(filepath.Join(dir, "state.json"), raw); err != nil {
			s.lastError = "cannot persist initial policy"
			return s
		}
		if err = syncDirectory(filepath.Dir(dir)); err != nil {
			s.lastError = "cannot persist policy directory"
			return s
		}
	} else if errors.Is(err, os.ErrExist) {
		raw, readErr := os.ReadFile(filepath.Join(dir, "state.json"))
		if readErr != nil {
			s.lastError = "policy state missing or unreadable"
			return s
		}
		if err = Decode(raw, &s.state); err != nil || s.state.Format != 1 || s.state.Receipts == nil {
			s.lastError = "invalid policy state"
			return s
		}
	} else {
		s.lastError = "cannot create policy directory"
		return s
	}
	engine, err := policy.Compile(s.state.Current)
	if err != nil {
		s.lastError = "invalid active policy: " + err.Error()
		return s
	}
	if s.state.Previous != nil {
		if _, err := policy.Compile(*s.state.Previous); err != nil {
			s.lastError = "invalid retained policy"
			return s
		}
	}
	s.engine = engine
	return s
}

// Decode rejects unknown fields and trailing JSON at management/storage boundaries.
func Decode(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return fmt.Errorf("invalid JSON document")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}

// Engine returns the current immutable engine, or an explicit availability error.
func (s *Store) Engine() (*policy.Engine, uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.engine == nil {
		return nil, 0, errors.New("policy unavailable")
	}
	return s.engine, s.state.Current.Revision, nil
}

// Current returns a detached copy so management cannot mutate live policy.
func (s *Store) Current() (policy.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.engine == nil {
		return policy.Snapshot{}, errors.New("policy unavailable")
	}
	return clone(s.state.Current), nil
}

func clone(p policy.Snapshot) policy.Snapshot {
	copyRule := func(r policy.Rule) policy.Rule {
		r.Models = slices.Clone(r.Models)
		r.DeniedModels = slices.Clone(r.DeniedModels)
		r.CredentialIDs = slices.Clone(r.CredentialIDs)
		r.DeniedCredentialIDs = slices.Clone(r.DeniedCredentialIDs)
		r.CredentialProviders = slices.Clone(r.CredentialProviders)
		r.DeniedCredentialProviders = slices.Clone(r.DeniedCredentialProviders)
		return r
	}
	p.Groups = slices.Clone(p.Groups)
	for i := range p.Groups {
		p.Groups[i].Rule = copyRule(p.Groups[i].Rule)
	}
	p.Keys = slices.Clone(p.Keys)
	for i := range p.Keys {
		p.Keys[i].Rule = copyRule(p.Keys[i].Rule)
		p.Keys[i].GroupIDs = slices.Clone(p.Keys[i].GroupIDs)
	}
	return p
}

// Health returns current revision and the last rejected synchronization error.
func (s *Store) Health() Health {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := Health{Status: "unavailable", ContractVersion: "v1", PluginVersion: policy.Version, LastError: s.lastError}
	if s.engine != nil {
		h.Status = "ok"
		h.Revision = s.state.Current.Revision
		h.GeneratedAt = s.state.Current.GeneratedAt
		h.AcceptedAt = s.state.AcceptedAt
		h.PolicyAgeSeconds = int64(time.Since(h.GeneratedAt).Seconds())
		if s.state.Previous != nil {
			h.PreviousRevision = s.state.Previous.Revision
		}
	}
	return h
}

// Apply accepts one complete policy using compare-and-swap and durable idempotency.
func (s *Store) Apply(p policy.Snapshot, base uint64, key string) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(p)
	if err != nil {
		return 0, err
	}
	return s.apply(p, base, key, digest(append([]byte(fmt.Sprintf("put:%d:", base)), raw...)))
}

// RecordRejection makes a boundary validation failure visible without changing policy.
func (s *Store) RecordRejection(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = message
}

// Rollback republishes the retained snapshot at the next revision.
func (s *Store) Rollback(base, revision uint64, key string) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := digest([]byte(fmt.Sprintf("rollback:%d:%d", base, revision)))
	if r, ok := s.state.Receipts[key]; key != "" && ok {
		if r.Digest == d {
			return r.Revision, nil
		}
		return 0, ErrConflict
	}
	if s.engine == nil || s.state.Previous == nil || s.state.Previous.Revision != revision {
		s.lastError = "retained revision unavailable"
		return 0, errors.New(s.lastError)
	}
	p := clone(*s.state.Previous)
	p.Revision = base + 1
	p.GeneratedAt = time.Now().UTC()
	return s.apply(p, base, key, d)
}

func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func (s *Store) apply(p policy.Snapshot, base uint64, key, d string) (uint64, error) {
	fail := func(err error) (uint64, error) { s.lastError = err.Error(); return 0, err }
	if key == "" {
		return fail(errors.New("Idempotency-Key is required"))
	}
	if r, ok := s.state.Receipts[key]; ok {
		if r.Digest == d {
			return r.Revision, nil
		}
		return fail(ErrConflict)
	}
	if s.engine == nil {
		return fail(errors.New("policy unavailable; restore state backup while CPA is stopped"))
	}
	if base != s.state.Current.Revision || p.Revision != base+1 {
		return fail(ErrConflict)
	}
	p = clone(p)
	engine, err := policy.Compile(p)
	if err != nil {
		return fail(err)
	}
	previous := s.state.Current
	next := diskState{Format: 1, Current: clone(p), Previous: &previous, Receipts: make(map[string]receipt, len(s.state.Receipts)+1), AcceptedAt: time.Now().UTC()}
	for k, v := range s.state.Receipts {
		next.Receipts[k] = v
	}
	next.Receipts[key] = receipt{d, p.Revision}
	raw, err := json.Marshal(next)
	if err != nil {
		return fail(errors.New("cannot encode policy"))
	}
	if err = s.write(filepath.Join(s.dir, "state.json"), raw); err != nil {
		if errors.Is(err, ErrPersistenceUncertain) {
			s.engine = nil
			return fail(fmt.Errorf("%w; restart CPA to reconcile state", ErrPersistenceUncertain))
		}
		return fail(errors.New("cannot persist policy transaction"))
	}
	s.state = next
	s.engine = engine
	s.lastError = ""
	return p.Revision, nil
}

func atomicWrite(path string, raw []byte) (err error) {
	return atomicWriteWithSync(path, raw, syncDirectory)
}

func atomicWriteWithSync(path string, raw []byte, syncDir func(string) error) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".policy-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() {
		if removeErr := os.Remove(name); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	if _, err = f.Write(raw); err != nil {
		return errors.Join(err, f.Close())
	}
	if err = f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	if err = syncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("%w: %v", ErrPersistenceUncertain, err)
	}
	return nil
}
