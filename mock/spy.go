package mock

import (
	"sync"
	"testing"
)

// Call represents a single invocation of a spy.
type Call struct {
	Args []any
}

// Spy records invocations for later inspection. Safe for concurrent use.
type Spy struct {
	mu    sync.Mutex
	calls []Call
}

// NewSpy returns a new spy.
func NewSpy() *Spy {
	return &Spy{}
}

// Call records an invocation with the given arguments.
func (s *Spy) Call(args ...any) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.calls = append(s.calls, Call{Args: args})
	s.mu.Unlock()
}

// Calls returns a copy of recorded calls (deterministic slice order).
func (s *Spy) Calls() []Call {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Call, len(s.calls))
	for i, c := range s.calls {
		out[i] = Call{Args: append([]any(nil), c.Args...)}
	}
	return out
}

// CallCount returns the number of recorded calls.
func (s *Spy) CallCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	n := len(s.calls)
	s.mu.Unlock()
	return n
}

// WasCalled returns true if at least one call was recorded.
func (s *Spy) WasCalled() bool {
	return s.CallCount() > 0
}

// CalledWith returns true if any recorded call matches all matchers (same length and each matcher matches the argument).
// Copies calls under lock then runs matchers without holding the lock to avoid contention when matchers are slow (e.g. reflect.DeepEqual).
func (s *Spy) CalledWith(matchers ...ArgMatcher) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	calls := make([]Call, len(s.calls))
	for i, c := range s.calls {
		calls[i] = Call{Args: append([]any(nil), c.Args...)}
	}
	s.mu.Unlock()
	for _, call := range calls {
		if len(call.Args) != len(matchers) {
			continue
		}
		ok := true
		for i, m := range matchers {
			if m == nil || !m.Match(call.Args[i]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// CalledTimes asserts the spy was called exactly n times; calls t.Fatalf otherwise.
func (s *Spy) CalledTimes(t *testing.T, n int) {
	t.Helper()
	if s == nil {
		t.Fatal("spy is nil")
	}
	if s.CallCount() != n {
		t.Fatalf("expected %d calls, got %d", n, s.CallCount())
	}
}
