package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cpa-helper-plugin/internal/catalog"
	"cpa-helper-plugin/internal/policy"
)

func legacyCatalog(t *testing.T) (string, catalog.Snapshot, []byte) {
	t.Helper()
	return legacyCatalogForVersion(t, "0.1.8", "v8.0.32")
}

func legacyCatalogForVersion(t *testing.T, pluginVersion, cpaVersion string) (string, catalog.Snapshot, []byte) {
	t.Helper()
	p := catalogSnapshot()
	p.PluginVersion, p.CPAVersion = pluginVersion, cpaVersion
	p.GeneratedAt = time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	s := catalogDiskState{Format: 1, Current: p, AcceptedAt: p.GeneratedAt.Add(time.Minute), Receipts: map[string]receipt{"legacy": {Digest: digest(append([]byte("catalog:0:"), raw...)), Revision: 1}}}
	raw, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "model-catalog.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, p, raw
}

func TestLegacyCatalogReadAndExplicitUpgrade(t *testing.T) {
	for _, producer := range [][2]string{{"0.1.8", "v8.0.32"}, {"0.1.9", "v8.0.45"}} {
		t.Run(producer[0], func(t *testing.T) {
			dir, old, before := legacyCatalogForVersion(t, producer[0], producer[1])
			s := OpenCatalog(dir)
			h := s.Health()
			if h.Status != "ok" || !h.RefreshRequired || h.CPAVersion != old.CPAVersion || h.PluginVersion != old.PluginVersion || h.Revision != 1 || h.GeneratedAt != old.GeneratedAt {
				t.Fatalf("lost legacy provenance: %+v", h)
			}
			after, err := os.ReadFile(filepath.Join(dir, "model-catalog.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("startup rewrote legacy state", err)
			}
			if rev, err := s.Apply(old, 0, "legacy"); err != nil || rev != 1 {
				t.Fatal("lost old receipt", rev, err)
			}
			if _, err := s.Apply(old, 0, "other"); !errors.Is(err, ErrConflict) {
				t.Fatal("stale transaction accepted", err)
			}
			next := catalog.Clone(old)
			next.Revision++
			if _, err := s.Apply(next, 1, "old-new-write"); err == nil {
				t.Fatal("old envelope accepted for new publication")
			}
			next.PluginVersion, next.CPAVersion = policy.Version, catalog.CPAVersion
			if _, err := s.Apply(next, 1, "upgraded"); err != nil {
				t.Fatal(err)
			}
			s = OpenCatalog(dir)
			if s.Health().RefreshRequired || s.Health().Revision != 2 || s.Health().CPAVersion != catalog.CPAVersion {
				t.Fatal(s.Health())
			}
			if rev, err := s.Apply(old, 0, "legacy"); err != nil || rev != 1 {
				t.Fatal("upgrade lost old receipt", rev, err)
			}
			old.Credentials[0].Enabled = false
			if _, err := s.Apply(old, 0, "legacy"); !errors.Is(err, ErrConflict) {
				t.Fatal("old receipt accepted changed contents", err)
			}
		})
	}
}

func TestLegacyCatalogWriteFailureKeepsDurableSnapshot(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		dir, old, before := legacyCatalog(t)
		s := OpenCatalog(dir)
		s.write = func(string, []byte) error {
			if uncertain {
				return ErrPersistenceUncertain
			}
			return errors.New("disk full")
		}
		next := catalog.Clone(old)
		next.PluginVersion, next.CPAVersion, next.Revision = policy.Version, catalog.CPAVersion, 2
		if _, err := s.Apply(next, 1, "upgrade"); err == nil {
			t.Fatal("failed upgrade reported success")
		}
		if _, err := s.Engine(); uncertain != errors.Is(err, ErrCatalogUnavailable) {
			t.Fatal("incorrect live durability handling", err)
		}
		after, err := os.ReadFile(filepath.Join(dir, "model-catalog.json"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("failed upgrade changed disk", err)
		}
		if h := OpenCatalog(dir).Health(); h.Status != "ok" || !h.RefreshRequired || h.Revision != 1 {
			t.Fatal(h)
		}
	}
}

func TestCatalogRejectsInvalidStoredReceipts(t *testing.T) {
	for _, r := range []receipt{{"invalid", 1}, {digest([]byte("x")), 0}, {digest([]byte("x")), 2}} {
		dir, _, raw := legacyCatalog(t)
		var state catalogDiskState
		if err := json.Unmarshal(raw, &state); err != nil {
			t.Fatal(err)
		}
		state.Receipts["legacy"] = r
		raw, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "model-catalog.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenCatalog(dir).Engine(); !errors.Is(err, ErrCatalogUnavailable) {
			t.Fatal("invalid receipt became active", err)
		}
	}
}
