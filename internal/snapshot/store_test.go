package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"cpa-helper-plugin/internal/policy"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s := Open(filepath.Join(t.TempDir(), "state"))
	if s.Health().Status != "ok" {
		t.Fatal(s.Health())
	}
	return s
}

func TestTransactionsRestartAndRollback(t *testing.T) {
	s := newStore(t)
	p, _ := s.Current()
	p.Revision = 2
	p.Keys = []policy.Key{{ID: policy.CallerScope("test"), Enabled: true, GroupIDs: []string{}, MaxConcurrency: 2}}
	if _, err := s.Apply(p, 1, "first"); err != nil {
		t.Fatal(err)
	}
	if rev, err := s.Apply(p, 1, "first"); err != nil || rev != 2 {
		t.Fatal(rev, err)
	}
	if _, err := s.Apply(p, 1, "different"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	reopened := Open(s.dir)
	if reopened.Health().Revision != 2 {
		t.Fatal(reopened.Health())
	}
	if rev, err := reopened.Rollback(2, 1, "rollback"); err != nil || rev != 3 {
		t.Fatal(rev, err)
	}
	if rev, err := reopened.Rollback(2, 1, "rollback"); err != nil || rev != 3 {
		t.Fatal("non-idempotent rollback", rev, err)
	}
	current, _ := reopened.Current()
	if len(current.Keys) != 0 {
		t.Fatal("rollback failed")
	}
	if rev, err := reopened.Apply(p, 1, "first"); err != nil || rev != 2 {
		t.Fatal("durable receipt lost", rev, err)
	}
}

func TestOpenAcceptsPreviousPluginVersion(t *testing.T) {
	s := newStore(t)
	s.state.Current.PluginVersion = "0.1.0"
	raw, err := json.Marshal(s.state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(s.dir, "state.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	reopened := Open(s.dir)
	if reopened.Health().Status != "ok" || reopened.Health().PluginVersion != policy.Version {
		t.Fatal(reopened.Health())
	}
	current, err := reopened.Current()
	if err != nil || current.PluginVersion != "0.1.0" {
		t.Fatal(current.PluginVersion, err)
	}
}

func TestFailedPersistenceDoesNotPublish(t *testing.T) {
	s := newStore(t)
	s.write = func(string, []byte) error { return errors.New("disk full") }
	p, _ := s.Current()
	p.Revision = 2
	if _, err := s.Apply(p, 1, "write"); err == nil {
		t.Fatal("reported false success")
	}
	if s.Health().Revision != 1 || s.Health().LastError == "" {
		t.Fatal(s.Health())
	}
	if Open(s.dir).Health().Revision != 1 {
		t.Fatal("disk changed")
	}
}

func TestUncertainPersistenceFailsClosed(t *testing.T) {
	s := newStore(t)
	s.write = func(string, []byte) error {
		return fmt.Errorf("%w: directory sync failed", ErrPersistenceUncertain)
	}
	p, _ := s.Current()
	p.Revision = 2
	if _, err := s.Apply(p, 1, "uncertain"); !errors.Is(err, ErrPersistenceUncertain) {
		t.Fatal(err)
	}
	if s.Health().Status != "unavailable" {
		t.Fatal("store continued serving an uncertain transaction", s.Health())
	}
	if _, _, err := s.Engine(); err == nil {
		t.Fatal("uncertain transaction left request policy available")
	}
}

func TestAtomicWriteReportsDirectorySyncFailureAfterRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	err := atomicWriteWithSync(path, []byte("new"), func(string) error {
		return errors.New("sync failed")
	})
	if !errors.Is(err, ErrPersistenceUncertain) {
		t.Fatal(err)
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil || string(raw) != "new" {
		t.Fatal("failure was not reported after replacement", string(raw), readErr)
	}
}

func TestMissingCorruptAndInvalidPolicyFailClosed(t *testing.T) {
	for _, data := range []string{"", `{`, `{"format":1,"receipts":{},"current":{}}`} {
		t.Run(data, func(t *testing.T) {
			dir := t.TempDir()
			if data != "" {
				if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			s := Open(dir)
			if _, _, err := s.Engine(); err == nil {
				t.Fatal("failed open")
			}
			if s.Health().Status != "unavailable" {
				t.Fatal(s.Health())
			}
		})
	}
}

func TestSnapshotCopiesAndValidation(t *testing.T) {
	s := newStore(t)
	p, _ := s.Current()
	p.Revision = 2
	p.Keys = []policy.Key{{ID: policy.CallerScope("x"), Enabled: true, GroupIDs: []string{}, Rule: policy.Rule{Models: []string{"allowed"}}}}
	if _, err := s.Apply(p, 1, "apply"); err != nil {
		t.Fatal(err)
	}
	p.Keys[0].Enabled = false
	p.Keys[0].Rule.Models[0] = "changed"
	copy, _ := s.Current()
	copy.Keys[0].Enabled = false
	copy.Keys[0].Rule.Models[0] = "changed-again"
	e, _, _ := s.Engine()
	if key := e.Resolve(policy.CallerScope("x")); !key.Enabled || !key.Rule.AllowsModel("allowed") || key.Rule.AllowsModel("changed") {
		t.Fatal("external mutation reached engine")
	}
	p.Revision = 3
	p.Keys[0].GroupIDs = []string{"missing"}
	if _, err := s.Apply(p, 2, "invalid"); err == nil {
		t.Fatal("accepted missing group")
	}
	if s.Health().Revision != 2 {
		t.Fatal("partial application")
	}
}

func TestStrictJSON(t *testing.T) {
	for _, raw := range []string{`{"extra":1}`, `{} {}`, `null {}`, `{`} {
		var p policy.Snapshot
		if err := Decode([]byte(raw), &p); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
