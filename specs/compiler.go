package specs

import "sync"

// bytecodeCompiler emits instructions directly into an ExecutionPlan during Describe.
// No NodeArena is allocated; BeforeEach/AfterEach/It append instructions immediately.
type bytecodeCompiler struct {
	plan        *ExecutionPlan
	nameStack   []string
	beforeStack [][]func(*Context)
	afterStack  [][]func(*Context)
	// beforeAllStack/afterAllStack hold each open scope's own once-per-group hooks (issue #207).
	// Unlike beforeStack/afterStack these are never flattened via flattenHooks: a BeforeAll/AfterAll
	// belongs to exactly the scope it was registered on (H1), so it is read directly, once, when
	// that scope closes (see closeGroupHooks).
	beforeAllStack [][]func(*Context)
	afterAllStack  [][]func(*Context)
	// groupStartStack[i] is len(plan.Names) at the moment PushScope opened scope i — the index of
	// the first spec that scope's subtree could contribute. Paired with len(plan.Names)-1 when the
	// scope closes, it is the spec range that scope's own BeforeAll/AfterAll (if any) enters/exits.
	groupStartStack []int
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
			beforeAllStack:  make([][]func(*Context), 0, 8),
			afterAllStack:   make([][]func(*Context), 0, 8),
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
	c.beforeAllStack = c.beforeAllStack[:0]
	c.afterAllStack = c.afterAllStack[:0]
	c.groupStartStack = c.groupStartStack[:0]
	return c
}

func (c *bytecodeCompiler) reset() {
	c.plan = nil
	c.nameStack = c.nameStack[:0]
	c.beforeStack = c.beforeStack[:0]
	c.afterStack = c.afterStack[:0]
	c.beforeAllStack = c.beforeAllStack[:0]
	c.afterAllStack = c.afterAllStack[:0]
	c.groupStartStack = c.groupStartStack[:0]
	bytecodeCompilerPool.Put(c)
}

// PushScope enters a Describe/When block. Call PopScope when the block callback returns.
func (c *bytecodeCompiler) PushScope(name string) {
	c.nameStack = append(c.nameStack, name)
	c.beforeStack = append(c.beforeStack, nil)
	c.afterStack = append(c.afterStack, nil)
	c.beforeAllStack = append(c.beforeAllStack, nil)
	c.afterAllStack = append(c.afterAllStack, nil)
	c.groupStartStack = append(c.groupStartStack, len(c.plan.Names))
}

// PopScope exits the current Describe/When block, closing that scope's own group hooks (H1-H3)
// before discarding its bookkeeping. The root scope opened by describeWithCompiler/BuildSuite is
// never popped this way (nothing calls PopScope for it) — see TakePlan, which closes it instead.
func (c *bytecodeCompiler) PopScope() {
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
	if n := len(c.beforeAllStack); n > 0 {
		c.beforeAllStack = c.beforeAllStack[:n-1]
	}
	if n := len(c.afterAllStack); n > 0 {
		c.afterAllStack = c.afterAllStack[:n-1]
	}
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
	before := c.beforeAllStack[n-1]
	after := c.afterAllStack[n-1]
	if len(before) == 0 && len(after) == 0 {
		return
	}
	start := c.groupStartStack[n-1]
	end := len(c.plan.Names) - 1
	registerHookGroup(c.plan, c.nameStack, before, after, start, end)
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
	if fn == nil || len(c.beforeAllStack) == 0 {
		return
	}
	i := len(c.beforeAllStack) - 1
	c.beforeAllStack[i] = append(c.beforeAllStack[i], fn)
}

// AppendAfterAll adds a once-per-group teardown hook to the current scope (issue #207).
func (c *bytecodeCompiler) AppendAfterAll(fn func(*Context)) {
	if fn == nil || len(c.afterAllStack) == 0 {
		return
	}
	i := len(c.afterAllStack) - 1
	c.afterAllStack[i] = append(c.afterAllStack[i], fn)
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

// Plan returns the built ExecutionPlan. Caller owns it after TakePlan; compiler is reset.
//
// The root scope opened by describeWithCompiler/BuildSuite (a single PushScope with no matching
// PopScope — see their callers in spec.go) is closed here instead: TakePlan is the one place every
// entry point already calls once the root's callback has returned, so it is the natural spot to
// register that scope's own BeforeAll/AfterAll (if it registered any) before the compiler is reset
// and its stacks discarded.
func (c *bytecodeCompiler) TakePlan() *ExecutionPlan {
	c.closeGroupHooksAtTop()
	plan := c.plan
	c.reset()
	return plan
}
