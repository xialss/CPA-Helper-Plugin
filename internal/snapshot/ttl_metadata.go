package snapshot

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cpa-helper-plugin/internal/policy"
)

// TTLMetadata supplements a CPA execution without duplicating its usage counts.
type TTLMetadata struct {
	ExecutionID    string    `json:"execution_id"`
	CallerScope    string    `json:"caller_scope"`
	TTL            string    `json:"ttl"`
	Source         string    `json:"ttl_source"`
	PolicyRevision uint64    `json:"policy_revision"`
	ObservedAt     time.Time `json:"observed_at"`
}

// TTLMetadataStore durably keeps supplemental facts for the 24-hour lookup window.
// Expired facts are explicitly missing; they are never reconstructed from policy.
type TTLMetadataStore struct {
	mu        sync.Mutex
	dir       string
	lastSweep time.Time
}

// TTLMetadataRetention is the documented delivery/recovery window, not a billing TTL.
const TTLMetadataRetention = 24 * time.Hour

// ReadTTLMetadata provides lookups without creating directories or pruning files.
// Disabled TTL must not add writable-storage requirements to existing policies.
func ReadTTLMetadata(stateDir string) *TTLMetadataStore {
	return &TTLMetadataStore{dir: filepath.Join(stateDir, "ttl-metadata")}
}

// OpenTTLMetadata creates a separate metadata directory inside an existing policy directory.
func OpenTTLMetadata(stateDir string) (*TTLMetadataStore, error) {
	s := ReadTTLMetadata(stateDir)
	dir := s.dir
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, errors.New("cannot open TTL metadata directory")
	}
	if err := s.sweep(time.Now().UTC()); err != nil {
		return nil, err
	}
	return s, nil
}

// ValidExecutionID accepts only canonical host UUIDs, also preventing path traversal.
func ValidExecutionID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' || strings.ToLower(id) != id {
		return false
	}
	b, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return err == nil && len(b) == 16
}

func validTTLMetadata(m TTLMetadata) bool {
	return ValidExecutionID(m.ExecutionID) && policy.ValidScope(m.CallerScope) && (m.TTL == "5m" || m.TTL == "1h") && m.Source == "enforced_request" && m.PolicyRevision > 0 && !m.ObservedAt.IsZero()
}

// Put is idempotent; conflicting facts fail instead of overwriting history.
func (s *TTLMetadataStore) Put(m TTLMetadata) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validTTLMetadata(m) {
		return errors.New("invalid TTL execution metadata")
	}
	path := filepath.Join(s.dir, m.ExecutionID+".json")
	if raw, err := os.ReadFile(path); err == nil {
		var previous TTLMetadata
		if Decode(raw, &previous) != nil || !validTTLMetadata(previous) {
			return errors.New("invalid stored TTL execution metadata")
		}
		if previous.ExecutionID != m.ExecutionID || previous.CallerScope != m.CallerScope || previous.TTL != m.TTL || previous.Source != m.Source || previous.PolicyRevision != m.PolicyRevision {
			return errors.New("conflicting TTL execution metadata")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot read TTL execution metadata")
	}
	if err := s.sweep(time.Now().UTC()); err != nil {
		return err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err = atomicWrite(path, raw); err != nil {
		return errors.New("cannot persist TTL execution metadata")
	}
	return nil
}

// Get distinguishes an absent/expired execution from a damaged metadata file.
func (s *TTLMetadataStore) Get(id string) (*TTLMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ValidExecutionID(id) {
		return nil, errors.New("invalid execution ID")
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("cannot read TTL execution metadata")
	}
	var m TTLMetadata
	if Decode(raw, &m) != nil || m.ExecutionID != id || !validTTLMetadata(m) {
		return nil, errors.New("invalid stored TTL execution metadata")
	}
	if time.Since(m.ObservedAt) >= TTLMetadataRetention {
		return nil, nil
	}
	return &m, nil
}

func (s *TTLMetadataStore) sweep(now time.Time) error {
	if now.Sub(s.lastSweep) < time.Hour {
		return nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return errors.New("cannot inspect TTL metadata retention")
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || !ValidExecutionID(strings.TrimSuffix(entry.Name(), ".json")) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return errors.New("cannot inspect TTL metadata age")
		}
		if now.Sub(info.ModTime()) >= TTLMetadataRetention {
			if err := os.Remove(filepath.Join(s.dir, entry.Name())); err != nil {
				return errors.New("cannot remove expired TTL metadata")
			}
		}
	}
	s.lastSweep = now
	return nil
}
