package payment_system

/*
This example demonstrates several go-specs features working together:

  - BDD-style test structure (Describe / It)
  - Automatic path exploration over input combinations
  - Invariant testing (property that must hold for all explored inputs)
  - Automatic shrinking of failing inputs to a minimal repro

The framework generates combinations of inputs and runs the test body for each.
If the invariant is violated, go-specs shrinks the failing input to the smallest
case that still reproduces the bug.
*/

import (
	"testing"

	specs "github.com/getsyntegrity/go-specs/specs"
)

func TestDeposit(t *testing.T) {
	specs.Describe(t, "deposit", func(s *specs.Spec) {
		s.It("increases balance by amount", func(ctx *specs.Context) {
			ctx.Expect(Deposit(100, 50)).ToEqual(150)
			ctx.Expect(Deposit(0, 10)).ToEqual(10)
		})
	})
}

func TestWithdrawInvariant(t *testing.T) {
	specs.Describe(t, "withdraw invariants", func(s *specs.Spec) {
		// Paths defines the input space the framework should explore.
		// go-specs will generate combinations of balance and amount and run the test for each.
		// ExploreSmart explores that space intelligently (boundary values, random sampling,
		// mutation of interesting inputs, corpus replay) instead of every combination.
		s.Paths(func(p *specs.PathBuilder) {
			p.IntRange("balance", 0, 1000)
			p.IntRange("amount", 0, 1000)
		}).ExploreSmart(5000).It("never produces negative balance", func(ctx *specs.Context) {
			balance := ctx.Path().Int("balance")
			amount := ctx.Path().Int("amount")
			newBalance := Withdraw(balance, amount)
			// This is the invariant we want to enforce:
			// the balance should never become negative after a withdrawal.
			ctx.Expect(newBalance >= 0).To(specs.BeTrue())
			// If the test fails, go-specs automatically shrinks the failing input
			// to the smallest case that still reproduces the bug (e.g. balance=1, amount=2).
		})
	})
}
