# Payment system example

A small **payment system** implemented and tested with the go-specs framework. The example shows BDD-style tests, invariant checks over representative inputs, mocks and spies, and snapshot testing.

## System overview

- **Deposit(balance, amount)** — returns `balance + amount`.
- **Withdraw(balance, amount)** — returns the new balance after withdrawing `amount`; never negative (returns `0` when `amount > balance`).
- **PaymentService** — holds a **Ledger** and exposes **Transfer(from, to, amount)** to move `amount` between two balances and optionally record the operation via the ledger.
- **Ledger** — interface for recording transfers; implemented with mocks in tests.

## Project structure

```
examples/payment_system/
    ledger.go           # Ledger interface
    payment.go          # Deposit, Withdraw
    payment_service.go  # PaymentService and Transfer
    payment_test.go     # BDD + invariant check
    payment_mock_test.go# Mock/spy verification
    snapshot_test.go    # Snapshot testing
    README.md
```

## Run the tests

```bash
go test ./examples/payment_system
```

## BDD testing

Specs are structured with `Describe` and `It`:

```go
specs.Describe(t, "deposit", func(s *specs.Spec) {
    s.It("increases balance by amount", func(ctx *specs.Context) {
        ctx.Expect(Deposit(100, 50)).ToEqual(150)
    })
})
```

Nested `Describe` blocks and multiple `It` specs keep the suite readable and organized.

## Invariant checking

The withdraw invariant — "never produces a negative balance" — is checked across a representative sample of inputs (including `amount > balance`) inside a single `It`:

```go
s.It("never produces negative balance", func(ctx *specs.Context) {
    cases := []struct{ balance, amount int }{
        {0, 0}, {0, 1}, {100, 50}, {50, 100}, {1000, 1000},
    }
    for _, c := range cases {
        newBalance := Withdraw(c.balance, c.amount)
        ctx.Expect(newBalance >= 0).To(specs.BeTrue())
    }
})
```

For broader input-space exploration or shrinking of failing inputs, use a dedicated property-testing tool such as `go test -fuzz`, [`rapid`](https://github.com/flyingmutant/rapid), or [`gopter`](https://github.com/leanovate/gopter) alongside go-specs — see the CHANGELOG for migration notes.

## Mock verification

The **Ledger** is mocked so we can assert that `RecordTransfer` is called correctly:

```go
m := mock.New()
ledger := &mockLedger{spy: m.Spy("RecordTransfer")}
service := &PaymentService{Ledger: ledger}

service.Transfer(100, 50, 20)

if !m.Spy("RecordTransfer").CalledWith(mock.Equal(100), mock.Equal(50), mock.Equal(20)) {
    t.Fatal("expected RecordTransfer(100, 50, 20)")
}
```

`mockLedger` implements `Ledger` by forwarding to `m.Spy("RecordTransfer")`. **CalledWith** and **CallCount** verify the interaction.

## Snapshot testing

Transfer results are captured and compared to stored snapshots:

```go
service := &PaymentService{Ledger: nil}
newFrom, newTo := service.Transfer(100, 50, 10)
result := map[string]any{"fromBalance": 100, "toBalance": 50, "amount": 10, "newFrom": newFrom, "newTo": newTo}
ctx.Snapshot("transfer_result", result)
```

Snapshots live in `__snapshots__/snapshot_test.snap.json`. To create or update them:

```bash
GO_SPECS_UPDATE_SNAPSHOTS=1 go test ./examples/payment_system -run TestTransferSnapshot
```
