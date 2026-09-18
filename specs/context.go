package specs

import (
	"math"
	"math/rand"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

// contextPool reuses Context instances in the runner to reduce allocations.
// Runners acquire via acquireContext(backend); do not retain a Context after releasing it back
// to the pool, as it may be reused.
var contextPool = sync.Pool{
	New: func() any {
		return &Context{}
	},
}

// acquireContext gets a Context from contextPool, resets it for backend, and returns it along
// with a release func that resets it again (to drop references) and returns it to the pool.
// Callers should `defer release()` immediately.
func acquireContext(backend testBackend) (*Context, func()) {
	ctx := contextPool.Get().(*Context)
	ctx.Reset(backend)
	return ctx, func() {
		ctx.Reset(nil)
		contextPool.Put(ctx)
	}
}

// expectationPool reuses Expectation instances for Expect(...).To() / ToEqual() to reduce allocations.
var expectationPool = sync.Pool{
	New: func() any {
		return &Expectation{}
	},
}

// Fixture is a before/after hook that receives the context.
type Fixture func(*Context)

// Context is the execution context passed to It and hooks.
type Context struct {
	backend testBackend
	T       *testing.T
	// tb is the concrete testing.TB behind backend, or nil when there is none (fake backends,
	// parallelBackend). Resolved once per spec in Reset rather than per assertion: assertions read
	// it on the failure path only, and a plain field load there keeps the passing fast path's code
	// size — and therefore its speed — unchanged. See the assertion failure branches for why the
	// Helper() call it feeds cannot be delegated to a wrapper.
	tb         testing.TB
	pathValues PathValues
	rng        *rand.Rand
	// coverage is set by the runner during coverage-guided exploration; assertions record edges here.
	coverage *Coverage
	// failed is set by assertions on failure; used by Runner for FailFast to stop execution.
	failed bool
	// failFast is set by Runner when FailFast is true; runner breaks after a step that set failed.
	failFast bool
	// execObserver, when non-nil, receives per-spec Started/Finished notifications from execution
	// points that only have a Context to work with (e.g. a parallelStep goroutine, which has no
	// Runner reference). Installed by Runner.Run only when it has a report.EventReporter; nil
	// otherwise, so execution is unaffected without one.
	execObserver specExecutionObserver
}

// NewContext builds a context for the given test/bench. Use *testing.T or *testing.B.
func NewContext(tb testing.TB) *Context {
	c := &Context{backend: asTestBackend(tb), tb: tb}
	if t, ok := tb.(*testing.T); ok {
		c.T = t
	}
	c.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	return c
}

// Reset clears context state for reuse (e.g. from pool). Pass nil to release references before Put.
func (c *Context) Reset(backend testBackend) {
	if c == nil {
		return
	}
	c.backend = backend
	c.pathValues = PathValues{}
	c.rng = nil
	c.T = nil
	c.tb = nil
	c.coverage = nil
	c.failed = false
	c.failFast = false
	c.execObserver = nil
	if backend != nil {
		// runnableBackend wraps the subtest T; unwrap so ctx.T points to the current subtest.
		if r, ok := backend.(*runnableBackend); ok {
			c.tb = r.tb
			if t, ok := r.tb.(*testing.T); ok {
				c.T = t
			}
		} else if tb, ok := backend.(testing.TB); ok {
			c.tb = tb
			if t, ok := tb.(*testing.T); ok {
				c.T = t
			}
		}
	}
}

// SetFailFast sets whether the runner should stop after the first failing step.
// Called by Runner at start of Run when Runner.FailFast is true.
func (c *Context) SetFailFast(v bool) {
	if c != nil {
		c.failFast = v
	}
}

// recordFailure marks the context as failed (e.g. before Fatalf). Used for FailFast.
func (c *Context) recordFailure() {
	if c != nil {
		c.failed = true
	}
}

// SetPathValues sets the current path combination (used by path runners).
func (c *Context) SetPathValues(pv PathValues) {
	if c == nil {
		return
	}
	pv.assignTo(&c.pathValues)
}

// Path returns the current path values for this run.
func (c *Context) Path() PathValues {
	if c == nil {
		return PathValues{}
	}
	return c.pathValues
}

// RecordCoverage records an execution-path edge for coverage-guided exploration.
// Called by assertions (To, ToEqual) with a cheap hash of branch + outcome; no-op if coverage is nil.
func (c *Context) RecordCoverage(edge uint64) {
	if c == nil || c.coverage == nil {
		return
	}
	c.coverage.Hit(edge)
}

// Expect returns an expectation for the given actual value. The returned Expectation
// is reused from a pool; it is returned to the pool when To() or ToEqual() completes.
func (c *Context) Expect(actual any) *Expectation {
	e := expectationPool.Get().(*Expectation)
	e.ctx = c
	e.actual = actual
	return e
}

// expectT is the generic return type of ExpectT; holds a pooled Expectation.
type expectT[T comparable] struct{ e *Expectation }

// EqualTo asserts that actual equals expected. Zero alloc; single comparison, no type switch, no reflection.
// Helper() is only called on failure so the fast path avoids runtime.Callers().
//
// ATTRIBUTION: this function must not be inlined into the user's spec body. tb.Helper() marks
// whichever function called it, so inlined into the caller it would mark the user's own frame and
// testing would skip past the assertion line it is supposed to report — the failure would surface
// at a runner frame instead. Today the inliner rejects it on size alone (verify with
// `go build -gcflags=-m ./specs/`: it must not appear under "can inline"). It carries no
// //go:noinline because that would cost a real call on the passing fast path this function exists
// to keep free; the attribution suite in specs/attribution_test.go is the guard, and it fails if
// this ever changes.
//
// Compares with Go's == (never reflect.DeepEqual): for a struct holding a pointer field, that
// compares the pointer value itself, not the pointed-to value — unlike ctx.Expect(x).ToEqual(y)'s
// reflect fallback for non-primitive types. See "Equality semantics" in docs/DSL.md.
//
// Example: specs.EqualTo(ctx, 42, 42) or specs.EqualTo(ctx, "got", "got")
func EqualTo[T comparable](c *Context, actual, expected T) {
	if c == nil || c.backend == nil {
		return
	}
	if actual == expected {
		if c.coverage != nil {
			c.RecordCoverage(coverageEdgeHash(2, actual, expected))
		}
		return
	}
	c.recordFailure()
	if c.tb != nil {
		c.tb.Helper()
	}
	c.backend.Fatalf("expected %v to equal %v", actual, expected)
}

// ExpectT returns a typed expectation for comparable types. Zero allocations (reuses pooled Expectation).
// ToEqual(expected) does one type assertion and direct comparison.
//
// ToEqual and To are NOT inlineable today, and must not become so — see the ATTRIBUTION note on
// EqualTo. (An earlier version of this comment claimed ToEqual was inlineable; `go build
// -gcflags=-m` disagrees, and were it true the reported source line would be wrong.)
//
// Same == comparison as EqualTo (see its doc comment) — not reflect.DeepEqual.
//
// Example: specs.ExpectT(ctx, 42).ToEqual(42) or specs.ExpectT(ctx, true).To(specs.BeTrue())
func ExpectT[T comparable](c *Context, v T) expectT[T] {
	e := expectationPool.Get().(*Expectation)
	e.ctx = c
	e.actual = v
	return expectT[T]{e: e}
}

// ToEqual asserts that the value equals expected using ==, not reflect.DeepEqual (see ExpectT's doc
// comment). No reflection. Helper() only on failure, and must stay un-inlined — see the ATTRIBUTION
// note on EqualTo.
func (x expectT[T]) ToEqual(expected T) {
	e := x.e
	if e == nil {
		return
	}
	defer e.release()
	if e.ctx == nil || e.ctx.backend == nil {
		return
	}
	actual, ok := e.actual.(T)
	if !ok {
		e.ctx.recordFailure()
		if e.ctx.tb != nil {
			e.ctx.tb.Helper()
		}
		e.reportNotEqual("expected %v to equal %v (type mismatch)", e.actual, expected)
		return
	}
	if actual != expected {
		e.ctx.recordFailure()
		if e.ctx.tb != nil {
			e.ctx.tb.Helper()
		}
		e.reportNotEqual("expected %v to equal %v", actual, expected)
		return
	}
	if e.ctx.coverage != nil {
		e.ctx.RecordCoverage(coverageEdgeHash(2, actual, expected))
	}
}

// reportNotEqual builds and reports an equality failure. Like reportMatcherFailure it is split out
// and marked noinline to keep the reporting code and its variadic Fatalf setup out of the caller's
// body; here it also keeps that tail out of every generic instantiation of expectT[T].ToEqual.
// Boxing expected into an any allocates, but only on the failure path, where the spec is ending
// anyway — the passing fast path still allocates nothing.
//
// It marks its own frame as a test helper, and the caller marks itself: one Helper() call marks
// only the function that made it, so both frames must opt out before Go attributes the failure to
// the user's assertion line. backend.Fatalf marks the backend's own frame from inside it (see
// runnableBackend.Fatalf), which is the third and last frame between here and testing.
//
//go:noinline
func (e *Expectation) reportNotEqual(format string, actual, expected any) {
	if e.ctx.tb != nil {
		e.ctx.tb.Helper()
	}
	e.ctx.backend.Fatalf(format, actual, expected)
}

// To asserts that the value matches the matcher (interface path; use ToEqual for comparable T).
// Helper() only on failure; must stay un-inlined (see EqualTo's ATTRIBUTION note).
func (x expectT[T]) To(m Matcher) {
	e := x.e
	if e == nil {
		return
	}
	defer e.release()
	// The backend guard matches EqualTo and ExpectT.ToEqual: with no backend there is nowhere to
	// report to, and reportMatcherFailure would dereference a nil interface. An assertion must
	// never turn a misconfigured context into a panic at an unrelated line.
	if e.ctx == nil || e.ctx.backend == nil || m == nil {
		return
	}
	if m.Match(e.actual) {
		if e.ctx.coverage != nil {
			e.ctx.RecordCoverage(coverageEdgeHash(2, e.actual, nil))
		}
		return
	}
	// recordFailure is what makes a typed matcher failure reach ctx.failed, exactly as the untyped
	// Expectation.To does. Without it a spec whose only assertion is ExpectT(ctx, x).To(m) still
	// fails the run (Fatalf reaches the backend) but reports Failed=false to every reporter, and
	// FailFast keeps running the groups after it — the same defect class as issue #115.
	e.ctx.recordFailure()
	if e.ctx.tb != nil {
		e.ctx.tb.Helper()
	}
	e.reportMatcherFailure(m)
}

// Snapshot serializes value as JSON and compares it to the stored snapshot named name.
// Snapshots are stored in __snapshots__ next to the test file. Set GO_SPECS_UPDATE_SNAPSHOTS=1 to create or update snapshots.
//
// Unlike the Expect assertions, the snapshot path marks its helper chain unconditionally rather than
// only on failure. Snapshot already performs caller discovery to locate __snapshots__, and the
// pass/fail verdict is decided inside the snapshots package, which reports the failure itself, so the
// mark cannot be deferred to a failure branch here. Snapshot comparison is JSON marshalling plus file
// I/O, so the extra Helper() call is not on a measurable hot path.
//
// runSnapshot's returned bool is what makes a mismatch reach c.failed: snapshots.RunFromFile reports
// the failure straight to the backend (so go test still exits red) without ever touching ctx.failed,
// so without this call SpecResultEvent.Failed and SuiteEndEvent.FailedSpecs would stay wrong for
// every reporter consumer even though the exit code was correct (issue #115).
func (c *Context) Snapshot(name string, value any) {
	if c == nil || c.backend == nil {
		return
	}
	if c.tb != nil {
		c.tb.Helper()
	}
	_, callerFile, _, ok := runtime.Caller(1)
	if !ok {
		c.recordFailure()
		c.backend.Fatalf("snapshot: could not get caller file")
		return
	}
	if !runSnapshot(c.backend, callerFile, name, value) {
		c.recordFailure()
	}
}

// Expectation is the result of Context.Expect(actual).
type Expectation struct {
	ctx    *Context
	actual any
}

// release returns the Expectation to the pool. Called at end of To()/ToEqual().
func (e *Expectation) release() {
	if e == nil {
		return
	}
	e.ctx = nil
	e.actual = nil
	expectationPool.Put(e)
}

// To asserts that the actual value matches the matcher. Helper() only on failure; must stay
// un-inlined (see EqualTo's ATTRIBUTION note).
func (e *Expectation) To(m Matcher) {
	if e == nil {
		return
	}
	defer e.release()
	// See expectT.To for why the backend is guarded alongside the context and the matcher.
	if e.ctx == nil || e.ctx.backend == nil || m == nil {
		return
	}
	if m.Match(e.actual) {
		if e.ctx.coverage != nil {
			e.ctx.RecordCoverage(coverageEdgeHash(2, e.actual, nil))
		}
		return
	}
	e.ctx.recordFailure()
	if e.ctx.tb != nil {
		e.ctx.tb.Helper()
	}
	e.reportMatcherFailure(m)
}

// reportMatcherFailure builds and reports a matcher failure message. It is split out of To and
// marked noinline so the matcher fast path carries only the branch, not the reporting code and its
// variadic Fatalf setup — inlining that tail back into To costs ~5% on BenchmarkMatcher_GoSpecs for
// code that never runs when a matcher passes.
//
// It marks its own frame as a test helper. The caller must mark itself too: one Helper() call marks
// only the function that made it, so both frames have to opt out before Go will attribute the
// failure to the user's assertion line.
//
//go:noinline
func (e *Expectation) reportMatcherFailure(m Matcher) {
	if e.ctx.tb != nil {
		e.ctx.tb.Helper()
	}
	e.ctx.backend.Fatalf("%s", m.FailureMessage(e.actual))
}

// ToEqual asserts that the actual value equals expected (fast path for benchmarks). Helper() only
// on failure; must stay un-inlined (see EqualTo's ATTRIBUTION note).
//
// Unlike EqualTo/ExpectT.ToEqual (which always use ==), this uses == only for a fast-path set of
// primitive types (int, string, bool, int64, float64, uint) and falls back to reflect.DeepEqual for
// everything else — including other primitives like int32/float32/uint64, and any struct, slice, or
// map. That makes this the right choice when you need value-based equality for non-primitive types;
// see "Equality semantics" in docs/DSL.md for why this differs from EqualTo/ExpectT.
func (e *Expectation) ToEqual(expected any) {
	if e == nil {
		return
	}
	defer e.release()
	// See expectT.To for why the backend is guarded alongside the context.
	if e.ctx == nil || e.ctx.backend == nil {
		return
	}
	// The switch only decides equality; reporting happens once at the bottom so that exactly one
	// frame (this one) marks itself as a test helper on the failure path.
	equal, handled := false, true
	switch a := e.actual.(type) {
	case int:
		if b, ok := expected.(int); ok {
			equal = a == b
		} else {
			handled = false
		}
	case string:
		if b, ok := expected.(string); ok {
			equal = a == b
		} else {
			handled = false
		}
	case bool:
		if b, ok := expected.(bool); ok {
			equal = a == b
		} else {
			handled = false
		}
	case int64:
		if b, ok := expected.(int64); ok {
			equal = a == b
		} else {
			handled = false
		}
	case float64:
		if b, ok := expected.(float64); ok {
			equal = a == b
		} else {
			handled = false
		}
	case uint:
		if b, ok := expected.(uint); ok {
			equal = a == b
		} else {
			handled = false
		}
	default:
		handled = false
	}
	if !handled {
		equal = reflect.DeepEqual(e.actual, expected)
	}
	if equal {
		if e.ctx.coverage != nil {
			e.ctx.RecordCoverage(coverageEdgeHash(2, e.actual, expected))
		}
		return
	}
	e.ctx.recordFailure()
	if e.ctx.tb != nil {
		e.ctx.tb.Helper()
	}
	e.ctx.backend.Fatalf("expected %v to equal %v", e.actual, expected)
}

func (c *Context) randomInt64() int64 {
	if c == nil || c.rng == nil {
		return 0
	}
	return c.rng.Int63()
}

// coverageEdgeHash returns a deterministic edge ID from caller location and comparison outcome (branch sampling).
// Used for coverage-guided exploration; no allocations.
func coverageEdgeHash(skip int, actual, expected any) uint64 {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return 0
	}
	const prime = 1099511628211
	h := uint64(14695981039346656037)
	for i := 0; i < len(file); i++ {
		h ^= uint64(file[i])
		h *= prime
	}
	h ^= uint64(line)
	h *= prime
	h ^= valueHash(actual)
	h *= prime
	h ^= valueHash(expected)
	h *= prime
	return h
}

func valueHash(v any) uint64 {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case int:
		return uint64(x)
	case int64:
		return uint64(x)
	case int32:
		return uint64(x)
	case uint:
		return uint64(x)
	case uint64:
		return x
	case uint32:
		return uint64(x)
	case bool:
		if x {
			return 1
		}
		return 0
	case string:
		h := uint64(len(x))
		for i := 0; i < len(x) && i < 8; i++ {
			h = h*31 + uint64(x[i])
		}
		return h
	case float64:
		return math.Float64bits(x)
	default:
		return 0xabad1dea
	}
}

// runAfterHooks runs after-each fixtures in reverse order (LIFO).
func runAfterHooks(ctx *Context, fixtures []Fixture) {
	for i := len(fixtures) - 1; i >= 0; i-- {
		if f := fixtures[i]; f != nil {
			f(ctx)
		}
	}
}
