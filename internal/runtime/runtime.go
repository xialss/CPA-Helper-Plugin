// Package runtime implements per-instance request admission and subset scheduling.
package runtime

import (
	"errors"
	"math/big"
	"sort"
	"sync"

	"cpa-helper-plugin/internal/policy"
)

type activeRequest struct {
	scope    string
	calls    int
	complete bool
	admitted bool
}
type poolState struct {
	weights map[string]int64
	scores  map[string]*big.Int
}

// Runtime tracks request lifetimes independently of policy replacement.
type Runtime struct {
	mu       sync.Mutex
	requests map[string]*activeRequest
	counts   map[string]int
	pools    map[string]*poolState
	revision uint64
	closed   bool
}

// New constructs an empty instance-local runtime.
func New() *Runtime {
	return &Runtime{requests: map[string]*activeRequest{}, counts: map[string]int{}, pools: map[string]*poolState{}}
}

// Begin registers an in-flight intercept before policy evaluation can race completion.
func (r *Runtime) Begin(id, scope string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("plugin is quiescing")
	}
	if id == "" || !policy.ValidScope(scope) {
		return errors.New("missing request identity")
	}
	a := r.requests[id]
	if a == nil {
		a = &activeRequest{scope: scope}
		r.requests[id] = a
	}
	if a.scope != scope || a.complete {
		return errors.New("request identity is no longer active")
	}
	a.calls++
	return nil
}

// Admit reserves at most one slot for a request, including repeated intercepts.
func (r *Runtime) Admit(id string, limit int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.requests[id]
	if a == nil || a.complete || r.closed {
		return false
	}
	if a.admitted {
		return true
	}
	if limit > 0 && r.counts[a.scope] >= limit {
		return false
	}
	a.admitted = true
	r.counts[a.scope]++
	return true
}

// End closes the intercept call, retaining only live admitted requests.
func (r *Runtime) End(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.requests[id]
	if a == nil {
		return
	}
	a.calls--
	if a.calls == 0 && (!a.admitted || a.complete) {
		delete(r.requests, id)
	}
}

// Complete releases an admitted slot exactly once and fences an in-flight intercept.
func (r *Runtime) Complete(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.requests[id]
	if a == nil {
		return
	}
	a.complete = true
	if a.admitted {
		r.counts[a.scope]--
		if r.counts[a.scope] == 0 {
			delete(r.counts, a.scope)
		}
		a.admitted = false
	}
	if a.calls == 0 {
		delete(r.requests, id)
	}
}

// Counts returns a detached snapshot of current concurrency.
func (r *Runtime) Counts() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := map[string]int{}
	for k, v := range r.counts {
		m[k] = v
	}
	return m
}

// Quiesce prevents new reservations while existing completion events drain.
func (r *Runtime) Quiesce() { r.mu.Lock(); defer r.mu.Unlock(); r.closed = true }

// Resume restores admission after host replacement rollback without resetting
// requests, concurrency counts, or scheduler state retained by the old instance.
func (r *Runtime) Resume() { r.mu.Lock(); defer r.mu.Unlock(); r.closed = false }

// Candidate contains only the selection information used by weighted scheduling.
type Candidate struct {
	ID     string
	Weight int64
}

// Pick performs smooth weighted round robin inside an already authorized subset.
// Arbitrary-precision scores avoid overflowing valid host-supplied int64 weights.
func (r *Runtime) Pick(revision uint64, scope, model string, candidates []Candidate) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.revision != revision {
		r.pools = map[string]*poolState{}
		r.revision = revision
	}
	positive := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		if c.Weight > 0 {
			positive = append(positive, c)
		}
	}
	if len(positive) == 0 {
		return ""
	}
	if len(positive) == 1 {
		return positive[0].ID
	}
	sort.Slice(positive, func(i, j int) bool { return positive[i].ID < positive[j].ID })
	key := scope + "\x00" + model
	s := r.pools[key]
	changed := s == nil
	if s != nil {
		for _, c := range positive {
			if old, ok := s.weights[c.ID]; ok && old != c.Weight {
				changed = true
			}
		}
	}
	if changed {
		s = &poolState{weights: map[string]int64{}, scores: map[string]*big.Int{}}
		r.pools[key] = s
	}
	// Candidate removal prunes obsolete state rather than accumulating historical credentials.
	live := map[string]bool{}
	total := new(big.Int)
	selected := ""
	for _, c := range positive {
		live[c.ID] = true
		s.weights[c.ID] = c.Weight
		if s.scores[c.ID] == nil {
			s.scores[c.ID] = new(big.Int)
		}
		weight := big.NewInt(c.Weight)
		total.Add(total, weight)
		s.scores[c.ID].Add(s.scores[c.ID], weight)
		if selected == "" || s.scores[c.ID].Cmp(s.scores[selected]) > 0 {
			selected = c.ID
		}
	}
	for id := range s.scores {
		if !live[id] {
			delete(s.scores, id)
			delete(s.weights, id)
		}
	}
	s.scores[selected].Sub(s.scores[selected], total)
	return selected
}
