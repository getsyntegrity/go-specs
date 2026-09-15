// program.go defines the compiled execution graph: groups of (before, specs, after) for hook reuse.
// No reflection; minimal layout. Large suites (100k+ specs) share before/after slices per group.
package specs

import (
	"fmt"
	"sync"

	"github.com/pablogore/go-specs/report"
)

// step is a single executable step (hook or spec body). Same signature as RunSpec.Fn.
type step func(*Context)

// group is one execution unit: run before once, then all specs, then after once (reverse order).
// Before/after slices are shared across all specs in the group to reduce memory and improve locality.
// hookKey is the builder's scope key for coalescing; not used by the runner.
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
// scopeNames parallels names, holding each spec's raw (unjoined) enclosing Describe names —
// outermost first, captured by Builder at registration time (see specItem.scopeNames) — for
// SpecStartEvent.Path (see specPath). Like names, it is left nil for a parallelStep group.
// skippedScopeNames is the same, parallel to skipped.
type group struct {
	before            []step
	specs             []step
	names             []string
	fullNames         []string
	scopeNames        [][]string
	after             []step
	hookKey           string
	skipped           []string
	skippedScopeNames [][]string
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

// specResult is the outcome of one spec's execution, threaded from the two places that can
// classify it (runSpecsRecovered's runStepRecovered call and parallelStep's per-goroutine recover)
// through to specExecutionObserver.specFinished. Message/Output are populated only where a real,
// distinct source exists today — a recovered panic (Message: the panic value, Output: its stack
// trace) or an ItParallel/parallelBackend failure (Message: the recorded string, Output: left
// empty — parallelBackend has no separable output source). An ordinary Fatalf-based sequential
// assertion failure never reaches specFinished at all (runtime.Goexit unwinds the whole Run call
// before returning here), so Message/Output stay empty for that case too; see runStepRecovered.
//
// Filtered is true when external test selection (e.g. `go test -run`) discarded the spec's subtest
// before its body ran, threaded from runSpecIsolated/runSpecProgramIsolated — see their doc
// comments for why this can't be read from testing.T.Run's own bool return. Message/Output stay
// empty for it, same as when nothing failed: nothing ran to produce either.
type specResult struct {
	Failed   bool
	Filtered bool
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
	// specSkipped reports one compile-time-skipped spec (SkipIt/Skip): a single SpecStarted +
	// SpecFinished{Skipped: true} pair, with no body ever run. Only Runner.Run's sequential group
	// execution calls this (see runner.go's reportSkipped) — ItParallel/parallelStep has no skip
	// concept, so it never needs it.
	specSkipped(name string, path []string)
}

// Program is a compiled execution program. Groups run in order; within a group: before once, all specs, after once (reverse).
// Built by Builder; executed by Runner.
type Program struct {
	Groups []group
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

// parallelStep returns a single step that runs all steps in parallel (each in its own goroutine).
// Used by the builder to compile ItParallel groups. Allocations (WaitGroup, goroutines) happen
// inside the step, not in the runner loop.
//
// Each goroutine runs its own *Context, pulled from contextPool and backed by a parallelBackend
// (the same type RunParallel's worker pool uses) with abortOnFatal set, instead of sharing ctx:
// Context.failed and the underlying *testing.T are not safe for concurrent access, and
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
// execObserver (i.e. Runner.Run has a Reporter), each goroutine reports its own spec directly —
// SpecStarted right before running it, SpecFinished once results[i] is known (after classifying
// nil/parallelAbort{}/a real panic) — instead of the group being reported as a single opaque unit.
// Failed is that spec's own result, not the group's aggregate ctx.failed. Events from different
// goroutines may interleave in any order; only started-before-finished is guaranteed per spec.
// obs is read once from ctx before any goroutine starts, then only read (never mutated) by them,
// so no synchronization is needed for the pointer itself; obs's own methods serialize the actual
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
		pathValues := ctx.Path()
		obs := ctx.execObserver
		results := make([]parallelFailure, len(steps))
		var wg sync.WaitGroup
		for i, s := range steps {
			i, s := i, s
			name := specName(names, i)
			wg.Add(1)
			go func() {
				defer wg.Done()
				backend := &parallelBackend{specIndex: i, results: &results, abortOnFatal: true}
				child, release := acquireContext(backend)
				child.SetPathValues(pathValues)
				var started report.SpecStartEvent
				if obs != nil {
					var scopes []string
					if i < len(scopeNames) {
						scopes = scopeNames[i]
					}
					path := make([]string, 0, len(scopes)+1)
					path = append(path, scopes...)
					path = append(path, name)
					started = obs.specStarted(name, path)
				}
				defer func() {
					switch r := recover(); r {
					case nil:
						// spec ran to completion (or a Fatal/Fatalf/FailNow already returned
						// normally via a different path — not reachable with abortOnFatal, kept
						// for clarity).
					case parallelAbort{}:
						// expected stop: Fatal/Fatalf/FailNow already recorded results[i].
					default:
						if results[i].Message == "" {
							results[i] = parallelFailure{Message: fmt.Sprintf("panic: %v", r)}
						}
					}
					if obs != nil {
						obs.specFinished(started, specResult{Failed: results[i].Message != "", Message: results[i].Message})
					}
					release()
				}()
				s(child)
			}()
		}
		wg.Wait()
		for _, r := range results {
			if r.Message != "" {
				ctx.recordFailure()
				break
			}
		}
		reportFailures(ctx.backend, results)
	}
}
