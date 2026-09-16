// bytecode.go defines the bytecode execution model for the Describe/BeforeEach/AfterEach/It DSL.
//
// BYTECODE DESIGN
//
// Opcodes (see instruction.go for type OpCode; bytecode uses these three):
//
//	const (
//	    OpRunSpec    OpCode = iota  // run the spec body (It callback)
//	    OpBeforeEach                // before-each hook
//	    OpAfterEach                 // after-each hook
//	)
//
// Instruction (program.go): struct { op OpCode; fn func(*Context) }.
// Program (program.go): struct { code []instruction }.
//
// COMPILATION
//
// DSL:
//
//	Describe("math", func() {
//	    BeforeEach(setup)
//	    It("adds", testAdd)
//	})
//
// Compiles to:
//
//	[
//	    {OpBeforeEach, setup},
//	    {OpRunSpec, testAdd},
//	]
//
// Builder (builder.go) flattens nested Describe/BeforeEach/AfterEach into this linear form.
//
// EXECUTION LOOP (runner_bytecode.go)
//
//	code := program.Code
//	for i := 0; i < len(code); i++ {
//	    code[i].fn(ctx)
//	}
//
// PERFORMANCE RULES
//
//   - No reflection.
//   - No interface dispatch in hot path (single indirect call fn(ctx)).
//   - Instruction slice is one contiguous []instruction (cache-friendly).
//   - No allocations during execution (context from pool, code read-only).
//   - Context reused across all instructions (or per worker in parallel).
//
// PERFORMANCE BENEFITS VS TREE TRAVERSAL
//
// Tree traversal would: walk a tree of Describe nodes; at each node run BeforeEach list, then
// recurse into children or run It, then run AfterEach list. That implies pointer chasing (node →
// child/sibling), branch-heavy control flow, and often interface dispatch (e.g. "run node").
//
// Bytecode flattens the same semantics into a linear instruction array:
//
//  1. Single loop, no recursion — predictable branch (loop back vs exit); CPU can speculate.
//  2. No pointer chasing — code[i] is next in the same slice; excellent cache locality.
//  3. No interface dispatch — each instruction is a concrete func(*Context); one indirect call.
//  4. Zero allocations in the loop — context obtained once from a pool; instructions are read-only.
//  5. Bounds-check elimination — for i := 0; i < len(code); i++ and code[i] let the compiler prove bounds.
//
// Same before/spec/after semantics as the tree, with minimal work per step and cache-friendly access.
package specs
