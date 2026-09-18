# go-specs vs Testify vs Gomega — assertion comparison

Measured conclusions from `benchmarks/comparison_bench_test.go`.

## What this measures, and what it does not

This is not a claim that "go-specs is N× faster than Testify". It measures **typed equality
compared with `==` against reflective equality over `any`**. That is a difference in API
design, not in implementation quality: any library that accepts `any` and prepares a failure
message before knowing whether the assertion failed will cost roughly two orders of magnitude
more. Stated that way the result still holds, and it is defensible.

Two guards against over-reading the numbers:

- Every scenario has a **`_Baseline` row**: the check written by hand, no assertion library at
  all. Without it, a library-to-library ratio conflates "the cost of Testify" with "the cost of
  having an assertion library".
- Every row names **which API it uses**. go-specs has three call shapes with very different
  costs, so an unlabelled table would be misleading on its own.

The three go-specs call shapes, which are three separate categories and never two:

| Suffix in the benchmark | Call shape | What it is |
| --- | --- | --- |
| `_GoSpecsTyped` | `specs.EqualTo(ctx, actual, expected)` | Typed generic direct path. Generic over `comparable`, compared with `==`. No handle is built and no claim is made — the cheapest shape. |
| `_GoSpecsExpectT` | `specs.ExpectT(ctx, v).ToEqual(...)` / `.To(...)` | A typed-*facing* API that is **not** the same code path: it builds a `typedExpectation[T]`, claims the single-use handle with an atomic `CompareAndSwap` and releases it. Since [#177](https://github.com/getsyntegrity/go-specs/issues/177)/#191 the value is held at its own type `T` rather than in an `any` field, so it no longer boxes — but it still costs several times `EqualTo`. |
| `_GoSpecsMatcher` | `ctx.Expect(v).To(Matcher)` | Matcher/interface path: `v` enters as `any` and the comparison goes through an interface call into the matcher. |

`EqualTo` and `ExpectT(...).ToEqual` are **not** the same code path. Grouping them under one
"typed" label would hide a real and measurable difference — see conclusion 2.

Reproduce:

```
go test ./benchmarks -run=NONE -bench=BenchmarkCompare -benchmem -benchtime=2s -count=10
benchstat <output>
```

Run recorded below: `go1.26.8 linux/amd64`, 13th Gen Intel Core i7-13620H (P-cores + E-cores,
turbo enabled), `-benchtime=2s`, `-count=10`, benchstat medians with the ±% spread, measured on
`develop` at `cf7e387` — i.e. after the panic-recovery fix (#185), the execution-engine
consolidation (#187), the error-identity fix (#189/#183), the typed-expectation change
(#191/#177) and the benchmark CI gate (#193). This branch is rebased onto `a87979e`; the three
commits between the two (#196, #197, #198) touch only `report/` and `docs/`, so every code path
measured here is byte-identical at both revisions and the numbers were not re-taken.

**Read the spread before the median.** At 0.2–3 ns the measurement sits at the floor of the
benchmark loop and depends on which core the goroutine lands on; on a machine that is not
otherwise idle those rows move by tens of percent between runs. Any row whose spread is wide is
marked, and a wide row means "this order of magnitude", not the digits printed. The allocation
columns are exact: `allocs/op` was identical (±0%) across every run taken while preparing this
document, and does not depend on machine load.

---

## Results

`allocs` is allocations per operation. Bold marks the fastest real assertion in each scenario
(the baseline is not an assertion).

| Scenario | Baseline (hand-written) | go-specs | Testify | Gomega |
| --- | --- | --- | --- | --- |
| int equality | 0.281 ns ±17% | `EqualTo` **1.367 ns** ±20% · 0 allocs<br>`ExpectT().ToEqual` 11.17 ns ±9% · 0 allocs | `assert.Equal` 171.7 ns ±21% · 0 allocs | `Expect().To(Equal())` 494.6 ns ±13% · 3 allocs |
| string equality | 0.662 ns ±4% | `EqualTo` **2.491 ns** ±12% · 0 allocs<br>`ExpectT().ToEqual` 11.93 ns ±4% · 0 allocs | `assert.Equal` 186.2 ns ±9% · 1 alloc | `Expect().To(Equal())` 552.6 ns ±8% · 4 allocs |
| comparable struct | 0.954 ns ±7% | `EqualTo` **2.782 ns** ±13% · 0 allocs<br>`ExpectT().ToEqual` 13.21 ns ±8% · 0 allocs | `assert.Equal` 240.7 ns ±3% · 1 alloc | `Expect().To(Equal())` 591.7 ns ±14% · 4 allocs |
| slice deep equality | `slices.Equal` 2.268 ns ±9% | `Expect().To(Equal())` **263.9 ns** ±10% · 3 allocs | `assert.Equal` 399.5 ns ±18% · 2 allocs | `Expect().To(Equal())` 724.0 ns ±9% · 5 allocs |
| boolean true | 0.180 ns ±7% | `EqualTo` 1.248 ns ±15% · 0 allocs<br>`ExpectT().To(BeTrue())` 11.24 ns ±10% · 0 allocs<br>`Expect().To(BeTrue())` 10.80 ns ±5% · 0 allocs | `assert.True` **1.083 ns** ±10% · 0 allocs | `Expect().To(BeTrue())` 444.6 ns ±7% · 3 allocs |
| nil / no error | 0.273 ns ±11% | `EqualTo` 3.155 ns ±13% · 0 allocs<br>`Expect().To(BeNil())` 13.39 ns ±15% · 0 allocs | `assert.NoError` **1.477 ns** ±9% · 0 allocs | `Expect().To(BeNil())` 413.7 ns ±6% · 2 allocs |
| substring contains | `strings.Contains` 4.843 ns ±11% | `Expect().To(Contain())` **79.34 ns** ±10% · 2 allocs | `assert.Contains` 153.5 ns ±4% · 1 alloc | `Expect().To(ContainSubstring())` 528.5 ns ±12% · 4 allocs |
| wrapped error identity | `errors.Is` 9.586 ns ±14% | `EqualTo(errors.Is(...), true)` **11.87 ns** ±11% · 0 allocs<br>`Expect().To(MatchError())` 46.18 ns ±9% · 1 alloc | `assert.ErrorIs` 126.5 ns ±22% · 0 allocs | `Expect().To(MatchError())` 514.9 ns ±8% · 3 allocs |

The `wrapped error identity` row carries two go-specs entries on purpose: `EqualTo(errors.Is(...))`
unwraps at the call site, while `Expect().To(MatchError())` unwraps inside the assertion and is the
entry that is semantically equivalent to `assert.ErrorIs` and `gomega.MatchError`. Compared like
for like, `specs.MatchError` (46 ns) is about **2.7× cheaper than `assert.ErrorIs`** (127 ns) and
about 11× cheaper than Gomega.

**On the two narrowest rows, do not read a winner.** `assert.True` (1.083 ns ±10%) and `EqualTo`
(1.248 ns ±15%) overlap inside their spreads on this machine; across the three runs taken for this
document the ordering flipped. Treat them as equal. `assert.NoError` (1.477 ns) versus `EqualTo`
(3.155 ns) on the nil check did **not** flip: Testify was roughly 2× faster in all three runs, and
that one is a real result.

All assertions are on passing values: this is the happy path. Failure formatting is not measured.

---

## Conclusions

**1. The typed path costs about 1 ns over writing the `if` by hand.** That is the defensible
statement, and the baseline row is what makes it verifiable. `specs.EqualTo` is generic over
`comparable` and compares with `==`: 0.28 ns by hand, 1.37 ns through the assertion. Testify's
`assert.Equal` costs ~172 ns for the same check because its signature takes `any`, which forces
boxing, then routes through `ObjectsAreEqual` (byte comparison plus `reflect.DeepEqual`). Gomega
costs ~495 ns and allocates three times, because every `Expect(...).To(Equal(...))` builds an
assertion object and a matcher value before comparing anything. The gap is caused by the API
signature, not by sloppy implementation.

**2. The three go-specs call shapes are still not interchangeable — but `ExpectT` is no longer a
trap.** This is the conclusion that changed most since the previous revision of this document.

| Call shape | int | string | struct |
| --- | --- | --- | --- |
| `EqualTo(ctx, a, b)` | 1.37 ns · 0 allocs | 2.49 ns · 0 allocs | 2.78 ns · 0 allocs |
| `ExpectT(ctx, a).ToEqual(b)` | 11.17 ns · 0 allocs | 11.93 ns · 0 allocs | 13.21 ns · 0 allocs |

Until [#177](https://github.com/getsyntegrity/go-specs/issues/177), `ExpectT` stored the value in
an `any` field, so it boxed: the cost **scaled with the type** and allocated for anything the
runtime's small-integer cache did not serve. The previous revision of this table measured exactly
that — 10.8 / 38.3 / 48.9 ns with 0 / 1 / 1 allocs — and concluded that any "zero allocation"
claim belonged to `EqualTo` alone.

[#191](https://github.com/getsyntegrity/go-specs/pull/191) changed the field to hold the value at
its own type `T`. The column is now **flat at 11–13 ns and allocation-free for all three types**.
The `string` row went from 1 alloc to 0 and from ~38 ns to ~12 ns.

What remains is a real but constant gap: `ExpectT` builds a `typedExpectation[T]`, claims the
single-use handle with an atomic `CompareAndSwap` and releases it, so it costs roughly 5–8× the
direct call. **`EqualTo` and `ExpectT().ToEqual` are still not the same code path**, and the two
belong in separate rows — but the reason is now handle bookkeeping, not boxing, and the
allocation claim now holds for both.

**3. Testify still wins the nil predicate; the boolean one is a tie.** `assert.NoError` (1.477 ns)
against `EqualTo` on the nil check (3.155 ns) is roughly 2× and held in all three runs taken for
this document — a real result, and it stays visible here. `assert.True` (1.083 ns) against
`EqualTo` (1.248 ns) is **not** a real result: the two overlap inside their spreads and the
ordering flipped between runs. Calling that a Testify win would be reading noise.

The boolean scenario measures all three go-specs shapes. `ExpectT(ctx, true).To(BeTrue())` is
11.24 ns and `ctx.Expect(true).To(BeTrue())` is 10.80 ns — the same within spread. Once a
`Matcher` is involved the `ExpectT` wrapper buys nothing; the typed win belongs to `EqualTo`.

**4. Deep equality flattens the field.** On a slice all three end inside `reflect.DeepEqual`
(264 / 400 / 724 ns) against 2.3 ns for `slices.Equal`. go-specs' advantage is *avoiding* deep
comparison when the type is `comparable`, not performing it faster. Once you compare slices and
maps you pay reflection like everyone else — and all three pay well over 100× the hand-written
cost.

---

## Where this actually matters

One hundred thousand assertions at 172 ns is 17 ms. For an ordinary test suite the per-assertion
cost is irrelevant next to `t.Run`, I/O, fixtures and process startup — a suite is not slow
because of its assertion library.

It matters where assertions run in a tight loop: exploration, mutation and property testing,
which is exactly what go-specs runs internally (`paths_bench_test.go`,
`examples/property_coverage_spy`). At 10⁷ assertions the difference between 1 ns and 495 ns is
seconds of wall clock and hundreds of megabytes of garbage. That is the case the optimisation was
built for, and saying so makes the result credible rather than weaker. A nanosecond difference is
not a universal impact claim: outside those loops it is invisible.

For the cost that dominates a normal suite, see `runner_bench_test.go`, `hooks_bench_test.go` and
`large_suite_bench_test.go`.

---

## Error semantics: the finding this benchmark raised, and its fix

The first revision of this comparison reported a **silent false positive**: `specs.Equal` resolved
through `assert.ValuesEqual` into `reflect.DeepEqual`, which dereferences two `*errorString`
pointers and compares the structs. Two distinct errors carrying the same text compared as equal,
and a wrapped error did not compare equal to the sentinel it wrapped. It failed in both directions:

```go
sentinel := errors.New("boom")
impostor := errors.New("boom")   // a different error, same message
wrapped  := fmt.Errorf("layer: %w", sentinel)

// Before #183:
specs.Equal(sentinel).Match(impostor)          // true   <- WRONG: unrelated errors
specs.Equal(io.EOF).Match(errors.New("EOF"))   // true   <- WRONG: any error reading "EOF"
specs.Equal(sentinel).Match(wrapped)           // false  <- WRONG: errors.Is says true
```

**That is fixed.** [#183](https://github.com/getsyntegrity/go-specs/issues/183) is closed, and the
four cases were re-run against `develop` at `cf7e387` while updating this document:

```go
// After #183:
specs.Equal(sentinel).Match(impostor)          // false  <- correct
specs.Equal(io.EOF).Match(errors.New("EOF"))   // false  <- correct
specs.Equal(sentinel).Match(wrapped)           // true   <- correct
specs.Equal(sentinel).Match(sentinel)          // true   <- correct
```

Equality now asks `errors.Is(actual, expected)` when both sides are errors, and `specs.MatchError`
/ `specs.MatchErrorAs` were added as the explicit spellings. The DSL gap this benchmark surfaced
is closed; nothing here is outstanding.

Worth keeping in view: **Testify's `assert.Equal` still has the false positive go-specs just
removed.** `ObjectsAreEqual(sentinel, impostor)` returns `true` and `ObjectsAreEqual(sentinel,
wrapped)` returns `false`. Testify's answer is the escape hatch — `assert.ErrorIs` / `assert.ErrorAs`
are one call away, so the wrong spelling is a user mistake rather than a library defect. go-specs
now has both: the sound default *and* the explicit matcher.

This is why the `wrapped error identity` scenario carries two go-specs rows. `_GoSpecsTyped` is
`errors.Is` at the call site fed to `EqualTo`, kept so the row stays comparable with earlier runs
of this table. `_GoSpecsMatcher` is `ctx.Expect(err).To(specs.MatchError(target))`, which is the
row semantically equivalent to `assert.ErrorIs` and `gomega.MatchError`: the unwrapping happens
inside the assertion. Comparing a call-site spelling against an in-assertion one would compare two
different shapes, so both are published.

---

## When each library is the right call

- **go-specs** — with `EqualTo` on comparable values the assertion cost is ~1 ns over hand-written
  code, with no allocation; `ExpectT` is a flat 11–13 ns and also allocation-free since #177.
  That only shows up as wall clock in assertion-dense loops. On errors it is now both sound by
  default and the cheapest of the three through `specs.MatchError`.
- **Testify** — level on the boolean predicate, genuinely faster on `NoError`, and much cheaper
  than Gomega everywhere. Boxing plus `reflect.DeepEqual` makes equality assertions two orders of
  magnitude more expensive than the typed path, and `assert.Equal` on two errors still reports
  unrelated errors as equal — use `assert.ErrorIs`.
- **Gomega** — consistently the most expensive and the only one that allocates in every scenario.
  Its value is expressiveness and the `Eventually`/async matcher family, not throughput.
