# 05 · Test doubles

> **Audience:** users · **Reading time:** ~6 minutes

The `mock` package records calls. That is the whole scope: no code generation, no interface
synthesis, no expectation DSL, no `.Times(3).Returns(x)` chain.

**The execution core never imports `mock`.** Mocking is a leaf utility that your test code pulls in
directly, not a stage of the pipeline. That is why you can use go-specs without it, and use it
without go-specs.

---

## 1. Spy — the primitive

```go
spy := mock.NewSpy()

spy.Call("payment received", 99)

spy.WasCalled()   // true
spy.CallCount()   // 1
spy.Calls()       // []mock.Call{{Args: []any{"payment received", 99}}}
```

| Method | Returns |
| --- | --- |
| `Call(args ...any)` | records one invocation |
| `Calls() []Call` | a **deep copy** of the recorded calls |
| `CallCount() int` | number of invocations |
| `WasCalled() bool` | whether there was at least one |
| `CalledWith(matchers ...ArgMatcher) bool` | whether any recorded call matches positionally |
| `CalledTimes(t *testing.T, n int)` | asserts the count, failing via `t.Fatalf` |

`Calls()` returns a copy on purpose: a caller that mutates the returned slice cannot corrupt the
spy's record.

## 2. Argument matchers

```go
spy.CalledWith(mock.Equal("payment received"), mock.Any())
```

| Matcher | Matches |
| --- | --- |
| `mock.Any()` | any value |
| `mock.Equal(expected)` | `reflect.DeepEqual(expected, actual)` |

`mock.ArgMatcher` is a separate interface from `assert.Matcher` — it answers "does this argument
match" (`Match(v any) bool`), with no failure message, because a positional argument mismatch is
reported by the assertion wrapping `CalledWith`, not by the matcher itself.

## 3. Mock — named spies

When one collaborator has several methods, `Mock` hands out spies by name and creates them lazily.

```go
m := mock.New()

notify := m.Spy("notify")
charge := m.Spy("charge")

notify.Call("payment received", 99)

ctx.Expect(notify.CallCount()).ToEqual(1)
ctx.Expect(charge.WasCalled()).To(specs.BeFalse())
```

`m.Spy(name)` is idempotent: calling it twice with the same name returns the same spy, including
under concurrent first-call races.

## 4. The interface-plus-spy pattern

The idiomatic use. Your production code depends on an interface; your test supplies an
implementation that records.

```go
type Notifier interface {
    Notify(message string) error
}

type SpyNotifier struct {
    *mock.Spy
}

func (s *SpyNotifier) Notify(message string) error {
    s.Call(message)
    return nil
}

func TestAlerts(t *testing.T) {
    specs.Describe(t, "AlertService", func(s *specs.Spec) {
        s.It("notifies on threshold breach", func(ctx *specs.Context) {
            spy := &SpyNotifier{Spy: mock.NewSpy()}
            svc := NewAlertService(spy)

            svc.Report(101)

            ctx.Expect(spy.CalledWith(mock.Equal("threshold breached"))).To(specs.BeTrue())
        })
    })
}
```

Working examples: `examples/spy/`, `examples/mocks/`, `examples/full_system_example/payment_mocks.go`.

## 5. Concurrency and nil safety

- **Every `Spy` and `Mock` method is safe for concurrent use.** `CalledWith` and `Calls` copy the
  recorded calls under the lock and then release it before running matchers, so a slow matcher —
  `reflect.DeepEqual` over a large struct — never holds the lock while other goroutines are
  recording.
- **Nil receivers are tolerated.** Every method checks for a nil receiver and degrades to a harmless
  no-op. A missing or zero-value spy does not panic your test; it records nothing, and your
  assertion on the call count is what fails — with a message about the count, which is the failure
  you actually want to read.

## 6. What this package deliberately does not do

| Not provided | Do this instead |
| --- | --- |
| Generated mocks from interfaces | write the struct; it is five lines and it compiles |
| Return-value programming (`.Returns(x)`) | give your spy struct a field and read it |
| Call-order assertions across spies | assert on `Calls()` yourself |
| Strict mocks that fail on unexpected calls | assert `CallCount()` explicitly |

The package is small because a test double that needs its own manual is a test double that will
outlive the code it doubles.
