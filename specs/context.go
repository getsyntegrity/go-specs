package specs

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/getsyntegrity/go-specs/assert"
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
		// A poisoned Context is deliberately neither reset nor pooled: a spec body called
		// ctx.T.Parallel() and is still parked on a subtest goroutine that holds this pointer, so
		// recycling it here is what turns that body's later assertions into silent no-ops or, worse,
		// into failures charged to whichever unrelated spec next took the Context out of the pool.
		// Abandoning it costs one Context on a run that is already failing. See spec_body_parallel.go.
		if ctx.poisoned {
			return
		}
		ctx.Reset(nil)
		contextPool.Put(ctx)
	}
}

// expectationReusedMessage is the panic raised when an assertion handle is used twice. An
// Expectation is spent by its first To/ToEqual call; a second one is a bug in the spec, and in a
// testing framework the only safe way to report a bug in a spec is loudly. Silently returning —
// which is what the released handle used to do — turns the second assertion into a false green.
const expectationReusedMessage = "go-specs: assertion handle reused. " +
	"The value from ctx.Expect(x) or specs.ExpectT(ctx, x) is spent by its first To/ToEqual call; " +
	"call Expect again for each assertion"

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
	tb testing.TB
	// failure is the authoritative record of whether this spec has failed — the single source
	// report.SpecResultEvent.Failed, SuiteEndEvent.FailedSpecs and Runner FailFast all derive from.
	// It is written only through recordFailure/failf and read only through hasFailed; see failure.go
	// for the record's lifecycle and for why no assertion may mutate it directly (#175).
	failure failureRecord
	// failFast is set by Runner when FailFast is true; runner breaks after a step that set failed.
	failFast bool
	// execObserver, when non-nil, receives per-spec Started/Finished notifications from execution
	// points that only have a Context to work with (e.g. a parallelStep goroutine, which has no
	// Runner reference). Installed by Runner.Run only when it has a report.EventReporter; nil
	// otherwise, so execution is unaffected without one.
	execObserver specExecutionObserver
	// poisoned marks a Context the runner may no longer own or recycle because a spec body called
	// the unsupported ctx.T.Parallel() and is still parked on its subtest goroutine (#172). Set by
	// poison, read only by the release func acquireContext hands out. Both run on the runner's own
	// goroutine while the offending body is parked, so a plain bool needs no synchronisation; the
	// parked body never reads it. See spec_body_parallel.go for the full rationale.
	poisoned bool
}

// poison marks c as unsafe to reset or return to contextPool. It is deliberately one-way: Reset
// clears it, and a poisoned Context is never reset again, so it can never be handed back out.
func (c *Context) poison() {
	if c != nil {
		c.poisoned = true
	}
}

// NewContext builds a context for the given test/bench. Use *testing.T or *testing.B.
func NewContext(tb testing.TB) *Context {
	c := &Context{backend: asTestBackend(tb), tb: tb}
	if t, ok := tb.(*testing.T); ok {
		c.T = t
	}
	return c
}

// Reset clears context state for reuse (e.g. from pool). Pass nil to release references before Put.
func (c *Context) Reset(backend testBackend) {
	if c == nil {
		return
	}
	c.backend = backend
	c.T = nil
	c.tb = nil
	c.resetFailure()
	c.failFast = false
	c.execObserver = nil
	c.poisoned = false
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

// Expect returns an expectation for the given actual value. The returned Expectation is spent by
// its first To() or ToEqual() call; asserting through it again panics (see
// expectationReusedMessage).
//
// This allocates nothing on the fast path despite the composite literal: Expect is small enough to
// inline into the caller, the Expectation never escapes the assertion that consumes it, and escape
// analysis therefore stack-allocates it. That is also why it is no longer pooled — see
// Expectation.release.
func (c *Context) Expect(actual any) *Expectation {
	return &Expectation{ctx: c, actual: actual}
}

// typedExpectation is the single-use state behind a typed handle. It is the typed twin of
// Expectation, and exists for one reason: it stores the value as a T rather than as an any.
//
// Expectation cannot do that. Its actual field is an any because ctx.Expect accepts any value, and
// storing a T there is an interface conversion — which for every value the runtime does not hand
// out for free (integers outside runtime.staticuint64s, every string, every struct) copies the
// value to the heap. That is one allocation per assertion on the path the docs call allocation-free,
// and it is invisible to a benchmark that asserts 42 against 42 (issue #177). Holding the value at
// its own type removes the conversion rather than optimising it.
//
// The pointer indirection stays, and is load-bearing: single use is claimed with CompareAndSwap on
// spent, so every copy of a handle must reach the same flag. Inlining these fields into expectT
// would give each copy its own, and two copies of one handle could then both assert — the silent
// second assertion issue #170 exists to prevent. The pointer costs nothing: the state does not
// escape the assertion that consumes it, so escape analysis stack-allocates it, which is why the
// typed path allocates zero for values of any size.
type typedExpectation[T comparable] struct {
	ctx    *Context
	actual T
	// spent carries the same contract as Expectation.spent — see its comment for why the claim is
	// a CompareAndSwap rather than a check followed by a write.
	spent atomic.Bool
}

// release drops the handle's references once its assertion has run; see Expectation.release, whose
// contract this mirrors. Zeroing actual matters more here than it does there: T may be a struct
// holding pointers, and a retained spent handle must not keep them alive.
func (s *typedExpectation[T]) release() {
	if s == nil {
		return
	}
	var zero T
	s.ctx = nil
	s.actual = zero
}

// expectT is the generic return type of ExpectT; holds the single-use state it will assert through.
// Copying the struct copies the pointer, not the handle, so two copies still contend for the one
// atomic claim and exactly one of them can assert.
type expectT[T comparable] struct{ s *typedExpectation[T] }

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
		return
	}
	if c.tb != nil {
		c.tb.Helper()
	}
	c.failf("expected %v to equal %v", actual, expected)
}

// ExpectT returns a typed expectation for comparable types. ToEqual(expected) is a direct
// comparison: no type assertion, no reflection, and no interface conversion.
//
// ToEqual allocates nothing, for a T of any size. The handle's state does not escape the assertion
// that consumes it, so escape analysis stack-allocates it, and the value is held at its own type
// rather than boxed into an any (see typedExpectation).
//
// To(Matcher) is different, and deliberately so: Matcher is Match(any), so the value must become an
// interface before a matcher can see it. For a value the runtime does not serve from its static
// small-integer table that conversion costs exactly one allocation. Nothing in ExpectT can remove
// it — only a generic Matcher[T] would — so it is measured rather than glossed over, by
// TestAssertionAllocationsByValueShape. Use ToEqual where the comparison is equality.
//
// Like Context.Expect, the returned handle is spent by its first To/ToEqual call; a second
// assertion through it panics (see expectationReusedMessage).
//
// ToEqual and To are NOT inlineable today, and must not become so — see the ATTRIBUTION note on
// EqualTo. (An earlier version of this comment claimed ToEqual was inlineable; `go build
// -gcflags=-m` disagrees, and were it true the reported source line would be wrong.)
//
// Same == comparison as EqualTo (see its doc comment) — not reflect.DeepEqual.
//
// Example: specs.ExpectT(ctx, 42).ToEqual(42) or specs.ExpectT(ctx, true).To(specs.BeTrue())
func ExpectT[T comparable](c *Context, v T) expectT[T] {
	return expectT[T]{s: &typedExpectation[T]{ctx: c, actual: v}}
}

// ToEqual asserts that the value equals expected using ==, not reflect.DeepEqual (see ExpectT's doc
// comment). No reflection, and no interface conversion. Helper() only on failure, and must stay
// un-inlined — see the ATTRIBUTION note on EqualTo.
//
// There is no type-assertion branch here any more. There used to be one, because the value arrived
// as an any and had to be asserted back to T; it could not fail — ExpectT is the only constructor
// and it stores exactly the T it was given — so it was a failure branch no spec could ever reach.
// Holding the value at its own type removes the question rather than re-answering it.
func (x expectT[T]) ToEqual(expected T) {
	s := x.s
	if s == nil {
		return
	}
	if !s.spent.CompareAndSwap(false, true) {
		panicReused()
	}
	defer s.release()
	if s.ctx == nil || s.ctx.backend == nil {
		return
	}
	if s.actual != expected {
		if s.ctx.tb != nil {
			s.ctx.tb.Helper()
		}
		reportNotEqual(s.ctx, "expected %v to equal %v", s.actual, expected)
		return
	}
}

// reportNotEqual hands an equality failure to Context.failf, the one path that records and reports
// it (see failure.go). Like reportMatcherFailure it is split out and marked noinline to keep the
// boxing and the call out of the caller's body; here it also keeps that tail out of every generic
// instantiation of expectT[T].ToEqual. Boxing expected into an any allocates, but only on the
// failure path, where the spec is ending anyway — the passing fast path still allocates nothing.
//
// It marks its own frame as a test helper, and the caller marks itself: one Helper() call marks
// only the function that made it, so every frame between the user's assertion and testing must opt
// out before Go attributes the failure to the user's line. failf marks its own frame, and
// backend.Fatalf marks the backend's from inside it (see runnableBackend.Fatalf) — the last one.
//
// It is a free function taking the Context rather than a method on the handle so that the typed and
// untyped paths share one frame. A method on typedExpectation[T] would be compiled once per
// instantiation, putting a copy of this reporting tail — and the //go:noinline it needs — into
// every T a suite asserts on, for code that only ever runs as a spec ends.
//
//go:noinline
func reportNotEqual(c *Context, format string, actual, expected any) {
	if c.tb != nil {
		c.tb.Helper()
	}
	c.failf(format, actual, expected)
}

// To asserts that the value matches the matcher (interface path; use ToEqual for comparable T).
// Helper() only on failure; must stay un-inlined (see EqualTo's ATTRIBUTION note).
//
// Unlike ToEqual this path converts the value to an interface, because Matcher is Match(any) and a
// matcher cannot see a T any other way. For values outside the runtime's static small-integer table
// that conversion allocates once — see ExpectT's doc comment. The conversion is written out as a
// named local so it happens once rather than at each use, and so the cost is visible in the code
// rather than hidden in two call sites.
func (x expectT[T]) To(m Matcher) {
	s := x.s
	if s == nil {
		return
	}
	if !s.spent.CompareAndSwap(false, true) {
		panicReused()
	}
	defer s.release()
	// The backend guard matches EqualTo and ExpectT.ToEqual: with no backend there is nowhere to
	// report to, and reportMatcherFailure would dereference a nil interface. An assertion must
	// never turn a misconfigured context into a panic at an unrelated line.
	if s.ctx == nil || s.ctx.backend == nil || m == nil {
		return
	}
	boxed := any(s.actual)
	if m.Match(boxed) {
		return
	}
	// reportMatcherFailure funnels into Context.failf, which is what makes a typed matcher failure
	// reach the authoritative failure record — exactly as the untyped Expectation.To does, because
	// both go through the same one path. Reaching the backend without recording is the defect class
	// of #115: the run fails, but every reporter is told Failed=false and FailFast runs on.
	if s.ctx.tb != nil {
		s.ctx.tb.Helper()
	}
	reportMatcherFailure(s.ctx, m, boxed)
}

// Snapshot serializes value as JSON and compares it to the stored snapshot named name.
// Snapshots are stored in __snapshots__ next to the test file. Set GO_SPECS_UPDATE_SNAPSHOTS=1 to create or update snapshots.
//
// Unlike the Expect assertions, the snapshot path marks its helper chain unconditionally rather than
// only on failure. Snapshot already performs caller discovery to locate __snapshots__ before the
// pass/fail verdict is even known, so the mark cannot be deferred to a failure branch here. Snapshot
// comparison is JSON marshalling plus file I/O, so the extra Helper() call is not on a measurable hot
// path.
//
// runSnapshot's returned Result is what makes a mismatch reach the failure record: runSnapshot only
// evaluates the comparison, it never reports it. Handing the verdict to c.failf is what records and
// reports it as one step — on a real testing.T, Fatalf ends in runtime.Goexit, which unwinds this
// goroutine and never comes back, so recording after reporting is dead code (issue #115).
func (c *Context) Snapshot(name string, value any) {
	if c == nil || c.backend == nil {
		return
	}
	if c.tb != nil {
		c.tb.Helper()
	}
	_, callerFile, _, ok := runtime.Caller(1)
	if !ok {
		c.failf("snapshot: could not get caller file")
		return
	}
	if result := runSnapshot(c.backend, callerFile, name, value); !result.Passed {
		c.failf("%s", result.Message)
	}
}

// Expectation is the result of Context.Expect(actual). It carries a single assertion: the first
// To/ToEqual call spends it, and spent is permanent.
type Expectation struct {
	ctx    *Context
	actual any
	// spent is claimed by whichever To/ToEqual call reaches this handle first, and is never
	// cleared: an Expectation is not recycled, so a retained handle can only ever refer to the
	// assertion it was created for. While expectations were pooled, release handed this object to
	// the next Expect call — so a retained handle silently became another spec's handle, and
	// asserting through it reported into that spec's backend (issue #170).
	//
	// It is an atomic.Bool rather than a plain bool because the assertions claim it with
	// CompareAndSwap. A plain `if e.spent { panic }; e.spent = true` is check-then-act: two
	// goroutines sharing one fresh handle both read false and both proceed, which is a data race on
	// ctx and actual as well as a second silent assertion. CompareAndSwap makes "spent" a real
	// property rather than a merely sequential one — exactly one caller can win it, whatever the
	// interleaving — and gives the loser a happens-before edge, so the race detector has nothing to
	// report either.
	spent atomic.Bool
}

// release drops the handle's references once its assertion has run. It does not mark the handle
// spent: the assertion already claimed it with CompareAndSwap on entry, which is what makes the
// claim exclusive. Called at the end of To()/ToEqual().
//
// It deliberately does not return the object to a pool. Pooling was worth an allocation only
// because the handle is short-lived, but correctness needs the opposite guarantee — that a handle
// user code still holds is never handed to anyone else — and the two cannot both be true. Dropping
// the pool costs nothing measurable: the Expectation does not escape the assertion that consumes
// it, so escape analysis stack-allocates it and the fast path still allocates zero.
func (e *Expectation) release() {
	if e == nil {
		return
	}
	e.ctx = nil
	e.actual = nil
}

// panicReused reports an assertion attempted through a spent handle. It is split out and marked
// noinline so each assertion's fast path carries only the branch, not the panic setup — the same
// reason reportMatcherFailure is split out of To.
//
//go:noinline
func panicReused() {
	panic(expectationReusedMessage)
}

// To asserts that the actual value matches the matcher. Helper() only on failure; must stay
// un-inlined (see EqualTo's ATTRIBUTION note).
func (e *Expectation) To(m Matcher) {
	if e == nil {
		return
	}
	if !e.spent.CompareAndSwap(false, true) {
		panicReused()
	}
	defer e.release()
	// See expectT.To for why the backend is guarded alongside the context and the matcher.
	if e.ctx == nil || e.ctx.backend == nil || m == nil {
		return
	}
	if m.Match(e.actual) {
		return
	}
	if e.ctx.tb != nil {
		e.ctx.tb.Helper()
	}
	reportMatcherFailure(e.ctx, m, e.actual)
}

// reportMatcherFailure builds the matcher's failure message and hands it to Context.failf, the one
// path that records and reports it (see failure.go). It is split out of To and marked noinline so
// the matcher fast path carries only the branch, not the message building and the call — inlining
// that tail back into To costs ~5% on BenchmarkMatcher_GoSpecs for code that never runs when a
// matcher passes.
//
// It marks its own frame as a test helper. The caller must mark itself too: one Helper() call marks
// only the function that made it, so every frame between the user's assertion and testing has to
// opt out before Go will attribute the failure to the user's line. failf marks its own.
//
// Like reportNotEqual it is a free function taking the Context, so the typed and untyped paths
// share one frame instead of compiling a copy of this tail into every instantiation of expectT[T].
//
//go:noinline
func reportMatcherFailure(c *Context, m Matcher, actual any) {
	if c.tb != nil {
		c.tb.Helper()
	}
	c.failf("%s", m.FailureMessage(actual))
}

// ToEqual asserts that the actual value equals expected (fast path for benchmarks). Helper() only
// on failure; must stay un-inlined (see EqualTo's ATTRIBUTION note).
//
// Unlike EqualTo/ExpectT.ToEqual (which always use ==), this uses == only for a fast-path set of
// primitive types (int, string, bool, int64, float64, uint) and otherwise defers to
// assert.ValuesEqual: errors.Is(actual, expected) when both values are errors, and
// reflect.DeepEqual for everything else — including other primitives like int32/float32/uint64, and
// any struct, slice, or map. That makes this the right choice when you need value-based equality for
// non-primitive types, or error-identity equality for errors; see "Equality semantics" in
// docs/DSL.md for why this differs from EqualTo/ExpectT.
func (e *Expectation) ToEqual(expected any) {
	if e == nil {
		return
	}
	if !e.spent.CompareAndSwap(false, true) {
		panicReused()
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
		// assert.ValuesEqual rather than reflect.DeepEqual directly, so this path and the Equal
		// matcher answer the same question about the same two values — including the oriented
		// errors.Is semantics for errors. See issue #183.
		equal = assert.ValuesEqual(expected, e.actual)
	}
	if equal {
		return
	}
	if e.ctx.tb != nil {
		e.ctx.tb.Helper()
	}
	// failf, never backend.Fatalf directly: it writes the authoritative failureRecord before
	// reporting, which is the ordering #175 was about. See failure.go.
	e.ctx.failf("%s", assert.EqualFailureMessage(expected, e.actual))
}

// runAfterHooks runs after-each fixtures in reverse order (LIFO).
func runAfterHooks(ctx *Context, fixtures []Fixture) {
	for i := len(fixtures) - 1; i >= 0; i-- {
		if f := fixtures[i]; f != nil {
			f(ctx)
		}
	}
}
