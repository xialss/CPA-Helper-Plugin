package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cpa-helper-plugin/internal/policy"
)

func TestTTLMetadataPersistenceAndConflicts(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenTTLMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := "c3db9b0d-0cd2-4565-aa4b-7c35459097f9"
	m := TTLMetadata{ExecutionID: id, CallerScope: policy.CallerScope("fixture"), TTL: "1h", Source: "enforced_request", PolicyRevision: 4, ObservedAt: time.Now().UTC()}
	firstObservedAt := m.ObservedAt
	if err = s.Put(m); err != nil {
		t.Fatal(err)
	}
	m.ObservedAt = m.ObservedAt.Add(time.Second)
	if err = s.Put(m); err != nil {
		t.Fatal(err)
	}
	s, err = OpenTTLMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(id)
	if err != nil || got == nil || got.TTL != "1h" || !got.ObservedAt.Equal(firstObservedAt) {
		t.Fatalf("restart %+v %v", got, err)
	}
	m.TTL = "5m"
	if s.Put(m) == nil {
		t.Fatal("overwrote immutable TTL")
	}
	if _, err = s.Get("../state"); err == nil {
		t.Fatal("accepted path traversal")
	}
	if err = os.WriteFile(filepath.Join(dir, "ttl-metadata", id+".json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(id); err == nil {
		t.Fatal("hidden corrupt metadata")
	}
}

func TestTTLMetadataRejectsCorruptTimestampReplay(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "zero"
		if missing {
			name = "missing"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := OpenTTLMetadata(dir)
			if err != nil {
				t.Fatal(err)
			}
			id := "c3db9b0d-0cd2-4565-aa4b-7c35459097f9"
			m := TTLMetadata{ExecutionID: id, CallerScope: policy.CallerScope("fixture"), TTL: "1h", Source: "enforced_request", PolicyRevision: 4, ObservedAt: time.Now().UTC()}
			corrupt := m
			corrupt.ObservedAt = time.Time{}
			raw, err := json.Marshal(corrupt)
			if err != nil {
				t.Fatal(err)
			}
			if missing {
				var fields map[string]json.RawMessage
				if err = json.Unmarshal(raw, &fields); err != nil {
					t.Fatal(err)
				}
				delete(fields, "observed_at")
				raw, err = json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(dir, "ttl-metadata", id+".json")
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Get(id); err == nil {
				t.Fatal("accepted corrupt stored timestamp")
			}
			if err = s.Put(m); err == nil {
				t.Fatal("reported corrupt stored metadata as an idempotent success")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(raw) {
				t.Fatal("replay changed corrupt metadata", err)
			}
		})
	}
}

func TestTTLMetadataExpiration(t *testing.T) {
	s, err := OpenTTLMetadata(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := "c3db9b0d-0cd2-4565-aa4b-7c35459097f9"
	m := TTLMetadata{ExecutionID: id, CallerScope: policy.CallerScope("fixture"), TTL: "5m", Source: "enforced_request", PolicyRevision: 1, ObservedAt: time.Now().Add(-25 * time.Hour)}
	if err = s.Put(m); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get(id); err != nil || got != nil {
		t.Fatalf("expired record %+v %v", got, err)
	}
	path := filepath.Join(s.dir, id+".json")
	expired := time.Now().Add(-25 * time.Hour)
	if err = os.Chtimes(path, expired, expired); err != nil {
		t.Fatal(err)
	}
	s.lastSweep = time.Time{}
	if err = s.sweep(time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expired file not removed")
	}
}

func TestTTLPolicyTransactionRejectsInvalidTTL(t *testing.T) {
	s := newStore(t)
	p, err := s.Current()
	if err != nil {
		t.Fatal(err)
	}
	base := p.Revision
	p.Revision++
	p.Keys = []policy.Key{{ID: policy.CallerScope("fixture"), Enabled: true, GroupIDs: []string{}, ClaudeCacheTTL: "1h"}}
	if _, err = s.Apply(p, base, "ttl-valid"); err != nil {
		t.Fatal(err)
	}
	base = p.Revision
	p.Revision++
	p.Keys[0].ClaudeCacheTTL = "30m"
	if _, err = s.Apply(p, base, "ttl-invalid"); err == nil {
		t.Fatal("accepted invalid TTL")
	}
	current, err := s.Current()
	if err != nil || current.Revision != base || current.Keys[0].ClaudeCacheTTL != "1h" {
		t.Fatal("invalid TTL partially changed active policy")
	}
	reopened := Open(s.dir)
	current, err = reopened.Current()
	if err != nil || current.Revision != base || current.Keys[0].ClaudeCacheTTL != "1h" {
		t.Fatal("TTL transaction did not survive restart")
	}
}
