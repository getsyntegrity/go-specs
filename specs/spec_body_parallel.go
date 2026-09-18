package specs

import (
	"fmt"
	"sync/atomic"
	"testing"
)

// spec_body_parallel.go defines and enforces what happens when a sequential spec body calls the Go
// testing primitive ctx.T.Parallel() (#172).
//
// # Supported semantics
//
// ctx.T.Parallel() is NOT supported inside a spec body, in any sequential execution model, and is
// detected and reported as a hard failure rather than tolerated.
//
// The reason is structural, not incidental. Sequential execution runs every spec of a group against
// one shared, mutable *Context, re-pointing its backend/T/tb at the current subtest for the duration
// of the body (runSpecBody in runner.go) or re-Reset-ing it per spec (runSpecProgramIsolated in
// execution_plan.go). Both are correct only because testing.T.Run does not return until the body has
// finished. t.Parallel() removes exactly that guarantee: it parks the subtest goroutine and hands
// control back to the parent, so the runner resumes with the Context still bound to a spec that has
// not run yet. Two corruptions follow, and the quiet one is the dangerous one:
//
//   - the next spec opens its subtest under the previous spec's still-installed *testing.T,
//     producing nested identities such as "suite/bravo/suite/charlie" that neither a reporter nor a
//     -run pattern can address; and
//   - the runner reaches the end of the spec and releases the shared Context back to contextPool
//     while the parked body has not executed. When that body finally resumes, every assertion it
//     makes reads a nil backend and returns silently (see the c.backend == nil guards in
//     context.go), so the suite reports PASS having proven nothing.
//
// # The supported alternative
//
// Concurrency is a first-class feature here, it just does not go through testing's parallel
// subtests: ItParallel (Builder) and RunParallel/RunParallelBatched (MinimalRunner) give every spec
// its own Context backed by a parallelBackend, which deliberately never exposes a live *testing.T
// (see program.go's parallelStep and scheduler.go). That model owns spec identity and failure
// attribution itself, which is precisely what testing's parallel subtests would take away.
//
// # How the guard works
//
// A parked goroutine is observable without cooperating with testing internals: a deferred store at
// the top of the subtest closure runs when the body returns, fails (runtime.Goexit) or panics, but
// not while t.Parallel() has the goroutine blocked. So "t.Run returned and the store never
// happened" is exactly "the body called t.Parallel() and is still pending".
//
// On detection the runner poisons the Context (see Context.poison) and fails immediately.

// runSubtestGuardingParallel runs body as a subtest of t and reports whether the subtest ran at all
// and whether it is still parked when t.Run returns.
//
// ran is set from inside the closure rather than read from t.Run's bool return, which is true for a
// filtered-out subtest too — the same reason runSpecIsolated and runSpecProgramIsolated already set
// it that way; the callers thread it back as Filtered. It is an atomic.Bool for the same reason
// done is, below: it is written on the subtest goroutine and read on the parent's.
//
// parked reports a body that called t.Parallel(). done is stored by a defer, so a body that
// returned, called Fatal/FailNow (runtime.Goexit) or panicked all count as finished; only a parked
// goroutine leaves it unset.
//
// done is an atomic.Bool rather than a plain bool, and deliberately so. A plain bool would in
// practice be ordered — testing.T.Parallel signals the parent over a channel that t.Run receives —
// but that ordering is a property of testing's current internals, not of anything this package
// controls, and a guard whose memory safety rests on an unexported implementation detail of another
// package is not a guard worth having. The atomic makes the store/load correct by construction, so
// the only thing left resting on testing's behaviour is the semantic claim this function actually
// means to make: a parked goroutine has not run its defers yet. That claim is inherent — it is what
// "parked" means — and is covered by the real-process regressions in spec_body_parallel_test.go.
//
// The cost is nil in context: this path already starts a goroutine via t.Run, and the hot assertion
// path never reaches it.
func runSubtestGuardingParallel(t *testing.T, name string, body func(subT *testing.T)) (ran, parked bool) {
	var started, done atomic.Bool
	t.Run(name, func(subT *testing.T) {
		started.Store(true)
		defer done.Store(true)
		body(subT)
	})
	ran = started.Load()
	return ran, ran && !done.Load()
}

// unsupportedSpecBodyParallelMessage builds the diagnostic for a spec body that called
// ctx.T.Parallel(). It follows the shape parallelBackend.Run already established for an unsupported
// operation: name what was attempted, say which execution model forbids it, state what did not
// happen as a result, and point at the supported alternative.
func unsupportedSpecBodyParallelMessage(specName string) string {
	where := "a sequential spec body"
	if specName != "" {
		where = fmt.Sprintf("the sequential spec %q", specName)
	}
	return fmt.Sprintf(
		"ctx.T.Parallel() is not supported in %s: the subtest parked and t.Run returned before the body finished, "+
			"which leaves the runner's shared Context bound to a spec that has not executed — the run was stopped here "+
			"and the remaining specs did not run, because continuing would nest their subtests under this one and "+
			"silently turn their assertions into no-ops. Use ItParallel (or RunParallel/RunParallelBatched) to run "+
			"specs concurrently; those give each spec its own Context and never expose a live *testing.T.",
		where,
	)
}

// failUnsupportedSpecBodyParallel poisons ctx and stops the run with the diagnostic above.
//
// Poisoning comes first and matters as much as the message. The parked body still holds ctx and
// will resume after this test's goroutine unwinds; leaving ctx recyclable would either blank its
// backend (silent no-op assertions) or hand the same Context to an unrelated spec that the parked
// body would then corrupt. A poisoned Context is simply abandoned, still bound to the subtest the
// parked body belongs to, so that body's assertions keep landing where they belong.
//
// t.Fatalf, not panic: the sequential path always holds a real *testing.T here, so Fatalf reports
// the failure against the test that owns these specs and unwinds only its goroutine, exactly as an
// ordinary spec failure would. A panic would additionally be attributed to go-specs' own frames.
func failUnsupportedSpecBodyParallel(t *testing.T, ctx *Context, specName string) {
	t.Helper()
	ctx.poison()
	t.Fatalf("%s", unsupportedSpecBodyParallelMessage(specName))
}
