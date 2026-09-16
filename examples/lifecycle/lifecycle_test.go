package lifecycle_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestLifecycle(t *testing.T) {
	var counter int

	specs.Describe(t, "lifecycle", func(s *specs.Spec) {
		s.BeforeEach(func(ctx *specs.Context) {
			counter++
		})

		s.AfterEach(func(ctx *specs.Context) {
			counter = 0
		})

		s.It("runs before each test", func(ctx *specs.Context) {
			ctx.Expect(counter).ToEqual(1)
		})

		s.It("resets after each test", func(ctx *specs.Context) {
			ctx.Expect(counter).ToEqual(1)
		})
	})
}
