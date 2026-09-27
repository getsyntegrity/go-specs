package payment_system

/*
This example demonstrates a go-specs BDD-style test structure (Describe / It)
and a basic invariant check (a property that must hold across a handful of
representative inputs).
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
		s.It("never produces negative balance", func(ctx *specs.Context) {
			// This is the invariant we want to enforce: the balance should never
			// become negative after a withdrawal, across a representative sample
			// of balance/amount combinations (including amount > balance).
			cases := []struct{ balance, amount int }{
				{0, 0}, {0, 1}, {100, 50}, {50, 100}, {1000, 1000},
			}
			for _, c := range cases {
				newBalance := Withdraw(c.balance, c.amount)
				ctx.Expect(newBalance >= 0).To(specs.BeTrue())
			}
		})
	})
}
