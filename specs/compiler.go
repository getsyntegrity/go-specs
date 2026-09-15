package specs

import "sync"

// bytecodeCompiler emits instructions directly into an ExecutionPlan during Describe.
// No NodeArena is allocated; BeforeEach/AfterEach/It append instructions immediately.
type bytecodeCompiler struct {
	plan        *ExecutionPlan
	nameStack   []string
	beforeStack [][]func(*Context)
	afterStack  [][]func(*Context)
	pathGen     *PathGenerator
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
			nameStack:   make([]string, 0, 16),
			beforeStack: make([][]func(*Context), 0, 8),
			afterStack:  make([][]func(*Context), 0, 8),
			beforeFlat:  make([]func(*Context), 0, 32),
			afterFlat:   make([]func(*Context), 0, 32),
			program:     make([]Instruction, 0, 32),
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
	c.pathGen = nil
	return c
}

func (c *bytecodeCompiler) reset() {
	c.plan = nil
	c.nameStack = c.nameStack[:0]
	c.beforeStack = c.beforeStack[:0]
	c.afterStack = c.afterStack[:0]
	c.pathGen = nil
	bytecodeCompilerPool.Put(c)
}

// PushScope enters a Describe/When block. Call PopScope when the block callback returns.
func (c *bytecodeCompiler) PushScope(name string) {
	c.nameStack = append(c.nameStack, name)
	c.beforeStack = append(c.beforeStack, nil)
	c.afterStack = append(c.afterStack, nil)
}

// PopScope exits the current Describe/When block.
func (c *bytecodeCompiler) PopScope() {
	if n := len(c.nameStack); n > 0 {
		c.nameStack = c.nameStack[:n-1]
	}
	if n := len(c.beforeStack); n > 0 {
		c.beforeStack = c.beforeStack[:n-1]
	}
	if n := len(c.afterStack); n > 0 {
		c.afterStack = c.afterStack[:n-1]
	}
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

// SetPathGen sets the path generator for the next EmitIt (e.g. from Paths().It()).
func (c *bytecodeCompiler) SetPathGen(gen *PathGenerator) {
	c.pathGen = gen
}

// fullName returns the spec's breadcrumb (e.g. "Describe/When/It"). It feeds both SpecStartEvent.Path
// and — via specSubtestName — the testing.T.Run identity, so it uses the shared SubtestName mapping.
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

// EmitIt appends one spec program with specialized opcodes: OpSetPath (if path spec), OpBeforeHook, OpBody, OpAfterHook.
func (c *bytecodeCompiler) EmitIt(name string, body func(*Context)) {
	c.flattenHooks()
	c.program = c.program[:0]
	if c.pathGen != nil {
		c.program = append(c.program, Instruction{Code: OpSetPath, Fn: nil})
	}
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
	c.plan.PathGens = append(c.plan.PathGens, c.pathGen)
	c.pathGen = nil
}

// Plan returns the built ExecutionPlan. Caller owns it after TakePlan; compiler is reset.
func (c *bytecodeCompiler) TakePlan() *ExecutionPlan {
	plan := c.plan
	c.reset()
	return plan
}
