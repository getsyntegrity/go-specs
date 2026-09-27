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
