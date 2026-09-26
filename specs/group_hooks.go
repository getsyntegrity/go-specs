// group_hooks.go runs once-per-group BeforeAll/AfterAll hooks for the canonical Describe engine
// (issue #207). docs/SUITE_HOOKS_CONTRACT.md is the normative contract (H1-H10); this file is its
// implementation for ExecutionPlan/CompiledSuite.
//
// # How a hooked suite runs
//
// A suite without group hooks never reaches this file: CompiledSuite.runSpecs sends it through
// runPlanSpecsInOrder, one flat subtest per spec, exactly as before the feature (H10).
//
// A suite with group hooks is walked as the tree its scopes declare. Against a real *testing.T,
// every hooked group gets a real Go subtest of its own, and its specs and nested groups run inside
// it. The subtest names compose to exactly the flat name each spec had before — the group
// "suite/when cart has items" contains the spec "charges the card", so the spec's full name is
// still TestX/suite/when_cart_has_items/charges_the_card — so `go test -run` patterns and the #102
// breadcrumb guarantees are unchanged. validateHookGroups rejects, while the suite is built, every
// hooked group whose name could not keep that promise.
//
// The group subtest's own goroutine runs the whole group, in order (runGroupBody):
//
//  1. its BeforeAlls, then
//  2. its specs and nested groups, then
//  3. its AfterAlls — registered as one defer per hook before the first BeforeAll runs, so they run
//     even when a BeforeAll, a child or an earlier AfterAll ends the goroutine with runtime.Goexit
//     (ctx.Expect failures, Fatal, FailNow, SkipNow) (H5/H6).
//
// Hooks are not subtests and never run on a goroutine of their own: ctx.T is the group subtest's
// *testing.T, and every call on it is made from that test's own goroutine, the only one Go supports
// Fatal/FailNow/SkipNow/Parallel from. ctx.T.Cleanup, TempDir and Setenv registered in a BeforeAll
// therefore live until the group subtest ends — after its AfterAll, before the next sibling group.
// A hook failure is reported as the synthetic [BeforeAll]/[AfterAll] case and marks the group
// subtest failed, so `go test` attributes it to the group.
//
// The group belongs to its subtest: whenever testing selects the group subtest, its hooks run, even
// if -run/-skip then selects none of its specs; a group subtest that testing filters out runs none
// of them (H3).
//
// Every other backend — a fake backend in a unit test, or a *testing.B — has no subtests to open,
// and runs the same tree walk directly, with the same H1-H7 semantics.
package specs

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/report"
)

// planGroups is a compiled suite's once-per-group hook bookkeeping (issue #207,
// docs/SUITE_HOOKS_CONTRACT.md). It is deliberately not a field of ExecutionPlan: ExecutionPlan is
// allocated for every suite and is exactly 192 bytes, the top of its allocation size class, so even
// one extra pointer there would cost every suite 16 bytes it never asked for (H10). It hangs off
// CompiledSuite instead, behind one pointer that stays nil — nothing behind it is allocated — for a
// suite that registers neither BeforeAll nor AfterAll anywhere. group_hook_cost_test.go pins both
// sizes.
type planGroups struct {
	// groups holds one entry per Describe/When (or root) scope that registered at least one
	// BeforeAll/AfterAll hook and has at least one spec in its subtree, in the order the scopes
	// closed (innermost first).
	groups []hookGroup
	// skipped/pending hold this suite's compile-time SkipIt/PendingIt registrations (issue #245),
	// in declaration order. They live here — behind the same lazily allocated pointer as groups —
	// rather than as new ExecutionPlan/CompiledSuite fields, so a suite that registers none of
	// SkipIt/PendingIt/BeforeAll/AfterAll still allocates nothing for any of them (H10). Unlike a
	// hookGroup, a mark is not tied to a spec-index range: it never compiles into a plan entry at
	// all (no before/body/after, no subtest), so it is reported once per suite run, independently
	// of which group (if any) it was declared inside — see CompiledSuite.runSpecs.
	skipped []specMark
	pending []specMark
	// parallel holds this suite's ItParallel groups (issue #245's second spec): maximal runs of
	// consecutive ItParallel specs, each compiled into the plan exactly like an ordinary It (see
	// buildExecutionPlanFromArenaRec / bytecodeCompiler.EmitItParallel) but recorded here, behind the
	// same lazily allocated pointer, so runPlanWithGroups can find and launch them concurrently. A
	// range never straddles a hookGroup's Start/End: both compile paths end an open run at every
	// Describe/When scope transition, which is stricter than the documented rule ("ends at a
	// BeforeAll/AfterAll group boundary") but never merges two runs the documented rule would have
	// kept apart either — see the compiler/arena doc comments for why the stronger rule is safe.
	parallel []parallelRange
}

// parallelRange is one ItParallel group (issue #245): a maximal run of consecutive ItParallel specs,
// identified only by the plan index range it occupies. Unlike hookGroup it carries no Path/Name of
// its own — each of its specs already has its own declared Name/Path in the plan (appendSpecPath) —
// and it never nests: every index in [Start,End] is a leaf spec, so a parallelRange can sit inside
// exactly one hookGroup, or be its sibling, but never contain one.
type parallelRange struct {
	Start, End int
}

// registerParallelGroup attaches one ItParallel run to *pg, allocating *pg on first use — the same
// lazy-allocation rule registerHookGroup/registerSkipMark already use (H10). startIdx > endIdx is
// impossible for a real run (see the two call sites) but guarded the same defensive way
// registerHookGroup is.
func registerParallelGroup(pg **planGroups, startIdx, endIdx int) {
	if startIdx < 0 || startIdx > endIdx {
		return
	}
	if *pg == nil {
		*pg = &planGroups{}
	}
	(*pg).parallel = append((*pg).parallel, parallelRange{Start: startIdx, End: endIdx})
}

// specMark is one compile-time-only spec registration (SkipIt/PendingIt, issue #245): it never
// compiles into a runnable plan entry, so only its name and enclosing scopes are kept, the same
// information Builder.finalize buffers for its own kindSkip/kindPending specItem (builder.go).
type specMark struct {
	name string
	path []string // enclosing scopes, own name not included — matches ExecutionPlan.PathScopes' convention
}

// registerSkipMark buffers one compile-time SkipIt registration into *pg (issue #245), allocating
// *pg on first use — the same lazy-allocation rule registerHookGroup already uses (H10).
func registerSkipMark(pg **planGroups, name string, scopes []string) {
	if *pg == nil {
		*pg = &planGroups{}
	}
	(*pg).skipped = append((*pg).skipped, specMark{name: name, path: append([]string(nil), scopes...)})
}

// registerPendingMark is registerSkipMark for PendingIt.
func registerPendingMark(pg **planGroups, name string, scopes []string) {
	if *pg == nil {
		*pg = &planGroups{}
	}
	(*pg).pending = append((*pg).pending, specMark{name: name, path: append([]string(nil), scopes...)})
}

// reportMarks emits SpecStarted immediately followed by SpecFinished{Skipped: true} (or
// {Pending: true}) for every buffered mark, mirroring runner.go's reportSkipped/reportPending for
// the Builder engine exactly: a mark carries no before/body/after and never opens a subtest, only
// its identity is reported — and only when a Reporter is attached, the same "nothing to report
// without one" rule every other compile-time-only report in this package already follows (see
// reportGroupSuppressedSpec).
func reportMarks(rep report.EventReporter, marks []specMark, pending bool) {
	if rep == nil {
		return
	}
	for _, m := range marks {
		path := append(append([]string(nil), m.path...), m.name)
		started := report.SpecStartEvent{Name: m.name, Path: path, Time: time.Now()}
		rep.SpecStarted(started)
		rep.SpecFinished(report.SpecResultEvent{SpecStartEvent: started, Skipped: !pending, Pending: pending})
	}
}

// hookGroup is one Describe/When (or root) scope's once-per-group hooks. Path is the declared
// scope chain including the group's own name — the Path its synthetic hook case reports (see
// reportHookCase). Start and End are the plan indices of the first and last spec in the scope's
// subtree; scopes nest, so two groups' ranges are either disjoint or one contains the other.
//
// Name is the group's own declared name, kept separately from Path because the Analyze/registry
// path does not record an empty name in Path; validateHookGroups needs it to reject one.
type hookGroup struct {
	Path       []string
	Name       string
	BeforeAll  []func(*Context)
	AfterAll   []func(*Context)
	Start, End int
}

// registerHookGroup attaches one scope's own BeforeAll/AfterAll hooks to *pg, covering specs
// startIdx..endIdx, allocating *pg on first use — the only point group-hook storage is ever
// allocated (H10). Both compile paths (bytecodeCompiler.closeGroupHooksAtTop and
// buildExecutionPlanFromArenaRec) call this at the exact point they close a Describe/When/root
// scope, mirroring how BeforeEach/AfterEach are attributed but never flattened onto child specs
// (H1).
//
// startIdx > endIdx means the scope's subtree contributed zero specs to the plan — H3's "a group
// with zero runnable specs is never entered": nothing is registered, so the hooks are silently
// discarded along with the scope itself.
func registerHookGroup(pg **planGroups, path []string, name string, before, after []func(*Context), startIdx, endIdx int) {
	if pg == nil || startIdx < 0 || startIdx > endIdx {
		return
	}
	if len(before) == 0 && len(after) == 0 {
		return
	}
	if *pg == nil {
		*pg = &planGroups{}
	}
	(*pg).groups = append((*pg).groups, hookGroup{
		Path:      append([]string(nil), path...),
		Name:      name,
		BeforeAll: before,
		AfterAll:  after,
		Start:     startIdx,
		End:       endIdx,
	})
}

// remapHookGroups rebuilds pg's Start/End spec-index ranges after bytecodeCompiler.applyFocusFilter
// removed some plan entries (issue #245): oldToNew[i] is the new index of old plan index i, or -1
// when i was dropped. A group whose range contains no surviving index had zero runnable specs left
// after focus filtering and is dropped entirely — extending H3's existing rule ("a group with zero
// runnable specs is never entered", today decided by "does this scope declare at least one It") to
// specs a focus filter removed instead of specs that were never declared. The arena/registry build
// path never needs this: it pre-scans for a focus before emitting anything (see arenaHasFocus), so
// a filtered-out spec simply never contributes to a group's Start/End in the first place.
//
// pg.parallel is remapped the same way and for the same reason: every ItParallel spec is unfocused
// by construction (there is no FItParallel), so a suite-wide focus drops every one of them, and any
// parallel range they formed must be dropped along with them rather than left pointing at plan
// indices that no longer exist.
func remapHookGroups(pg *planGroups, oldToNew []int) *planGroups {
	if pg == nil {
		return nil
	}
	remapRange := func(start, end int) (newStart, newEnd int, ok bool) {
		newStart, newEnd = -1, -1
		for old := start; old <= end; old++ {
			if old < 0 || old >= len(oldToNew) {
				continue
			}
			if nu := oldToNew[old]; nu >= 0 {
				if newStart == -1 {
					newStart = nu
				}
				newEnd = nu
			}
		}
		return newStart, newEnd, newStart != -1
	}
	var kept []hookGroup
	for _, g := range pg.groups {
		newStart, newEnd, ok := remapRange(g.Start, g.End)
		if !ok {
			continue
		}
		g.Start, g.End = newStart, newEnd
		kept = append(kept, g)
	}
	pg.groups = kept
	var keptParallel []parallelRange
	for _, p := range pg.parallel {
		newStart, newEnd, ok := remapRange(p.Start, p.End)
		if !ok {
			continue
		}
		keptParallel = append(keptParallel, parallelRange{Start: newStart, End: newEnd})
	}
	pg.parallel = keptParallel
	if len(pg.groups) == 0 && len(pg.skipped) == 0 && len(pg.pending) == 0 && len(pg.parallel) == 0 {
		return nil
	}
	return pg
}

// groupRun is the state of one hooked CompiledSuite.Run.
type groupRun struct {
	backend testBackend
	rep     report.EventReporter
	plan    *ExecutionPlan
	pg      *planGroups
	// children[g] are group g's directly nested groups, ordered by Start; top are the groups nested
	// in no other group.
	children [][]int
	top      []int
	// parallelByStart maps an ItParallel range's Start index (issue #245) to its index in
	// pg.parallel, built once by buildTree so runRange can recognize the start of a parallel run in
	// O(1) at each step of its flat index walk, the same way it already recognizes a hookGroup start
	// via children. nil when the suite registers no ItParallel.
	parallelByStart map[int]int
	// real is true when backend wraps a real *testing.T, i.e. when groups get subtests.
	real bool
	// stopped is set when the run must stop because a spec body or a hook called the unsupported
	// ctx.T.Parallel(); every enclosing group subtest then unwinds too (see stopIfStopped), running
	// its AfterAlls on the way out, and no further spec runs. It is atomic because a parked subtest
	// resumes on its own goroutine after the stop was set.
	stopped atomic.Bool
}

// runPlanWithGroups is runPlanSpecsInOrder for a suite that registered at least one group hook.
func runPlanWithGroups(backend testBackend, rep report.EventReporter, plan *ExecutionPlan, pg *planGroups) {
	r := &groupRun{backend: backend, rep: rep, plan: plan, pg: pg}
	r.buildTree()
	var topT *testing.T
	if rb, ok := backend.(*runnableBackend); ok {
		topT, r.real = rb.tb.(*testing.T)
	}
	r.runRange(topT, "", 0, len(plan.ProgramStart)-1, r.top)
}

// buildTree derives each group's directly nested groups from the spec ranges. Sorting by Start,
// then by End descending, puts every group right after the groups that contain it; for two groups
// covering the same range the one with the shorter Path is the outer one.
func (r *groupRun) buildTree() {
	groups := r.pg.groups
	r.children = make([][]int, len(groups))
	order := make([]int, len(groups))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		ga, gb := &groups[a], &groups[b]
		if ga.Start != gb.Start {
			return ga.Start - gb.Start
		}
		if ga.End != gb.End {
			return gb.End - ga.End
		}
		return len(ga.Path) - len(gb.Path)
	})
	var stack []int
	for _, g := range order {
		for len(stack) > 0 && groups[stack[len(stack)-1]].End < groups[g].Start {
			stack = stack[:len(stack)-1]
		}
		if len(stack) == 0 {
			r.top = append(r.top, g)
		} else {
			parent := stack[len(stack)-1]
			r.children[parent] = append(r.children[parent], g)
		}
		stack = append(stack, g)
	}
	if n := len(r.pg.parallel); n > 0 {
		r.parallelByStart = make(map[int]int, n)
		for i := range r.pg.parallel {
			r.parallelByStart[r.pg.parallel[i].Start] = i
		}
	}
}

// runRange runs specs lo..hi, which all sit in the same scope: children are the hooked groups
// directly inside that scope, t the scope's *testing.T (nil on a non-*testing.T backend) and prefix
// the part of every spec's subtest name that t's own name already carries.
//
// A parallel run (issue #245) is looked up independently of children, via parallelByStart, rather
// than merged into the same ordered list: a parallelRange never straddles a hookGroup's Start/End
// (see planGroups.parallel's doc comment), so it is always either a hook group's direct child range
// or a sibling of one — exactly the same relationship an individual spec index already has to
// children — and the same three-way walk (child group, then parallel run, then single spec) covers
// it without needing to sort the two kinds together.
func (r *groupRun) runRange(t *testing.T, prefix string, lo, hi int, children []int) {
	k := 0
	for i := lo; i <= hi; {
		if k < len(children) && r.pg.groups[children[k]].Start == i {
			g := children[k]
			k++
			r.runGroup(t, prefix, g)
			r.stopIfStopped(t)
			i = r.pg.groups[g].End + 1
			continue
		}
		if pi, ok := r.parallelByStart[i]; ok {
			end := r.pg.parallel[pi].End
			r.runParallelGroup(t, prefix, pi)
			r.stopIfStopped(t)
			i = end + 1
			continue
		}
		r.runSpec(t, prefix, i)
		r.stopIfStopped(t)
		i++
	}
}

// stopIfStopped unwinds t's goroutine once the run has been stopped deeper down, so the stop
// reaches the test that owns the suite exactly as it does without group hooks. The Goexit runs the
// AfterAll defers of every group whose subtest is on the way out (H5).
func (r *groupRun) stopIfStopped(t *testing.T) {
	if r.stopped.Load() && t != nil {
		t.FailNow()
	}
}

// runGroup runs group g inside the scope t: in a subtest of its own against a real *testing.T,
// directly otherwise.
func (r *groupRun) runGroup(t *testing.T, prefix string, g int) {
	group := &r.pg.groups[g]
	if !r.real {
		r.runGroupBody(nil, prefix, g)
		return
	}
	name, groupPrefix := groupSubtestName(prefix, group)
	ran, parked := runSubtestGuardingParallel(t, name, func(gt *testing.T) {
		r.runGroupBody(gt, groupPrefix, g)
	})
	if parked {
		// A hook of this group called the unsupported ctx.T.Parallel() on the group subtest.
		r.stopped.Store(true)
		t.Helper()
		t.Fatalf("%s", unsupportedGroupHookParallelMessage(groupDisplayName(r.pg, g)))
	}
	if !ran {
		// testing filtered the group subtest out: none of its hooks ran (H3), and its specs are
		// reported filtered, exactly as they would be without group hooks (#111).
		for i := group.Start; i <= group.End; i++ {
			started := reportSpecStarted(r.rep, specEventName(r.plan, i), r.reportPath(i))
			reportSpecFinished(r.rep, started, specResult{Filtered: true})
		}
	}
}

// runGroupBody runs group g on the calling goroutine — the group subtest's own, gt, against a real
// *testing.T (nil otherwise): its BeforeAlls, then its children, then its AfterAlls.
//
// The AfterAlls are registered as defers, one per hook and in reverse so they run in registration
// order, before the first BeforeAll starts. A Go defer runs on a normal return and on
// runtime.Goexit alike, and a Goexit raised inside a deferred call still runs the remaining defers,
// so every AfterAll runs however the group ends — a BeforeAll or AfterAll failing through
// ctx.Expect/Fatal/FailNow, a SkipNow, or the stop of an unsupported ctx.T.Parallel() (H5/H6).
func (r *groupRun) runGroupBody(gt *testing.T, prefix string, g int) {
	group := &r.pg.groups[g]
	acc := &afterAllResult{}
	defer r.reportAfterAll(gt, g, acc)
	for k := len(group.AfterAll) - 1; k >= 0; k-- {
		if h := group.AfterAll[k]; h != nil {
			defer r.runAfterAll(gt, h, acc)
		}
	}
	if !r.runBeforeAlls(gt, g) || r.stopped.Load() {
		return
	}
	r.runRange(gt, prefix, group.Start, group.End, r.children[g])
}

// runBeforeAlls runs group g's BeforeAlls in registration order, stopping at the first failure
// (H4), and reports whether the group's children may run.
//
// The outcome is settled in a defer, because a BeforeAll can end the goroutine with
// runtime.Goexit. Detection is exact: the group subtest gt is fresh when its BeforeAll runs — none
// of its children has started — so gt.Failed() turns true only through this group's own hooks,
// which covers a direct ctx.T.Errorf as well as ctx assertions, Fatal and FailNow. A BeforeAll that
// stopped without returning and without failing called SkipNow: the group is skipped, its specs
// are reported skipped, and it is not a failure.
func (r *groupRun) runBeforeAlls(gt *testing.T, g int) (ok bool) {
	group := &r.pg.groups[g]
	var message, output string
	var failed, returned bool
	defer func() {
		switch {
		case failed || (gt != nil && gt.Failed()) || (!returned && (gt == nil || !gt.Skipped())):
			ok = false
			reportHookCase(r.rep, group.Path, hookKindBeforeAll, message, output)
			if gt != nil {
				gt.Errorf("go-specs: BeforeAll failed for group %q; its specs are reported skipped", groupDisplayName(r.pg, g))
			}
			r.reportGroupSkipped(g, fmt.Sprintf("skipped: BeforeAll failed for group %q", groupDisplayName(r.pg, g)))
		case !returned:
			ok = false
			r.reportGroupSkipped(g, fmt.Sprintf("skipped: BeforeAll skipped group %q", groupDisplayName(r.pg, g)))
		}
	}()
	for _, h := range group.BeforeAll {
		if h == nil {
			continue
		}
		m, o, hookFailed := r.runHook(gt, h)
		if hookFailed {
			failed, message, output = true, m, o
			break
		}
	}
	returned = true
	return true
}

// afterAllResult accumulates one group's AfterAll outcome across its deferred hooks.
type afterAllResult struct {
	failed          bool
	message, output string
}

// runAfterAll runs one AfterAll, recording in acc whether it failed. Like runBeforeAlls it settles
// in a defer, since the hook can end the goroutine with runtime.Goexit.
//
// A failure a hook reports only through a non-fatal ctx.T.Error/Errorf/Fail is visible here only
// while gt had not failed before the hook: testing propagates Fail to every parent, so once a spec
// of the group failed, gt.Failed() is already true and says nothing about this hook. `go test`
// still prints the message and fails the group; only the [AfterAll] case is missing — the
// documented H6 limitation. testing offers no per-call observation of a *testing.T that would lift
// it.
func (r *groupRun) runAfterAll(gt *testing.T, h func(*Context), acc *afterAllResult) {
	failedBefore := gt != nil && gt.Failed()
	var message, output string
	var hookFailed, returned bool
	defer func() {
		skipped := gt != nil && gt.Skipped() && !gt.Failed()
		if hookFailed || (!returned && !skipped) || (gt != nil && !failedBefore && gt.Failed()) {
			acc.failed = true
			if acc.message == "" {
				acc.message, acc.output = message, output
			}
		}
	}()
	message, output, hookFailed = r.runHook(gt, h)
	returned = true
}

// reportAfterAll emits group g's [AfterAll] case once all its AfterAlls have run, if any failed.
func (r *groupRun) reportAfterAll(gt *testing.T, g int, acc *afterAllResult) {
	if !acc.failed {
		return
	}
	reportHookCase(r.rep, r.pg.groups[g].Path, hookKindAfterAll, acc.message, acc.output)
	if gt != nil {
		gt.Errorf("go-specs: AfterAll failed for group %q", groupDisplayName(r.pg, g))
	}
}

// runHook runs one group hook on the calling goroutine against a Context bound to gt, the group
// subtest (or to the suite's backend when there is no *testing.T). A panic is recovered through the
// single recovery authority (runGroupHookOnce); an assertion failure or Fatal/FailNow ends the
// goroutine with runtime.Goexit on a real *testing.T, which the callers' defers account for.
func (r *groupRun) runHook(gt *testing.T, h func(*Context)) (message, output string, failed bool) {
	backend := r.backend
	if gt != nil {
		b := asTestBackend(gt)
		defer putTestBackend(b)
		backend = b
	}
	ctx := acquireContext(backend)
	defer releaseContext(ctx)
	message, output = runGroupHookOnce(ctx, h)
	return message, output, ctx.hasFailed()
}

// reportGroupSkipped reports every spec of group g, including its nested groups' specs, skipped
// with message, without opening a subtest for any of them (H4).
func (r *groupRun) reportGroupSkipped(g int, message string) {
	group := &r.pg.groups[g]
	for i := group.Start; i <= group.End; i++ {
		reportGroupSuppressedSpec(r.rep, r.plan, i, message)
	}
}

// runSpec runs spec i, which sits directly in the scope t.
func (r *groupRun) runSpec(t *testing.T, prefix string, i int) {
	if !r.real {
		runExecution(r.backend, r.rep, r.plan, i)
		return
	}
	start, length := r.plan.ProgramStart[i], r.plan.ProgramLen[i]
	if start+length > len(r.plan.Instructions) {
		return
	}
	program := r.plan.Instructions[start : start+length]
	ctx := acquireContext(r.backend)
	defer releaseContext(ctx)
	// Registered after release so it runs first: when the body called the unsupported
	// ctx.T.Parallel(), runSpecProgramIsolated poisons ctx and ends this goroutine with t.Fatalf; the
	// stop must reach every enclosing group subtest too.
	defer func() {
		if ctx.poisoned {
			r.stopped.Store(true)
		}
	}()
	started := reportSpecStarted(r.rep, specEventName(r.plan, i), r.reportPath(i))
	message, output, ran := runSpecProgramIsolated(t, ctx, program, specSubtestName(r.plan, i)[len(prefix):])
	reportSpecFinished(r.rep, started, specResult{Failed: ctx.hasFailed(), Message: message, Output: output, Filtered: !ran})
}

// reportPath is specEventPath(plan, i) when there is a reporter, and nil otherwise, so the
// reporter-less path does not build a Path nothing reads (see runExecution).
func (r *groupRun) reportPath(i int) []string {
	if r.rep == nil {
		return nil
	}
	return specEventPath(r.plan, i)
}

// parallelSpecOutcome is one ItParallel spec's buffered result (issue #245), captured entirely
// inside the goroutine that ran it — no reporter call, no *stopped/poison write to shared state —
// so runParallelGroup can emit it, serialized, on its own goroutine once every spec in the range has
// finished (the user's concurrency conditions 1 and 2: reporter events are serialized, and results
// are reported in declaration order).
type parallelSpecOutcome struct {
	start    report.SpecStartEvent
	result   specResult
	duration time.Duration
	// parked and specName describe a spec whose body called the unsupported ctx.T.Parallel()
	// (spec_body_parallel.go): specName is only used to build the diagnostic once every spec in the
	// range has been collected, since Fatal/FailNow must run on the goroutine actually running t (see
	// runParallelGroup), never on the goroutine that detected it.
	parked   bool
	specName string
}

// runParallelGroup runs the ItParallel range r.pg.parallel[pi] (issue #245). Every spec in the range
// gets its own Go subtest, launched from its own goroutine via testing.T.Run — never t.Parallel(),
// which spec_body_parallel.go explains cannot share this package's *Context model — and every
// launched t.Run call returns before this method does (wg.Wait), which is what the testing package
// requires for calling t.Run concurrently from multiple goroutines in the first place.
//
// Without a real *testing.T (r.real == false: a *testing.B, or a fake backend in this package's own
// tests), there is no subtest mechanism to run the range against concurrently, so its specs run one
// at a time, in declaration order, exactly like an ordinary sequential spec in that mode — the
// user's explicit choice for that case.
func (r *groupRun) runParallelGroup(t *testing.T, prefix string, pi int) {
	rng := r.pg.parallel[pi]
	if !r.real {
		for i := rng.Start; i <= rng.End; i++ {
			r.runSpec(t, prefix, i)
			if r.stopped.Load() {
				return
			}
		}
		return
	}
	n := rng.End - rng.Start + 1
	outcomes := make([]parallelSpecOutcome, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for k := 0; k < n; k++ {
		i := rng.Start + k
		go func(i, k int) {
			defer wg.Done()
			outcomes[k] = r.runParallelSpec(t, prefix, i)
		}(i, k)
	}
	wg.Wait()

	parkedName := ""
	for k := 0; k < n; k++ {
		i := rng.Start + k
		r.emitParallelOutcome(i, outcomes[k])
		if outcomes[k].parked && parkedName == "" {
			parkedName = outcomes[k].specName
		}
	}
	if parkedName != "" {
		// Every launched spec has finished (or leaked its poisoned Context, in the parked one's
		// case), so this goroutine is the one actually running t again — the same guarantee
		// runSpec's sequential equivalent (runSpecProgramIsolated/failUnsupportedSpecBodyParallel)
		// relies on. r.stopped is set first so an enclosing scope that is not itself unwound by this
		// Fatalf's Goexit (a sibling this group's own t.Run already returned control to) still stops
		// at its own next stopIfStopped check (H5's "every group already entered still runs its
		// AfterAlls while the stop unwinds").
		r.stopped.Store(true)
		if t != nil {
			t.Helper()
			t.Fatalf("%s", unsupportedSpecBodyParallelMessage(parkedName))
		}
	}
}

// runParallelSpec runs one ItParallel spec (plan index i) as its own Go subtest, on its own
// goroutine, against its own pooled *Context — mirroring runSpec/runSpecProgramIsolated for the
// sequential path, except every side effect that is not local to this one goroutine (reporting,
// *stopped, releasing a parked Context) is deferred to the caller, which runs once every spec in the
// group has returned (see runParallelGroup).
//
// ctx is acquired here, not shared with any other spec, so a parked body (ctx.T.Parallel(), see
// spec_body_parallel.go) can safely be left un-released — the same poison-and-abandon rule
// releaseContext already applies, just decided per spec here instead of per sequential step.
func (r *groupRun) runParallelSpec(t *testing.T, prefix string, i int) parallelSpecOutcome {
	name := specEventName(r.plan, i)
	path := r.reportPath(i)
	startTime := time.Now()
	subtestName := specSubtestName(r.plan, i)
	if len(subtestName) >= len(prefix) {
		subtestName = subtestName[len(prefix):]
	}
	start, length := r.plan.ProgramStart[i], r.plan.ProgramLen[i]
	var program []Instruction
	if start+length <= len(r.plan.Instructions) {
		program = r.plan.Instructions[start : start+length]
	}
	ctx := acquireContext(r.backend)
	var message, output string
	ran, parked := runSubtestGuardingParallel(t, subtestName, func(subT *testing.T) {
		subBackend := asTestBackend(subT)
		defer putTestBackend(subBackend)
		ctx.Reset(subBackend)
		// runProgram's deferred recover is the ONLY panic containment on this path, and the same
		// defer is what runs AfterEach after a failing body. runSubtestGuardingParallel does not
		// recover, and a panic on this goroutine is fatal to the whole test binary (the testing
		// package re-panics it). Calling the instructions directly here would crash the run on the
		// first panicking ItParallel and skip AfterEach. See spec_itparallel_failure_test.go.
		message, output = runProgram(program, ctx)
	})
	failed := ctx.hasFailed()
	duration := time.Since(startTime)
	if parked {
		// Leaked deliberately, same rule as releaseContext/failUnsupportedSpecBodyParallel: the
		// parked body still holds ctx and will resume on its own goroutine later, so recycling it
		// now would turn that body's assertions into silent no-ops or corrupt whichever unrelated
		// spec the pool hands it to next.
		ctx.poison()
	} else {
		releaseContext(ctx)
	}
	return parallelSpecOutcome{
		start:    report.SpecStartEvent{Name: name, Path: path, Time: startTime},
		result:   specResult{Failed: failed, Filtered: !ran, Message: message, Output: output},
		duration: duration,
		parked:   parked,
		specName: subtestName,
	}
}

// emitParallelOutcome reports one buffered ItParallel outcome. Called only from runParallelGroup,
// after every spec in the range has finished, in declaration (index) order — so, unlike
// reportSpecStarted/reportSpecFinished, it builds both events directly instead of deriving Duration
// from time.Now(): that would measure the gap since the whole group finished, not the one spec's own
// run.
func (r *groupRun) emitParallelOutcome(i int, o parallelSpecOutcome) {
	if r.rep == nil {
		return
	}
	r.rep.SpecStarted(o.start)
	duration := o.duration
	if o.result.Filtered {
		duration = 0
	}
	r.rep.SpecFinished(report.SpecResultEvent{
		SpecStartEvent: o.start,
		Failed:         o.result.Failed,
		Filtered:       o.result.Filtered,
		Duration:       duration,
		Message:        o.result.Message,
		Output:         o.result.Output,
	})
}

// runGroupHookOnce runs one hook, recovering a panic through the same single authority every other
// execution path in this engine uses (H7). A plain ctx-assertion failure or Fatal/FailNow leaves
// message/output empty here (recover() sees nothing to recover); the caller reads the failure from
// ctx.hasFailed() or the group subtest instead.
func runGroupHookOnce(ctx *Context, fn func(*Context)) (message, output string) {
	defer func() { message, output = recoverSpecFailure(ctx, recover(), "panic in group hook") }()
	fn(ctx)
	return
}

// groupSubtestName returns the name a hooked group's subtest is opened with inside a scope whose
// specs carry prefix, and the prefix the group's own specs then carry. validateHookGroups has
// already guaranteed, while the suite was built, that the name is non-empty and that no spec or
// group in the suite normalizes to the same subtest name, so every spec's full name composes to
// exactly the name it has without group hooks.
//
// Collisions that only exist at run time — a second same-named hooked Describe in the same test
// function, or a subtest the caller opened earlier under the same name — are left to testing,
// which gives the group subtest its usual deterministic "#01" suffix (docs/SUITE_HOOKS_CONTRACT.md
// H7).
func groupSubtestName(prefix string, group *hookGroup) (name, groupPrefix string) {
	groupPrefix = joinSubtestPath(group.Path, "")
	return groupPrefix[len(prefix) : len(groupPrefix)-1], groupPrefix
}

// validateHookGroups rejects, while the suite is being built and before anything runs, every hooked
// group that could not run as its own Go subtest without changing a spec's subtest name. A group
// that registers BeforeAll or AfterAll must have an explicit, non-empty name that no spec and no
// other hooked group in the suite shares once go test has normalized both (spaces become "_",
// non-printable runes are escaped). The shapes rejected, and why each would break a name:
//
//   - an empty group name (When(""), or Describe(t, "", ...) with root hooks): testing names an
//     empty subtest "#00";
//   - an It("") directly inside the group: same, for the spec's subtest inside the group;
//   - a spec whose full name equals the group's: the group subtest would take the name first and
//     push the spec to "#01", where the hook-free layout keeps it;
//   - another hooked group with the same full name (two sibling When("x"), "sp ace" next to
//     "sp_ace", or When("a/b") next to When("a") { When("b") }): the second group subtest would be
//     renamed "#01" at the group level.
//
// It panics with a message naming the group, the convention this engine already uses for a
// registration it cannot honour (see Spec.requireBuildTarget). Groups without hooks are never
// checked; they have no subtest of their own.
func validateHookGroups(plan *ExecutionPlan, pg *planGroups) {
	if pg == nil {
		return
	}
	specNames := make(map[string]int, len(plan.FullNames))
	for i := range plan.ProgramStart {
		n := normalizeSubtestName(specSubtestName(plan, i))
		if _, ok := specNames[n]; !ok {
			specNames[n] = i
		}
	}
	groupNames := make(map[string]int, len(pg.groups))
	// Registration order is innermost-first; check outer groups first so the reported group is the
	// later declaration of a duplicate.
	order := make([]int, len(pg.groups))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		ga, gb := &pg.groups[a], &pg.groups[b]
		if ga.Start != gb.Start {
			return ga.Start - gb.Start
		}
		return len(ga.Path) - len(gb.Path)
	})
	for _, g := range order {
		group := &pg.groups[g]
		if group.Name == "" {
			failHookGroup(group, "it has an empty name")
		}
		prefix := joinSubtestPath(group.Path, "")
		full := prefix[:len(prefix)-1]
		for i := group.Start; i <= group.End; i++ {
			fullName := specSubtestName(plan, i)
			if specEventName(plan, i) == "" && (fullName == prefix || fullName == full) {
				failHookGroup(group, `it declares an It with an empty name directly inside it, which go test would name "#00"`)
			}
		}
		normalized := normalizeSubtestName(full)
		if i, ok := specNames[normalized]; ok {
			failHookGroup(group, fmt.Sprintf("a spec in the suite has the same subtest name %q (%q)", normalized, specEventPath(plan, i)))
		}
		if other, ok := groupNames[normalized]; ok {
			failHookGroup(group, fmt.Sprintf("another group in the suite has the same subtest name %q (%q)", normalized, pg.groups[other].Path))
		}
		groupNames[normalized] = g
	}
}

func failHookGroup(group *hookGroup, reason string) {
	panic(fmt.Sprintf("specs: group %q registers BeforeAll/AfterAll, so it must run as its own Go subtest, "+
		"which needs an explicit, non-empty name that is unique among the specs and groups of its suite "+
		"(compared the way go test names subtests: spaces become underscores): %s. Rename the group or the "+
		"conflicting spec or group (docs/SUITE_HOOKS_CONTRACT.md H7).", group.Path, reason))
}

// normalizeSubtestName applies testing's own presentation rewrite to a subtest name — spaces
// become "_", non-printable runes are escaped — so two names that testing would treat as the same
// subtest compare equal. It mirrors testing's unexported rewrite in match.go.
func normalizeSubtestName(s string) string {
	b := make([]byte, 0, len(s))
	for _, c := range s {
		switch {
		case isTestingSpace(c):
			b = append(b, '_')
		case !strconv.IsPrint(c):
			q := strconv.QuoteRune(c)
			b = append(b, q[1:len(q)-1]...)
		default:
			b = append(b, string(c)...)
		}
	}
	return string(b)
}

// isTestingSpace mirrors testing's unexported isSpace, which is not unicode.IsSpace.
func isTestingSpace(c rune) bool {
	if c < 0x2000 {
		switch c {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0xA0, 0x1680:
			return true
		}
		return false
	}
	if c <= 0x200a {
		return true
	}
	switch c {
	case 0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
		return true
	}
	return false
}

// unsupportedGroupHookParallelMessage is the diagnostic for a group hook that called
// ctx.T.Parallel(), which would make the group's own subtest parallel while its hooks and specs
// are still running sequentially inside it.
func unsupportedGroupHookParallelMessage(group string) string {
	return fmt.Sprintf(
		"ctx.T.Parallel() is not supported in a BeforeAll/AfterAll hook (group %q): ctx.T there is the group's own subtest, "+
			"and making it parallel would detach the group's hooks from its specs — the run was stopped here. "+
			"Use ItParallel (or RunParallel/RunParallelBatched) to run specs concurrently.",
		group,
	)
}

// reportGroupSuppressedSpec reports spec i as skipped (H4: "reports every spec of the group ...
// as skipped, with a message naming the failing group"), without opening a subtest for it at all —
// the same convention the Builder engine already uses for a compile-time-skipped spec (see
// program.go's reportSkipped): only identity is reported, nothing runs.
func reportGroupSuppressedSpec(rep report.EventReporter, plan *ExecutionPlan, i int, message string) {
	if rep == nil {
		return
	}
	name := specEventName(plan, i)
	path := specEventPath(plan, i)
	started := report.SpecStartEvent{Name: name, Path: path, Time: time.Now()}
	rep.SpecStarted(started)
	rep.SpecFinished(report.SpecResultEvent{
		SpecStartEvent: started,
		Skipped:        true,
		Message:        message,
	})
}

// groupDisplayName renders a group's Path the same way a spec's own breadcrumb reads, for the
// skipped-spec message above.
func groupDisplayName(pg *planGroups, g int) string {
	if pg == nil || g < 0 || g >= len(pg.groups) {
		return ""
	}
	return strings.Join(pg.groups[g].Path, "/")
}

const (
	hookKindBeforeAll = report.HookBeforeAll
	hookKindAfterAll  = report.HookAfterAll
)

// hookCaseName is the synthetic case's display name (docs/SUITE_HOOKS_CONTRACT.md H4/H6):
// "[BeforeAll]"/"[AfterAll]". The structural marker a consumer should actually switch on is
// report.SpecResultEvent.Hook / report.Case.Hook, not this string (H8).
func hookCaseName(kind report.HookKind) string { return "[" + kind.String() + "]" }

// reportHookCase emits the single synthetic case a failed group hook produces (H4/H6/H8): one
// SpecStarted/SpecFinished pair whose Path is exactly the group's own declared scope chain and
// nothing else. Unlike a real spec's Path, it has no trailing leaf element: the case's Name (the
// bracketed "[BeforeAll]"/"[AfterAll]" marker) is its leaf, and the renderers already treat a hook
// case's Path as the group (report.junitClassName, report.caseDisplayName). Appending the marker
// to Path as well printed it twice in TXT and pushed it into the JUnit classname. Its Hook field
// is set only on the SpecFinished event, never SpecStarted (report.SpecStartEvent carries no Hook
// field at all — see report/events.go's HookKind doc), so it marks the result structurally
// distinct from a real spec. Duration is always 0 — group hooks are not timed today, only whether
// they failed; only a failed hook produces a case at all (H8).
func reportHookCase(rep report.EventReporter, groupPath []string, kind report.HookKind, message, output string) {
	if rep == nil {
		return
	}
	name := hookCaseName(kind)
	path := append([]string(nil), groupPath...)
	started := report.SpecStartEvent{Name: name, Path: path, Time: time.Now()}
	rep.SpecStarted(started)
	rep.SpecFinished(report.SpecResultEvent{SpecStartEvent: started, Failed: true, Message: message, Output: output, Hook: kind})
}
