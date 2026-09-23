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
// breadcrumb guarantees are unchanged (see groupSubtestName for the few shapes where that is only
// possible without a group subtest).
//
// The group subtest is what gives hooks correct Go semantics:
//
//   - A group is entered lazily, inside the spec subtest of its first descendant spec that actually
//     starts, right before that spec's body. `go test -run` therefore can never filter a group's
//     hooks independently of the specs they guard: a filtered-out spec never enters its group, and
//     a selected one always does (H3). AfterAll runs at the end of the group subtest, and only if
//     the group was entered (H5).
//   - Hooks are not subtests. Each hook runs synchronously on a goroutine of its own, so a
//     runtime.Goexit or panic in it can unwind nothing but the hook (H7), and its ctx.T is the
//     GROUP's subtest. ctx.T.Cleanup, TempDir and Setenv registered in a BeforeAll therefore live
//     until the group subtest ends — after the group's AfterAll, before the next sibling group.
//   - A hook failure is reported as the synthetic [BeforeAll]/[AfterAll] case and marks the group
//     subtest failed, so `go test` attributes it to the group.
//
// Every other backend — a fake backend in a unit test, or a *testing.B — has no subtests to open,
// and runs the same tree walk directly, with the same H1-H7 semantics.
package specs

import (
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
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
}

// hookGroup is one Describe/When (or root) scope's once-per-group hooks. Path is the declared
// scope chain including the group's own name — the Path its synthetic hook case reports (see
// reportHookCase). Start and End are the plan indices of the first and last spec in the scope's
// subtree; scopes nest, so two groups' ranges are either disjoint or one contains the other.
type hookGroup struct {
	Path       []string
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
func registerHookGroup(pg **planGroups, path []string, before, after []func(*Context), startIdx, endIdx int) {
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
		BeforeAll: before,
		AfterAll:  after,
		Start:     startIdx,
		End:       endIdx,
	})
}

// groupScope is one group's run-time state for a single CompiledSuite.Run.
type groupScope struct {
	// children are the group's directly nested groups, ordered by Start.
	children []int
	// t is the *testing.T the group's hooks see as ctx.T: the group's own subtest, or, for a group
	// run without one (see groupSubtestName), the enclosing scope's. nil on a non-*testing.T
	// backend.
	t *testing.T
	// entered is set when the group's first descendant spec starts (H2/H3) and makes its AfterAll
	// owed (H5); failed is set when one of its BeforeAlls fails (H4).
	entered, failed bool
}

// groupRun is the state of one hooked CompiledSuite.Run.
type groupRun struct {
	backend testBackend
	rep     report.EventReporter
	plan    *ExecutionPlan
	pg      *planGroups
	scopes  []groupScope
	top     []int // groups not nested in another group, ordered by Start
	// real is true when backend wraps a real *testing.T, i.e. when groups get subtests.
	real bool
	// taken holds the normalized subtest names already claimed by a spec or a group subtest; see
	// groupSubtestName. Built only when real.
	taken map[string]bool
	// stopped is set when the run must stop because a spec body or a hook called the unsupported
	// ctx.T.Parallel(); every enclosing group subtest then unwinds too (see stopIfStopped).
	stopped bool
}

// runPlanWithGroups is runPlanSpecsInOrder for a suite that registered at least one group hook.
func runPlanWithGroups(backend testBackend, rep report.EventReporter, plan *ExecutionPlan, pg *planGroups) {
	r := &groupRun{backend: backend, rep: rep, plan: plan, pg: pg}
	r.buildTree()
	var topT *testing.T
	if rb, ok := backend.(*runnableBackend); ok {
		topT, r.real = rb.tb.(*testing.T)
	}
	if r.real {
		r.taken = claimedSubtestNames.snapshot(topT)
		for i := range plan.ProgramStart {
			r.taken[normalizeSubtestName(specSubtestName(plan, i))] = true
		}
		// Deferred so the names are recorded even when the run is stopped by runtime.Goexit.
		defer claimedSubtestNames.record(topT, r.taken)
	}
	r.runRange(topT, "", 0, len(plan.ProgramStart)-1, r.top, nil)
}

// claimedSubtestNames remembers, per *testing.T, the normalized subtest names that hooked suites
// already used under it, so a second hooked Describe with the same name in the same test function
// sees the first one's names (see groupSubtestName). Without it, the second call's root group would
// open "suite" again — which testing renames "suite#01" — where the hook-free layout keeps "suite"
// and suffixes the specs instead. Only hooked runs record: a suite without group hooks must not pay
// for this (H10), and its flat names can collide with a group name only through a spec whose full
// breadcrumb equals the group's, a residual case documented in docs/SUITE_HOOKS_CONTRACT.md H7.
// An entry is dropped when its *testing.T finishes.
var claimedSubtestNames = subtestNameRegistry{byT: map[*testing.T]map[string]bool{}}

type subtestNameRegistry struct {
	mu  sync.Mutex
	byT map[*testing.T]map[string]bool
}

// snapshot returns a private copy of the names claimed under t so far.
func (reg *subtestNameRegistry) snapshot(t *testing.T) map[string]bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	names := make(map[string]bool, len(reg.byT[t]))
	for n := range reg.byT[t] {
		names[n] = true
	}
	return names
}

// record adds names to the names claimed under t.
func (reg *subtestNameRegistry) record(t *testing.T, names map[string]bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	claimed, ok := reg.byT[t]
	if !ok {
		claimed = make(map[string]bool, len(names))
		reg.byT[t] = claimed
		t.Cleanup(func() {
			reg.mu.Lock()
			defer reg.mu.Unlock()
			delete(reg.byT, t)
		})
	}
	for n := range names {
		claimed[n] = true
	}
}

// buildTree derives each group's directly nested groups from the spec ranges. Sorting by Start,
// then by End descending, puts every group right after the groups that contain it; for two groups
// covering the same range the one with the shorter Path is the outer one.
func (r *groupRun) buildTree() {
	groups := r.pg.groups
	r.scopes = make([]groupScope, len(groups))
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
			r.scopes[parent].children = append(r.scopes[parent].children, g)
		}
		stack = append(stack, g)
	}
}

// runRange runs specs lo..hi, which all sit in the same scope: children are the hooked groups
// directly inside that scope, chain the hooked groups enclosing it (outermost first), t the scope's
// *testing.T (nil on a non-*testing.T backend) and prefix the part of every spec's subtest name
// that t's own name already carries.
func (r *groupRun) runRange(t *testing.T, prefix string, lo, hi int, children, chain []int) {
	k := 0
	for i := lo; i <= hi; {
		if k < len(children) && r.pg.groups[children[k]].Start == i {
			g := children[k]
			k++
			r.runGroup(t, prefix, g, chain)
			r.stopIfStopped(t)
			i = r.pg.groups[g].End + 1
			continue
		}
		r.runSpec(t, prefix, i, chain)
		r.stopIfStopped(t)
		i++
	}
}

// stopIfStopped unwinds t's goroutine once the run has been stopped deeper down, so the stop
// reaches the test that owns the suite exactly as it does without group hooks.
func (r *groupRun) stopIfStopped(t *testing.T) {
	if r.stopped && t != nil {
		t.FailNow()
	}
}

// runGroup runs group g inside the scope t: in a subtest of its own when groupSubtestName allows
// one, otherwise inline in t. Its AfterAll runs at the end, iff the group was entered.
func (r *groupRun) runGroup(t *testing.T, prefix string, g int, chain []int) {
	group := &r.pg.groups[g]
	if failing := r.failingIn(chain); failing >= 0 {
		// An ancestor's BeforeAll failed: this group is never entered (H4), and no subtest is
		// opened for it or its specs.
		for i := group.Start; i <= group.End; i++ {
			reportGroupSuppressedSpec(r.rep, r.plan, r.pg, i, failing)
		}
		return
	}
	inner := append(chain[:len(chain):len(chain)], g)
	children := r.scopes[g].children
	name, groupPrefix, ok := r.groupSubtestName(prefix, g)
	if !ok {
		r.scopes[g].t = t
		r.runRange(t, prefix, group.Start, group.End, children, inner)
		r.exitGroup(g)
		return
	}
	ran, parked := runSubtestGuardingParallel(t, name, func(gt *testing.T) {
		r.scopes[g].t = gt
		r.runRange(gt, groupPrefix, group.Start, group.End, children, inner)
		r.exitGroup(g)
	})
	if parked {
		// A hook of this group called the unsupported ctx.T.Parallel() on the group subtest.
		r.stopped = true
		t.Helper()
		t.Fatalf("%s", unsupportedGroupHookParallelMessage(groupDisplayName(r.pg, g)))
	}
	if !ran {
		// `go test -run` filtered the whole group out: none of its hooks ran (H3), and its specs
		// are reported filtered, exactly as they would be without group hooks (#111).
		for i := group.Start; i <= group.End; i++ {
			started := reportSpecStarted(r.rep, specEventName(r.plan, i), r.reportPath(i))
			reportSpecFinished(r.rep, started, specResult{Filtered: true})
		}
	}
}

// groupSubtestName returns the name group g's subtest is opened with inside a scope whose specs
// carry prefix, and the prefix the group's own specs then carry. ok is false when the group must
// run inline in its enclosing scope instead, because a subtest of its own would change a spec's
// full Go subtest name. That happens only for names the flat mapping already treats specially (see
// joinSubtestPath):
//
//   - the group's own segment is empty (Describe(t, "", ...), When("")), which testing would name
//     "#00" as a subtest of its own;
//   - a spec in the group has an empty name, which testing would likewise rename to "#00";
//   - the group's normalized name is already taken by a spec or an earlier group — duplicate
//     sibling scopes, or a spec whose breadcrumb equals the group's — which testing would suffix
//     with "#01" at the group level rather than at the spec.
//
// A group run inline still follows every H1-H10 rule; only its hooks' ctx.T is the enclosing
// scope's, so what they register with ctx.T.Cleanup/TempDir/Setenv lives until that scope ends.
func (r *groupRun) groupSubtestName(prefix string, g int) (name, groupPrefix string, ok bool) {
	if !r.real {
		return "", "", false
	}
	group := &r.pg.groups[g]
	groupPrefix = joinSubtestPath(group.Path, "")
	if len(groupPrefix) < len(prefix)+2 || groupPrefix[:len(prefix)] != prefix {
		return "", "", false
	}
	name = groupPrefix[len(prefix) : len(groupPrefix)-1]
	full := normalizeSubtestName(groupPrefix[:len(groupPrefix)-1])
	if r.taken[full] {
		return "", "", false
	}
	for i := group.Start; i <= group.End; i++ {
		if specSubtestName(r.plan, i) == groupPrefix {
			return "", "", false
		}
	}
	r.taken[full] = true
	return name, groupPrefix, true
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

// failingIn returns the group in chain whose BeforeAll failed, or -1.
func (r *groupRun) failingIn(chain []int) int {
	for _, g := range chain {
		if r.scopes[g].failed {
			return g
		}
	}
	return -1
}

// enter enters every group of chain that is not entered yet, outermost first (H2), running its
// BeforeAlls. It returns the group whose BeforeAll failed, or -1; groups nested in a failed one are
// never entered (H4).
func (r *groupRun) enter(chain []int) int {
	for _, g := range chain {
		scope := &r.scopes[g]
		if scope.failed {
			return g
		}
		if scope.entered {
			continue
		}
		scope.entered = true
		group := &r.pg.groups[g]
		message, output, failed, unobservable := r.runHooks(g, group.BeforeAll, true)
		if unobservable && !failed {
			// See runGroupHookInScope: this BeforeAll ran on an enclosing scope that had already
			// failed, so a failure it reported through ctx.T would leave no trace. H4 forbids running a
			// spec whose setup may have failed, so the group fails closed.
			failed = true
			message = fmt.Sprintf("go-specs: cannot tell whether BeforeAll of group %q failed: the group has no subtest of its own "+
				"(see docs/SUITE_HOOKS_CONTRACT.md H7), so the hook reported on its enclosing scope's *testing.T, which had "+
				"already failed, and a failure reported directly through ctx.T would be invisible. Its specs are skipped "+
				"rather than run without a verified setup; give the group a unique, non-empty name to avoid this.",
				groupDisplayName(r.pg, g))
		}
		if failed {
			scope.failed = true
			reportHookCase(r.rep, group.Path, hookKindBeforeAll, message, output)
			if scope.t != nil {
				scope.t.Errorf("go-specs: BeforeAll failed for group %q; its specs are reported skipped", groupDisplayName(r.pg, g))
			}
			return g
		}
	}
	return -1
}

// exitGroup runs group g's AfterAlls if it was entered — even when its BeforeAll failed (H5) —
// each one regardless of an earlier one failing (H6).
func (r *groupRun) exitGroup(g int) {
	scope := &r.scopes[g]
	if !scope.entered {
		return
	}
	group := &r.pg.groups[g]
	// An AfterAll whose scope had already failed and that reported only through a non-fatal
	// ctx.T.Error/Errorf/Fail is not observable here (unobservable is ignored): `go test` still fails
	// the scope, but no [AfterAll] case is emitted — the documented H6 limitation.
	message, output, failed, _ := r.runHooks(g, group.AfterAll, false)
	if failed {
		reportHookCase(r.rep, group.Path, hookKindAfterAll, message, output)
		if scope.t != nil {
			scope.t.Errorf("go-specs: AfterAll failed for group %q", groupDisplayName(r.pg, g))
		}
	}
}

// runHooks runs one group's own BeforeAll or AfterAll list in registration order. stopOnFailure
// selects H4 (BeforeAll: stop at the first failing hook) versus H6 (AfterAll: every hook still
// runs). message/output are the first failing hook's recovered panic pair; they stay "" for an
// assertion or Fatal/FailNow failure, exactly like a real spec's SpecResultEvent.Message.
// unobservable reports that at least one hook ran on a scope that had already failed and returned
// without a failure this function could see (see runGroupHookInScope).
func (r *groupRun) runHooks(g int, hooks []func(*Context), stopOnFailure bool) (message, output string, failed, unobservable bool) {
	t := r.scopes[g].t
	for _, h := range hooks {
		if h == nil {
			continue
		}
		var m, o string
		var hookFailed, hookUnobservable bool
		if t != nil {
			m, o, hookFailed, hookUnobservable = runGroupHookInScope(t, h)
			unobservable = unobservable || hookUnobservable
		} else {
			m, o, hookFailed = runGroupHookDirect(r.backend, h)
		}
		if hookFailed {
			failed = true
			if message == "" {
				message, output = m, o
			}
			if stopOnFailure {
				break
			}
		}
	}
	return
}

// runSpec runs spec i, which sits directly in the scope t. A spec inside a group whose BeforeAll
// failed is reported skipped without a subtest (H4).
func (r *groupRun) runSpec(t *testing.T, prefix string, i int, chain []int) {
	if failing := r.failingIn(chain); failing >= 0 {
		reportGroupSuppressedSpec(r.rep, r.plan, r.pg, i, failing)
		return
	}
	if !r.real {
		if failing := r.enter(chain); failing >= 0 {
			reportGroupSuppressedSpec(r.rep, r.plan, r.pg, i, failing)
			return
		}
		runExecution(r.backend, r.rep, r.plan, i)
		return
	}
	r.runSpecSubtest(t, specSubtestName(r.plan, i)[len(prefix):], i, chain)
}

// reportPath is specEventPath(plan, i) when there is a reporter, and nil otherwise, so the
// reporter-less path does not build a Path nothing reads (see runExecution).
func (r *groupRun) reportPath(i int) []string {
	if r.rep == nil {
		return nil
	}
	return specEventPath(r.plan, i)
}

// runSpecSubtest is runExecution for a spec inside the group tree against a real *testing.T. It
// differs in one respect: the spec's enclosing groups are entered inside its own subtest, right
// before its body, so a spec that `go test -run` filters out never enters a group, and one that is
// selected always does (H3). When that entry fails, this spec is reported skipped like the rest of
// the group, and its subtest is marked skipped with the reason.
func (r *groupRun) runSpecSubtest(t *testing.T, name string, i int, chain []int) {
	start, length := r.plan.ProgramStart[i], r.plan.ProgramLen[i]
	if start+length > len(r.plan.Instructions) {
		return
	}
	program := r.plan.Instructions[start : start+length]
	specName := specEventName(r.plan, i)
	path := r.reportPath(i)
	ctx, release := acquireContext(r.backend)
	defer release()
	var started report.SpecStartEvent
	var message, output string
	var suppressed bool
	ran, parked := runSubtestGuardingParallel(t, name, func(st *testing.T) {
		if failing := r.enter(chain); failing >= 0 {
			suppressed = true
			reportGroupSuppressedSpec(r.rep, r.plan, r.pg, i, failing)
			st.Skipf("skipped: BeforeAll failed for group %q", groupDisplayName(r.pg, failing))
		}
		started = reportSpecStarted(r.rep, specName, path)
		subBackend := asTestBackend(st)
		defer putTestBackend(subBackend)
		ctx.Reset(subBackend)
		message, output = runProgram(program, ctx)
	})
	if parked {
		r.stopped = true
		failUnsupportedSpecBodyParallel(t, ctx, name)
	}
	if suppressed {
		return
	}
	if !ran {
		started = reportSpecStarted(r.rep, specName, path)
	}
	reportSpecFinished(r.rep, started, specResult{Failed: ctx.hasFailed(), Message: message, Output: output, Filtered: !ran})
}

// runGroupHookInScope runs one group hook against a real *testing.T scope — the group's subtest.
//
// The hook runs synchronously on a goroutine of its own. That is what lets its ctx.T be the group's
// *testing.T without the hook ever being a subtest: a runtime.Goexit in the hook — Fatal/FailNow
// through ctx.T, or an assertion failure — unwinds only that goroutine, never the group subtest or
// the spec subtest that triggered the group's entry (H7). The hook's backend (groupHookBackend)
// reports every failure to the group's *testing.T with Errorf, so `go test` attributes it to the
// group, and a fatal one then ends only the hook goroutine.
//
// failed is true when the hook recorded a failure on ctx, reported one through ctx.T while the
// scope had not failed yet, or did not return normally at all (a direct ctx.T.FailNow/SkipNow or
// runtime.Goexit, which leaves no other trace here). A panic is recovered through the single
// recovery authority (runGroupHookOnce -> recoverSpecFailure), which reports it once through the
// backend and returns normally.
//
// One kind of failure cannot always be seen: a non-fatal report made directly on ctx.T
// (ctx.T.Error/Errorf/Fail). The only trace it leaves is scopeT.Failed() turning true, and
// testing.T.Fail propagates to every parent, so once anything in the scope has failed, that flag is
// already true and says nothing about this hook. testing offers no per-call observation of a
// *testing.T, and ctx.T must be the scope's own *testing.T for Cleanup/TempDir/Setenv to live as long
// as the group, so this is inherent rather than an oversight. unobservable is true in exactly that
// case — the scope had failed before the hook and the hook returned normally without any failure
// visible here — and the caller decides: a BeforeAll fails closed (H4), an AfterAll is documented as
// unreported (H6). A real group subtest has never failed when its BeforeAll runs (entry happens
// before any of its specs), so only a group run inline in its enclosing scope can be affected there.
func runGroupHookInScope(scopeT *testing.T, fn func(*Context)) (message, output string, failed, unobservable bool) {
	backend := &groupHookBackend{t: scopeT}
	ctx, release := acquireContext(backend)
	defer release()
	ctx.T, ctx.tb = scopeT, scopeT
	scopeFailedBefore := scopeT.Failed()
	var returned bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		message, output = runGroupHookOnce(ctx, fn)
		returned = true
	}()
	<-done
	failed = ctx.hasFailed() || !returned || (!scopeFailedBefore && scopeT.Failed())
	if !returned && !ctx.hasFailed() && !scopeT.Failed() {
		// A hook that stopped via SkipNow or a bare runtime.Goexit without failing: a group cannot be
		// skipped from inside its own hook, so this is a failure, and one that must be visible.
		scopeT.Errorf("go-specs: a group hook stopped without returning (t.SkipNow or runtime.Goexit); group hooks cannot skip their group")
	}
	unobservable = scopeFailedBefore && !failed
	return message, output, failed, unobservable
}

// runGroupHookDirect runs fn against a fresh Context for backend, with no *testing.T scope (a
// fake/controlled backend, or a *testing.B).
func runGroupHookDirect(backend testBackend, fn func(*Context)) (message, output string, failed bool) {
	ctx, release := acquireContext(backend)
	defer release()
	message, output = runGroupHookOnce(ctx, fn)
	failed = ctx.hasFailed()
	return
}

// runGroupHookOnce runs one hook, recovering a panic through the same single authority every other
// execution path in this engine uses (H7). A plain ctx-assertion failure or Fatal/FailNow leaves
// message/output empty here (recover() sees nothing to recover); the caller reads the failure from
// ctx.hasFailed() instead.
func runGroupHookOnce(ctx *Context, fn func(*Context)) (message, output string) {
	defer func() { message, output = recoverSpecFailure(ctx, recover(), "panic in group hook") }()
	fn(ctx)
	return
}

// groupHookBackend is the testBackend a group hook's Context reports through when the hook runs
// against a real *testing.T scope. It forwards every report to the group's *testing.T with Errorf —
// never Fatalf/FailNow, which would mark the group subtest finished from the hook goroutine — and
// ends a fatal report with runtime.Goexit of the hook goroutine only (see runGroupHookInScope).
type groupHookBackend struct {
	t *testing.T
}

func (b *groupHookBackend) Helper() { b.t.Helper() }

func (b *groupHookBackend) FailNow() {
	b.t.Fail()
	runtime.Goexit()
}

func (b *groupHookBackend) Fatal(args ...any) {
	b.t.Helper()
	b.t.Error(args...)
	runtime.Goexit()
}

func (b *groupHookBackend) Fatalf(format string, args ...any) {
	b.t.Helper()
	b.t.Errorf(format, args...)
	runtime.Goexit()
}

func (b *groupHookBackend) Error(args ...any) {
	b.t.Helper()
	b.t.Error(args...)
}

func (b *groupHookBackend) Errorf(format string, args ...any) {
	b.t.Helper()
	b.t.Errorf(format, args...)
}

func (b *groupHookBackend) Log(args ...any) {
	b.t.Helper()
	b.t.Log(args...)
}

func (b *groupHookBackend) Logf(format string, args ...any) {
	b.t.Helper()
	b.t.Logf(format, args...)
}

func (b *groupHookBackend) Name() string      { return b.t.Name() }
func (b *groupHookBackend) Cleanup(fn func()) { b.t.Cleanup(fn) }

// Run runs fn against the group's *testing.T directly: a hook is not a place to open subtests.
func (b *groupHookBackend) Run(_ string, fn func(testing.TB)) { fn(b.t) }

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
func reportGroupSuppressedSpec(rep report.EventReporter, plan *ExecutionPlan, pg *planGroups, i, failingGroup int) {
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
		Message:        fmt.Sprintf("skipped: BeforeAll failed for group %q", groupDisplayName(pg, failingGroup)),
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
