package specs

import (
	"fmt"
	"testing"

	"github.com/pablogore/go-specs/assert"
)

// ExampleDescribe and ExampleContext_Expect are not executed by `go test` (no "Output:" comment):
// Describe requires a real *testing.T handed to it by the go test runner, and there is no way to
// construct one — testing.TB's private() method is unexported precisely to stop external types
// from satisfying it — from inside a parameterless Example function. They still compile on every
// `go test` run, so a signature change here fails the build, and they still render on pkg.go.dev.

func ExampleDescribe() {
	var t *testing.T
	Describe(t, "math", func(s *Spec) {
		s.It("adds numbers", func(ctx *Context) {
			ctx.Expect(1 + 1).ToEqual(2)
		})
	})
}

func ExampleContext_Expect() {
	var t *testing.T
	Describe(t, "strings", func(s *Spec) {
		s.It("is not empty", func(ctx *Context) {
			ctx.Expect(len("hello") > 0).To(assert.BeTrue())
		})
	})
}

// ExampleMutator_MutateInt is genuinely executed and Output-verified: Mutator needs no
// *testing.T, and its RNG is deterministic for a given seed.
func ExampleMutator_MutateInt() {
	m := NewMutator(1)
	fmt.Println(m.MutateInt(10, 0, 100))
	fmt.Println(m.MutateInt(10, 0, 100))
	// Output:
	// 6
	// 8
}
