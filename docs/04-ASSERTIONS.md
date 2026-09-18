# 04 · Assertions and matchers

> **Audience:** users · **Reading time:** ~10 minutes

The `assert` package is the single source of truth for equality and matching. It imports nothing
from `specs`, so it is usable and testable on its own; `specs` re-exports it for DSL ergonomics.

---

## 1. The three assertion forms

Covered in detail in [02 · The DSL](02-DSL.md#9-assertions). The short table:

| Form | Reflection | Accepts matchers | Use for |
| --- | --- | --- | --- |
| `specs.EqualTo(ctx, actual, expected)` | never | no | primitives and value structs, hot paths |
| `specs.ExpectT(ctx, x).ToEqual(y)` | never | yes | the preferred typed fluent form |
| `ctx.Expect(x).ToEqual(y)` | fallback | yes | slices, maps, structs with pointer fields |

## 2. The matcher interface

```go
type Matcher interface {
    Match(actual any) bool
    FailureMessage(actual any) string
}
```

`specs.Matcher` is a **type alias** for `assert.Matcher`, not a separate interface. One
implementation satisfies both names, so a custom matcher works with either import path and no
adapter.

## 3. Built-in matchers

| Matcher | Passes when | Notes |
| --- | --- | --- |
| `Equal(expected)` | values are equal | fast paths for `int`, `string`, `bool`, `int64`, `float64`; `reflect.DeepEqual` otherwise |
| `NotEqual(expected)` | values are not equal | inverse of `Equal` |
| `BeNil()` | the value is nil | nil-safe, including typed-nil interface values |
| `BeTrue()` | the value is `true` | |
| `BeFalse()` | the value is `false` | |
| `Contain(expected)` | the container holds the element | fast paths for `string`, `[]int`, `[]string`, `[]float64`; generic reflect scan for other slices and arrays |

All six are re-exported as `specs.Equal`, `specs.NotEqual`, `specs.BeNil`, `specs.BeTrue`,
`specs.BeFalse`, `specs.Contain`.

```go
ctx.Expect("hello").To(specs.Equal("hello"))
ctx.Expect(true).To(specs.BeTrue())
ctx.Expect(err).To(specs.BeNil())
ctx.Expect([]int{1, 2, 3}).To(specs.Contain(2))
```

> **There is no `ToBeNil` method.** Use `ctx.Expect(x).To(specs.BeNil())`. Earlier documentation
> listed `ctx.Expect(...).ToBeNil(...)` as part of the stable DSL; that form has never existed in the
> module and calling it is a compile error. The claim has been corrected in `RULES.md` and in the
> [legacy parity appendix](appendix/legacy-parity.md).

## 4. Helper functions

| Function | Purpose |
| --- | --- |
| `assert.ValuesEqual(expected, actual) bool` | nil-safe equality; tries the scalar fast path before `reflect.DeepEqual` |
| `assert.IsNilValue(v) bool` | nil check that also catches typed-nil interface values |
| `assert.EqualComparable[T comparable](a, b T) bool` | generic, allocation-free, inlineable — the runner's hot path uses it |
| `assert.EqualValues(t testing.TB, a, b any) bool` | test-helper variant; unwraps `error` via `errors.Is` in both directions, calls `t.Helper()` |

`EqualValues` is the only one that knows about errors. If you are comparing errors, use it or
`errors.Is` directly — `reflect.DeepEqual` on two errors compares structure, which is almost never
what you meant.

## 5. Writing a custom matcher

Implement the two methods. Nothing needs to be registered.

```go
type beWithin struct {
    low, high int
}

func BeWithin(low, high int) specs.Matcher {
    return beWithin{low: low, high: high}
}

func (m beWithin) Match(actual any) bool {
    n, ok := actual.(int)
    return ok && n >= m.low && n <= m.high
}

func (m beWithin) FailureMessage(actual any) string {
    return fmt.Sprintf("expected %v to be within [%d, %d]", actual, m.low, m.high)
}
```

Used the same way as a built-in:

```go
ctx.Expect(score).To(BeWithin(0, 100))
```

Two rules worth honouring, both from `SKILLS.md`:

1. **Add a typed fast path before reaching for reflection.** A type switch on the concrete types you
   actually expect costs nothing and keeps the assertion off the reflect path.
2. **Write the failure message for the person who will read it at 2am.** Include the actual value,
   the expectation, and the unit. `FailureMessage` is only ever called on failure, so it can afford
   to be thorough.

## 6. Why equality has a fast path at all

`Equal` type-switches `int`, `string`, `bool`, `int64` and `float64` before falling back to
`reflect.DeepEqual`, and `EqualComparable` type-switches every builtin comparable kind. This is not
micro-optimization for its own sake: an assertion runs once per expectation, and a suite of 50,000
specs runs hundreds of thousands of them. The measured difference between the typed and reflected
paths is roughly an order of magnitude — see [08 · Performance](08-PERFORMANCE.md).

The trade-off is the semantic divergence documented in
[02 · The DSL](02-DSL.md#why-the-three-differ--by-design): the typed path means `==`, the reflected
path means deep equality, and for a struct holding pointers those are different answers. The
framework does not hide that. It documents it and lets you choose.

## 7. Failure attribution

Every reporting call on the test backend invokes `tb.Helper()` before delegating, so a failure is
attributed to **your** assertion line, not to a frame inside go-specs. Stack traces additionally
treat `specs` and `snapshots` frames as internal so the first user frame is the one surfaced.
