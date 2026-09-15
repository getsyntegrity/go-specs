// runner.go executes a compiled Program. No hook resolution at runtime; no allocations in the loop
// unless a Reporter is set, or the backend is a real *testing.T (each spec then runs its own
// before/body/after as one unit inside its own subtest for isolation — see runSpecRecovered).
//
// A group's before/after hooks run once per spec, not once for the whole group (#109): coalescing
// specs that share the same hooks into one group is purely a compile-time memory/locality
// optimization (see program.go), invisible here — it has no effect on how often before/after run.
// This converges with the Describe/ExecutionPlan model, whose compiler flattens each It's hooks into
// that spec's own instruction range (see compiler.go's EmitIt).
package specs

import (
	"fmt"
	"runtime/debug"
	"sync"
	"testing"
	"time"

	"github.com/pablogore/go-specs/report"
)

// Runner runs a compiled Program against a test backend. One context from the pool, reused for every step.
type Runner struct {
	program  *Program
	FailFast bool // if true, stop after the first step that sets ctx.failed (e.g. assertion failure)

	// Name and Reporter are optional: when Reporter is nil, Run behaves exactly as it did before
	// either field existed — no events, no extra work. When set, Run emits SuiteStarted before the
	// program runs and SuiteFinished after, with SpecStarted/SpecFinished around every named spec
	// (see group.names) — including each real spec inside an ItParallel group, reported from its own
	// goroutine (see parallelStep). Name falls back to the backend's name if empty.
	Name     string
	Reporter report.EventReporter
}

// NewRunner creates a runner for the given program. Program must not be nil; do not modify program.Groups after creation.
func NewRunner(program *Program) *Runner {
	return &Runner{program: program}
}

// NewRunnerWithReporter creates a runner that reports SuiteStarted/SuiteFinished and
// SpecStarted/SpecFinished events to rep as the program runs. name is used for
// SuiteStartEvent/SuiteEndEvent.Name; if empty, the backend's name is used instead.
func NewRunnerWithReporter(program *Program, name string, rep report.EventReporter) *Runner {
	return &Runner{program: program, Name: name, Reporter: rep}
}

// reporterObserver adapts a report.EventReporter to specExecutionObserver, serializing calls with a
// mutex: a parallel group's goroutines call specStarted/specFinished concurrently, and not every
// EventReporter implementation can be assumed to be concurrency-safe on its own — the framework
// serializes on its behalf instead of expanding EventReporter's contract to require it. total/failed/
// skipped tally every reported spec (sequential and parallel alike, since both paths share one
// instance via ctx.execObserver) for the run's SuiteEndEvent. total counts passed+failed+skipped.
type reporterObserver struct {
	mu      sync.Mutex
	rep     report.EventReporter
	total   int
	failed  int
	skipped int
}

func (o *reporterObserver) specStarted(name string) report.SpecStartEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	e := report.SpecStartEvent{Name: name, Time: time.Now()}
	o.rep.SpecStarted(e)
	return e
}

func (o *reporterObserver) specFinished(start report.SpecStartEvent, result specResult) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.total++
	if result.Failed {
		o.failed++
	}
	o.rep.SpecFinished(report.SpecResultEvent{
		SpecStartEvent: start,
		Failed:         result.Failed,
		Duration:       time.Since(start.Time),
		Message:        result.Message,
		Output:         result.Output,
	})
}

// specSkipped reports a compile-time-skipped spec: SpecStarted immediately followed by
// SpecFinished{Skipped: true}, reusing the exact same SpecStartEvent for both (same invariant
// specStarted/specFinished hold for a real spec) — Duration is left at its zero value, since no
// body ever ran between them, and Failed is always false.
func (o *reporterObserver) specSkipped(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	e := report.SpecStartEvent{Name: name, Time: time.Now()}
	o.rep.SpecStarted(e)
	o.total++
	o.skipped++
	o.rep.SpecFinished(report.SpecResultEvent{SpecStartEvent: e, Skipped: true})
}

var _ specExecutionObserver = (*reporterObserver)(nil)

// Run executes all groups in order. Within each group, every spec runs its own before hooks, body,
// and after hooks (reverse order) as one unit — see runSpecWithHooks. Zero allocations in the loop
// when Reporter is nil; deterministic.
//
// A panic in before, a spec, or an after hook is recovered instead of crashing the process. A real
// testing.T.Fatal/Fatalf/FailNow in before or the spec (runtime.Goexit) still guarantees after runs,
// via a defer registered before before/body ever start — see runSpecWithHooks for the exact contract.
func (r *Runner) Run(tb testing.TB) {
	if r == nil || r.program == nil || tb == nil || len(r.program.Groups) == 0 {
		return
	}
	backend := asTestBackend(tb)
	defer putTestBackend(backend)
	ctx, release := acquireContext(backend)
	defer release()
	ctx.SetPathValues(PathValues{})
	if r.FailFast {
		ctx.SetFailFast(true)
	}

	if r.Reporter == nil {
		runGroups(ctx, r.program.Groups)
		return
	}

	name := r.Name
	if name == "" {
		name = backend.Name()
	}
	obs := &reporterObserver{rep: r.Reporter}
	ctx.execObserver = obs
	suiteStart := time.Now()
	r.Reporter.SuiteStarted(report.SuiteStartEvent{Name: name, Time: suiteStart})
	runGroups(ctx, r.program.Groups)
	r.Reporter.SuiteFinished(report.SuiteEndEvent{
		Name:         name,
		Time:         time.Now(),
		Duration:     time.Since(suiteStart),
		TotalSpecs:   obs.total,
		FailedSpecs:  obs.failed,
		SkippedSpecs: obs.skipped,
	})
}

// runGroups runs each group in order, stopping before the next group if FailFast is set and a
// previous group left ctx failed (an ordinary assertion failure or a recovered panic — recordFailure
// marks both the same way, so this check needs no panic-specific case). Split out from Run so the
// group-iteration/FailFast contract can be tested without a real testing.TB.
func runGroups(ctx *Context, groups []group) {
	n := len(groups)
	for gi := 0; gi < n; gi++ {
		if ctx.failFast && ctx.failed {
			break
		}
		runGroup(ctx, &groups[gi])
		if ctx.failFast && ctx.failed {
			break
		}
	}
}

// runGroup reports g's skipped specs, then runs its real specs. g.before/g.after are shared across
// every spec in the group (see program.go's group doc comment) purely so they don't need to be
// recompiled per spec — see runSpecWithHooks for the per-spec execution contract itself.
//
// g.skipped is reported first, before any real spec runs: those names carry no before/after of their
// own (see builder.go's finalize), so their identity as skipped must not depend on whether this
// group's unrelated before hook — which they were only attached to for compilation reasons —
// succeeds, fails, or FailFast ends up skipping the rest of this group.
func runGroup(ctx *Context, g *group) {
	reportSkipped(ctx, g)
	runSpecsRecovered(ctx, g)
}

// reportSkipped reports each of g.skipped as its own SpecStarted/SpecFinished{Skipped: true} pair.
// A skipped spec was never compiled into a step (see builder.go's finalize), so there is nothing to
// run for it here — only its identity is reported, via ctx.execObserver same as any other spec. A
// nil execObserver (no Reporter attached) means nothing happens at all, same as any unreported spec.
func reportSkipped(ctx *Context, g *group) {
	obs := ctx.execObserver
	if obs == nil {
		return
	}
	for _, name := range g.skipped {
		obs.specSkipped(name)
	}
}

// runSpecsRecovered runs a group's specs in order, recovering each one individually.
//
// ctx.failed is reset before every spec unconditionally — not gated on whether ctx.execObserver is
// set — because gating it would make attaching a reporter change execution semantics; reporting must
// stay purely observational. This also fixes a latent bug: without the reset, ctx.failed stuck true
// after the first failing spec in a group and stayed true for the rest of this loop (harmless today,
// since nothing outside the immediate failFast-gated checks ever read it, but a real correctness
// issue for any future consumer, and now for reporting).
//
// When ctx.execObserver is set and this group has a name for index i (see group.names — left nil for
// a parallel group, whose parallelStep closure reports its own real specs instead of one entry per
// group.specs), SpecStarted/SpecFinished are emitted around the spec using its own captured failed
// value, not any carry-over from a before hook or a previous spec.
//
// Each spec runs its own before hooks, body, and after hooks as one unit via runSpecRecovered,
// isolated in its own subtest when possible — see its doc comment (#74, #109). failFast still works
// correctly across that isolation: t.Run blocks until the subtest's goroutine finishes, so ctx.failed
// (set synchronously by recordFailure before any Fatalf, not by recover) is visible here exactly like
// before isolation existed.
func runSpecsRecovered(ctx *Context, g *group) {
	obs := ctx.execObserver
	for i, s := range g.specs {
		ctx.failed = false
		named := obs != nil && i < len(g.names)
		var started report.SpecStartEvent
		if named {
			started = obs.specStarted(g.names[i])
		}
		message, output := runSpecRecovered(ctx, g.before, s, g.after, g.subtestName(i))
		failed := ctx.failed
		if named {
			obs.specFinished(started, specResult{Failed: failed, Message: message, Output: output})
		}
		if ctx.failFast && failed {
			return
		}
	}
}

// runSpecRecovered runs one spec's before hooks, body, and after hooks (see runSpecWithHooks),
// isolated in its own subtest when ctx.backend is a real *testing.T (via runnableBackend) — so a
// Fatalf/Fatal/FailNow inside before or the body (runtime.Goexit) unwinds only that subtest's
// goroutine, not the entire Run/Describe call, letting the remaining specs in this group still run
// and be reported. A fake backend (e.g. controlledBackend in tests, whose Run is a no-op — see
// runIsolatedCase's identical gate) falls back to running the unit directly via runSpecWithHooks,
// same as before this isolation existed; so does a *testing.B — its concrete type is checked directly
// here (not via runnableBackend.Run, which would still allocate a closure per call even though it
// never ends up subtesting) to keep BenchmarkRunner_GoSpecs's existing zero-alloc contract intact.
//
// subtestName is the spec's full Describe breadcrumb (group.subtestName — possibly ""), passed to
// testing.T.Run purely for -v/-run/IDE/test2json identity (#102). It is never read back from
// t.Name(): SpecStartEvent/SpecResultEvent's Name always comes from group.names, so Go's subtest
// sanitization (spaces to "_") and duplicate-name "#01" suffixing never leak into reported identity.
func runSpecRecovered(ctx *Context, before []step, s step, after []step, subtestName string) (message, output string) {
	real, ok := ctx.backend.(*runnableBackend)
	if !ok {
		return runSpecWithHooks(ctx, before, s, after)
	}
	t, ok := real.tb.(*testing.T)
	if !ok {
		return runSpecWithHooks(ctx, before, s, after)
	}
	return runSpecIsolated(ctx, t, subtestName, before, s, after)
}

// runSpecIsolated creates the real subtest and runs before/s/after inside it. Split out from
// runSpecRecovered because the closure below captures named returns by reference: if it lived
// directly in runSpecRecovered, Go's escape analysis would heap-allocate that function's
// message/output for every call — including the *testing.B fast path above, which never reaches this
// line at all — since escape analysis decides a variable's storage class for the whole function, not
// per branch. Keeping the capture inside its own function scopes that heap allocation to the
// isolation path only.
func runSpecIsolated(ctx *Context, t *testing.T, subtestName string, before []step, s step, after []step) (message, output string) {
	t.Run(subtestName, func(subT *testing.T) {
		message, output = runSpecBody(ctx, subT, before, s, after)
	})
	return
}

// runSpecBody runs before/s/after against ctx with ctx.backend/ctx.T/ctx.tb temporarily swapped to
// tb's own backend (tb is this spec's subtest *testing.T, handed in by runSpecRecovered's t.Run) so
// every assertion helper — all of which read c.backend/e.ctx.backend at call time, never cache it —
// fails tb, not the parent. That is what makes the Goexit land in this subtest's goroutine instead of
// the parent's. Restored before returning so the next spec in this group (back in the parent's
// goroutine) sees the parent's backend/T/tb again, exactly as Context.Reset already does for the
// analogous runIsolatedCase case.
//
// ctx.tb must be swapped alongside ctx.backend: it is the only field assertion failure paths use to
// mark themselves as test helpers, so leaving it pointing at the parent (or at nil) sends the
// failure location back to a go-specs frame instead of the user's assertion line.
func runSpecBody(ctx *Context, tb testing.TB, before []step, s step, after []step) (message, output string) {
	subBackend := asTestBackend(tb)
	defer putTestBackend(subBackend)
	prevBackend, prevT, prevTB := ctx.backend, ctx.T, ctx.tb
	ctx.backend = subBackend
	ctx.tb = tb
	if t, ok := tb.(*testing.T); ok {
		ctx.T = t
	}
	defer func() {
		ctx.backend, ctx.T, ctx.tb = prevBackend, prevT, prevTB
	}()
	return runSpecWithHooks(ctx, before, s, after)
}

// runSpecWithHooks runs one spec's before hooks, body, and after hooks as a single unit, converging
// Builder/Runner's hook-execution frequency with the Describe/ExecutionPlan model (#109): before/after
// run exactly once per spec, not once for a whole group of coalesced specs — see runProgram in
// execution_plan.go, which this mirrors.
//
// after runs via a defer registered before before/body ever run, not as a plain statement following
// them: a real *testing.T.Fatal/Fatalf/FailNow inside before or the body calls runtime.Goexit, which
// unwinds this goroutine without ever reaching a following statement — only deferred calls still run.
// A plain "run before/body, then run after" sequence would silently skip after entirely in that case,
// resurrecting the pre-#109 gap where a fatal teardown-relevant failure left resources uncleaned, and
// diverging from execution_plan.go's runProgram, which guarantees after via the same defer technique.
// recover() cannot observe Goexit (see runStepRecovered), so message/output stay "" for a Goexit-based
// failure here exactly as they already do for a body panic — this only fixes after not running, not
// that pre-existing, documented reporting gap.
//
// before and the body are recovered together, as one step passed to runStepRecovered: a panic
// anywhere in before stops the remaining before hooks and the body (later before hooks, and the body
// itself, may depend on setup that never completed), and is recorded with the same "panic: value"
// message a body-only panic gets — this spec's own before is no different, semantically, from more of
// its own body. A panic in one spec's before never touches its siblings: the next spec in g.specs
// gets its own fresh call to the same before hooks. FailFast is checked after every before hook, same
// as between specs, so a non-panic failure with FailFast set also skips the body — but this spec's
// after hooks still run regardless (via the deferred runAfterRecovered below), matching runGroup's
// original "FailFast decides whether we run more, not whether we leave resources uncleaned" contract.
//
// after hooks always run, each recovered individually by runAfterRecovered, so one panicking after
// hook doesn't stop its siblings. message/output follow runProgram's first-write-wins contract: a
// before/body panic's message wins over a later after-hook panic's, since it happened first.
func runSpecWithHooks(ctx *Context, before []step, s step, after []step) (message, output string) {
	defer func() {
		afterMessage, afterOutput := runAfterRecovered(ctx, after)
		if message == "" {
			message, output = afterMessage, afterOutput
		}
	}()
	message, output = runStepRecovered(ctx, func(ctx *Context) {
		for _, b := range before {
			b(ctx)
			if ctx.failFast && ctx.failed {
				return
			}
		}
		s(ctx)
	}, "panic")
	return
}

// runAfterRecovered runs a spec's after hooks in reverse order, recovering each individually.
// Unlike before/specs, this does not check FailFast between hooks: FailFast decides whether we run
// more specs/groups, not whether we leave resources uncleaned. Every after hook always runs. Returns
// the first after-hook panic's message/output (first-write-wins, matching runSpecWithHooks' priority
// of a before/body failure over a later after-hook one) — "" if none panicked.
func runAfterRecovered(ctx *Context, after []step) (message, output string) {
	for i := len(after) - 1; i >= 0; i-- {
		m, o := runStepRecovered(ctx, after[i], "panic in after hook")
		if message == "" {
			message, output = m, o
		}
	}
	return
}

// runStepRecovered runs a single step, recovering a panic so it fails just this step (recorded via
// ctx.recordFailure + ctx.backend.Errorf with message and stack trace) instead of crashing the
// process. label distinguishes a before/spec panic from an after-hook panic in the reported message.
//
// On a recovered panic, message and output are built exactly once here — message is the short
// "label: value" summary, output is the raw stack trace — and reused both for ctx.backend.Errorf
// (unchanged wire format: "message\noutput") and as the return value runSpecsRecovered feeds into
// specFinished's specResult. They are never reconstructed anywhere else. Both are zero-value ("")
// when s(ctx) returns normally, including via runtime.Goexit (a real testing.T.Fatalf/FailNow) —
// recover() cannot observe that case, so it is indistinguishable here from a spec that never failed.
func runStepRecovered(ctx *Context, s step, label string) (message, output string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			ctx.recordFailure()
			message = fmt.Sprintf("%s: %v", label, recovered)
			output = string(debug.Stack())
			ctx.backend.Errorf("%s\n%s", message, output)
		}
	}()
	s(ctx)
	return
}
