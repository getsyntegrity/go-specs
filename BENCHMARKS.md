# BENCHMARKS.md

This document explains how benchmarks are structured in go-specs.

---

# Benchmark Philosophy

Benchmarks measure **subsystems independently**.

The framework has three primary cost centers:

1. Assertions
2. Runner execution
3. Path exploration

---

# Benchmark Suites

All benchmarks live in a single directory:

```
benchmarks/
```

Files:

| File                             | Benchmark                          |
| -------------------------------- | ---------------------------------- |
| assertion_bench_test.go          | single assertion cost (go-specs, Testify, Gomega) |
| matcher_bench_test.go            | matcher / Expect().To() performance |
| comparison_bench_test.go         | head-to-head go-specs vs Testify vs Gomega across 8 assertion scenarios (conclusions in [benchmarks/COMPARISON.md](benchmarks/COMPARISON.md)) |
| runner_bench_test.go            | N specs, one assertion per spec   |
| hooks_bench_test.go             | before-each + assertion per spec  |
| large_suite_bench_test.go       | scaling (100, 1000, 10000, 50000)  |
| minimal_and_buildsuite_bench_test.go | BuildSuite runner, MinimalRunner, parallel, nested hooks |

See [benchmarks/README.md](benchmarks/README.md) for categories and scripts.

---

# Running Benchmarks

```
go test ./benchmarks -run='^$' -bench=. -benchmem
```

For real numbers, use `make bench-report` (10 iterations) or read the charts that
`benchmarks.yml` publishes from `main`.

---

# Benchmarks in PR CI

`go test ./...` never runs a `Benchmark` function. Benchmark bodies therefore compile
in CI and are never executed, which once let matcher code paths be exercised by
benchmarks and by nothing else — a panic on such a path would have shipped green.

`make bench-smoke` closes that gap and runs on every PR:

```
go test -run='^$' -bench=. -benchtime=1x -benchmem ./...
```

`-benchtime=1x` runs each benchmark for exactly one iteration. That is enough to
**execute** the path and far too little to **measure** it, which is the whole design:
a shared CI runner is throttled and noisy, so this step asserts no wall-clock
threshold and cannot fail on a slow machine. It costs roughly three seconds.

Correctness tests remain the primary contract. Benchmark execution is supplementary
coverage — it proves the code runs, not that it runs fast.

---

# Contractual vs observational claims

Not every number this project publishes is a promise. The distinction matters, because
a *contract* is something a PR may fail on and a maintainer must fix, while an
*observation* is a measurement that is allowed to move.

## Contractual — enforced by `go test ./...`

Pinned in [`specs/allocation_contract_test.go`](specs/allocation_contract_test.go) with
`testing.AllocsPerRun`. Allocation counts are a property of the generated code, not of
the machine, so they are stable across runners and safe to gate a PR on.

| Guarantee | Pinned by |
| --------- | --------- |
| The **published per-form, per-value-shape allocation table**: `EqualTo` and `ExpectT(...).ToEqual` cost nothing for any comparable `T` at any width; `ExpectT(...).To(matcher)` and `ctx.Expect(...)` cost exactly the interface conversions their signatures force | `TestAssertionAllocationsByValueShape` (`specs/assertion_allocations_test.go`) |
| Valueless matchers (`BeTrue`, `BeFalse`, `BeNil`) allocate nothing | `TestValuelessMatchersAllocateNothing` |
| The recommended error idiom, `errors.Is(...)` fed into `ExpectT(...).To(BeTrue())`, allocates nothing | `TestErrorClassificationAllocatesNothing` |
| The runner loop has **no per-spec allocation**: 5000 specs cost no more allocations than 100 | `TestMinimalRunnerLoopAllocatesNothingPerSpec`, `TestBlockRunnerLoopAllocatesNothingPerSpec`, `TestProgramRunnerLoopAllocatesNothingPerSpecOnTheFlatPath` |

`TestAssertionAllocationsByValueShape` is the authority on assertions, and it pins **exact
counts in both directions** -- so a cost that silently grows *or* disappears fails the
build. It builds its probe values in `init()` rather than writing them as literals, which
matters more than it looks: a string or struct written inline in a test function is folded
by the compiler before the assertion sees it, so a naive test measures the optimiser and
reports a zero the real code path does not deliver. Note in particular that
`ctx.Expect(str).ToEqual(str)` costs **two** allocations for a real string, one per operand
-- it is *not* allocation-free, and only the small-integer case looks like it is.

## Observational — measured, never asserted

These are true today and worth knowing. None of them fails a build.

- **Every ns/op figure**, here and in the README tables. Wall-clock depends on the CPU,
  the runner and what else is on the box. No shared-CI job asserts a timing threshold.
- **Comparisons against Testify and Gomega.** They pin the shape of the difference, not
  a ratio; see [benchmarks/COMPARISON.md](benchmarks/COMPARISON.md).
- **Constructing a value-capturing matcher costs one allocation.** `Equal(x)`,
  `NotEqual(x)` and `Contain(x)` allocate once for the matcher value itself. Build the
  matcher outside a hot loop if it is constant, as the benchmarks and the allocation table
  do, so the cost is attributed to the matcher rather than to the assertion.
- **`ExpectT(...).To(matcher)` costs one further allocation for most values.** `Matcher` is
  `Match(any)`, so the value becomes an interface before a matcher can see it, and for a
  value Go does not convert for free that conversion allocates once. Nothing in `ExpectT`
  can remove it — only a generic `Matcher[T]` would — so it is measured by
  `TestAssertionAllocationsByValueShape` rather than pinned. `ToEqual` is the
  allocation-free route for equality, at any width.
- **A run of the compiled `Program` runner costs a small, fixed number of allocations**
  (about 2 for a one-spec suite, 7 for a large one) for pooled setup. The contract is
  that this number does not grow with spec count, not that it is zero.
- **The `*testing.T` path allocates per spec.** When the runner is handed a real
  `*testing.T` it opens a `t.Run` subtest per spec, which costs tens of allocations each
  inside the standard library. That is the deliberate price of per-spec test identity in
  `go test -v` output; the allocation-free claim covers the flat path only.

## Not measured in shared CI at all

Wall-clock regression detection. [`tools/perfcheck`](tools/perfcheck) can compare two
benchmark runs against a threshold, but it belongs on a dedicated, quiet machine —
running it on a shared GitHub runner would produce flakes, not signal.

---

# Expected Performance

Observational, not contractual — see above. Typical figures:

| Operation        | ns/op     |
| ---------------- | --------- |
| Assertion        | 80–150    |
| Runner           | 10–40 µs  |
| Path exploration | 50–200 µs |

---

# Important Rule

Do not benchmark:

```
suite construction
DSL parsing
registry initialization
```

inside `b.N` loops.

These must happen before `b.ResetTimer()`.
