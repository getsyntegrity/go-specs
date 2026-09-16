package assertions_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestAssertions(t *testing.T) {
	specs.Describe(t, "assertions", func(s *specs.Spec) {
		s.It("supports matchers", func(ctx *specs.Context) {
			ctx.Expect("hello").To(specs.Equal("hello"))
			ctx.Expect(true).To(specs.BeTrue())
			ctx.Expect(nil).To(specs.BeNil())

			values := []int{1, 2, 3}
			ctx.Expect(values).To(specs.Contain(2))
		})
	})
}
