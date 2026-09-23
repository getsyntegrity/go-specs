// execution_plan.go defines the bytecode ExecutionPlan and CompiledSuite used by the compiler and runner.
package specs

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/report"
)

// ExecutionPlan holds the flat instruction stream and per-spec metadata for the runner.
type ExecutionPlan struct {
	Instructions []Instruction
	ProgramStart []int
	ProgramLen   []int
	Names        []string
	FullNames    []string
	// PathScopes is the shared backing array holding the declared scope names that enclose each
	// spec, laid out the same way Instructions is: PathScopeStart[i] and PathScopeLen[i] delimit
	// spec i's window. The spec's own name is not repeated here — Names[i] already holds it, and
	// SpecStartEvent.Path is the window followed by Names[i].
	//
	// The scopes are stored rather than recovered from FullNames because joining with "/" is not
	// injective — a declared name that itself contains "/" is indistinguishable from a scope
	// boundary once joined, so splitting the breadcrumb back apart invents scopes that were never
	// declared. FullNames stays as-is: it is the subtest identity, a different derivation.
	//
	// Consecutive specs sharing the same scopes share one window instead of each storing a copy.
	// Storing a private copy per spec is O(specs × depth) string headers, and Go grows a large slice
	// by ~1.25x, so the discarded intermediate arrays cost several times the final size again. On
	// 50k sibling specs that shape measured +91% build memory; sharing the window removes it.
	//
	// The sharing is by adjacency, not by set: appendSpecPath reuses only the window it handed the
	// previous spec (see there). Every spec in one block shares a window, which is the shape suites
	// have, but an interleaved tree — When("a"){It}, When("b"){It}, When("a"){It} — reuses nothing
	// and degrades back to a copy per spec. That is a size trade, never a correctness one: the
	// reported Path is identical either way. Deduplicating across the whole plan would need a lookup
	// keyed on the chain, which costs more than it saves for the tree shapes seen in practice.
	PathScopes     []string
	PathScopeStart []int
	PathScopeLen   []int

	// Groups holds one entry per Describe/When (or root) scope that registered at least one
	// BeforeAll/AfterAll hook and has at least one runnable spec in its subtree (issue #207,
	// docs/SUITE_HOOKS_CONTRACT.md). Nil for a suite that registers neither hook anywhere — H10:
	// the runtime skips every bit of group-lifecycle bookkeeping below in that case, so such a
	// suite's allocation counts and report output are exactly what they were before this feature.
	Groups []hookGroup
	// GroupEnter[i] lists indices into Groups entered immediately before spec i runs; GroupEnter[i]
	// is built by appending at each scope's close (innermost scope first), so it holds inner-to-
	// outer order and must be read in REVERSE to get the outer-to-inner order H2 requires (outer
	// BeforeAll before inner BeforeAll). GroupExit[i] lists indices exited immediately after spec i
	// runs, built the same way but read FORWARD, which already is inner-to-outer (H2's teardown
	// order). Both are nil, or shorter than len(Names), whenever no group targets a given spec or
	// any later one — every reader must bounds-check before indexing.
	GroupEnter [][]int
	GroupExit  [][]int
}

// hookGroup is one Describe/When (or root) scope's once-per-group hooks, plus the declared scope
// chain (including the group's own name) reported as the synthetic hook case's Path when one of
// its hooks fails — see reportHookCase.
type hookGroup struct {
	Path      []string
	BeforeAll []func(*Context)
	AfterAll  []func(*Context)
}

// registerHookGroup attaches one scope's own BeforeAll/AfterAll hooks to plan, entered right
// before spec startIdx runs and exited right after spec endIdx runs. Both compile paths
// (bytecodeCompiler.closeGroupHooksAtTop and buildExecutionPlanFromArenaRec) call this at the
// exact point they close a Describe/When/root scope, mirroring how BeforeEach/AfterEach are
// attributed but never flattened onto child specs (H1).
//
// startIdx > endIdx means the scope's subtree contributed zero specs to the plan — H3's "a group
// with zero runnable specs is never entered": nothing is registered, so the hooks are silently
// discarded along with the scope itself, and the case where neither hook was registered at all is
// already filtered by both callers before this is reached.
func registerHookGroup(plan *ExecutionPlan, path []string, before, after []func(*Context), startIdx, endIdx int) {
	if plan == nil || startIdx < 0 || startIdx > endIdx {
		return
	}
	if len(before) == 0 && len(after) == 0 {
		return
	}
	g := hookGroup{Path: append([]string(nil), path...), BeforeAll: before, AfterAll: after}
	idx := len(plan.Groups)
	plan.Groups = append(plan.Groups, g)
	growGroupSlots(&plan.GroupEnter, startIdx)
	growGroupSlots(&plan.GroupExit, endIdx)
	plan.GroupEnter[startIdx] = append(plan.GroupEnter[startIdx], idx)
	plan.GroupExit[endIdx] = append(plan.GroupExit[endIdx], idx)
}

// growGroupSlots grows *s so index idx is valid, preserving existing entries. Only ever called
// from registerHookGroup, so the cost is paid only by a suite that actually registers a group
// hook (H10).
func growGroupSlots(s *[][]int, idx int) {
	if idx < len(*s) {
		return
	}
	grown := make([][]int, idx+1)
	copy(grown, *s)
	*s = grown
}

func newExecutionPlan(estimatedSpecs int) *ExecutionPlan {
	if estimatedSpecs <= 0 {
		estimatedSpecs = 64
	}
	return &ExecutionPlan{
		Instructions: make([]Instruction, 0, estimatedSpecs*8),
		ProgramStart: make([]int, 0, estimatedSpecs),
		ProgramLen:   make([]int, 0, estimatedSpecs),
		Names:        make([]string, 0, estimatedSpecs),
		FullNames:    make([]string, 0, estimatedSpecs),
		// PathScopes is sized for distinct scope chains, not for one copy per spec: sibling specs
		// share a window, so this grows with the shape of the tree rather than with the spec count.
		PathScopes:     make([]string, 0, 16),
		PathScopeStart: make([]int, 0, estimatedSpecs),
		PathScopeLen:   make([]int, 0, estimatedSpecs),
	}
}

type planScratch struct {
	beforeFlat []func(*Context)
	afterFlat  []func(*Context)
	program    []Instruction
	path       []string
}

var planScratchPool = sync.Pool{
	New: func() any {
		return &planScratch{
			beforeFlat: make([]func(*Context), 0, 32),
			afterFlat:  make([]func(*Context), 0, 32),
			program:    make([]Instruction, 0, 32),
			path:       make([]string, 0, 8),
		}
	},
}

func countSpecsArena(arena *NodeArena, rootID int) int {
	if arena == nil || rootID < 0 || rootID >= len(arena.Nodes) {
		return 0
	}
	n := 0
	if arena.Nodes[rootID].Type == ItNode {
		n = 1
	}
	for _, cid := range arena.Children[rootID] {
		n += countSpecsArena(arena, cid)
	}
	return n
}

func buildExecutionPlanFromArena(arena *NodeArena, rootID int, plan *ExecutionPlan, scratch *planScratch) {
	if arena == nil || plan == nil || scratch == nil {
		return
	}
	scratch.path = scratch.path[:0]
	buildExecutionPlanFromArenaRec(arena, rootID, plan, scratch)
}

func buildExecutionPlanFromArenaRec(arena *NodeArena, nodeID int, plan *ExecutionPlan, scratch *planScratch) {
	if arena == nil || nodeID < 0 || nodeID >= len(arena.Nodes) {
		return
	}
	node := &arena.Nodes[nodeID]
	name := node.Name
	if name != "" && node.Type != SuiteNode {
		scratch.path = append(scratch.path, name)
	}
	// groupStart is this node's own once-per-group hooks' entry point (H2/H3): the index the next
	// spec emitted from here on would get, i.e. the first spec of this node's own subtree, if any.
	groupStart := len(plan.Names)
	if node.Type == ItNode {
		scratch.beforeFlat = scratch.beforeFlat[:0]
		scratch.afterFlat = scratch.afterFlat[:0]
		ancestorIDs := collectAncestorIDs(arena, node.Parent)
		for _, id := range ancestorIDs {
			scratch.beforeFlat = append(scratch.beforeFlat, arena.BeforeHooks[id]...)
			scratch.afterFlat = append(scratch.afterFlat, arena.AfterHooks[id]...)
		}
		scratch.beforeFlat = append(scratch.beforeFlat, arena.BeforeHooks[nodeID]...)
		scratch.afterFlat = append(scratch.afterFlat, arena.AfterHooks[nodeID]...)
		scratch.program = scratch.program[:0]
		for _, h := range scratch.beforeFlat {
			if h != nil {
				scratch.program = append(scratch.program, Instruction{Code: OpBeforeHook, Fn: h})
			}
		}
		if node.Fn != nil {
			scratch.program = append(scratch.program, Instruction{Code: OpBody, Fn: node.Fn})
		}
		for i := len(scratch.afterFlat) - 1; i >= 0; i-- {
			if h := scratch.afterFlat[i]; h != nil {
				scratch.program = append(scratch.program, Instruction{Code: OpAfterHook, Fn: h})
			}
		}
		start := len(plan.Instructions)
		plan.Instructions = append(plan.Instructions, scratch.program...)
		plan.ProgramStart = append(plan.ProgramStart, start)
		plan.ProgramLen = append(plan.ProgramLen, len(scratch.program))
		plan.Names = append(plan.Names, name)
		plan.FullNames = append(plan.FullNames, strings.Join(scratch.path, "/"))
		// scratch.path already has this spec's own name pushed as its last element (an ItNode is
		// not a SuiteNode, so the push above applies to it too); the scopes are everything before
		// it. An unnamed spec was never pushed, so for it the whole of scratch.path is scopes.
		scopes := scratch.path
		if name != "" {
			scopes = scopes[:len(scopes)-1]
		}
		appendSpecPath(plan, scopes)
	}
	for _, cid := range arena.Children[nodeID] {
		buildExecutionPlanFromArenaRec(arena, cid, plan, scratch)
	}
	// Close this node's own group hooks (issue #207), the arena-path equivalent of
	// bytecodeCompiler.closeGroupHooksAtTop: an ItNode has none of its own (BeforeAll/AfterAll are
	// never registered on a leaf spec), and the SuiteNode root is never exposed to the public
	// BeforeAll/AfterAll DSL surface, so both are skipped here defensively.
	if node.Type != ItNode && node.Type != SuiteNode {
		// Bounds-checked, not a direct index: NodeArena is exported with exported fields, and a
		// hand-built arena that predates this feature (e.g. a test fixture) may carry
		// BeforeHooks/AfterHooks but no BeforeAllHooks/AfterAllHooks at all — nil is the correct
		// "no group hooks declared" answer for it, not a panic.
		var before, after []func(*Context)
		if nodeID < len(arena.BeforeAllHooks) {
			before = arena.BeforeAllHooks[nodeID]
		}
		if nodeID < len(arena.AfterAllHooks) {
			after = arena.AfterAllHooks[nodeID]
		}
		if len(before) > 0 || len(after) > 0 {
			registerHookGroup(plan, scratch.path, before, after, groupStart, len(plan.Names)-1)
		}
	}
	if name != "" && node.Type != SuiteNode && len(scratch.path) > 0 {
		scratch.path = scratch.path[:len(scratch.path)-1]
	}
}

// collectAncestorIDs returns ancestor IDs from root to the given node (inclusive), so that hooks are in declaration order.
func collectAncestorIDs(arena *NodeArena, nodeID int) []int {
	if arena == nil || nodeID < 0 {
		return nil
	}
	var ids []int
	for id := nodeID; id >= 0 && id < len(arena.Nodes); id = arena.Nodes[id].Parent {
		ids = append(ids, id)
	}
	for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
		ids[i], ids[j] = ids[j], ids[i]
	}
	return ids
}

// CompiledSuite holds the compiled plan and optional arena reference. Run executes the plan.
type CompiledSuite struct {
	Plan     *ExecutionPlan
	Arena    *NodeArena
	RootID   int
	Name     string // suite name for SuiteStartEvent/SuiteEndEvent; falls back to the backend's name if empty
	Reporter report.EventReporter
}

// Run executes all specs in the plan. Uses one context from the pool per spec.
func (s *CompiledSuite) Run(tb testing.TB) {
	if s == nil || s.Plan == nil || tb == nil || len(s.Plan.ProgramStart) == 0 {
		return
	}
	backend := asTestBackend(tb)
	defer putTestBackend(backend)

	if s.Reporter == nil {
		runPlanSpecsInOrder(backend, nil, s.Plan)
		return
	}
	// counter observes every SpecFinished event to total TotalSpecs/FailedSpecs for SuiteEndEvent:
	// -run filtering can still make the reported count differ from len(plan.ProgramStart).
	counter := &specCounter{EventReporter: s.Reporter}
	name := s.Name
	if name == "" {
		name = backend.Name()
	}
	suiteStart := time.Now()
	s.Reporter.SuiteStarted(report.SuiteStartEvent{Name: name, Time: suiteStart})
	runPlanSpecsInOrder(backend, counter, s.Plan)
	s.Reporter.SuiteFinished(report.SuiteEndEvent{
		Name:          name,
		Time:          time.Now(),
		Duration:      time.Since(suiteStart),
		TotalSpecs:    counter.total,
		FailedSpecs:   counter.failed,
		FilteredSpecs: counter.filtered,
		SkippedSpecs:  counter.skipped,
	})
}

// specCounter decorates an EventReporter to tally executed/failed/filtered specs for the enclosing
// suite's SuiteEndEvent, then forwards every event unchanged to the underlying reporter.
type specCounter struct {
	report.EventReporter
	total    int
	failed   int
	filtered int
	// skipped counts a spec reported Skipped: true — today that is only a spec suppressed by an
	// ancestor group's failed BeforeAll (issue #207 H4); the canonical Describe engine has no
	// compile-time Skip/Pending of its own (see docs/EXECUTION_ENGINES.md), so this field stays 0
	// for every suite that predates that feature.
	skipped int
}

func (c *specCounter) SpecFinished(e report.SpecResultEvent) {
	c.total++
	if e.Failed {
		c.failed++
	}
	if e.Filtered {
		c.filtered++
	}
	if e.Skipped {
		c.skipped++
	}
	c.EventReporter.SpecFinished(e)
}

// runPlanSpecsInOrder runs every spec in the plan once, in declaration order. Each spec's own
// before/body/after hooks are already flat — compiled into its own instruction range — so there is
// no group nesting to walk for those. Group hooks (BeforeAll/AfterAll, issue #207) are the one
// exception: plan.Groups is nil for a suite that registers neither (H10), and the fast path below
// (no groupState at all) is then exactly runPlanSpecsInOrder's pre-#207 body — same calls, same
// allocations. A suite that does register a group hook pays for groupState, sized once to
// len(plan.Groups), and this loop additionally opens/closes groups around the spec index they were
// attached to; see runGroupEnter/runGroupExit.
func runPlanSpecsInOrder(backend testBackend, rep report.EventReporter, plan *ExecutionPlan) {
	if len(plan.Groups) == 0 {
		for i := 0; i < len(plan.ProgramStart); i++ {
			runExecution(backend, rep, plan, i)
		}
		return
	}
	gs := &groupState{entered: make([]bool, len(plan.Groups)), failingGroup: -1}
	for i := 0; i < len(plan.ProgramStart); i++ {
		runGroupEnter(backend, rep, plan, gs, i)
		if gs.failingGroup != -1 {
			reportGroupSuppressedSpec(rep, plan, i, gs.failingGroup)
		} else {
			runExecution(backend, rep, plan, i)
		}
		runGroupExit(backend, rep, plan, gs, i)
	}
}

// groupState is the group-lifecycle bookkeeping threaded through one CompiledSuite.Run: which
// groups have been entered (so their AfterAll is owed, H5) and which group — if any — is currently
// the reason descendant specs are being skipped (H4). failingGroup is a single index, not a stack,
// because the moment a group's own BeforeAll fails, none of its descendant groups are ever entered
// at all (see runGroupEnter): there is at most one "currently failing ancestor" in scope anywhere
// in this plan's execution at a time, and it always resolves at that same group's own exit.
type groupState struct {
	entered      []bool
	failingGroup int
}

// runGroupEnter processes plan.GroupEnter[i]: every group whose first runnable spec is i. It reads
// the list in REVERSE — GroupEnter is built inner-scope-first (see ExecutionPlan.GroupEnter's doc
// comment) — so groups are entered outer-to-inner, per H2. A group is skipped entirely (never
// entered, its entered[g] left false) when an ancestor is already failing (H4: "every nested
// group's hooks" stay unentered); otherwise its BeforeAll hooks run, and a failure there makes it
// the new failingGroup for its own descendants.
func runGroupEnter(backend testBackend, rep report.EventReporter, plan *ExecutionPlan, gs *groupState, i int) {
	if i >= len(plan.GroupEnter) {
		return
	}
	entries := plan.GroupEnter[i]
	for k := len(entries) - 1; k >= 0; k-- {
		g := entries[k]
		if gs.failingGroup != -1 {
			continue // an ancestor is already failing: this group is never entered at all (H4)
		}
		gs.entered[g] = true
		group := &plan.Groups[g]
		message, output, failed := runGroupHookSequence(backend, group.BeforeAll, joinSubtestPath(group.Path, hookCaseName(hookKindBeforeAll)), true)
		if failed {
			gs.failingGroup = g
			reportHookCase(rep, group.Path, hookKindBeforeAll, message, output)
		}
	}
}

// runGroupExit processes plan.GroupExit[i]: every group whose last runnable spec is i. It reads
// the list FORWARD — GroupExit is built inner-scope-first, which already is the inner-to-outer
// teardown order H2 requires. A group that was never entered (skipped by an ancestor's failure)
// runs no AfterAll at all (H4/H5: the guarantee is scoped to a group that was actually entered).
// An entered group's AfterAll always runs, even if its own BeforeAll failed (H5) — every hook in
// its own list runs regardless of an earlier one failing (H6) — and once i is the group that
// introduced the current failure, that failure is cleared here: sibling groups after this point
// run normally again.
func runGroupExit(backend testBackend, rep report.EventReporter, plan *ExecutionPlan, gs *groupState, i int) {
	if i >= len(plan.GroupExit) {
		return
	}
	for _, g := range plan.GroupExit[i] {
		if gs.entered[g] {
			group := &plan.Groups[g]
			message, output, failed := runGroupHookSequence(backend, group.AfterAll, joinSubtestPath(group.Path, hookCaseName(hookKindAfterAll)), false)
			if failed {
				reportHookCase(rep, group.Path, hookKindAfterAll, message, output)
			}
		}
		if gs.failingGroup == g {
			gs.failingGroup = -1
		}
	}
}

// reportGroupSuppressedSpec reports spec i as skipped (H4: "reports every spec of the group ...
// as skipped, with a message naming the failing group"), without opening a subtest for it at all —
// the same convention the Builder engine already uses for a compile-time-skipped spec (see
// program.go's reportSkipped): only identity is reported, nothing runs.
func reportGroupSuppressedSpec(rep report.EventReporter, plan *ExecutionPlan, i, failingGroup int) {
	if rep == nil {
		return
	}
	name := specEventName(plan, i)
	path := specEventPath(plan, i)
	started := report.SpecStartEvent{Name: name, Path: path, Time: time.Now()}
	rep.SpecStarted(started)
	groupName := groupDisplayName(plan, failingGroup)
	rep.SpecFinished(report.SpecResultEvent{
		SpecStartEvent: started,
		Skipped:        true,
		Message:        fmt.Sprintf("skipped: BeforeAll failed for group %q", groupName),
	})
}

// groupDisplayName renders a group's Path the same way a spec's own breadcrumb reads, for the
// skipped-spec message above.
func groupDisplayName(plan *ExecutionPlan, g int) string {
	if g < 0 || g >= len(plan.Groups) {
		return ""
	}
	return joinSubtestPath(plan.Groups[g].Path, "")
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
// SpecStarted/SpecFinished pair whose Path is the group's own declared scope chain (the same Path
// a real spec declared directly in that group would carry, plus this hook's bracketed name as the
// leaf — the same shape specEventPath already gives a real spec). Its Hook field is set only on
// the SpecFinished event, never SpecStarted (report.SpecStartEvent carries no Hook field at all —
// see report/events.go's HookKind doc), so it marks the result structurally distinct from a real
// spec. Duration is always 0 — group hooks are not timed today, only whether they failed; only a
// failed hook produces a case at all (H8), which is why every call site above only reaches this
// when failed is true.
func reportHookCase(rep report.EventReporter, groupPath []string, kind report.HookKind, message, output string) {
	if rep == nil {
		return
	}
	name := hookCaseName(kind)
	path := append(append([]string(nil), groupPath...), name)
	started := report.SpecStartEvent{Name: name, Path: path, Time: time.Now()}
	rep.SpecStarted(started)
	rep.SpecFinished(report.SpecResultEvent{SpecStartEvent: started, Failed: true, Message: message, Output: output, Hook: kind})
}

// runGroupHookSequence runs hooks (one group's own BeforeAll or AfterAll list) in registration
// order, each isolated in its own subtest when backend wraps a real *testing.T — the same
// isolation a real spec body already gets (runSpecProgramIsolated) — so that ANY failure in one
// hook (an assertion via ctx, a panic, or Fatal/FailNow, i.e. runtime.Goexit) unwinds only that
// hook's own subtest goroutine, never a sibling group or the rest of the suite (H7).
//
// Isolation is deliberately per HOOK, not per whole list: an ordinary ctx.Expect(...) failure ends
// in backend.Fatalf, exactly like Fatal/FailNow, so it triggers runtime.Goexit too — which unwinds
// its goroutine immediately and never returns to a loop. Running the whole list in one shared
// subtest would therefore silently stop every hook after the first failing one, which is exactly
// right for H4 (BeforeAll) but exactly wrong for H6 ("the remaining AfterAlls ... still run").
// Giving every hook its own subtest is what makes both rules achievable from the same function.
//
// stopOnFailure selects H4 (BeforeAll: stop at the first failing hook in this group's own list) vs
// H6 (AfterAll: every hook in this group's own list still runs regardless of an earlier failure).
// message/output are the first failing hook's recovered panic pair; they stay "" for a plain
// ctx-assertion or Fatal/FailNow failure, exactly like a real spec's SpecResultEvent.Message does
// today (see events.go) — Goexit unwinds the goroutine before any such pair would be built. failed
// is true whenever any hook failed, read from that hook's own ctx.hasFailed() (see
// runGroupHookIsolated), which stays correct even when message/output are empty.
func runGroupHookSequence(backend testBackend, hooks []func(*Context), subtestName string, stopOnFailure bool) (message, output string, failed bool) {
	for _, h := range hooks {
		if h == nil {
			continue
		}
		m, o, hookFailed := runGroupHookIsolated(backend, h, subtestName)
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

// runGroupHookIsolated runs one group hook, isolated in its own subtest when backend wraps a real
// *testing.T (same fallback runSpecProgram already takes for anything else — a fake/controlled
// backend in a unit test, or a *testing.B). failed is read from ctx.hasFailed() after the subtest
// returns rather than from the closure's own return path, because a Fatal/FailNow-triggered
// runtime.Goexit inside it never reaches a return statement at all: ctx.failure is set before the
// Fatalf call that triggers Goexit, and that write survives past the subtest closure because ctx is
// a pointer shared across the isolation boundary — the same trick runSpecProgramIsolated relies on.
func runGroupHookIsolated(backend testBackend, fn func(*Context), subtestName string) (message, output string, failed bool) {
	real, ok := backend.(*runnableBackend)
	if !ok {
		return runGroupHookDirect(backend, fn)
	}
	t, ok := real.tb.(*testing.T)
	if !ok {
		return runGroupHookDirect(backend, fn)
	}
	ctx, release := acquireContext(nil)
	defer release()
	_, parked := runSubtestGuardingParallel(t, subtestName, func(subT *testing.T) {
		subBackend := asTestBackend(subT)
		defer putTestBackend(subBackend)
		ctx.Reset(subBackend)
		message, output = runGroupHookOnce(ctx, fn)
	})
	if parked {
		failUnsupportedSpecBodyParallel(t, ctx, subtestName)
	}
	failed = ctx.hasFailed()
	return
}

// runGroupHookDirect runs fn against a fresh Context for backend, with no *testing.T subtest to
// isolate into (a fake/controlled backend, or a *testing.B).
func runGroupHookDirect(backend testBackend, fn func(*Context)) (message, output string, failed bool) {
	ctx, release := acquireContext(backend)
	defer release()
	message, output = runGroupHookOnce(ctx, fn)
	failed = ctx.hasFailed()
	return
}

// runGroupHookOnce runs one hook, recovering a panic through the same single authority every other
// execution path in this engine uses (H7). A plain ctx-assertion failure or Fatal/FailNow leaves
// message/output empty here (recover() sees nothing to recover) — see runGroupHookSequence's doc
// comment for why that is still correctly reflected in ctx.hasFailed().
func runGroupHookOnce(ctx *Context, fn func(*Context)) (message, output string) {
	defer func() { message, output = recoverSpecFailure(ctx, recover(), "panic in group hook") }()
	fn(ctx)
	return
}

func runExecution(backend testBackend, rep report.EventReporter, plan *ExecutionPlan, i int) {
	start := plan.ProgramStart[i]
	length := plan.ProgramLen[i]
	if start+length > len(plan.Instructions) {
		return
	}
	program := plan.Instructions[start : start+length]
	name := specEventName(plan, i)
	// path feeds reportSpecStarted and nothing else, and that returns a zero event when rep is nil.
	// Building it unconditionally would cost one allocation per spec per run on the reporter-less
	// path — the one this package advertises as allocation-free.
	var path []string
	if rep != nil {
		path = specEventPath(plan, i)
	}
	ctx, release := acquireContext(backend)
	defer release()
	started := reportSpecStarted(rep, name, path)
	message, output, ran := runSpecProgram(backend, ctx, program, specSubtestName(plan, i))
	reportSpecFinished(rep, started, specResult{Failed: ctx.hasFailed(), Message: message, Output: output, Filtered: !ran})
}

// runSpecProgram runs program for one ExecutionPlan spec against ctx, isolated in its own subtest
// when backend wraps a real *testing.T (#74) — same gate runIsolatedCase already uses (see this
// function's mirror, runSpecRecovered, in runner.go) — so a real Fatalf/FailNow (runtime.Goexit)
// inside program unwinds only that subtest's goroutine, letting the remaining specs in this plan
// still run and be reported. A fake backend (e.g. controlledBackend, whose Run is a no-op) or a
// *testing.B falls back to running program directly against ctx/backend, same as before this
// isolation existed.
//
// subtestName is the spec's full Describe/When/It breadcrumb (specSubtestName), passed to
// testing.T.Run purely for -v/-run/IDE/test2json identity. It is never read back from t.Name():
// SpecStartEvent/SpecResultEvent keep taking Name from plan.Names and Path from plan.FullNames, so
// testing's sanitization (spaces to "_") and "#01" suffixing never leak into reported identity.
//
// ran is false exactly when external test selection (e.g. `go test -run`) discarded the subtest,
// threaded back so the caller can report the spec as Filtered instead of passed (#111). It cannot
// be read from t.Run's own bool return: testing.T.Run returns true for a filtered-out subtest too
// (a subtest that never ran vacuously "succeeded"), so runSpecProgramIsolated instead sets ran from
// inside the closure itself — which only runs at all when the filter accepted the subtest. The two
// fast paths above never go through t.Run at all, so they always ran.
func runSpecProgram(backend testBackend, ctx *Context, program []Instruction, subtestName string) (message, output string, ran bool) {
	real, ok := backend.(*runnableBackend)
	if !ok {
		ctx.Reset(backend)
		message, output = runProgram(program, ctx)
		return message, output, true
	}
	t, ok := real.tb.(*testing.T)
	if !ok {
		ctx.Reset(backend)
		message, output = runProgram(program, ctx)
		return message, output, true
	}
	return runSpecProgramIsolated(t, ctx, program, subtestName)
}

// runSpecProgramIsolated creates the real subtest and runs program inside it. Split out from
// runSpecProgram because the closure below captures named returns by reference: if it lived directly
// in runSpecProgram, Go's escape analysis would heap-allocate that function's message/output for
// every call — including the non-*testing.T fast paths above that never reach this line — since
// escape analysis decides a variable's storage class for the whole function, not per branch. Keeping
// the capture inside its own function scopes that heap allocation to the isolation path only (see the
// identical split for runner.go's runSpecRecovered/runSpecIsolated).
func runSpecProgramIsolated(t *testing.T, ctx *Context, program []Instruction, subtestName string) (message, output string, ran bool) {
	var parked bool
	ran, parked = runSubtestGuardingParallel(t, subtestName, func(subT *testing.T) {
		subBackend := asTestBackend(subT)
		defer putTestBackend(subBackend)
		ctx.Reset(subBackend)
		message, output = runProgram(program, ctx)
	})
	if parked {
		// The body called the unsupported ctx.T.Parallel(). ctx is still Reset to that subtest's
		// backend and the parked body needs it that way, so stop the run rather than let the caller
		// release ctx back to the pool underneath it (#172).
		failUnsupportedSpecBodyParallel(t, ctx, subtestName)
	}
	return
}

// specEventName returns plan.Names[i], or "" for a plan built without per-spec metadata (e.g. a
// hand-built ExecutionPlan in a test that only populates Instructions/ProgramStart/ProgramLen).
func specEventName(plan *ExecutionPlan, i int) string {
	if i < 0 || i >= len(plan.Names) {
		return ""
	}
	return plan.Names[i]
}

// specSubtestName returns the Go subtest identity for plan spec i: its full Describe/When/It
// breadcrumb (plan.FullNames[i]), not the leaf It name, so two specs sharing a leaf name under
// different scopes stay independently selectable with `go test -run` instead of being told apart
// only by testing's incidental "#01" suffix (#102). That holds whenever the two breadcrumbs differ
// once testing has normalized them; see joinSubtestPath for the mapping's contract and for the
// ambiguity it accepts and inherits from testing.T.Run.
//
// It falls back to plan.Names[i] for a plan built without breadcrumbs — a hand-built ExecutionPlan,
// or a compiler with an empty name stack, where the leaf name already is the whole breadcrumb.
//
// The empty string doubles as the "no breadcrumb" sentinel, which It("") declared outside any scope
// also produces. That case is deliberately not given a separate representation: both branches return
// "" for it, so the sentinel is indistinguishable from the value it stands for only where the two
// agree. Carrying a presence flag would cost a field on the exported ExecutionPlan — and a slice per
// plan — to encode a distinction nothing can observe.
func specSubtestName(plan *ExecutionPlan, i int) string {
	if i >= 0 && i < len(plan.FullNames) && plan.FullNames[i] != "" {
		return plan.FullNames[i]
	}
	return specEventName(plan, i)
}

// appendSpecPath records the scopes enclosing one spec and stores the window they occupy. Called
// once per spec, in the same order as Names/FullNames. The spec's own name is not passed: Names
// already holds it and specEventPath appends it.
//
// When scopes matches the window the previous spec was given — the common case, since every spec
// declared in the same block sees the same enclosing scopes — that window is reused instead of
// appending a second copy.
//
// Only the previous window is considered, deliberately: specs arrive in declaration order, so one
// comparison against the tail catches every run of siblings without a per-chain lookup. It does not
// catch a chain that recurs after an interruption, which then stores a second copy. See the
// PathScopes field comment for what that trade is and is not.
func appendSpecPath(plan *ExecutionPlan, scopes []string) {
	start := len(plan.PathScopes) - len(scopes)
	if start < 0 || !slices.Equal(plan.PathScopes[start:], scopes) {
		start = len(plan.PathScopes)
		plan.PathScopes = append(plan.PathScopes, scopes...)
	}
	plan.PathScopeStart = append(plan.PathScopeStart, start)
	plan.PathScopeLen = append(plan.PathScopeLen, len(scopes))
}

// specEventPath returns spec i's enclosing scopes followed by its own name, for
// SpecStartEvent.Path, or nil for a plan without per-spec metadata (see specEventName).
//
// The result is a fresh copy, never a window into plan.PathScopes: Path is handed to arbitrary
// report.EventReporter implementations, and one that sorts or truncates it in place would
// otherwise corrupt the plan for every later spec and every later run of the same CompiledSuite —
// and now that scope windows are shared, for every sibling spec as well.
func specEventPath(plan *ExecutionPlan, i int) []string {
	if i < 0 || i >= len(plan.PathScopeStart) || i >= len(plan.PathScopeLen) || i >= len(plan.Names) {
		return nil
	}
	start, length := plan.PathScopeStart[i], plan.PathScopeLen[i]
	if start < 0 || length < 0 || start+length > len(plan.PathScopes) {
		return nil
	}
	path := make([]string, 0, length+1)
	path = append(path, plan.PathScopes[start:start+length]...)
	return append(path, plan.Names[i])
}

// reportSpecStarted emits SpecStarted and returns the event it sent, so reportSpecFinished can
// reuse its Time — SpecResultEvent embeds SpecStartEvent, and that Time means when the spec
// started, not when it finished.
func reportSpecStarted(rep report.EventReporter, name string, path []string) report.SpecStartEvent {
	if rep == nil {
		return report.SpecStartEvent{}
	}
	e := report.SpecStartEvent{Name: name, Path: path, Time: time.Now()}
	rep.SpecStarted(e)
	return e
}

func reportSpecFinished(rep report.EventReporter, start report.SpecStartEvent, result specResult) {
	if rep == nil {
		return
	}
	duration := time.Since(start.Time)
	if result.Filtered {
		duration = 0
	}
	rep.SpecFinished(report.SpecResultEvent{
		SpecStartEvent: start,
		Failed:         result.Failed,
		Filtered:       result.Filtered,
		Duration:       duration,
		Message:        result.Message,
		Output:         result.Output,
	})
}

// runProgram executes one spec's instructions directly in the caller's goroutine (the default,
// non-path, non-isolated path — see runIsolatedCaseDirect for the path-generated equivalent).
// A panic anywhere in the before/body instructions is recovered so it fails this one spec instead
// of crashing the process; after-hook instructions still run regardless, each isolated from the
// others so one panicking after-hook doesn't stop the rest.
//
// On a recovered panic, message and output are built exactly once — message is the short
// "panic: value" summary, output is the raw stack trace — and reused both for reportRecoveredPanic
// (unchanged wire format: "message\noutput") and as the return value runExecutionContext feeds
// into reportSpecFinished's specResult; they are never reconstructed elsewhere. If the body didn't
// panic but an after-hook instruction (scoped to this one spec here, unlike the group-shared after
// hooks in runner.go) did, that after-hook's own message/output (built the same way, once, in
// runAfterInstructionRecovered) is used instead — first recorded panic wins, matching the
// first-write-wins convention parallelStep/parallelBackend already use. Both stay "" when the spec
// didn't panic at all, including when it failed via runtime.Goexit (a real testing.T.Fatalf/FailNow)
// — recover() cannot observe that case; see specResult's doc comment.
func runProgram(program []Instruction, ctx *Context) (message, output string) {
	var after []Instruction
	for _, inst := range program {
		if inst.Code == OpAfterHook && inst.Fn != nil {
			after = append(after, inst)
		}
	}
	defer func() {
		message, output = recoverSpecFailure(ctx, recover(), "panic")
		for _, inst := range after {
			m, o := runAfterInstructionRecovered(ctx, inst)
			if message == "" {
				message, output = m, o
			}
		}
	}()
	for _, inst := range program {
		if inst.Code == OpAfterHook {
			continue
		}
		if inst.Fn != nil {
			inst.Fn(ctx)
		}
	}
	return
}

// runAfterInstructionRecovered runs a single after-hook instruction, recovering any panic so it
// can't stop the remaining after-hooks for this spec. message/output follow the same build-once
// contract as runProgram's own panic recovery — see its doc comment.
func runAfterInstructionRecovered(ctx *Context, inst Instruction) (message, output string) {
	defer func() { message, output = recoverSpecFailure(ctx, recover(), "panic in after hook") }()
	inst.Fn(ctx)
	return
}

// isExpectedAbort reports whether a recovered value is the isolatedCaseAbort sentinel a
// controlled testBackend uses to stop one case via FailNow/Fatal/Fatalf — an already-recorded
// stop, not an unexpected panic to report.
func isExpectedAbort(recovered any) bool {
	_, ok := recovered.(isolatedCaseAbort)
	return ok
}

// isolatedCaseAbort lets a controlled backend stop one isolated case without
// terminating the parent test.
type isolatedCaseAbort struct{}
