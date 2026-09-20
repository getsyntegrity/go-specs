# Spy example

This example shows how to use **Spy** from `github.com/getsyntegrity/go-specs/mock` to record and assert on function invocations in BDD-style specs, including **interface + spy** for testing dependencies.

## Run

```bash
go test ./examples/spy/... -v
```

## What it demonstrates

- **`mock.NewSpy()`** — create a standalone spy that records `Call(args...)`.
- **`CallCount()`** / **`WasCalled()`** — query how many times the spy was called.
- **`CalledWith(matchers...)`** — check if any recorded call matches the given argument matchers:
  - `mock.Equal(x)` — same value (DeepEqual).
  - `mock.Any()` — any value for that argument.
- **`CalledTimes(t, n)`** — assert the spy was called exactly `n` times (fails the test otherwise).
- **`mock.New().Spy("name")`** — get a named spy from a Mock; same name returns the same spy.
- **Interface + spy** — define an interface (e.g. `Notifier`), a real struct (`RealNotifier`), and a `SpyNotifier` that implements the interface and records to a Spy; inject the spy implementation to verify the subject under test calls the dependency correctly.

## Struct / interface and spy

- **`notifier.go`**: `Notifier` interface, `RealNotifier` (concrete), `SpyNotifier` (implements `Notifier`, records to `*mock.Spy`).
- **`TestInterfaceWithSpy`**: `AlertService` depends on `Notifier`; tests inject `&SpyNotifier{Spy: spy}` and assert `spy.CallCount()`, `spy.CalledWith(mock.Equal(...))`.

```go
spy := mock.NewSpy()
svc := &AlertService{Notifier: &SpyNotifier{Spy: spy}}
svc.RaiseAlert("payment failed")
ctx.Expect(spy.CalledWith(mock.Equal("payment failed"))).To(specs.BeTrue())
```

## Example snippet (standalone spy)

```go
spy := mock.NewSpy()
spy.Call("user@example.com", 42)
ctx.Expect(spy.CallCount()).ToEqual(1)
ctx.Expect(spy.CalledWith(mock.Equal("user@example.com"), mock.Equal(42))).To(specs.BeTrue())
```
