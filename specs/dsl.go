// Package specs provides a high-performance test execution architecture.
//
// DSL (Describe, BeforeEach, AfterEach, It) is compiled by the Builder into an
// optimized Program (grouped execution plan). The Runner executes the program
// with zero allocations in the loop. Focus and Skip are applied at compile time.
//
//   Builder → Program → Runner → (optional) RunShard for CI
//
// Usage:
//
//	b := specs.NewBuilder()
//	b.Describe("suite", func() {
//	    b.BeforeEach(setup)
//	    b.It("test", fn)
//	})
//	prog := b.Build()
//	specs.NewRunner(prog).Run(t)
//
// Or use BuildProgram to compile in one call:
//
//	prog := specs.BuildProgram(func(b *specs.Builder) {
//	    b.Describe("suite", func() { ... })
//	})
//	specs.NewRunner(prog).Run(t)
package specs

// BuildProgram compiles a test suite from a function that configures the Builder.
// Returns the compiled Program; run it with NewRunner(prog).Run(tb).
func BuildProgram(fn func(*Builder)) *Program {
	if fn == nil {
		return &Program{}
	}
	b := NewBuilder()
	fn(b)
	return b.Build()
}
