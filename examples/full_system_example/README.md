# Full system example: payment service

This example demonstrates several major features of the go-specs testing framework using a small payment system. It is suitable for documentation and as a reference for BDD, mocks, and snapshots.

## System overview

The payment service provides:

- **Deposit(balance, amount)** — returns `balance + amount`
- **Withdraw(balance, amount)** — returns the new balance; never negative (returns `0` when `amount > balance`)
- **Transfer(ledger, fromBalance, toBalance, amount)** — moves `amount` from source to destination and optionally records via a **Ledger** (external dependency)

The **Ledger** interface is mocked in tests so we can verify that `RecordTransfer(from, to, amount)` is called with the expected arguments.

## Run the example

```bash
go test ./examples/full_system_example
```

## Mock verification

The **Ledger** dependency is replaced by a mock that records calls:

```go
m := mock.New()
ledger := NewMockLedger(m)
svc := NewTransferService(ledger)

svc.Transfer(100, 50, 20)

recordSpy := m.Spy("RecordTransfer")
ctx.Expect(recordSpy.CallCount()).ToEqual(1)
if !recordSpy.CalledWith(mock.Equal(100), mock.Equal(50), mock.Equal(20)) {
    t.Fatal("expected RecordTransfer(100, 50, 20)")
}
```

- **NewMockLedger(m)** returns a `Ledger` that forwards `RecordTransfer` to **m.Spy("RecordTransfer")**.
- **Spy.CallCount()** and **Spy.CalledWith(matchers...)** verify that the right call happened.

## Snapshots

Structured results can be captured and compared to stored snapshots:

```go
result := map[string]any{"fromBalance": from, "toBalance": to, "amount": 25}
ctx.Snapshot("transfer_result", result)
```

Snapshots are stored under `__snapshots__/<test_file>.snap.json`. To create or update them:

```bash
GO_SPECS_UPDATE_SNAPSHOTS=1 go test ./examples/full_system_example
```

## Files

| File               | Purpose                                                |
|--------------------|--------------------------------------------------------|
| `payment.go`       | Deposit, Withdraw, Transfer + Ledger call              |
| `payment_mocks.go`| Ledger interface, TransferService, NewMockLedger      |
| `payment_test.go` | BDD specs: Deposit, Transfer (mocks), snapshot, Withdraw |
| `README.md`       | This overview                                          |
