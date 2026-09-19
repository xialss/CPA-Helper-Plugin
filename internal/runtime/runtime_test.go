package runtime

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"

	"cpa-helper-plugin/internal/policy"
)

func TestConcurrentAdmissionAndRelease(t *testing.T) {
	r := New()
	scope := policy.CallerScope("key")
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprint(i)
			if err := r.Begin(id, scope); err != nil {
				t.Error(err)
				return
			}
			if r.Admit(id, 7) {
				admitted.Add(1)
			}
			r.End(id)
		}()
	}
	wg.Wait()
	if admitted.Load() != 7 || r.Counts()[scope] != 7 {
		t.Fatalf("concurrency exceeded: %d", admitted.Load())
	}
	for i := 0; i < 100; i++ {
		r.Complete(fmt.Sprint(i))
		r.Complete(fmt.Sprint(i))
	}
	if len(r.Counts()) != 0 || len(r.requests) != 0 {
		t.Fatal("leaked reservation")
	}
}

func TestCompletionRacesAdmission(t *testing.T) {
	for i := 0; i < 100; i++ {
		r := New()
		id := "r"
		scope := policy.CallerScope("key")
		if err := r.Begin(id, scope); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); r.Admit(id, 1) }()
		go func() { defer wg.Done(); r.Complete(id) }()
		wg.Wait()
		r.End(id)
		if len(r.Counts()) != 0 || len(r.requests) != 0 {
			t.Fatal("late admission leaked")
		}
	}
}

func TestRepeatedInterceptAndQuiesce(t *testing.T) {
	r := New()
	scope := policy.CallerScope("key")
	for i := 0; i < 3; i++ {
		if err := r.Begin("r", scope); err != nil {
			t.Fatal(err)
		}
		if !r.Admit("r", 1) {
			t.Fatal("retry counted again")
		}
		r.End("r")
	}
	if r.Counts()[scope] != 1 {
		t.Fatal("duplicate slot")
	}
	r.Quiesce()
	if err := r.Begin("other", scope); err == nil {
		t.Fatal("admitted while quiescing")
	}
	r.Complete("r")
	if len(r.Counts()) != 0 {
		t.Fatal("quiesce lost completion")
	}
}

func TestResumeRetainsInFlightAccounting(t *testing.T) {
	r := New()
	scope := policy.CallerScope("key")
	if err := r.Begin("active", scope); err != nil || !r.Admit("active", 1) {
		t.Fatal("initial admission failed", err)
	}
	r.End("active")
	r.Quiesce()
	if err := r.Begin("blocked", scope); err == nil {
		t.Fatal("quiesce admitted a new request")
	}
	r.Resume()
	if err := r.Begin("next", scope); err != nil {
		t.Fatal("resume did not restore admission", err)
	}
	if r.Admit("next", 1) {
		t.Fatal("resume lost the active concurrency count")
	}
	r.End("next")
	r.Complete("active")
	if err := r.Begin("after", scope); err != nil || !r.Admit("after", 1) {
		t.Fatal("completion after resume did not release the slot", err)
	}
	r.End("after")
	r.Complete("after")
}

func TestSubsetWeightsAndChanges(t *testing.T) {
	r := New()
	counts := map[string]int{}
	c := []Candidate{{"a", 3}, {"b", 1}, {"zero", 0}}
	for i := 0; i < 40; i++ {
		counts[r.Pick(1, "key", "model", c)]++
	}
	if counts["a"] != 30 || counts["b"] != 10 || counts["zero"] != 0 {
		t.Fatal(counts)
	}
	if got := r.Pick(1, "key", "model", []Candidate{{"b", 1}}); got != "b" {
		t.Fatal(got)
	}
	if got := r.Pick(1, "key", "model", []Candidate{{"a", 0}}); got != "" {
		t.Fatal(got)
	}
	for i := 0; i < 10; i++ {
		if got := r.Pick(2, "key", "model", []Candidate{{"a", math.MaxInt64}, {"b", math.MaxInt64}}); got == "" {
			t.Fatal("overflow")
		}
	}
	if len(r.pools) != 1 {
		t.Fatal("obsolete revision pools retained")
	}
}
