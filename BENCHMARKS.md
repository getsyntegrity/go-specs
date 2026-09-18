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
| runner_bench_test.go            | N specs, one assertion per spec   |
| hooks_bench_test.go             | before-each + assertion per spec  |
| large_suite_bench_test.go       | scaling (100, 1000, 10000, 50000)  |
| minimal_and_buildsuite_bench_test.go | BuildSuite runner, MinimalRunner, parallel, nested hooks |
| builder_build_bench_test.go      | Builder construction cost          |
| describe_variants_bench_test.go  | Describe / DescribeFlat / DescribeFast variants |
| paths_bench_test.go              | Paths generation and exploration   |

See [benchmarks/README.md](benchmarks/README.md) for categories and scripts.

---

# Running Benchmarks

```
go test ./benchmarks -run='^$' -bench=. -benchmem
```

---

# Measured Performance

Last recorded baseline (`benchmarks/results/BENCH_SUMMARY.md`, averaged over 10 runs, Apple M4 Max):

| Operation                       | go-specs | allocs |
| ------------------------------- | -------- | ------ |
| Assertion, `EqualTo`            | ~1 ns    | 0      |
| Assertion, `Expect().ToEqual`   | ~7.4 ns  | 0      |
| Runner, 1000 specs              | ~1.7 µs  | 0      |
| Hooks, 100 specs × 5 `BeforeEach` | ~199 ns | 0      |

Comparisons against Testify and Gomega, scaling to 50,000 specs, and the reasoning behind these
numbers are in [docs/08-PERFORMANCE.md](docs/08-PERFORMANCE.md). Regenerate the baseline with
`make bench-report`.

An earlier revision of this file gave an "expected" assertion cost of 80–150 ns; that is the
comparators' range, not go-specs', and it predates the zero-allocation fast path.

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
