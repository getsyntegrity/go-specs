// end_to_end_test.go puts several go-specs features together on one small, realistic domain: a
// payment service that moves money between two balances and records every transfer in a ledger.
//
// Read it after the single-feature files. It shows how the pieces compose in a real suite:
//
//   - the nested Describe/When/It DSL with a BeforeEach fixture;
//   - specs.Table for rule-by-rule cases (deposit, withdraw, transfer);
//   - a quantified matcher (EveryElement) to state an invariant over many inputs;
//   - a hand-written spy double (mock.Spy) for "was the ledger told?";
//   - a mock.Controller double with argument matching, a Captor and Never, for "exactly what was the
//     ledger told, and nothing else?";
//   - ctx.Snapshot for the receipt, whose stored JSON lives in
//     examples/__snapshots__/end_to_end_test.snap.json (refresh it with GO_SPECS_UPDATE_SNAPSHOTS=1);
//   - a failure diagnostic printed from a runnable Example instead of a failing test.
//
// Run just this file's specs with: go test ./examples/ -run 'EndToEnd' -v
package examples_test

import (
	"fmt"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
)

// --- Domain ------------------------------------------------------------------------------------

// e2eLedger is the external dependency the service tells about every successful transfer. A real
// implementation would write to a database or publish an event.
type e2eLedger interface {
	RecordTransfer(from, to, amount int)
}

// e2eDeposit returns balance + amount.
func e2eDeposit(balance, amount int) int { return balance + amount }

// e2eWithdraw returns the balance after withdrawing amount. It never returns a negative balance:
// asking for more than is available empties the account.
func e2eWithdraw(balance, amount int) int {
	if amount > balance {
		return 0
	}
	return balance - amount
}

// e2ePaymentService performs transfers and records them via its ledger. A nil ledger means
// "do not record", and a nil service leaves balances unchanged.
type e2ePaymentService struct{ Ledger e2eLedger }

// Transfer moves amount from the balance from to the balance to and returns both new balances.
// When from holds less than amount nothing moves and nothing is recorded.
func (s *e2ePaymentService) Transfer(from, to, amount int) (newFrom, newTo int) {
	if s == nil || from < amount {
		return from, to
	}
	if s.Ledger != nil {
		s.Ledger.RecordTransfer(from, to, amount)
	}
	return from - amount, to + amount
}

// e2eReceipt is what a caller shows the customer after a transfer.
type e2eReceipt struct {
	FromBefore int    `json:"fromBefore"`
	ToBefore   int    `json:"toBefore"`
	Amount     int    `json:"amount"`
	FromAfter  int    `json:"fromAfter"`
	ToAfter    int    `json:"toAfter"`
	Status     string `json:"status"`
}

func (s *e2ePaymentService) TransferWithReceipt(from, to, amount int) e2eReceipt {
	newFrom, newTo := s.Transfer(from, to, amount)
	status := "completed"
	if newFrom == from && amount > 0 {
		status = "rejected: insufficient funds"
	}
	return e2eReceipt{from, to, amount, newFrom, newTo, status}
}

// --- Test doubles ------------------------------------------------------------------------------

// e2eLedgerSpy is the lightest double: it forwards every call to a spy.
type e2eLedgerSpy struct{ spy *mock.Spy }

func (l *e2eLedgerSpy) RecordTransfer(from, to, amount int) { l.spy.Call(from, to, amount) }

// e2eLedgerMock is the expectation-based double: it forwards to a controller method.
type e2eLedgerMock struct{ c *mock.Controller }

func (l e2eLedgerMock) RecordTransfer(from, to, amount int) {
	l.c.Method("Ledger.RecordTransfer").Call(from, to, amount)
}

// --- Specs -------------------------------------------------------------------------------------

type e2eBalanceCase struct {
	name            string
	balance, amount int
	want            int
}

func TestEndToEnd_paymentSystem(t *testing.T) {
	specs.Describe(t, "payment system", func(s *specs.Spec) {
		s.Describe("Deposit", func(s *specs.Spec) {
			specs.Table(s, []e2eBalanceCase{
				{"adds to an existing balance", 100, 50, 150},
				{"adds to an empty account", 0, 10, 10},
			}, func(c e2eBalanceCase) string { return c.name },
				func(ctx *specs.Context, c e2eBalanceCase) {
					ctx.Expect(e2eDeposit(c.balance, c.amount)).ToEqual(c.want)
				})
		})

		s.Describe("Withdraw", func(s *specs.Spec) {
			specs.Table(s, []e2eBalanceCase{
				{"takes the amount from the balance", 100, 50, 50},
				{"can empty the account exactly", 50, 50, 0},
				{"clamps to zero when asking for more than available", 50, 100, 0},
				{"does nothing on an empty account", 0, 1, 0},
			}, func(c e2eBalanceCase) string { return c.name },
				func(ctx *specs.Context, c e2eBalanceCase) {
					ctx.Expect(e2eWithdraw(c.balance, c.amount)).ToEqual(c.want)
				})

			// The invariant behind the table: whatever the inputs, the balance is never negative.
			// EveryElement states it once over the whole sample, and its failure message points at
			// the first offending element.
			s.It("never produces a negative balance", func(ctx *specs.Context) {
				inputs := []struct{ balance, amount int }{
					{0, 0}, {0, 1}, {100, 50}, {50, 100}, {1000, 1000},
				}
				var results []int
				for _, in := range inputs {
					results = append(results, e2eWithdraw(in.balance, in.amount))
				}
				ctx.Expect(results).To(specs.EveryElement(specs.BeGreaterThan(-1)))
			})
		})

		s.Describe("Transfer", func(s *specs.Spec) {
			var svc *e2ePaymentService
			s.BeforeEach(func(*specs.Context) { svc = &e2ePaymentService{} }) // no ledger by default

			s.It("moves the amount from source to destination", func(ctx *specs.Context) {
				from, to := svc.Transfer(100, 50, 30)

				ctx.Expect(from).ToEqual(70)
				ctx.Expect(to).ToEqual(80)
			})

			s.It("moves nothing when the source cannot cover the amount", func(ctx *specs.Context) {
				from, to := svc.Transfer(10, 50, 20)

				ctx.Expect(from).ToEqual(10)
				ctx.Expect(to).ToEqual(50)
			})

			s.It("lets the source pay its whole balance", func(ctx *specs.Context) {
				from, to := svc.Transfer(20, 0, 20)

				ctx.Expect(from).ToEqual(0)
				ctx.Expect(to).ToEqual(20)
			})

			s.It("ignores a nil service", func(ctx *specs.Context) {
				var none *e2ePaymentService
				from, to := none.Transfer(100, 50, 20)

				ctx.Expect(from).ToEqual(100)
				ctx.Expect(to).ToEqual(50)
			})

			// A spy is enough to answer "was the ledger told?".
			s.When("the ledger is a spy", func(s *specs.Spec) {
				var spy *mock.Spy
				s.BeforeEach(func(*specs.Context) {
					spy = mock.New().Spy("RecordTransfer")
					svc.Ledger = &e2eLedgerSpy{spy: spy}
				})

				s.It("records a successful transfer with its arguments", func(ctx *specs.Context) {
					svc.Transfer(100, 50, 20)

					ctx.Expect(spy.CallCount()).ToEqual(1)
					ctx.Expect(spy.CalledWith(mock.Equal(100), mock.Equal(50), mock.Equal(20))).To(specs.BeTrue())
				})

				s.It("does not record when funds are insufficient", func(ctx *specs.Context) {
					svc.Transfer(10, 50, 20)

					ctx.Expect(spy.CallCount()).ToEqual(0)
				})
			})

			// A controller answers "was the ledger told exactly this, and nothing else?", and
			// verifies itself when the case ends.
			s.When("the ledger is a mock.Controller", func(s *specs.Spec) {
				var ctrl *mock.Controller
				s.BeforeEach(func(ctx *specs.Context) {
					ctrl = mock.NewController(ctx)
					svc.Ledger = e2eLedgerMock{ctrl}
				})

				s.It("records exactly one transfer with the pre-transfer balances", func(ctx *specs.Context) {
					ctrl.Method("Ledger.RecordTransfer").Expect(100, 50, 20).Times(1)

					svc.Transfer(100, 50, 20)
				})

				s.It("records every successful transfer and captures the amounts", func(ctx *specs.Context) {
					amounts := mock.NewCaptor[int]()
					ctrl.Method("Ledger.RecordTransfer").Expect(mock.Any(), mock.Any(), amounts.Matcher()).Times(2)

					svc.Transfer(100, 0, 30)
					svc.Transfer(70, 30, 20)

					ctx.Expect(amounts.Values()).To(specs.Equal([]int{30, 20}))
				})

				s.It("never touches the ledger for a rejected transfer", func(ctx *specs.Context) {
					ctrl.Method("Ledger.RecordTransfer").Expect(mock.Any(), mock.Any(), mock.Any()).Never()

					svc.Transfer(10, 50, 20)
				})
			})
		})

		s.Describe("Receipt", func(s *specs.Spec) {
			svc := &e2ePaymentService{}

			// The snapshot stores the whole receipt once; any later change to it is a visible diff.
			s.It("matches the stored receipt for a completed transfer", func(ctx *specs.Context) {
				ctx.Snapshot("transfer_result", svc.TransferWithReceipt(100, 50, 10))
			})

			s.It("matches the stored receipt for a rejected transfer", func(ctx *specs.Context) {
				ctx.Snapshot("transfer_rejected", svc.TransferWithReceipt(5, 50, 10))
			})

			// A snapshot is not a replacement for a precise assertion on the one fact that matters.
			s.It("also states the key fact directly", func(ctx *specs.Context) {
				receipt := svc.TransferWithReceipt(100, 50, 10)

				ctx.Expect(receipt.FromAfter + receipt.ToAfter).ToEqual(receipt.FromBefore + receipt.ToBefore)
			})
		})
	})
}

// What does a mistake look like? Here the service forgets to tell the ledger. Nothing fails while
// the code runs; the missing call is reported when the test ends, naming the expectation and the
// counts. The fake test (mocksTB, from mocks_test.go) records the message instead of failing.
func Example_endToEndForgottenLedgerCall() {
	tb := &mocksTB{}
	ctrl := mock.NewController(tb)
	ctrl.Method("Ledger.RecordTransfer").Expect(100, 50, 20)

	forgetful := struct {
		Transfer func(from, to, amount int) (int, int)
	}{
		Transfer: func(from, to, amount int) (int, int) { return from - amount, to + amount }, // never records
	}
	newFrom, newTo := forgetful.Transfer(100, 50, 20)
	fmt.Println(newFrom, newTo)

	tb.finish()
	tb.print()
	// Output:
	// 80 70
	// mock: unmet expectation Ledger.RecordTransfer(equal to 100, equal to 50, equal to 20) declared at <site>: want 1, got 0
}
