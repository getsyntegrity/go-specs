package specs

import "sync"

// bytecodeCompiler emits instructions directly into an ExecutionPlan during Describe.
// No NodeArena is allocated; BeforeEach/AfterEach/It append instructions immediately.
type bytecodeCompiler struct {
	plan        *ExecutionPlan
	nameStack   []string
	beforeStack [][]func(*Context)
	afterStack  [][]func(*Context)
	// groupScopes holds each open scope's own once-per-group hooks (issue #207), nil until the
	// first BeforeAll/AfterAll this pooled compiler ever sees, and kept (emptied) across reuse after
	// that. Unlike beforeStack/afterStack these are never flattened via flattenHooks: a
	// BeforeAll/AfterAll belongs to exactly the scope it was registered on (H1), so it is read
	// directly, once, when that scope closes (see closeGroupHooksAtTop).
	groupScopes *compilerGroupScopes
	// groupStartStack[i] is len(plan.Names) at the moment PushScope opened scope i — the index of
	// the first spec that scope's subtree could contribute. Paired with len(plan.Names)-1 when the
	// scope closes, it is the spec range that scope's own BeforeAll/AfterAll (if any) enters/exits.
	// It is the one piece of group bookkeeping kept for every scope, because a scope's start cannot
	// be recovered after the fact once a hook is registered; it is one int per open scope on a
	// pooled slice, so it allocates nothing per suite.
	groupStartStack []int
	// groups is the plan's compiled group bookkeeping, nil unless a scope with a group hook closed
	// with at least one spec (see registerHookGroup), or a SkipIt/PendingIt was registered
	// somewhere in the suite (see registerSkipMark/registerPendingMark, issue #245); handed to the
	// caller by takePlanAndGroups.
	groups *planGroups
	// focusIndices holds plan.Names indices emitted by FIt (issue #245); nil unless the suite
	// registers at least one. Non-empty at TakePlan time means applyFocusFilter must run.
	focusIndices []int
	// openParallelStart is the plan.Names index a currently-open run of consecutive ItParallel specs
	// began at (issue #245's second spec), or -1 when no run is open. Unlike the arena path (whose
	// per-node children loop resets this for free on every recursive call — see
	// buildExecutionPlanFromArenaRec), this compiler emits linearly as DSL calls happen, so
	// closeOpenParallelRun has to be called explicitly at every point that ends a run: PushScope,
	// PopScope, TakePlan (the root scope's close), and every non-ItParallel spec registration
	// (It/FIt/SkipIt/PendingIt, called from spec.go before the matching Emit* method). See
	// closeOpenParallelRun for why ending at every scope transition — not just a hooked one — is
	// still correct.
	openParallelStart int
	// scratch for flattening hooks and building program
	beforeFlat []func(*Context)
	afterFlat  []func(*Context)
	program    []Instruction
}

const compilerInitialSpecCap = 64
const compilerInitialInstructionsCap = 512

var bytecodeCompilerPool = sync.Pool{
	New: func() any {
		return &bytecodeCompiler{
			nameStack:       make([]string, 0, 16),
			beforeStack:     make([][]func(*Context), 0, 8),
			afterStack:      make([][]func(*Context), 0, 8),
			groupStartStack: make([]int, 0, 8),
			beforeFlat:      make([]func(*Context), 0, 32),
			afterFlat:       make([]func(*Context), 0, 32),
			program:         make([]Instruction, 0, 32),
		}
	},
}

func newBytecodeCompiler() *bytecodeCompiler {
	c := bytecodeCompilerPool.Get().(*bytecodeCompiler)
	c.plan = newExecutionPlan(compilerInitialSpecCap)
	c.plan.Instructions = make([]Instruction, 0, compilerInitialInstructionsCap)
	c.nameStack = c.nameStack[:0]
	c.beforeStack = c.beforeStack[:0]
	c.afterStack = c.afterStack[:0]
	c.groupScopes.truncate(0)
	c.groupStartStack = c.groupStartStack[:0]
	c.groups = nil
	c.focusIndices = nil
	c.openParallelStart = -1
	return c
}

func (c *bytecodeCompiler) reset() {
	c.plan = nil
	c.nameStack = c.nameStack[:0]
	c.beforeStack = c.beforeStack[:0]
	c.afterStack = c.afterStack[:0]
	c.groupScopes.truncate(0)
	c.groupStartStack = c.groupStartStack[:0]
	c.groups = nil
	c.focusIndices = nil
	c.openParallelStart = -1
	bytecodeCompilerPool.Put(c)
}

// PushScope enters a Describe/When block. Call PopScope when the block callback returns.
func (c *bytecodeCompiler) PushScope(name string) {
	c.closeOpenParallelRun()
	c.nameStack = append(c.nameStack, name)
	c.beforeStack = append(c.beforeStack, nil)
	c.afterStack = append(c.afterStack, nil)
	c.groupStartStack = append(c.groupStartStack, len(c.plan.Names))
}

// PopScope exits the current Describe/When block, closing that scope's own group hooks (H1-H3)
// before discarding its bookkeeping. The root scope opened by describeWithCompiler/BuildSuite is
// never popped this way (nothing calls PopScope for it) — see TakePlan, which closes it instead.
func (c *bytecodeCompiler) PopScope() {
	c.closeOpenParallelRun()
	c.closeGroupHooksAtTop()
	if n := len(c.nameStack); n > 0 {
		c.nameStack = c.nameStack[:n-1]
	}
	if n := len(c.beforeStack); n > 0 {
		c.beforeStack = c.beforeStack[:n-1]
	}
	if n := len(c.afterStack); n > 0 {
		c.afterStack = c.afterStack[:n-1]
	}
	c.groupScopes.truncate(len(c.nameStack))
	if n := len(c.groupStartStack); n > 0 {
		c.groupStartStack = c.groupStartStack[:n-1]
	}
}

// closeGroupHooksAtTop registers the currently-open innermost scope's own BeforeAll/AfterAll (if
// any) against the plan, entered/exited around the spec range that scope's subtree contributed —
// [groupStartStack top, len(plan.Names)-1]. A scope with neither hook, or with zero specs in that
// range (H3), registers nothing. Shared between PopScope (nested scopes) and TakePlan (the root
// scope, which is never popped).
func (c *bytecodeCompiler) closeGroupHooksAtTop() {
	n := len(c.nameStack)
	if n == 0 {
		return
	}
	before, after := c.groupScopes.at(n - 1)
	if len(before) == 0 && len(after) == 0 {
		return
	}
	start := c.groupStartStack[n-1]
	end := len(c.plan.Names) - 1
	registerHookGroup(&c.groups, c.nameStack, c.nameStack[n-1], before, after, start, end)
}

// closeOpenParallelRun ends the compiler's currently open ItParallel run, if any (issue #245's
// second spec), registering it into c.groups — the same lazily allocated pointer skip/pending marks
// and hook groups already share (H10). Called from PushScope, PopScope, TakePlan (the root scope's
// close) and from spec.go before every non-ItParallel registration (It, FIt, SkipIt, PendingIt): a
// run is only ever built from ItParallel specs registered directly next to each other, with nothing
// else — not even an empty, hook-free Describe/When — between them. See the field comment on
// openParallelStart for why ending at every scope transition, not just a hooked one, still agrees
// with the arena/registry build path.
func (c *bytecodeCompiler) closeOpenParallelRun() {
	if c.openParallelStart < 0 {
		return
	}
	registerParallelGroup(&c.groups, c.openParallelStart, len(c.plan.Names)-1)
	c.openParallelStart = -1
}

// AppendBefore adds a before-each hook to the current scope.
func (c *bytecodeCompiler) AppendBefore(fn func(*Context)) {
	if fn == nil {
		return
	}
	if len(c.beforeStack) == 0 {
		return
	}
	i := len(c.beforeStack) - 1
	c.beforeStack[i] = append(c.beforeStack[i], fn)
}

// AppendAfter adds an after-each hook to the current scope.
func (c *bytecodeCompiler) AppendAfter(fn func(*Context)) {
	if fn == nil {
		return
	}
	if len(c.afterStack) == 0 {
		return
	}
	i := len(c.afterStack) - 1
	c.afterStack[i] = append(c.afterStack[i], fn)
}

// AppendBeforeAll adds a once-per-group setup hook to the current scope (issue #207). Unlike
// AppendBefore, this is never flattened onto descendant specs — it is read directly, once, when
// this scope closes (see closeGroupHooksAtTop).
func (c *bytecodeCompiler) AppendBeforeAll(fn func(*Context)) {
	if fn == nil || len(c.nameStack) == 0 {
		return
	}
	c.scopesForHooks().append(&c.groupScopes.before, len(c.nameStack)-1, fn)
}

// AppendAfterAll adds a once-per-group teardown hook to the current scope (issue #207).
func (c *bytecodeCompiler) AppendAfterAll(fn func(*Context)) {
	if fn == nil || len(c.nameStack) == 0 {
		return
	}
	c.scopesForHooks().append(&c.groupScopes.after, len(c.nameStack)-1, fn)
}

// scopesForHooks returns c.groupScopes, allocating it the first time this pooled compiler sees a
// group hook.
func (c *bytecodeCompiler) scopesForHooks() *compilerGroupScopes {
	if c.groupScopes == nil {
		c.groupScopes = &compilerGroupScopes{}
	}
	return c.groupScopes
}

// compilerGroupScopes holds the BeforeAll/AfterAll hooks of the compiler's open scopes, indexed by
// scope depth (0 is the root). Neither slice is kept aligned with nameStack: each grows only as
// deep as the deepest scope that registered a hook, and PopScope truncates both back to the
// remaining depth, so a suite pays for this only once it registers a group hook (H10).
type compilerGroupScopes struct {
	before [][]func(*Context)
	after  [][]func(*Context)
}

// append adds fn to (*stack)[depth], growing *stack with empty scopes as needed.
func (g *compilerGroupScopes) append(stack *[][]func(*Context), depth int, fn func(*Context)) {
	for len(*stack) <= depth {
		*stack = append(*stack, nil)
	}
	(*stack)[depth] = append((*stack)[depth], fn)
}

// at returns the hooks registered on the scope at depth; nil-safe.
func (g *compilerGroupScopes) at(depth int) (before, after []func(*Context)) {
	if g == nil {
		return nil, nil
	}
	if depth < len(g.before) {
		before = g.before[depth]
	}
	if depth < len(g.after) {
		after = g.after[depth]
	}
	return before, after
}

// truncate drops every scope at depth n or deeper, clearing the dropped entries so the pooled
// compiler does not keep a finished suite's hook closures alive; nil-safe.
func (g *compilerGroupScopes) truncate(n int) {
	if g == nil {
		return
	}
	g.before = truncateHookScopes(g.before, n)
	g.after = truncateHookScopes(g.after, n)
}

func truncateHookScopes(s [][]func(*Context), n int) [][]func(*Context) {
	if n >= len(s) {
		return s
	}
	clear(s[n:])
	return s[:n]
}

// fullName returns the spec's breadcrumb (e.g. "Describe/When/It"). It feeds both SpecStartEvent.Path
// and — via specSubtestName — the testing.T.Run identity, so it uses the shared joinSubtestPath
// mapping.
func (c *bytecodeCompiler) fullName(itName string) string {
	return joinSubtestPath(c.nameStack, itName)
}

// flattenHooks fills beforeFlat and afterFlat from stacks.
// Before: declaration order root to innermost. After: same order; EmitIt reverse-appends so execution is LIFO.
func (c *bytecodeCompiler) flattenHooks() {
	c.beforeFlat = c.beforeFlat[:0]
	c.afterFlat = c.afterFlat[:0]
	for _, b := range c.beforeStack {
		c.beforeFlat = append(c.beforeFlat, b...)
	}
	for _, a := range c.afterStack {
		c.afterFlat = append(c.afterFlat, a...)
	}
}

// EmitIt appends one spec program with specialized opcodes: OpBeforeHook, OpBody, OpAfterHook.
func (c *bytecodeCompiler) EmitIt(name string, body func(*Context)) {
	c.flattenHooks()
	c.program = c.program[:0]
	for _, h := range c.beforeFlat {
		if h != nil {
			c.program = append(c.program, Instruction{Code: OpBeforeHook, Fn: h})
		}
	}
	if body != nil {
		c.program = append(c.program, Instruction{Code: OpBody, Fn: body})
	}
	// Append after hooks in reverse so execution order is LIFO (innermost first).
	for i := len(c.afterFlat) - 1; i >= 0; i-- {
		if h := c.afterFlat[i]; h != nil {
			c.program = append(c.program, Instruction{Code: OpAfterHook, Fn: h})
		}
	}
	start := len(c.plan.Instructions)
	c.plan.Instructions = append(c.plan.Instructions, c.program...)
	c.plan.ProgramStart = append(c.plan.ProgramStart, start)
	c.plan.ProgramLen = append(c.plan.ProgramLen, len(c.program))
	c.plan.Names = append(c.plan.Names, name)
	c.plan.FullNames = append(c.plan.FullNames, c.fullName(name))
	// Record the enclosing scopes themselves: fullName's join is not injective, so a name containing
	// "/" cannot be recovered from the breadcrumb afterwards. Only the scopes are stored — the plan
	// already holds name in Names — and nameStack is passed as-is, never pushed to and popped from.
	appendSpecPath(c.plan, c.nameStack)
}

// EmitItParallel appends one ItParallel spec (issue #245's second spec): the exact same instruction
// stream EmitIt would produce for the same name/body — a parallel spec's own before/body/after runs
// no differently from an ordinary one's, only how the runner launches it differs (see
// group_hooks.go's runParallelGroup) — plus the grouping bookkeeping consecutive ItParallel calls
// need. It never calls closeOpenParallelRun itself: doing so would immediately close the run this
// call is trying to open or extend. spec.go calls closeOpenParallelRun before It/FIt/SkipIt/
// PendingIt instead, so only those calls (and scope transitions) end a run.
func (c *bytecodeCompiler) EmitItParallel(name string, body func(*Context)) {
	if c.openParallelStart < 0 {
		c.openParallelStart = len(c.plan.Names)
	}
	c.EmitIt(name, body)
}

// EmitSkip buffers a compile-time SkipIt registration (issue #245): name and the current scope
// chain are kept so it can still be reported skipped, but no instruction is ever emitted for it —
// mirroring Builder.SkipIt's kindSkip buffering (builder.go) for this compile path. If the suite
// later turns out to have a focus, applyFocusFilter drops every buffered skip/pending mark
// wholesale, the same as Builder drops every unfocused It/SkipIt/PendingIt.
func (c *bytecodeCompiler) EmitSkip(name string) {
	registerSkipMark(&c.groups, name, c.nameStack)
}

// EmitPending is EmitSkip for PendingIt.
func (c *bytecodeCompiler) EmitPending(name string) {
	registerPendingMark(&c.groups, name, c.nameStack)
}

// EmitFocusedIt is EmitIt for FIt (issue #245): compiles the same instruction stream as a regular
// It, and additionally records this spec's plan index so applyFocusFilter (run once at TakePlan)
// can keep only focused specs when the suite registers at least one FIt — mirroring
// Builder.FIt/finalize's focus filter for this compile path.
func (c *bytecodeCompiler) EmitFocusedIt(name string, body func(*Context)) {
	c.EmitIt(name, body)
	c.focusIndices = append(c.focusIndices, len(c.plan.Names)-1)
}

// applyFocusFilter keeps only this suite's focused (FIt) specs when it registered at least one,
// dropping every other It/SkipIt/PendingIt — Builder.finalize's focus filter (specKind kindFocus),
// mirrored here for the bytecode-compiler path (issue #245). Unlike the arena/registry path
// (buildExecutionPlanFromArenaRec), which can pre-scan its already-built tree for a focus before
// emitting anything, this compiler emits directly as the DSL callbacks run, so filtering can only
// happen here, once, after the whole suite tree has been declared — closeGroupHooksAtTop has
// already registered every scope's BeforeAll/AfterAll range by the time TakePlan calls this.
func (c *bytecodeCompiler) applyFocusFilter() {
	if len(c.focusIndices) == 0 {
		return
	}
	plan := c.plan
	n := len(plan.Names)
	oldToNew := make([]int, n)
	for i := range oldToNew {
		oldToNew[i] = -1
	}
	keep := c.focusIndices
	names := make([]string, 0, len(keep))
	fullNames := make([]string, 0, len(keep))
	programStart := make([]int, 0, len(keep))
	programLen := make([]int, 0, len(keep))
	pathScopeStart := make([]int, 0, len(keep))
	pathScopeLen := make([]int, 0, len(keep))
	for newIdx, oldIdx := range keep {
		if oldIdx < 0 || oldIdx >= n {
			continue
		}
		oldToNew[oldIdx] = newIdx
		names = append(names, plan.Names[oldIdx])
		fullNames = append(fullNames, plan.FullNames[oldIdx])
		programStart = append(programStart, plan.ProgramStart[oldIdx])
		programLen = append(programLen, plan.ProgramLen[oldIdx])
		pathScopeStart = append(pathScopeStart, plan.PathScopeStart[oldIdx])
		pathScopeLen = append(pathScopeLen, plan.PathScopeLen[oldIdx])
	}
	plan.Names, plan.FullNames = names, fullNames
	plan.ProgramStart, plan.ProgramLen = programStart, programLen
	plan.PathScopeStart, plan.PathScopeLen = pathScopeStart, pathScopeLen

	if c.groups != nil {
		// Every unfocused SkipIt/PendingIt is dropped too, the same as every unfocused It.
		c.groups.skipped = nil
		c.groups.pending = nil
		c.groups = remapHookGroups(c.groups, oldToNew)
	}
}

// Plan returns the built ExecutionPlan. Caller owns it after TakePlan; compiler is reset.
//
// The root scope opened by describeWithCompiler/BuildSuite (a single PushScope with no matching
// PopScope — see their callers in spec.go) is closed here instead: TakePlan is the one place every
// entry point already calls once the root's callback has returned, so it is the natural spot to
// register that scope's own BeforeAll/AfterAll (if it registered any) before the compiler is reset
// and its stacks discarded.
func (c *bytecodeCompiler) TakePlan() *ExecutionPlan {
	plan, _ := c.takePlanAndGroups()
	return plan
}

// takePlanAndGroups is TakePlan that also hands over the plan's group-hook bookkeeping (nil when
// the suite registered no BeforeAll/AfterAll), which lives beside the plan rather than on it — see
// planGroups.
func (c *bytecodeCompiler) takePlanAndGroups() (*ExecutionPlan, *planGroups) {
	c.closeOpenParallelRun()
	c.closeGroupHooksAtTop()
	c.applyFocusFilter()
	plan, groups := c.plan, c.groups
	c.reset()
	return plan, groups
}
