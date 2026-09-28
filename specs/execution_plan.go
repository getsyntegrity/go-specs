// execution_plan.go defines the bytecode ExecutionPlan and CompiledSuite used by the compiler and runner.
package specs

import (
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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
	// hooks and groups carry the arena path's once-per-group hooks in and its compiled planGroups
	// out for the duration of one buildExecutionPlanFromArenaGroups call (issue #207). They live on
	// the pooled scratch rather than as extra recursion parameters or plan fields, so a suite
	// without group hooks pays nothing for them (H10); both are cleared before the call returns.
	hooks  *arenaGroupHooks
	groups *planGroups
	// hasFocus is set once per buildExecutionPlanFromArenaGroups call (issue #245): whether the
	// rootID subtree being built contains at least one FIt. Unlike the bytecode-compiler path,
	// which only learns this after the whole suite has been declared (see
	// bytecodeCompiler.applyFocusFilter), the arena is already a plain data tree with no side
	// effects left to trigger by the time a plan is built from it, so arenaHasFocus can scan it
	// first and let buildExecutionPlanFromArenaRec filter as it emits, in one pass.
	hasFocus bool
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
	buildExecutionPlanFromArenaGroups(arena, rootID, plan, scratch, nil)
}

// buildExecutionPlanFromArenaGroups is buildExecutionPlanFromArena for an arena whose scopes may
// carry once-per-group hooks (hooks, owned by the registry that built the arena — see
// arenaGroupHooks). It returns the compiled group bookkeeping, or nil when no scope registered a
// BeforeAll/AfterAll.
func buildExecutionPlanFromArenaGroups(arena *NodeArena, rootID int, plan *ExecutionPlan, scratch *planScratch, hooks *arenaGroupHooks) *planGroups {
	if arena == nil || plan == nil || scratch == nil {
		return nil
	}
	scratch.path = scratch.path[:0]
	scratch.hooks = hooks
	scratch.groups = nil
	scratch.hasFocus = arenaHasFocus(arena, rootID)
	buildExecutionPlanFromArenaRec(arena, rootID, plan, scratch)
	groups := scratch.groups
	scratch.hooks, scratch.groups = nil, nil
	scratch.hasFocus = false
	return groups
}

// arenaHasFocus reports whether any ItNode in the subtree rooted at nodeID is Kind itFocus (issue
// #245), scanning the already-built arena tree once before buildExecutionPlanFromArenaRec starts
// emitting — see planScratch.hasFocus for why this path can pre-scan while the bytecode compiler
// cannot.
func arenaHasFocus(arena *NodeArena, nodeID int) bool {
	if arena == nil || nodeID < 0 || nodeID >= len(arena.Nodes) {
		return false
	}
	if arena.Nodes[nodeID].Type == ItNode && arena.Nodes[nodeID].Kind == itFocus {
		return true
	}
	for _, cid := range arena.Children[nodeID] {
		if arenaHasFocus(arena, cid) {
			return true
		}
	}
	return false
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
		// A non-focused It/SkipIt/PendingIt is dropped entirely — no plan entry, no skip/pending
		// mark — whenever this Describe call registered at least one FIt (issue #245,
		// Builder.finalize's focus filter). A focused It runs exactly like a normal one below.
		focusedOut := scratch.hasFocus && node.Kind != itFocus
		switch {
		case focusedOut:
			// Nothing emitted; scratch.path is still popped below like any other node.
		case node.Kind == itSkip:
			registerSkipMark(&scratch.groups, name, markScopes(scratch.path, name))
		case node.Kind == itPending:
			registerPendingMark(&scratch.groups, name, markScopes(scratch.path, name))
		default: // itNormal, or itFocus (already confirmed focused above)
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
			appendSpecPath(plan, markScopes(scratch.path, name))
		}
	}
	// openParallel tracks a currently-open run of consecutive ItParallel siblings (issue #245's
	// second spec), scoped to this call's own children loop — i.e. to nodeID's direct children only.
	// It is a local variable, not scratch state, precisely so it resets on every recursive call: a
	// nested Describe/When processes its own children with its own fresh -1, which is what makes a
	// scope boundary end any run the parent had open, and starting one there never leaks back out to
	// the parent either. This is stricter than the documented rule ("ends at a BeforeAll/AfterAll
	// group boundary") — it also ends a run at a scope with no hooks at all — but never merges two
	// runs that rule would have kept apart, and it is what keeps a parallel range from ever
	// straddling a hookGroup's Start/End (see planGroups.parallel's doc comment), because both
	// build paths (this one and bytecodeCompiler's) apply exactly the same rule.
	openParallel := -1
	for _, cid := range arena.Children[nodeID] {
		child := &arena.Nodes[cid]
		wantParallel := child.Type == ItNode && child.Kind == itParallel
		if !wantParallel && openParallel >= 0 {
			registerParallelGroup(&scratch.groups, openParallel, len(plan.Names)-1)
			openParallel = -1
		}
		before := len(plan.Names)
		buildExecutionPlanFromArenaRec(arena, cid, plan, scratch)
		if wantParallel && len(plan.Names) > before && openParallel < 0 {
			// Focused out under a suite-wide FIt, len(plan.Names) does not grow (see focusedOut
			// above), so a focused-out ItParallel sibling neither opens nor extends a run — it is
			// invisible to grouping, exactly as it is invisible to the compiled plan.
			openParallel = len(plan.Names) - 1
		}
	}
	if openParallel >= 0 {
		registerParallelGroup(&scratch.groups, openParallel, len(plan.Names)-1)
	}
	// Close this node's own group hooks (issue #207), the arena-path equivalent of
	// bytecodeCompiler.closeGroupHooksAtTop: an ItNode has none of its own (BeforeAll/AfterAll are
	// never registered on a leaf spec), and the SuiteNode root is never exposed to the public
	// BeforeAll/AfterAll DSL surface, so both are skipped here defensively.
	if node.Type != ItNode && node.Type != SuiteNode {
		if before, after := scratch.hooks.of(nodeID); len(before) > 0 || len(after) > 0 {
			path := scratch.path
			if name == "" {
				// The registry path never pushes an empty name; record it so the rejection names the
				// group as declared (validateHookGroups).
				path = append(slices.Clip(path), "")
			}
			registerHookGroup(&scratch.groups, path, name, before, after, groupStart, len(plan.Names)-1)
		}
	}
	if name != "" && node.Type != SuiteNode && len(scratch.path) > 0 {
		scratch.path = scratch.path[:len(scratch.path)-1]
	}
}

// markScopes returns path's enclosing-scope prefix for a spec named name: scratch.path already has
// the spec's own name pushed as its last element (an ItNode is not a SuiteNode, so the push in
// buildExecutionPlanFromArenaRec applies to it too), so the scopes are everything before it. An
// unnamed spec was never pushed, so for it the whole of path is scopes. Shared by the real-spec
// case and by SkipIt/PendingIt marks (issue #245), which need the same computation.
func markScopes(path []string, name string) []string {
	if name == "" {
		return path
	}
	return path[:len(path)-1]
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
	// groups is the suite's BeforeAll/AfterAll bookkeeping, nil when it registers neither (see
	// planGroups for why it lives here rather than on ExecutionPlan). Unexported: a CompiledSuite
	// built by hand has no group hooks, which is exactly what nil means.
	groups *planGroups
}

// SetFailFast sets whether Run stops after the first spec, BeforeAll, or AfterAll that fails,
// mirroring Runner.FailFast (runner.go) for this engine (issue #251,
// docs/SUITE_HOOKS_CONTRACT.md H9). A sequential spec that fails still runs its own AfterEach;
// after it, no further spec or group starts, and a spec that never starts is not reported at all —
// the same contract Runner.FailFast already gives the Builder/Program engine. An already-launched
// ItParallel batch runs every one of its siblings to completion; the stop applies only once the
// whole batch has finished. A BeforeAll or AfterAll failure counts as a failure too, but the
// AfterAll of every group already entered still runs. A filtered (-run) or compile-time-skipped
// spec is not a failure and never triggers the stop.
//
// The flag lives behind the same lazily allocated groups pointer CompiledSuite already carries for
// its other optional bookkeeping (skipped/pending marks, hooked groups, ItParallel ranges), rather
// than as its own field: an exported bool the shape Runner.FailFast uses would push every
// CompiledSuite from 64 to 80 bytes and into the next allocation size class — a cost every suite
// would pay whether or not it uses fail-fast (H10, pinned by
// TestGroupHookStorageAddsNoBytesToAlwaysAllocatedStructs). Calling SetFailFast(false) on a suite
// that has never registered a BeforeAll/AfterAll/SkipIt/PendingIt/ItParallel, and has never called
// SetFailFast(true) either, leaves groups nil: it is never allocated just to remember "false".
func (s *CompiledSuite) SetFailFast(v bool) {
	if s == nil {
		return
	}
	if s.groups == nil {
		if !v {
			return
		}
		s.groups = &planGroups{}
	}
	s.groups.failFast = v
}

// Run executes all specs in the plan. Uses one context from the pool per spec.
func (s *CompiledSuite) Run(tb testing.TB) {
	s.run(tb, nil)
}

// RunShard runs shard shardIndex of shardCount of s for CI (issue #251): the `Describe`/`Spec`
// engine's counterpart to the `Builder` engine's package-level RunShard (scheduler.go), so a suite
// built through specs.Describe/BuildSuite (CompiledSuite over ExecutionPlan) can be split across CI
// jobs the same way a *Program built with Builder already can. It reports through s.Reporter exactly
// as Run does, so one method covers both the plain and the reporter-attached case; nothing about the
// shard is ever stored on s — the selection is computed here and dropped when RunShard returns.
//
// # Assignment unit
//
// Units are numbered u = 0..U-1, in declaration order, over three shapes:
//
//   - a whole top-level BeforeAll/AfterAll group, including every group nested inside it — its hooks
//     run exactly once for all of its specs (docs/SUITE_HOOKS_CONTRACT.md H1-H6), so the group can
//     never be split across shards without running a hook more than once or not at all;
//   - a whole ItParallel batch outside such a group — one concurrent unit, and H9's "the batch
//     finishes before a stop" is defined over it as a whole; a batch never straddles a hook group's
//     boundary (planGroups.parallel's doc comment), so this is always well-formed;
//   - a single spec otherwise — it has its own BeforeEach/AfterEach compiled into its own instruction
//     range and shares no state with any other spec, so it is sharded on its own.
//
// This is finer-grained than the Builder engine's package-level RunShard, which can only shard whole
// coalesced hook groups, and it balances shards better on a suite with few large groups. Unit u is
// assigned to shard u % shardCount — the same round-robin rule ShardSpecs, ShardBCProgram and
// Builder's RunShard already use, so the same declared tree always yields the same units and the same
// assignment, on both compile paths (bytecode compiler and Analyze/registry).
//
// # Validation
//
// shardCount must be >= 1 and 0 <= shardIndex < shardCount; anything else fails tb through tb.Fatalf
// with the shared validateShardPartition diagnostic (#174), exactly like the Builder engine's
// RunShard, instead of silently running the whole suite on every worker. s, s.Plan or tb being nil is
// a silent no-op, mirroring Run.
//
// # Reporting
//
// This shard's own specs run and report exactly as they do under Run, including Filtered when -run
// discards one. A spec belonging to another shard's unit is never started at all: no hook runs for
// it, no reporter event is emitted for it, and SuiteEndEvent.TotalSpecs/FailedSpecs count only this
// shard's own specs — the same contract the Builder engine's RunShardWithReporter already gives.
// Compile-time SkipIt/PendingIt marks have no plan index and belong to no shard's units; shard 0
// alone reports them, so the union across every shard reports each one exactly once. A valid shard
// that draws nothing at all — more shards than units, or a non-zero shard with no marks — runs
// nothing and emits no suite events, which is not a failure: it mirrors Run's own early return for an
// empty suite, and the Builder engine's RunShard/RunShardWithReporter for an empty shard.
//
// # FailFast
//
// CompiledSuite.SetFailFast(true) applies within this shard only: a failure stops the rest of this
// shard's own units; other shards are other processes and never see it.
func (s *CompiledSuite) RunShard(tb testing.TB, shardIndex, shardCount int) {
	if s == nil || s.Plan == nil || tb == nil {
		return
	}
	if !shardConfigOK(tb, shardIndex, shardCount) {
		return
	}
	s.run(tb, s.buildShardSelection(shardIndex, shardCount))
}

// run is the shared body of Run and RunShard (issue #251). sel == nil is Run's own case, "every
// spec" — the only case TestDescribeEngineLoopAllocatesNothingPerSpecOnTheFlatPath covers, and the
// one this function must keep allocation-free: building a shardSelection happens only in RunShard,
// never here. A non-nil sel restricts the run to one shard's units; see shardSelection and
// buildShardSelection for how it is built and what it filters.
func (s *CompiledSuite) run(tb testing.TB, sel *shardSelection) {
	if s == nil || s.Plan == nil || tb == nil {
		return
	}
	if len(s.Plan.ProgramStart) == 0 && !s.hasMarks() {
		return
	}
	if !sel.reportsMarks() && !sel.anySelected() {
		// RunShard only (sel != nil here, since a nil sel always reportsMarks): this shard draws
		// nothing — no marks to report (shard 0's job) and no unit assigned to it. A valid shard
		// that draws nothing runs nothing and emits no suite events at all; that is not a failure
		// (RunShard's own doc comment, "Reporting").
		return
	}
	if observe := suiteRunObserver.Load(); observe != nil {
		(*observe)(s)
	}
	backend := asTestBackend(tb)
	defer putTestBackend(backend)

	if s.Reporter == nil {
		s.runSpecs(backend, nil, sel)
		return
	}
	// counter observes every SpecFinished event to total TotalSpecs/FailedSpecs for SuiteEndEvent:
	// -run filtering can still make the reported count differ from len(plan.ProgramStart), and so
	// can sel excluding another shard's specs (RunShard, issue #251).
	counter := &specCounter{EventReporter: s.Reporter}
	name := s.Name
	if name == "" {
		name = backend.Name()
	}
	suiteStart := time.Now()
	s.Reporter.SuiteStarted(report.SuiteStartEvent{Name: name, Time: suiteStart})
	s.runSpecs(backend, counter, sel)
	s.Reporter.SuiteFinished(report.SuiteEndEvent{
		Name:           name,
		Time:           time.Now(),
		Duration:       time.Since(suiteStart),
		TotalSpecs:     counter.total,
		FailedSpecs:    counter.failed,
		FilteredSpecs:  counter.filtered,
		SkippedSpecs:   counter.skipped,
		PendingSpecs:   counter.pending,
		UnstartedSpecs: counter.unstarted,
	})
}

// shardSelection restricts CompiledSuite.run to one shard's assignment units (RunShard, issue #251).
// specs[i] is true when flat-plan index i belongs to a unit this shard draws; every index belonging
// to an excluded unit stays false (the zero value). Built once, fresh, inside RunShard by
// buildShardSelection, and always discarded when RunShard returns: nothing here is ever stored on
// CompiledSuite, ExecutionPlan or planGroups, so Run's own path (sel == nil throughout run/runSpecs/
// runPlanSpecsInOrder/runRange) never allocates one and never pays for sharding it never asked
// for.
type shardSelection struct {
	specs      []bool
	shardIndex int
}

// included reports whether flat-plan index i belongs to this shard. A nil sel (Run's own case)
// always reports true — "every spec", with no slice to index into — so every call site can treat a
// nil sel exactly like "no sharding" without a separate branch.
func (sel *shardSelection) included(i int) bool {
	if sel == nil {
		return true
	}
	return i >= 0 && i < len(sel.specs) && sel.specs[i]
}

// reportsMarks reports whether this run is the one that reports compile-time SkipIt/PendingIt marks
// (RunShard's decision: shard 0 only, so the union across every shard reports each mark exactly
// once). A nil sel (Run's own case) always reports true: there is only one run, and it always
// reports them, exactly as it did before RunShard existed.
func (sel *shardSelection) reportsMarks() bool {
	return sel == nil || sel.shardIndex == 0
}

// anySelected reports whether this shard's selection includes at least one flat-plan index. A nil
// sel (Run's own case) always reports true.
func (sel *shardSelection) anySelected() bool {
	if sel == nil {
		return true
	}
	for _, included := range sel.specs {
		if included {
			return true
		}
	}
	return false
}

// buildShardSelection computes shard shardIndex's subset of s's units (RunShard's assignment rule):
// a whole top-level BeforeAll/AfterAll group (with every group nested inside it), a whole ItParallel
// batch outside such a group, or a single spec otherwise, numbered u = 0..U-1 in declaration order
// and assigned to shard u % shardCount. Callers must have validated shardIndex/shardCount first (see
// shardConfigOK); this never fails — it only computes an assignment, which may legitimately draw
// nothing (see run's empty-shard handling).
//
// On the flat path — no BeforeAll/AfterAll and no ItParallel, CompiledSuite.runSpecs' own condition
// for it — every unit is a single spec at its own plan index, so the unit number and the plan index
// are the same number and no tree walk is needed. Otherwise a throwaway groupRun.buildTree() gives
// the exact top-level walk runRange itself later does (r.top, r.parallelByStart), so the two can
// never disagree about where one unit ends and the next begins: a top-level group's unit spans
// group.Start..group.End (every nested group falls inside that range by construction — see
// buildTree's doc comment), a top-level ItParallel batch spans its own Start..End, and every other
// top-level index is a single-spec unit.
//
// Allocates a []bool the length of the flat plan (and, on the group path, the throwaway groupRun's
// own bookkeeping); only ever called from RunShard, never from Run's own path.
func (s *CompiledSuite) buildShardSelection(shardIndex, shardCount int) *shardSelection {
	n := len(s.Plan.ProgramStart)
	sel := &shardSelection{specs: make([]bool, n), shardIndex: shardIndex}
	assign := func(u, start, end int) {
		if u%shardCount != shardIndex {
			return
		}
		for i := start; i <= end; i++ {
			sel.specs[i] = true
		}
	}
	if s.groups == nil || (len(s.groups.groups) == 0 && len(s.groups.parallel) == 0) {
		for i := 0; i < n; i++ {
			assign(i, i, i)
		}
		return sel
	}
	r := &groupRun{pg: s.groups, plan: s.Plan}
	r.buildTree()
	u := 0
	k := 0
	for i := 0; i < n; {
		if k < len(r.top) && s.groups.groups[r.top[k]].Start == i {
			g := r.top[k]
			k++
			group := &s.groups.groups[g]
			assign(u, group.Start, group.End)
			u++
			i = group.End + 1
			continue
		}
		if pi, ok := r.parallelByStart[i]; ok {
			rng := s.groups.parallel[pi]
			assign(u, rng.Start, rng.End)
			u++
			i = rng.End + 1
			continue
		}
		assign(u, i, i)
		u++
		i++
	}
	return sel
}

// hasMarks reports whether s registered at least one compile-time SkipIt/PendingIt (issue #245):
// such a suite may have zero entries in Plan.ProgramStart (e.g. a suite made only of SkipIt calls)
// yet still needs Run to report those marks instead of returning early as an empty suite.
func (s *CompiledSuite) hasMarks() bool {
	return s.groups != nil && (len(s.groups.skipped) > 0 || len(s.groups.pending) > 0)
}

// suiteRunObserver, when set, sees every CompiledSuite right before it runs. It exists only so this
// package's tests can inspect a suite that an entry point such as Describe builds and runs without
// ever returning it — group_hook_cost_test.go uses it to prove H10 on every entry point. Unset in
// production, where it costs one atomic load per suite run and allocates nothing.
var suiteRunObserver atomic.Pointer[func(*CompiledSuite)]

// specCounter decorates an EventReporter to tally executed/failed/filtered specs for the enclosing
// suite's SuiteEndEvent, then forwards every event unchanged to the underlying reporter.
type specCounter struct {
	report.EventReporter
	total    int
	failed   int
	filtered int
	// skipped counts a spec reported Skipped: true — a spec suppressed by an ancestor group's
	// failed BeforeAll (issue #207 H4), or a compile-time SkipIt/Skip mark (issue #245).
	skipped int
	// pending counts a spec reported Pending: true — a compile-time PendingIt/Pending mark (issue
	// #245); SuiteEndEvent.PendingSpecs existed since #208 but this engine never populated it until
	// this field did.
	pending int
	// unstarted counts a spec reported Unstarted: true — a spec FailFast prevented from ever being
	// reached (issue #274). Counted separately, never folded into total: SuiteEndEvent.TotalSpecs
	// keeps its pre-existing meaning of "specs that entered execution, plus declared
	// SkipIt/PendingIt that were actually processed" (report.Totals.add's doc comment states the
	// same rule on the report side).
	unstarted int
}

func (c *specCounter) SpecFinished(e report.SpecResultEvent) {
	if e.Unstarted {
		c.unstarted++
		c.EventReporter.SpecFinished(e)
		return
	}
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
	if e.Pending {
		c.pending++
	}
	c.EventReporter.SpecFinished(e)
}

// runSpecs runs every spec of the suite once: it first reports this suite's compile-time
// SkipIt/PendingIt marks (issue #245), if any and if sel says this run reports them (RunShard, issue
// #251, restricts that to shard 0 — see shardSelection.reportsMarks) — they carry no before/body/
// after and never open a subtest, so they are reported independently of whichever path runs the real
// specs below — then runs the real specs through runPlanSpecsInOrder, unchanged from before issue
// #207, for a suite without any BeforeAll/AfterAll group and without any ItParallel group (H10), or
// through runPlanWithGroups otherwise — runPlanWithGroups is also where an ItParallel range is
// launched concurrently (issue #245's second spec; see group_hooks.go's runParallelGroup). sel
// restricts either path to one shard's units; nil means every spec (Run's own case).
func (s *CompiledSuite) runSpecs(backend testBackend, rep report.EventReporter, sel *shardSelection) {
	if s.groups != nil && sel.reportsMarks() {
		reportMarks(rep, s.groups.skipped, false)
		reportMarks(rep, s.groups.pending, true)
	}
	if s.groups == nil || (len(s.groups.groups) == 0 && len(s.groups.parallel) == 0) {
		// failFast is read directly off s.groups rather than through a helper: s.groups may be
		// non-nil here purely because it carries skipped/pending marks or because SetFailFast(true)
		// was called with no group/parallel ever registered (issue #251) — either way this is still
		// the flat path, and s.groups.failFast is false unless SetFailFast(true) was actually called.
		failFast := s.groups != nil && s.groups.failFast
		runPlanSpecsInOrder(backend, rep, s.Plan, failFast, sel)
		return
	}
	runPlanWithGroups(backend, rep, s.Plan, s.groups, sel)
}

// runPlanSpecsInOrder runs every spec in the plan once, in declaration order — or, when sel is
// non-nil (RunShard, issue #251), only the specs sel selects for this shard: an excluded index is
// skipped entirely, with no report and no effect on failFast, exactly as if it had never been
// declared. On this path (no BeforeAll/AfterAll, no ItParallel) every spec is its own sharding unit
// and its own plan index (buildShardSelection), so filtering by plan index is exactly filtering by
// unit. Each spec's own before/body/after hooks are already flat — compiled into its own instruction
// range — so there is no group nesting to walk. A suite with BeforeAll/AfterAll never reaches here;
// see runPlanWithGroups.
//
// failFast implements CompiledSuite.SetFailFast (issue #251) for this path: once a spec fails, the
// loop returns before starting the next one, so the specs after it never run and are never
// reported at all — mirroring Runner.FailFast's stop-at-the-next-check contract (runner.go). A
// spec's own AfterEach still runs regardless, since it is part of that spec's own instruction
// range (runProgram's deferred loop), not a separate step this loop could skip.
func runPlanSpecsInOrder(backend testBackend, rep report.EventReporter, plan *ExecutionPlan, failFast bool, sel *shardSelection) {
	for i := 0; i < len(plan.ProgramStart); i++ {
		if !sel.included(i) {
			continue
		}
		failed := runExecution(backend, rep, plan, i)
		if failFast && failed {
			reportRemainingUnstarted(rep, plan, i+1, sel)
			return
		}
	}
}

// reportRemainingUnstarted reports plan[from:] as Unstarted (issue #274): a FailFast stop means
// none of these specs will ever run. sel restricts this to the specs this shard actually owns
// (RunShard, issue #251): a shard reports only the unstarted specs of its own units, never a
// sibling shard's, which sel.included already excludes exactly as it does for the normal run.
func reportRemainingUnstarted(rep report.EventReporter, plan *ExecutionPlan, from int, sel *shardSelection) {
	if rep == nil {
		return
	}
	for i := from; i < len(plan.ProgramStart); i++ {
		if sel.included(i) {
			reportSpecUnstarted(rep, plan, i)
		}
	}
}

// reportSpecUnstarted reports plan spec i as Unstarted (issue #274): a single SpecStarted +
// SpecFinished{Unstarted: true} pair, with no body ever run — the same identity-only shape
// reportSpecStarted/reportSpecFinished already use for a real spec, built directly here since
// there is no specResult for a spec whose body never ran at all.
func reportSpecUnstarted(rep report.EventReporter, plan *ExecutionPlan, i int) {
	if rep == nil {
		return
	}
	name := specEventName(plan, i)
	path := specEventPath(plan, i)
	started := report.SpecStartEvent{Name: name, Path: path, Time: time.Now()}
	rep.SpecStarted(started)
	rep.SpecFinished(report.SpecResultEvent{SpecStartEvent: started, Unstarted: true})
}

// runExecution runs plan spec i and reports it, returning whether it failed — false for a spec
// that a -run filter discarded (Filtered) or a compile-time/runtime skip suppressed, exactly like
// report.SpecResultEvent.Failed itself (see runSpecProgram) — so runPlanSpecsInOrder's FailFast
// check above never mistakes either for a failure.
func runExecution(backend testBackend, rep report.EventReporter, plan *ExecutionPlan, i int) bool {
	start := plan.ProgramStart[i]
	length := plan.ProgramLen[i]
	if start+length > len(plan.Instructions) {
		return false
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
	ctx := acquireContext(backend)
	defer releaseContext(ctx)
	started := reportSpecStarted(rep, name, path)
	message, output, ran, failed, skipped := runSpecProgram(backend, ctx, program, specSubtestName(plan, i))
	reportSpecFinished(rep, started, specResult{Failed: failed, Message: message, Output: output, Filtered: !ran, Skipped: skipped})
	return failed
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
//
// failed is the spec's outcome. On the fast paths it is the Context's own failure record, the only
// place a failure can land there. On the isolation path it also folds in the subtest's own Failed(),
// because a body that fails only through ctx.T (Error, Fatal, Fail, FailNow) marks the subtest
// failed without touching the Context (#253).
//
// skipped is always false on the fast paths — neither a fake testBackend nor a *testing.B gives the
// body a real subtest to skip, so there is nothing for ctx.T.Skip to act on. On the isolation path it
// is the subtest's own Skipped(), and false whenever failed is true (#254): see
// runSpecProgramIsolated for where that fold happens.
func runSpecProgram(backend testBackend, ctx *Context, program []Instruction, subtestName string) (message, output string, ran, failed, skipped bool) {
	real, ok := backend.(*runnableBackend)
	if !ok {
		ctx.Reset(backend)
		message, output = runProgram(program, ctx)
		failed = ctx.hasFailed()
		return ctx.assertionMessage(message, failed), output, true, failed, false
	}
	t, ok := real.tb.(*testing.T)
	if !ok {
		ctx.Reset(backend)
		message, output = runProgram(program, ctx)
		failed = ctx.hasFailed()
		return ctx.assertionMessage(message, failed), output, true, failed, false
	}
	return runSpecProgramIsolated(t, ctx, program, subtestName)
}

// runSpecProgramIsolated creates the real subtest and runs program inside it.
//
// It deliberately does not go through spec_body_parallel.go's runSubtestGuardingParallel, unlike
// runner.go's runSpecIsolated and group_hooks.go's two call sites. That helper is written to be
// reusable, so it builds its bookkeeping struct and wrapping closure fresh on every call — exactly
// once per spec, since a fresh closure is the only way to hand it a fresh body. On this path,
// though, the same *Context is reused spec to spec (contextPool; see acquireContext), and program is
// the only thing that actually changes between calls, so this function inlines the same guarding
// logic against fields on ctx instead (isoStarted, isoDone, isoSub, isoProgram, isoMessage,
// isoOutput — see their doc comment on Context) and builds the subtest closure itself, ctx.isoRun,
// only once per Context rather than once per spec (#244). Before this cache, every spec here paid
// five allocations testing.T.Run itself does not charge: the message/output pair escaping as
// closure-captured named returns, that closure's own funcval, runSubtestGuardingParallel's
// bookkeeping struct, and its wrapping closure. ctx.isoRun closes only over ctx, so once it exists it
// is valid for every later spec that lands on this same pooled Context, whichever suite it belongs
// to — runSpecProgramIsolated always repopulates isoProgram and resets isoStarted/isoDone
// immediately before every t.Run call, so nothing from a previous spec leaks into the next one.
//
// ctx stays bound to the subtest's backend until t.Run has returned, and the backend goes back to its
// pool only then, not in a defer inside the closure. testing runs the subtest's Cleanup functions
// after the closure has returned, so a cleanup the body registered, such as
// ctx.T.Cleanup(func() { ctx.Expect(x).ToEqual(y) }), still needs ctx pointing at this subtest's own
// live backend; a defer would already have cleared it and handed it to the pool (#253). The backend
// is read back from ctx.backend, which ctx.Reset set to it, rather than from a variable the closure
// captures, so this adds no allocation.
func runSpecProgramIsolated(t *testing.T, ctx *Context, program []Instruction, subtestName string) (message, output string, ran, failed, skipped bool) {
	if ctx.isoRun == nil {
		ctx.isoRun = func(subT *testing.T) {
			ctx.isoSub = subT
			ctx.isoStarted.Store(true)
			defer ctx.isoDone.Store(true)
			ctx.Reset(asTestBackend(subT))
			ctx.isoMessage, ctx.isoOutput = runProgram(ctx.isoProgram, ctx)
		}
	}
	ctx.isoProgram = program
	// runProgram may end through runtime.Goexit before assigning the cached results.
	// A filtered subtest never runs the closure at all: t.Run reports shouldRun=false and returns
	// without ever invoking ctx.isoRun, so ctx.Reset (inside the closure) never runs either. In either
	// case, the next spec must not inherit a previous spec's panic text, stack trace or failure bit —
	// resetFailure covers ctx.failure the same way the isoMessage/isoOutput/isoStarted/isoDone resets
	// below cover their own fields, so this function's isolation holds on its own and does not depend
	// on a caller (such as runExecution's acquireContext) having reset ctx first.
	ctx.resetFailure()
	ctx.isoMessage, ctx.isoOutput = "", ""
	ctx.isoStarted.Store(false)
	ctx.isoDone.Store(false)
	t.Run(subtestName, ctx.isoRun)
	ran = ctx.isoStarted.Load()
	parked := ran && !ctx.isoDone.Load()
	if ran && !parked {
		failed = ctx.isoSub.Failed()
		skipped = ctx.isoSub.Skipped()
	}
	message, output = ctx.isoMessage, ctx.isoOutput
	if parked {
		// The body called the unsupported ctx.T.Parallel(). ctx is still Reset to that subtest's
		// backend and the parked body needs it that way, so stop the run rather than let the caller
		// release ctx back to the pool underneath it (#172).
		failUnsupportedSpecBodyParallel(t, ctx, subtestName)
	}
	failed = failed || ctx.hasFailed()
	// skipped is folded against this final failed, not the raw subtest failed the guard above
	// computed, so it always agrees with the Failed value this function actually reports: a body that
	// fails and then calls SkipNow must be reported Failed, not Skipped (#254), whichever of the
	// subtest or the Context recorded that failure.
	skipped = skipped && !failed
	// message stays "" here exactly when the body returned via runtime.Goexit (a real Fatalf/FailNow)
	// with nothing recovered, so ctx.assertionMessage falls back to the built-in assertion text failf
	// recorded on ctx before that Goexit — the only way this function can still report it (#272).
	message = ctx.assertionMessage(message, failed)
	if ran {
		putTestBackend(ctx.backend)
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
		Skipped:        result.Skipped,
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
