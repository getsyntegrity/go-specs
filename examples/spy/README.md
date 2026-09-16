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
- **Paths (auto-scan)** — use `s.Paths(func(p *specs.PathBuilder) { ... }).It(...)` so the framework **automatically** runs the spec for every combination of path variables (e.g. `p.Bool("notify")`, `p.IntRange("severity", 1, 3)`). Each run gets `ctx.Path().Bool("notify")`, `ctx.Path().Int("severity")`, etc. Combine with a spy to assert behavior for each combination.

## Paths: automatic combination scanning

`TestSpyWithPaths` uses **Paths** to scan all combinations of `notify` (true/false) and `severity` (1..3). The framework runs the spec body once per combination; you read values with `ctx.Path().Bool("notify")` and `ctx.Path().Int("severity")` and assert on the spy for each case.

```go
s.Paths(func(p *specs.PathBuilder) {
    p.Bool("notify")
    p.IntRange("severity", 1, 3)
}).It("notifies with message built from path when notify is true", func(ctx *specs.Context) {
    notify := ctx.Path().Bool("notify")
    severity := ctx.Path().Int("severity")
    spy := mock.NewSpy()
    svc := &AlertService{Notifier: &SpyNotifier{Spy: spy}}
    if notify {
        svc.RaiseAlert("alert")
    }
    // assert spy per path...
})
```

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
