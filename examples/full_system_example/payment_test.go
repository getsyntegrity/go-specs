package full_system_example

import (
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
	specs "github.com/getsyntegrity/go-specs/specs"
)

/*
This example demonstrates most features of go-specs working together:

• BDD-style DSL (Describe / It)
• lightweight mocks and spies
• snapshot testing

Run with:

	go test ./examples/payment_system -v
*/

func TestPaymentSystem(t *testing.T) {

	specs.Describe(t, "payment system", func(s *specs.Spec) {

		// -------------------------------------------------------
		// Deposit behavior
		// -------------------------------------------------------

		s.Describe("Deposit", func(s2 *specs.Spec) {

			s2.It("increases balance by amount", func(ctx *specs.Context) {

				ctx.Expect(Deposit(100, 50)).ToEqual(150)
				ctx.Expect(Deposit(0, 10)).ToEqual(10)

			})

		})

		// -------------------------------------------------------
		// Transfer behavior
		// -------------------------------------------------------

		s.Describe("Transfer", func(s2 *specs.Spec) {

			s2.It("moves amount from source to destination", func(ctx *specs.Context) {

				from, to := Transfer(nil, 100, 50, 30)

				ctx.Expect(from).ToEqual(70)
				ctx.Expect(to).ToEqual(80)

			})

			// ---------------------------------------------------
			// Mock + Spy example
			// ---------------------------------------------------

			s2.It("records transfer on ledger", func(ctx *specs.Context) {

				m := mock.New()

				ledger := NewMockLedger(m)

				svc := NewTransferService(ledger)

				svc.Transfer(100, 50, 20)

				recordSpy := m.Spy("RecordTransfer")

				ctx.Expect(recordSpy.CallCount()).ToEqual(1)

				ctx.Expect(
					recordSpy.CalledWith(
						mock.Equal(100),
						mock.Equal(50),
						mock.Equal(20),
					),
				).To(specs.BeTrue())

			})

			s2.It("does not record when insufficient funds", func(ctx *specs.Context) {

				m := mock.New()

				ledger := NewMockLedger(m)

				svc := NewTransferService(ledger)

				svc.Transfer(10, 50, 20)

				recordSpy := m.Spy("RecordTransfer")

				ctx.Expect(recordSpy.CallCount()).ToEqual(0)

			})

		})

		// -------------------------------------------------------
		// Snapshot example
		// -------------------------------------------------------

		s.It("transfer result snapshot", func(ctx *specs.Context) {

			from, to := Transfer(nil, 100, 50, 25)

			result := map[string]any{
				"fromBalance": from,
				"toBalance":   to,
				"amount":      25,
			}

			ctx.Snapshot("transfer_result", result)

		})

		// -------------------------------------------------------
		// Withdraw behavior
		// -------------------------------------------------------

		s.Describe("Withdraw", func(s2 *specs.Spec) {

			s2.It("never produces negative balance", func(ctx *specs.Context) {

				// Representative sample covering the normal case, the
				// amount > balance clamp, and the amount == balance boundary
				// (both at zero and at a larger value).
				ctx.Expect(Withdraw(100, 50) >= 0).To(specs.BeTrue())
				ctx.Expect(Withdraw(0, 50) >= 0).To(specs.BeTrue())
				ctx.Expect(Withdraw(0, 1) >= 0).To(specs.BeTrue())
				ctx.Expect(Withdraw(50, 50) >= 0).To(specs.BeTrue())
				ctx.Expect(Withdraw(1000, 1000) >= 0).To(specs.BeTrue())

			})

		})

	})
}
