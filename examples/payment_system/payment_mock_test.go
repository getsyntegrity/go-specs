package payment_system

/*
The mock package provides lightweight spies for verifying that dependencies
are used correctly. Here we verify that the payment service records transfers
in the Ledger dependency when a transfer succeeds, and does not record when
funds are insufficient.
*/

import (
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
	specs "github.com/getsyntegrity/go-specs/specs"
)

// mockLedger adapts a mock.Mock to the Ledger interface for tests.
type mockLedger struct {
	spy *mock.Spy
}

func (l *mockLedger) RecordTransfer(from, to, amount int) {
	if l != nil && l.spy != nil {
		l.spy.Call(from, to, amount)
	}
}

func TestTransferRecordsLedger(t *testing.T) {
	specs.Describe(t, "transfer ledger recording", func(s *specs.Spec) {
		// The mock package provides lightweight spies. Here we verify that the
		// payment service correctly records transfers in the ledger dependency.
		s.It("records transfers in ledger", func(ctx *specs.Context) {
			m := mock.New()
			ledger := &mockLedger{spy: m.Spy("RecordTransfer")}
			service := &PaymentService{Ledger: ledger}

			service.Transfer(100, 50, 20)

			if !m.Spy("RecordTransfer").CalledWith(mock.Equal(100), mock.Equal(50), mock.Equal(20)) {
				t.Fatal("expected RecordTransfer to be called with (100, 50, 20)")
			}
		})

		s.It("does not record when insufficient funds", func(ctx *specs.Context) {
			m := mock.New()
			ledger := &mockLedger{spy: m.Spy("RecordTransfer")}
			service := &PaymentService{Ledger: ledger}

			service.Transfer(10, 50, 20)

			ctx.Expect(m.Spy("RecordTransfer").CallCount()).ToEqual(0)
		})
	})
}
