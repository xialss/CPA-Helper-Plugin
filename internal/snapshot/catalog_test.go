package snapshot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cpa-helper-plugin/internal/catalog"
	"cpa-helper-plugin/internal/policy"
)

func catalogSnapshot() catalog.Snapshot {
	return catalog.Snapshot{ContractVersion: "v1", PluginVersion: policy.Version, CPAVersion: catalog.CPAVersion, Revision: 1, GeneratedAt: time.Now().UTC(), Credentials: []catalog.Credential{{Ref: policy.CredentialRef("a"), Source: "ai-providers", Provider: "fixture", Enabled: true, Models: []string{"alias"}}}}
}

func TestCatalogTransactionsAndRestart(t *testing.T) {
	policyStore := newStore(t)
	s := OpenCatalog(policyStore.dir)
	if _, err := s.Engine(); !errors.Is(err, ErrCatalogUnavailable) || s.Health().Revision != 0 {
		t.Fatal("missing directory reported available", err)
	}
	p := catalogSnapshot()
	if _, err := s.Apply(p, 0, "first"); err != nil {
		t.Fatal(err)
	}
	p.Credentials[0].Models[0] = "caller-mutated"
	current, _ := s.Current()
	if current.Credentials[0].Models[0] != "alias" || policyStore.Health().Revision != 1 {
		t.Fatal("catalog changed policy or retained caller slices")
	}
	p = catalog.Clone(*current)
	current.Credentials[0].Enabled = false
	if s.Health().Revision != 1 {
		t.Fatal(s.Health())
	}
	// The receipt remains durable even after a later successful synchronization.
	next := catalog.Clone(p)
	next.Revision = 2
	next.Credentials[0].Enabled = false
	if _, err := s.Apply(next, 1, "next"); err != nil {
		t.Fatal(err)
	}
	s = OpenCatalog(policyStore.dir)
	if rev, err := s.Apply(p, 0, "first"); err != nil || rev != 1 {
		t.Fatal("lost durable idempotency", rev, err)
	}
	if rev, err := s.Apply(next, 1, "next"); err != nil || rev != 2 {
		t.Fatal(rev, err)
	}
	if _, err := s.Apply(p, 0, "stale"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.Apply(p, 0, "next"); !errors.Is(err, ErrConflict) {
		t.Fatal("reused receipt accepted", err)
	}
	engine, err := s.Engine()
	if err != nil {
		t.Fatal(err)
	}
	if allowed, err := engine.CanRoute(policy.Rule{}, "alias"); err != nil || allowed {
		t.Fatal("restart widened credential availability", allowed, err)
	}
	view, h := s.View()
	if view.Revision != 2 || h.Revision != 2 || h.Status != "ok" || h.LastError == "" {
		t.Fatal("inconsistent view or hidden rejection", h)
	}
}

func TestCatalogWriteFailureAndUncertainDurability(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "write failed", true: "durability uncertain"}[uncertain], func(t *testing.T) {
			s := OpenCatalog(t.TempDir())
			p := catalogSnapshot()
			if _, err := s.Apply(p, 0, "first"); err != nil {
				t.Fatal(err)
			}
			s.write = func(string, []byte) error {
				if uncertain {
					return ErrPersistenceUncertain
				}
				return errors.New("disk full")
			}
			p.Revision = 2
			p.Credentials[0].Enabled = false
			if _, err := s.Apply(p, 1, "failed"); err == nil {
				t.Fatal("reported false success")
			}
			engine, err := s.Engine()
			if uncertain {
				if !errors.Is(err, ErrCatalogUnavailable) || s.Health().Status != "unavailable" {
					t.Fatal("served an uncertain directory", err)
				}
				if _, err = s.Apply(p, 1, "retry"); !errors.Is(err, ErrCatalogUnavailable) {
					t.Fatal("uncertain state was overwritten", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if allowed, err := engine.CanRoute(policy.Rule{}, "alias"); err != nil || !allowed {
					t.Fatal("failed write replaced last valid directory", err)
				}
			}
			if OpenCatalog(s.dir).Health().Revision != 1 {
				t.Fatal("failed transaction reached disk")
			}
		})
	}
}

func TestCorruptCatalogRequiresRecovery(t *testing.T) {
	for _, raw := range []string{`{`, `{"format":1,"receipts":{},"current":{}}`} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "model-catalog.json"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		s := OpenCatalog(dir)
		if _, err := s.Apply(catalogSnapshot(), 0, "restore"); !errors.Is(err, ErrCatalogUnavailable) {
			t.Fatal("overwrote corrupt state", err)
		}
	}
	dir := t.TempDir()
	raw, err := json.Marshal(catalogDiskState{Format: 1, Current: catalogSnapshot(), Receipts: map[string]receipt{}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "model-catalog.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCatalog(dir).Engine(); !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatal("missing acceptance metadata was treated as durable state", err)
	}
}

func TestCatalogValidationDoesNotPublishPartialData(t *testing.T) {
	s := OpenCatalog(t.TempDir())
	p := catalogSnapshot()
	if _, err := s.Apply(p, 0, "valid"); err != nil {
		t.Fatal(err)
	}
	p.Revision++
	p.Credentials = append(p.Credentials, catalog.Credential{Ref: policy.CredentialRef("invalid"), Source: "ai-providers", Provider: "fixture"})
	if _, err := s.Apply(p, 1, "invalid"); err == nil || s.Health().Revision != 1 {
		t.Fatal("partial catalog published", err)
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, "model-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved catalogDiskState
	if err = json.Unmarshal(raw, &saved); err != nil || len(saved.Current.Credentials) != 1 || saved.Current.Revision != 1 {
		t.Fatal("invalid transaction altered storage", err)
	}
}

func TestAmbiguousCatalogStateRequiresRecovery(t *testing.T) {
	raw, err := json.Marshal(catalogDiskState{Format: 1, Current: catalogSnapshot(), Receipts: map[string]receipt{}, AcceptedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, from, to string }{
		{"envelope case", `"credentials":`, `"Credentials":`},
		{"credential duplicate", `"enabled":true`, `"enabled":false,"enabled":true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			body := []byte(strings.Replace(string(raw), tc.from, tc.to, 1))
			if err := os.WriteFile(filepath.Join(dir, "model-catalog.json"), body, 0600); err != nil {
				t.Fatal(err)
			}
			s := OpenCatalog(dir)
			if _, err := s.Engine(); !errors.Is(err, ErrCatalogUnavailable) {
				t.Fatal("ambiguous stored catalog became active", err)
			}
			if _, err := s.Apply(catalogSnapshot(), 0, "replace"); !errors.Is(err, ErrCatalogUnavailable) {
				t.Fatal("ambiguous catalog state was overwritten", err)
			}
		})
	}
}
