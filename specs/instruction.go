package specs

// OpCode identifies the kind of instruction. Specialized opcodes improve branch prediction
// and make profiling clearer than a single generic OpCall.
type OpCode uint8

const (
	OpSetPath     OpCode = iota // ctx.SetPathValues(path); path is passed at run time for path specs
	OpBeforeHook                // before-each hook (compiler/ExecutionPlan)
	OpBody                      // spec body
	OpAfterHook                 // after-each hook
	OpRunSpec                   // bytecode: run spec body
	OpBeforeEach                // bytecode: before-each hook
	OpAfterEach                 // bytecode: after-each hook
)

// Instruction is a single bytecode step. Fn is invoked for OpBeforeHook, OpBody, OpAfterHook.
// For OpSetPath, the runner uses the path argument passed to runProgram; Fn may be nil.
type Instruction struct {
	Code OpCode
	Fn   func(*Context)
}
