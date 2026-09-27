package specs

// OpCode identifies the kind of instruction. Specialized opcodes improve branch prediction
// and make profiling clearer than a single generic OpCall.
type OpCode uint8

const (
	OpBeforeHook OpCode = iota // before-each hook (compiler/ExecutionPlan)
	OpBody                     // spec body
	OpAfterHook                // after-each hook
	OpRunSpec                  // bytecode: run spec body
	OpBeforeEach               // bytecode: before-each hook
	OpAfterEach                // bytecode: after-each hook
)

// Instruction is a single bytecode step. Fn is invoked for OpBeforeHook, OpBody, OpAfterHook.
type Instruction struct {
	Code OpCode
	Fn   func(*Context)
}
