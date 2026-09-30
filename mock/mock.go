// Package mock provides call recording and interface mocking for specs.
//
// Spy records calls and Mock groups named spies. Controller adds expectations on top of one shared
// recorder: Method(name).Expect(args...) declares how a call is expected to happen (Times counts,
// default exactly once), Method(name).Call(args...) is what a hand-written adapter of an interface
// method calls, Result/Value read the stubbed return values, InOrder checks the global call order
// across methods and spies, and Verify (registered with t.Cleanup by NewController) reports unmet
// expectations. Unexpected calls are reported immediately through t.Errorf.
//
// The package depends only on assert: a *testing.T, *testing.B or *specs.Context satisfies TB.
package mock

import "sync"

// Mock holds named spies for verification. The zero value is ready to use; New is kept for
// symmetry with NewSpy. Safe for concurrent use, matching Spy's own guarantee — e.g. specs run via
// RunParallel/ItParallel can call Spy(name) concurrently, including the first call for a given name
// (which lazily creates the entry).
type Mock struct {
	mu    sync.Mutex
	spies map[string]*Spy
}

// New returns a new Mock.
func New() *Mock {
	return &Mock{
		spies: map[string]*Spy{},
	}
}

// Spy returns the spy for the given name, creating it if needed.
func (m *Mock) Spy(name string) *Spy {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.spies == nil {
		m.spies = map[string]*Spy{}
	}
	if s, ok := m.spies[name]; ok {
		return s
	}
	s := NewSpy()
	m.spies[name] = s
	return s
}
