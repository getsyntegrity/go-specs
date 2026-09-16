package full_system_example

import "github.com/getsyntegrity/go-specs/mock"

// Ledger records transfer operations (external dependency to mock).
type Ledger interface {
	RecordTransfer(from, to, amount int)
}

// NewMockLedger returns a Ledger that records RecordTransfer calls to m.Spy("RecordTransfer").
// Use it to verify transfer interactions: m.Spy("RecordTransfer").CalledWith(mock.Equal(from), mock.Equal(to), mock.Equal(amount)).
func NewMockLedger(m *mock.Mock) Ledger {
	if m == nil {
		return nil
	}
	return &mockLedger{spy: m.Spy("RecordTransfer")}
}

type mockLedger struct {
	spy *mock.Spy
}

func (l *mockLedger) RecordTransfer(from, to, amount int) {
	if l != nil && l.spy != nil {
		l.spy.Call(from, to, amount)
	}
}

// TransferService performs transfers and notifies the ledger.
type TransferService struct {
	ledger Ledger
}

// NewTransferService returns a transfer service that uses the given ledger.
func NewTransferService(ledger Ledger) *TransferService {
	return &TransferService{ledger: ledger}
}

// Transfer moves amount from fromBalance to toBalance, updates balances, and records via the ledger.
func (s *TransferService) Transfer(fromBalance, toBalance, amount int) (newFrom, newTo int) {
	return Transfer(s.ledger, fromBalance, toBalance, amount)
}
