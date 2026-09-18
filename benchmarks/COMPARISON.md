# go-specs vs Testify vs Gomega — assertion comparison

Conclusions from `benchmarks/comparison_bench_test.go`.

Reproduce:

```
go test ./benchmarks -run=NONE -bench=BenchmarkCompare -benchmem -count=5
benchstat <output>
```

Environment of the run recorded below: `go1.26.6 linux/amd64`, 13th Gen Intel Core i7-13620H (16 threads), `-count=5`, medians via benchstat. Absolute numbers move with hardware; the ratios are what matter.

---

## Results

| Scenario | go-specs | Testify | Gomega |
| --- | --- | --- | --- |
| int equality | **1.22 ns** · 0 allocs | 135.5 ns · 0 allocs | 434.0 ns · 3 allocs (96 B) |
| string equality | **2.17 ns** · 0 allocs | 171.8 ns · 1 alloc (16 B) | 481.5 ns · 4 allocs (112 B) |
| comparable struct equality | **2.41 ns** · 0 allocs | 198.5 ns · 1 alloc (24 B) | 549.6 ns · 4 allocs (120 B) |
| slice deep equality | **273.6 ns** · 3 allocs (64 B) | 323.8 ns · 2 allocs (48 B) | 656.2 ns · 5 allocs (144 B) |
| boolean true | 14.23 ns · 0 allocs | **1.08 ns** · 0 allocs | 425.0 ns · 3 allocs (96 B) |
| nil / no error | 15.30 ns · 0 allocs | **1.38 ns** · 0 allocs | 395.7 ns · 2 allocs (80 B) |
| substring contains | **61.10 ns** · 2 allocs (32 B) | 145.1 ns · 1 alloc (16 B) | 417.6 ns · 4 allocs (144 B) |
| wrapped error identity | **23.57 ns** · 0 allocs | 118.6 ns · 0 allocs | 487.6 ns · 3 allocs (128 B) |

Assertions measured are the passing (happy) path. Setup lives outside the timed region.

---

## Conclusions

**1. The typed fast path is the whole story.** `specs.EqualTo` / `specs.ExpectT(...).ToEqual` are generic over `comparable` and compare with `==`. No boxing into `any`, no reflection, no allocation — 1–2 ns. Testify is 60–110× slower on the same assertion because `assert.Equal` takes `any`, which forces boxing, then goes through `ObjectsAreEqual` (byte compare plus `reflect.DeepEqual`). Gomega is 200–350× slower and allocates on every call, because each `Expect(...).To(Equal(...))` builds an assertion object and a matcher value before doing any comparison.

**2. Gomega never reaches zero allocations.** Every scenario costs 2–5 allocations. That is inherent to the matcher-value design, not a tuning issue. In a suite of 50k assertions it is real GC pressure.

**3. Testify wins the boolean and nil checks — and go-specs should not pretend otherwise.** `assert.True` and `assert.NoError` are hand-optimised branches: check the value, return. ~1 ns. The go-specs equivalents go through the `Matcher` interface (`ctx.Expect(v).To(BeTrue())`), which costs an interface call plus boxing: ~14–15 ns. Still 25–30× faster than Gomega, but slower than Testify. **Practical rule: in hot paths prefer the typed form (`ExpectT(...).ToEqual`) over the matcher form.** The matcher form buys readability and composition, and it costs roughly 13 ns.

**4. Deep equality flattens the field.** On a slice all three end up inside `reflect.DeepEqual`, so the results converge (274 / 324 / 656 ns). go-specs' advantage is not a faster deep comparison — it is *avoiding* deep comparison whenever the type is `comparable`. Once you compare slices and maps, you pay reflection like everyone else.

**5. Semantic gap found while writing this benchmark: `specs.Equal` does not unwrap errors.** It resolves through `assert.ValuesEqual`, which ends in `reflect.DeepEqual` and therefore does not see through `fmt.Errorf("%w")` or `errors.Join`. Testify (`assert.ErrorIs`) and Gomega (`MatchError`) both unwrap inside the assertion. The idiomatic go-specs spelling today is `errors.Is` at the call site — which is what the benchmark measures, and which is also why go-specs is the fastest row there. There is a separate `assert.EqualValues` helper that *does* use `errors.Is`, but it is not wired into the `Equal` matcher. **An `ErrorIs` / `MatchError` matcher is a genuine gap in the DSL.**

---

## When each library is the right call

- **go-specs** — the assertion cost effectively disappears for comparable values. In large or generated suites (10k+ assertions) the difference against Gomega is seconds of wall clock and megabytes of garbage.
- **Testify** — competitive on the trivial boolean/nil predicates and much cheaper than Gomega everywhere, but boxing plus `reflect.DeepEqual` makes every equality assertion two orders of magnitude more expensive than the typed path.
- **Gomega** — consistently the most expensive and the only one that always allocates. Its value is expressiveness and the `Eventually`/async matcher family, not throughput.

One caveat to keep honest: these are microbenchmarks of a single assertion. In a real suite, runner scheduling and hooks dominate — see `runner_bench_test.go`, `hooks_bench_test.go` and `large_suite_bench_test.go`. Assertion cost only becomes the bottleneck when assertions are dense.
