package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-helper-plugin/internal/policy"
)

func TestRemovedClaudeTTLLegacyStateAndReceipts(t *testing.T) {
	for _, version := range []string{"0.1.10", "0.1.11"} {
		t.Run(version, func(t *testing.T) {
			s := newStore(t)
			old := policy.Initial()
			old.PluginVersion, old.Revision, old.GeneratedAt = version, 3, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
			old.Groups = []policy.Group{{ID: "group", Name: "Group", Rule: policy.Rule{DeniedModels: []string{"blocked"}}}}
			old.Keys = []policy.Key{{ID: policy.CallerScope("fixture-key"), Label: "Retained key", Enabled: true, GroupIDs: []string{"group"}, MaxConcurrency: 2, LegacyClaudeCacheTTL: "1h"}}
			raw, err := json.Marshal(old)
			if err != nil {
				t.Fatal(err)
			}
			// Preserve the pre-removal field order used by canonical receipt digests.
			if !bytes.Contains(raw, []byte(`"max_concurrency":2,"claude_cache_ttl":"1h","rule":{}`)) {
				t.Fatalf("legacy canonical serialization changed: %s", raw)
			}
			previous := clone(old)
			previous.Revision, previous.Keys[0].Enabled = 2, false
			state := diskState{Format: 1, Current: old, Previous: &previous, AcceptedAt: old.GeneratedAt.Add(time.Minute), Receipts: map[string]receipt{"legacy": {digest(append([]byte("put:2:"), raw...)), 3}}}
			before, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.dir, "state.json")
			if err = os.WriteFile(path, before, 0600); err != nil {
				t.Fatal(err)
			}
			s = Open(s.dir)
			if s.Health().Status != "ok" || s.Health().PreviousRevision != 2 || !s.Health().AcceptedAt.Equal(state.AcceptedAt) {
				t.Fatal("legacy state is unavailable or changed", s.Health())
			}
			e, rev, err := s.Engine()
			if err != nil || rev != 3 || e.Resolve(old.Keys[0].ID).LegacyClaudeCacheTTL != "" || e.Resolve(old.Keys[0].ID).MaxConcurrency != 2 || e.Resolve(old.Keys[0].ID).Rule.AllowsModel("blocked") {
				t.Fatal("legacy policy changed remaining enforcement", rev, err)
			}
			current, err := s.Current()
			if err != nil || current.Keys[0].LegacyClaudeCacheTTL != "" || current.Keys[0].Label != "Retained key" {
				t.Fatal("current policy exposes TTL or lost key data", current, err)
			}
			if rev, err = s.Apply(old, 2, "legacy"); err != nil || rev != 3 {
				t.Fatal("legacy receipt no longer replays", rev, err)
			}
			conflict := clone(old)
			conflict.Keys[0].LegacyClaudeCacheTTL = "5m"
			if _, err = s.Apply(conflict, 2, "legacy"); !errors.Is(err, ErrConflict) {
				t.Fatal("changed legacy payload reused a receipt", err)
			}
			removed := clone(old)
			removed.Revision = 4
			if _, err = s.Apply(removed, 3, "removed"); err == nil || !strings.Contains(err.Error(), "has been removed") {
				t.Fatal("new TTL transaction was not rejected explicitly", err)
			}
			if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
				t.Fatal("startup/replay/rejection rewrote old state", err)
			}
			if rev, err = s.Rollback(3, 2, "rollback"); err != nil || rev != 4 {
				t.Fatal("rollback failed", rev, err)
			}
			current, err = s.Current()
			if err != nil || current.Keys[0].Enabled || current.Keys[0].LegacyClaudeCacheTTL != "" || current.Keys[0].MaxConcurrency != 2 {
				t.Fatal("rollback restored TTL or lost other policy", current, err)
			}
			current.Revision, current.Keys[0].Enabled = 5, true
			if rev, err = s.Apply(current, 4, "updated"); err != nil || rev != 5 {
				t.Fatal("normal policy update failed after removal", rev, err)
			}
			s = Open(s.dir)
			if rev, err = s.Apply(old, 2, "legacy"); err != nil || rev != 3 {
				t.Fatal("durable historical receipt lost after save/restart", rev, err)
			}
			if saved, err := os.ReadFile(path); err != nil || bytes.Contains(saved, []byte("claude_cache_ttl")) {
				t.Fatal("clean current/history retained TTL after two publications", err)
			}
		})
	}
}

func TestRemovedClaudeTTLRejectsDamagedLegacyState(t *testing.T) {
	for _, value := range []string{`"30m"`, `null`, `true`, `7`} {
		s := newStore(t)
		p := policy.Initial()
		p.Keys = []policy.Key{{ID: policy.CallerScope("fixture-key"), Enabled: true, GroupIDs: []string{}}}
		raw, err := json.Marshal(diskState{Format: 1, Current: p, Receipts: map[string]receipt{}, AcceptedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		raw = bytes.Replace(raw, []byte(`"max_concurrency":0`), []byte(`"max_concurrency":0,"claude_cache_ttl":`+value), 1)
		if err = os.WriteFile(filepath.Join(s.dir, "state.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		if reopened := Open(s.dir); reopened.Health().Status != "unavailable" {
			t.Fatal("damaged policy silently became available", value, reopened.Health())
		}
	}
}
