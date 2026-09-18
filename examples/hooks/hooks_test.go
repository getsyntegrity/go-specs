package hooks_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func add(a, b int) int {
	return a + b
}

func setup(ctx *specs.Context) {
	// Optional per-spec setup (e.g. reset state, create fixtures).
	_ = ctx
}

func TestHooks(t *testing.T) {
	specs.Describe(t, "math", func(s *specs.Spec) {
		s.BeforeEach(setup)

		s.It("adds numbers", func(ctx *specs.Context) {
			ctx.Expect(add(1, 2)).ToEqual(3)
		})
	})
}
