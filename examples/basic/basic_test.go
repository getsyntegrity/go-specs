package basic_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestBasic(t *testing.T) {
	specs.Describe(t, "math", func(s *specs.Spec) {
		s.It("adds numbers", func(ctx *specs.Context) {
			ctx.Expect(1 + 1).ToEqual(2)
		})
	})
}
