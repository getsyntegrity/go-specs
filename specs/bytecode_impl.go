package specs

// bytecode_impl.go provides the bytecode execution model: instruction, BCProgram, and BCBuilder.
// Used by BytecodeRunner and ShardBCProgram. Kept in a single file for cohesion.

// instruction is a single bytecode step: run fn(ctx). Lowercase for use in slices (runner_bytecode, sharding).
type instruction struct {
	fn func(*Context)
}

// BCProgram is a flat bytecode program: Code is the instruction stream, SpecStarts[i] is the start
// index of spec i (SpecStarts[NumSpecs()] == len(Code)).
type BCProgram struct {
	Code       []instruction
	SpecStarts []int
}

// BCLen returns the number of instructions.
func (p BCProgram) BCLen() int {
	return len(p.Code)
}

// NumSpecs returns the number of specs (len(SpecStarts)-1).
func (p BCProgram) NumSpecs() int {
	if len(p.SpecStarts) < 2 {
		return 0
	}
	return len(p.SpecStarts) - 1
}

// BCBuilder builds a BCProgram from AddBefore/AddAfter/AddSpec. Flat list: for each spec,
// emit before hooks, spec body, after hooks (after in LIFO).
type BCBuilder struct {
	before []func(*Context)
	after  []func(*Context)
	code   []instruction
	starts []int
}

// NewBCBuilder creates a bytecode builder with optional capacity hint for the instruction slice.
func NewBCBuilder(capacity int) *BCBuilder {
	if capacity <= 0 {
		capacity = 64
	}
	return &BCBuilder{
		before: nil,
		after:  nil,
		code:   make([]instruction, 0, capacity*4),
		starts: make([]int, 0, capacity+1),
	}
}

// AddBefore adds a before-each hook.
func (b *BCBuilder) AddBefore(fn func(*Context)) {
	if fn == nil {
		return
	}
	b.before = append(b.before, fn)
}

// AddAfter adds an after-each hook (run in LIFO order per spec).
func (b *BCBuilder) AddAfter(fn func(*Context)) {
	if fn == nil {
		return
	}
	b.after = append(b.after, fn)
}

// AddSpec adds one spec: emits before hooks, then spec body, then after hooks (reverse).
func (b *BCBuilder) AddSpec(fn func(*Context)) {
	if fn == nil {
		return
	}
	b.starts = append(b.starts, len(b.code))
	for _, h := range b.before {
		b.code = append(b.code, instruction{fn: h})
	}
	b.code = append(b.code, instruction{fn: fn})
	for i := len(b.after) - 1; i >= 0; i-- {
		b.code = append(b.code, instruction{fn: b.after[i]})
	}
}

// BuildBC returns the compiled bytecode program.
func (b *BCBuilder) BuildBC() BCProgram {
	b.starts = append(b.starts, len(b.code))
	return BCProgram{
		Code:       b.code,
		SpecStarts: b.starts,
	}
}
