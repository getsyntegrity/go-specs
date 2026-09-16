package mock

import (
	"sync"
	"testing"
)

func TestMockCallConcurrentInvocations(t *testing.T) {
	m := New()
	spy := m.Spy("invoke")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			spy.Call(v)
		}(i)
	}
	wg.Wait()
	spy.CalledTimes(t, 50)
}

// TestMockSpyConcurrentFirstCallSameName verifies #26's fix: many goroutines racing to create the
// spy for the SAME name for the first time (the lazy-write path in Mock.Spy) must not race on the
// underlying map and must all observe the same *Spy instance — calls made through different
// goroutines' returned pointers must all land on it, not be silently lost to separate spies.
func TestMockSpyConcurrentFirstCallSameName(t *testing.T) {
	m := New()
	const n = 100
	spies := make([]*Spy, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			spies[i] = m.Spy("shared")
		}(i)
	}
	wg.Wait()

	first := spies[0]
	for i, s := range spies {
		if s != first {
			t.Fatalf("spy[%d] = %p, want the same instance as spy[0] = %p", i, s, first)
		}
	}
	first.Call("x")
	first.CalledTimes(t, 1)
}

// TestMockSpyConcurrentFirstCallDistinctNames verifies concurrent first-time Spy(name) calls for
// distinct names don't race on the map and every name ends up with its own spy.
func TestMockSpyConcurrentFirstCallDistinctNames(t *testing.T) {
	m := New()
	const n = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Spy(string(rune('a' + i%26))).Call(i)
		}(i)
	}
	wg.Wait()

	for c := 'a'; c < 'a'+26; c++ {
		if s := m.Spy(string(c)); len(s.Calls()) == 0 {
			t.Errorf("expected spy %q to have recorded at least one call", string(c))
		}
	}
}

func TestSpyConcurrentInvocations(t *testing.T) {
	spy := NewSpy()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			spy.Call(v)
		}(i)
	}
	wg.Wait()
	spy.CalledTimes(t, 100)
}
