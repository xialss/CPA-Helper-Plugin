package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"cpa-helper-plugin/internal/catalog"
	"cpa-helper-plugin/internal/policy"
)

// ErrCatalogUnavailable signals missing or invalid model associations.
var ErrCatalogUnavailable = errors.New("model catalog unavailable")

// ErrCatalogPersistence signals a failed write that did not publish the transaction.
var ErrCatalogPersistence = errors.New("cannot persist model catalog transaction")

type catalogDiskState struct {
	Format     int                `json:"format"`
	Current    catalog.Snapshot   `json:"current"`
	Receipts   map[string]receipt `json:"receipts"`
	AcceptedAt time.Time          `json:"accepted_at"`
}

func validReceipt(r receipt, revision uint64) bool {
	return policy.ValidScope(r.Digest) && r.Revision > 0 && r.Revision <= revision
}

// CatalogStore persists model associations separately from the v1 policy file.
type CatalogStore struct {
	mu        sync.RWMutex
	dir       string
	state     catalogDiskState
	engine    *catalog.Engine
	frozen    bool
	lastError string
	write     func(string, []byte) error
}

// CatalogHealth exposes directory availability and synchronization diagnostics.
type CatalogHealth struct {
	Status          string    `json:"status"`
	Revision        uint64    `json:"catalog_revision"`
	GeneratedAt     time.Time `json:"generated_at"`
	AcceptedAt      time.Time `json:"accepted_at"`
	AgeSeconds      int64     `json:"catalog_age_seconds"`
	CredentialCount int       `json:"credential_count"`
	LastError       string    `json:"last_error,omitempty"`
	PluginVersion   string    `json:"plugin_version,omitempty"`
	CPAVersion      string    `json:"cpa_version,omitempty"`
	RefreshRequired bool      `json:"refresh_required"`
}

// OpenCatalog reloads a saved directory. A missing file requires explicit sync.
// Corrupt or uncertain state requires recovery while CPA is stopped.
func OpenCatalog(dir string) *CatalogStore {
	s := &CatalogStore{dir: dir, state: catalogDiskState{Format: 1, Receipts: map[string]receipt{}}, write: atomicWrite}
	raw, err := os.ReadFile(filepath.Join(dir, "model-catalog.json"))
	if errors.Is(err, os.ErrNotExist) {
		s.lastError = "model catalog has not been synchronized"
		return s
	}
	if err != nil {
		s.frozen = true
		s.lastError = "cannot read model catalog state"
		return s
	}
	if err = Decode(raw, &s.state); err != nil || s.state.Format != 1 || s.state.Receipts == nil || s.state.AcceptedAt.IsZero() {
		s.frozen = true
		s.lastError = "invalid model catalog state"
		return s
	}
	for key, r := range s.state.Receipts {
		if key == "" || !validReceipt(r, s.state.Current.Revision) {
			s.frozen = true
			s.lastError = "invalid model catalog receipt"
			return s
		}
	}
	s.engine, err = catalog.CompileStored(s.state.Current)
	if err != nil {
		s.frozen = true
		s.lastError = "invalid model catalog: " + err.Error()
	}
	return s
}

// Engine returns the last durably accepted immutable directory.
func (s *CatalogStore) Engine() (*catalog.Engine, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.engine == nil {
		return nil, ErrCatalogUnavailable
	}
	return s.engine, nil
}

// Current returns a detached copy of the saved snapshot.
func (s *CatalogStore) Current() (*catalog.Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.engine == nil {
		return nil, ErrCatalogUnavailable
	}
	p := catalog.Clone(s.state.Current)
	return &p, nil
}

// Health reports the revision independently from policy availability.
func (s *CatalogStore) Health() CatalogHealth {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.health()
}

// View returns a detached snapshot and diagnostics from the same revision.
func (s *CatalogStore) View() (*catalog.Snapshot, CatalogHealth) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.engine == nil {
		return nil, s.health()
	}
	p := catalog.Clone(s.state.Current)
	return &p, s.health()
}

func (s *CatalogStore) health() CatalogHealth {
	h := CatalogHealth{Status: "unavailable", Revision: s.state.Current.Revision, LastError: s.lastError}
	if s.engine != nil {
		h.Status = "ok"
		h.GeneratedAt = s.state.Current.GeneratedAt
		h.AcceptedAt = s.state.AcceptedAt
		h.AgeSeconds = int64(time.Since(h.GeneratedAt).Seconds())
		h.CredentialCount = len(s.state.Current.Credentials)
		h.PluginVersion = s.state.Current.PluginVersion
		h.CPAVersion = s.state.Current.CPAVersion
		h.RefreshRequired = h.CPAVersion != catalog.CPAVersion || h.PluginVersion != policy.Version
	}
	return h
}

// RecordRejection exposes validation errors without replacing the active directory.
func (s *CatalogStore) RecordRejection(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = message
}

// Apply accepts a full snapshot with compare-and-swap and durable idempotency.
func (s *CatalogStore) Apply(p catalog.Snapshot, base uint64, key string) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fail := func(err error) (uint64, error) { s.lastError = err.Error(); return 0, err }
	if s.frozen {
		return fail(fmt.Errorf("%w; restore the catalog backup while CPA is stopped", ErrCatalogUnavailable))
	}
	if key == "" {
		return fail(errors.New("Idempotency-Key is required"))
	}
	p = catalog.Clone(p)
	raw, err := json.Marshal(p)
	if err != nil {
		return fail(err)
	}
	d := digest(append([]byte(fmt.Sprintf("catalog:%d:", base)), raw...))
	if r, ok := s.state.Receipts[key]; ok {
		if r.Digest == d {
			return r.Revision, nil
		}
		return fail(ErrConflict)
	}
	if base != s.state.Current.Revision || p.Revision != base+1 {
		return fail(ErrConflict)
	}
	engine, err := catalog.Compile(p)
	if err != nil {
		return fail(err)
	}
	next := catalogDiskState{Format: 1, Current: p, Receipts: make(map[string]receipt, len(s.state.Receipts)+1), AcceptedAt: time.Now().UTC()}
	for k, v := range s.state.Receipts {
		next.Receipts[k] = v
	}
	next.Receipts[key] = receipt{d, p.Revision}
	raw, err = json.Marshal(next)
	if err != nil {
		return fail(err)
	}
	if err = s.write(filepath.Join(s.dir, "model-catalog.json"), raw); err != nil {
		if errors.Is(err, ErrPersistenceUncertain) {
			s.engine = nil
			s.frozen = true
			return fail(fmt.Errorf("%w; restart CPA to reconcile the catalog", ErrPersistenceUncertain))
		}
		return fail(ErrCatalogPersistence)
	}
	s.state, s.engine, s.lastError = next, engine, ""
	return p.Revision, nil
}
