// program.go defines the compiled execution graph: groups of (before, specs, after) that share a
// before/after slice at compile time. No reflection; minimal layout. Large suites (100k+ specs)
// share before/after slices per group to reduce memory — but every spec still runs its group's
// before/after once for itself at execution time (see runner.go's runSpecWithHooks, #109); sharing
// the slice is a memory optimization, not a change in how often the hooks run.
package specs

import (
	"runtime/debug"
	"sync"
	"time"

	"github.com/getsyntegrity/go-specs/report"
)

// step is a single executable step (hook or spec body). Same signature as RunSpec.Fn.
type step func(*Context)

// group is one execution unit: specs that declared the same before/after hooks (same Describe scope
// layout), compiled together so their before/after slices can be shared. Each spec still runs its
// own before, its own body, then its own after (reverse order) — see runner.go's runSpecWithHooks —
// so sharing the slices is purely a compile-time memory/locality optimization, not a change in
// execution frequency (#109). hookKey is the builder's scope key for coalescing; not used by the
// runner.
//
// fullNames parallels names, holding each spec's Describe breadcrumb (see Builder.fullName). It is
// used only as the spec's testing.T.Run identity (#102) — never as reported identity, which always
// comes from names — and, like names, is nil for a parallelStep group.
//
// names holds one entry per group.specs entry (SpecStartEvent.Name), or is left nil for a group
// whose single spec is a parallelStep closure: that closure reports each real spec itself (see
// parallelStep), so Runner.Run's sequential per-spec reporting must not also wrap it.
//
// skipped holds the names of compile-time-skipped specs (SkipIt/Skip) that finalize attached to
// this group — see builder.go's finalize for why a skip can't have its own group. They carry no
// before/spec/after of their own and never touch this group's before/after; Runner reports them
// (SpecStarted+SpecFinished{Skipped:true}, no body run) independently of whether this group's real
// specs run at all.
//
// pendingSpecs is the same mechanism for compile-time-pending specs (PendingIt/Pending, #208):
// buffered by finalize the same way skipped is, and reported by Runner as
// SpecStarted+SpecFinished{Pending:true} — distinct from Skipped, since "not implemented yet" is a
// different fact from "intentionally not executed". pendingSpecScopeNames parallels it, same as
// skippedScopeNames parallels skipped.
//
// scopeNames parallels names, holding each spec's raw (unjoined) enclosing Describe names —
// outermost first, captured by Builder at registration time (see specItem.scopeNames) — for
// SpecStartEvent.Path (see specPath). Like names, it is left nil for a parallelStep group.
// skippedScopeNames is the same, parallel to skipped.
type group struct {
	before                []step
	specs                 []step
	names                 []string
	fullNames             []string
	scopeNames            [][]string
	after                 []step
	hookKey               string
	skipped               []string
	skippedScopeNames     [][]string
	pendingSpecs          []string
	pendingSpecScopeNames [][]string
}

// specPath returns g.specs[i]'s SpecStartEvent.Path: its declared enclosing scope names (outermost
// first) followed by its own name — mirroring the ExecutionPlan model's specEventPath. The result is
// a fresh slice, never a window into g.scopeNames, matching Path's "freshly allocated, safe to
// retain or modify" contract. Returns nil when i is out of range for g.names (an unnamed spec, or a
// parallelStep group, whose real specs report their own path — see parallelStep).
func (g *group) specPath(i int) []string {
	if i < 0 || i >= len(g.names) {
		return nil
	}
	var scopes []string
	if i < len(g.scopeNames) {
		scopes = g.scopeNames[i]
	}
	path := make([]string, 0, len(scopes)+1)
	path = append(path, scopes...)
	return append(path, g.names[i])
}

// skippedPath is specPath for g.skipped[i] (a compile-time-skipped spec), same shape and same
// freshly-allocated contract.
func (g *group) skippedPath(i int) []string {
	if i < 0 || i >= len(g.skipped) {
		return nil
	}
	var scopes []string
	if i < len(g.skippedScopeNames) {
		scopes = g.skippedScopeNames[i]
	}
	path := make([]string, 0, len(scopes)+1)
	path = append(path, scopes...)
	return append(path, g.skipped[i])
}

// pendingPath is specPath for g.pendingSpecs[i] (a compile-time-pending spec), same shape and same
// freshly-allocated contract as skippedPath.
func (g *group) pendingPath(i int) []string {
	if i < 0 || i >= len(g.pendingSpecs) {
		return nil
	}
	var scopes []string
	if i < len(g.pendingSpecScopeNames) {
		scopes = g.pendingSpecScopeNames[i]
	}
	path := make([]string, 0, len(scopes)+1)
	path = append(path, scopes...)
	return append(path, g.pendingSpecs[i])
}

// subtestName returns the Go subtest identity for g.specs[i]: its full Describe breadcrumb, falling
// back to the leaf name for a group built without breadcrumbs (a hand-built group in a test, or a
// spec declared outside any Describe, where the leaf name already is the whole breadcrumb).
func (g *group) subtestName(i int) string {
	if full := specName(g.fullNames, i); full != "" {
		return full
	}
	return specName(g.names, i)
}

// specName returns names[i], or "" when names doesn't cover index i (an unnamed spec, e.g. from
// It("", fn), or a group that reports its own specs internally — see group.names).
func specName(names []string, i int) string {
	if i < 0 || i >= len(names) {
		return ""
	}
	return names[i]
}

// specResult is the outcome of one spec's execution, threaded from the places that can classify it
// (runSpecsRecovered's runStepRecovered call, parallelStep's per-goroutine recover, and the
// Describe/ExecutionPlan and Spec.ItParallel real-subtest paths in execution_plan.go/group_hooks.go)
// through to specExecutionObserver.specFinished. Output is populated only where a real, distinct
// source exists today — a recovered panic (its stack trace) — and stays empty otherwise, including
// for an ItParallel/parallelBackend failure, which has no separable output source.
//
// Message is populated for a recovered panic (the panic value), an ItParallel/parallelBackend
// failure (the recorded string), and, since issue #272, an ordinary built-in ctx.Expect/Context.Snapshot
// assertion failure too: Context.failf (failure.go) records its formatted text on the Context before
// calling backend.Fatalf, so a real *testing.T's Fatalf ending the goroutine with runtime.Goexit no
// longer loses it — the caller falls back to that recorded text (Context.assertionMessage) exactly
// when nothing was recovered. Message legitimately stays empty for a spec that failed only through
// ctx.T directly (Error, Fatal, Fail, FailNow, or a Cleanup, #253): failf is never on that path, so
// nothing was ever recorded to fall back to; see report/events.go's SpecResultEvent.Message doc.
//
// Filtered is true when external test selection (e.g. `go test -run` pattern) discarded the spec's
// subtest before its body ran, threaded from runSpecIsolated/runSpecProgramIsolated — see their doc
// comments for why this can't be read from testing.T.Run's own bool return. Message/Output stay
// empty for it, same as when nothing failed: nothing ran to produce either.
//
// Skipped is true when the spec's own subtest skipped at runtime (ctx.T.Skip, Skipf or SkipNow) and
// the spec did not also fail — a failure followed by SkipNow stays Failed, matching go test itself
// (#254). It is distinct from the suite's compile-time SkipIt/Skip marks, which never reach this
// struct at all: they carry no before/body/after and are reported directly (specSkipped/reportMarks)
// without ever running, so there is no specResult to build for them.
//
// A spec FailFast prevented from ever being reached (issue #274) never reaches this struct either,
// for the same reason: nothing ran to classify. Both engines report it directly —
// specExecutionObserver.specUnstarted (Builder/Runner) and reportSpecUnstarted (CompiledSuite,
// execution_plan.go) — bypassing specResult/specFinished entirely, exactly as
// specSkipped/specPending/reportMarks already do for a compile-time mark.
type specResult struct {
	Failed   bool
	Filtered bool
	Skipped  bool
	Message  string
	Output   string
}

// specExecutionObserver receives per-spec Started/Finished notifications from execution points
// that only have a *Context to work with, not a *Runner reference — namely a parallelStep
// goroutine, compiled into the Program before any Runner or Reporter exists. Runner.Run installs
// one on Context only when it has a report.EventReporter; nil otherwise, so a parallel group's
// execution is unaffected without one.
type specExecutionObserver interface {
	// path is the spec's SpecStartEvent.Path (declared scope names, outermost first, then name), or
	// nil for a group built without breadcrumbs — see group.specPath.
	specStarted(name string, path []string) report.SpecStartEvent
	specFinished(start report.SpecStartEvent, result specResult)
	// specReported reports one already-finished spec: SpecStarted immediately followed by
	// SpecFinished, with the spec's own recorded duration instead of one measured at call time.
	// parallelStep uses it to emit a batch in declaration order once every body has completed.
	specReported(start report.SpecStartEvent, duration time.Duration, result specResult)
	// specSkipped reports one compile-time-skipped spec (SkipIt/Skip): a single SpecStarted +
	// SpecFinished{Skipped: true} pair, with no body ever run. Only Runner.Run's sequential group
	// execution calls this (see runner.go's reportSkipped) — ItParallel/parallelStep has no skip
	// concept, so it never needs it.
	specSkipped(name string, path []string)
	// specPending reports one compile-time-pending spec (PendingIt/Pending, #208): a single
	// SpecStarted + SpecFinished{Pending: true} pair, with no body ever run — mirroring
	// specSkipped, but distinct from it (see runner.go's reportPending). Only Runner.Run's
	// sequential group execution calls this, same as specSkipped.
	specPending(name string, path []string)
	// specUnstarted reports one spec fail-fast prevented from ever running (issue #274): a single
	// SpecStarted + SpecFinished{Unstarted: true} pair, with no body ever run — mirroring
	// specSkipped/specPending, but distinct from both: Skipped/Pending are the suite's own
	// deliberate compile-time decisions, Unstarted means FailFast stopped the run before this spec
	// (or its enclosing group) was ever reached. declared preserves the spec's original
	// SkipIt/PendingIt declaration (report.DeclaredSkip/report.DeclaredPending) when the unreached
	// spec was itself a compile-time mark, or report.DeclaredNone for an ordinary spec. Only
	// runner.go's reportGroupsUnstarted/reportSpecsUnstartedFrom call this.
	specUnstarted(name string, path []string, declared report.DeclaredKind)
}

// Program is a compiled execution program. Groups run in order; within a group, every spec runs its
// own before, body, and after (reverse order) — see runSpecWithHooks. Built by Builder; executed by
// Runner.
type Program struct {
	Groups []group
	// FocusedNames holds the full Describe breadcrumb of every focused (FIt/Focus) spec this
	// program's Builder kept (issue #273): empty unless the suite has at least one. Used by
	// Runner.Run's fail-on-focus check (reportFocusPolicy) for its diagnostic; RunShard propagates
	// it to every shard's own Program unchanged, so each shard's own enclosing test still fails when
	// focus is active, even one that draws none of the focused group's specs.
	FocusedNames []string
	// FocusExcludedCount is the total number of specs this program's Builder dropped because of an
	// active focus (issue #273) — every non-focus It/SkipIt/PendingIt/ItParallel item in the same
	// Builder.Describe/top-level call. Kept apart from len(FocusExcluded) because RunShard only
	// propagates the detailed marks (below) to shard 0, but every shard's diagnostic still needs the
	// suite-wide total.
	FocusExcludedCount int
	// FocusExcluded holds the identity of each spec FocusExcludedCount counts, for Filtered
	// reporting. Reported exactly once — by Run, or by RunShard's shard 0 (see scheduler.go's
	// shardProgram) — never nil'd out for a later shard's Program, only left empty: an excluded spec
	// never became part of any group to begin with (see builder.go's finalize), so there is nothing
	// shard-specific about it to select.
	FocusExcluded []specMark
}

// runAll returns a single step that runs the given steps in order. Used to wrap one spec's
// full sequence (beforeEach+fn+afterEach) for parallelStep.
func runAll(steps []step) step {
	return func(ctx *Context) {
		for _, s := range steps {
			s(ctx)
		}
	}
}

// parallelTiming is one parallelStep goroutine's own start time and duration, buffered until the
// whole batch has finished so the calling goroutine can report it in declaration order.
type parallelTiming struct {
	start    time.Time
	duration time.Duration
}

// parallelStep returns a single step that runs all steps in parallel (each in its own goroutine).
// Used by the builder to compile ItParallel groups. Allocations (WaitGroup, goroutines) happen
// inside the step, not in the runner loop.
//
// Each goroutine runs its own *Context, pulled from contextPool and backed by a parallelBackend
// (the same type RunParallel's worker pool uses) with abortOnFatal set, instead of sharing ctx:
// Context's failure record and the underlying *testing.T are not safe for concurrent access, and
// testing.T.FailNow (used by Fatal/Fatalf) must only be called from the goroutine running the
// test. Once every goroutine has finished, failures are replayed on ctx from the calling
// goroutine, so Fatalf/FailFast still happen on the right goroutine.
//
// abortOnFatal makes a fatal assertion (Fatal/Fatalf/FailNow) panic(parallelAbort{}) after
// recording, so — like the real testing.T.FailNow it replaces — it stops the rest of the current
// spec's before/fn/after sequence (runAll) instead of silently continuing into code that assumed
// the spec had already stopped. The deferred recover below treats that sentinel as an expected,
// already-recorded stop, not a failure to report; any other panic (e.g. from the nil ctx.T below)
// is recorded as an ordinary spec failure instead of crashing the process. Pool cleanup always
// runs via defer, panic or not.
//
// A parallel group always runs every one of its specs to completion before this step returns
// (wg.Wait() below) — FailFast only takes effect at the next group boundary in Runner.Run, since
// the whole parallel group is compiled as a single opaque step; it cannot cancel sibling specs
// mid-group. See TestParallelStep_FailFastRunsAllSpecsInGroup.
//
// parallelBackend cannot safely expose a live *testing.T (doing so would let a spec body call
// t.Fatalf from the wrong goroutine, reintroducing the race this fixes), so child.T is nil inside
// an ItParallel body; use ctx.Expect(...) instead of ctx.T directly.
//
// names holds one entry per steps entry (its ItParallel name); when the caller's Context has an
// execObserver (i.e. Runner.Run has a Reporter), each spec is reported individually instead of the
// group being one opaque unit. Each goroutine only captures its own outcome (start time, duration,
// result — classified after nil/parallelAbort{}/a real panic) and never touches the reporter; once
// wg.Wait returns, the calling goroutine emits SpecStarted then SpecFinished per spec in declaration
// order (#315), mirroring Spec's runParallelGroup, so the report order is stable however the bodies
// happen to complete. Each event carries its spec's own start time and duration.
// Failed is that spec's own result, not the group's aggregate failure record; a recovered panic also
// carries its stack trace in Output, so it is classified as an error, not a failure (#314).
// obs is read once from ctx before any goroutine starts; obs's own methods serialize the actual
// report.EventReporter calls, since not every EventReporter implementation is concurrency-safe.
//
// scopeNames parallels names and steps, holding each spec's declared enclosing Describe names (see
// group.scopeNames) for SpecStartEvent.Path; an out-of-range or nil entry reports a nil path, same
// as an unnamed spec reports an empty name.
func parallelStep(steps []step, names []string, scopeNames [][]string) step {
	return func(ctx *Context) {
		if len(steps) == 0 {
			return
		}
		obs := ctx.execObserver
		results := make([]failureRecord, len(steps))
		outputs := make([]string, len(steps)) // a recovered panic's stack trace, by spec index
		var timings []parallelTiming          // each spec's own start/duration; only with an observer
		if obs != nil {
			timings = make([]parallelTiming, len(steps))
		}
		var wg sync.WaitGroup
		for i, s := range steps {
			i, s := i, s
			wg.Add(1)
			go func() {
				defer wg.Done()
				backend := &parallelBackend{specIndex: i, results: &results, abortOnFatal: true}
				child := acquireContext(backend)
				var startTime time.Time
				if obs != nil {
					startTime = time.Now()
				}
				defer func() {
					// Recording rule shared with the worker-pool engines — see panic_report.go. Only
					// the reporting and release below are specific to this path.
					alreadyFailed := results[i].Failed
					recoverParallelSpecFailure(recover(), &results, i)
					if !alreadyFailed && results[i].Failed {
						// Only a real panic can flip Failed inside this defer. Its stack trace in
						// Output is what report classifies as Error rather than Failed (#314), like
						// the sequential engines' recoverSpecFailure.
						outputs[i] = string(debug.Stack())
					}
					// ctx.Go tasks are awaited before the spec is reported or its Context released; a
					// task panic fails the spec only if nothing failed first (#318).
					if out := settleParallelTasks(child, &results, i); out != "" {
						outputs[i] = out
					}
					if obs != nil {
						timings[i] = parallelTiming{start: startTime, duration: time.Since(startTime)}
					}
					releaseContext(child)
				}()
				s(child)
			}()
		}
		wg.Wait()
		if obs != nil {
			for i := range steps {
				var scopes []string
				if i < len(scopeNames) {
					scopes = scopeNames[i]
				}
				name := specName(names, i)
				path := make([]string, 0, len(scopes)+1)
				path = append(path, scopes...)
				path = append(path, name)
				obs.specReported(
					report.SpecStartEvent{Name: name, Path: path, Time: timings[i].start},
					timings[i].duration,
					specResult{Failed: results[i].Failed, Message: results[i].Message, Output: outputs[i]},
				)
			}
		}
		for _, r := range results {
			if r.Failed {
				ctx.recordFailure()
				break
			}
		}
		reportFailures(ctx.backend, results)
	}
}
