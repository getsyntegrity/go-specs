package specs

import (
	"sync"
	"testing"
)

// runnableBackendPool recycles runnableBackend to reduce allocations during execution.
var runnableBackendPool = sync.Pool{
	New: func() any { return &runnableBackend{} },
}

// testBackend abstracts the subset of testing.T / testing.B used by assertions.
// Run runs fn as a subtest (for *testing.T) or directly (for *testing.B) for IDE-friendly execution.
type testBackend interface {
	Helper()
	FailNow()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Error(args ...any)
	Errorf(format string, args ...any)
	Log(args ...any)
	Logf(format string, args ...any)
	Name() string
	Cleanup(func())
	Run(name string, fn func(testing.TB))
}

// runnableBackend wraps testing.TB to implement testBackend and Run for subtest execution.
type runnableBackend struct {
	tb testing.TB
}

func (r *runnableBackend) Helper()  { r.tb.Helper() }
func (r *runnableBackend) FailNow() { r.tb.FailNow() }

// The reporting methods below each call r.tb.Helper() before delegating. testing.T.Helper marks
// the function that called it, so this is what makes the runnableBackend frame itself transparent;
// without it Go attributes every go-specs failure to testing_backend.go. Callers must additionally
// mark their own frame (see Context.helperTB) — one Helper() call can never mark two frames.
func (r *runnableBackend) Fatal(args ...any) {
	r.tb.Helper()
	r.tb.Fatal(args...)
}

func (r *runnableBackend) Fatalf(format string, args ...any) {
	r.tb.Helper()
	r.tb.Fatalf(format, args...)
}

func (r *runnableBackend) Error(args ...any) {
	r.tb.Helper()
	r.tb.Error(args...)
}

func (r *runnableBackend) Errorf(format string, args ...any) {
	r.tb.Helper()
	r.tb.Errorf(format, args...)
}

func (r *runnableBackend) Log(args ...any) {
	r.tb.Helper()
	r.tb.Log(args...)
}

func (r *runnableBackend) Logf(format string, args ...any) {
	r.tb.Helper()
	r.tb.Logf(format, args...)
}

func (r *runnableBackend) Name() string      { return r.tb.Name() }
func (r *runnableBackend) Cleanup(fn func()) { r.tb.Cleanup(fn) }

func (r *runnableBackend) Run(name string, fn func(testing.TB)) {
	if t, ok := r.tb.(*testing.T); ok {
		t.Run(name, func(t *testing.T) {
			fn(t)
		})
		return
	}
	fn(r.tb)
}

// helperTB returns the concrete testing.TB behind b, or nil when b is not backed by one (fake
// backends in unit tests, parallelBackend). It deliberately does NOT call Helper() itself: source
// attribution depends on *which* function invokes testing.T.Helper, so the returned TB must be used
// as a literal `tb.Helper()` inside the frame that wants to be marked transparent. Routing that call
// through any wrapper marks the wrapper instead — that is the bug this accessor exists to avoid.
//
// Assertions do not call this: they read the already-resolved Context.tb, because calling this per
// assertion put the two type assertions inside hot assertion bodies and cost ~10% on the passing
// fast path (BenchmarkAssertion_GoSpecs_EqualTo) for code that only ever runs on failure. It
// remains for the snapshot path, which has no Context in hand, and stays noinline for the same
// code-size reason.
//
//go:noinline
func helperTB(b testBackend) testing.TB {
	if b == nil {
		return nil
	}
	if r, ok := b.(*runnableBackend); ok {
		return r.tb
	}
	if tb, ok := b.(testing.TB); ok {
		return tb
	}
	return nil
}

func asTestBackend(tb testing.TB) testBackend {
	if tb == nil {
		return nil
	}
	r := runnableBackendPool.Get().(*runnableBackend)
	r.tb = tb
	return r
}

// putTestBackend returns a testBackend to the pool when it is a runnableBackend. Call after execution.
func putTestBackend(b testBackend) {
	if r, ok := b.(*runnableBackend); ok {
		r.tb = nil
		runnableBackendPool.Put(r)
	}
}

//nolint:unused // Retained for testing.TB interoperation.
func asTestingTB(tb testBackend) testing.TB {
	if r, ok := tb.(*runnableBackend); ok {
		return r.tb
	}
	if real, ok := tb.(testing.TB); ok {
		return real
	}
	panic("specs: backend does not implement testing.TB")
}

//nolint:unused // Retained for testing.T-specific interoperation.
func requireTestingT(tb testBackend) *testing.T {
	if r, ok := tb.(*runnableBackend); ok {
		if t, ok := r.tb.(*testing.T); ok {
			return t
		}
	}
	panic("specs: Spec requires *testing.T; use Context directly in benchmarks")
}
