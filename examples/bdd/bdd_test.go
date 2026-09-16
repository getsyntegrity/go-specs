package bdd_test

import (
	"testing"

	specs "github.com/getsyntegrity/go-specs/specs"
)

// TestBDD demonstrates the BDD-style DSL: Describe, When, and It.
func TestBDD(t *testing.T) {
	specs.Describe(t, "calculator", func(s *specs.Spec) {
		s.Describe("addition", func(s2 *specs.Spec) {
			s2.It("adds two numbers", func(ctx *specs.Context) {
				ctx.Expect(2 + 2).ToEqual(4)
			})
			s2.It("handles zero", func(ctx *specs.Context) {
				ctx.Expect(0 + 5).ToEqual(5)
			})
		})

		s.When("subtracting", func(s2 *specs.Spec) {
			s2.It("subtracts smaller from larger", func(ctx *specs.Context) {
				ctx.Expect(10 - 3).ToEqual(7)
			})
		})
	})
}
