# Benchmark suite: go-specs vs Testify vs Gomega

Reproducible benchmarks comparing **go-specs**, **Testify** (assert), and **Gomega** on assertions, matchers, runner execution, hooks, and large suites.

## Run all benchmarks

From the **repository root** (recommended):

```bash
make bench              # quick run, output to terminal
make bench-report       # run 10 times, write report to benchmarks/results/current.txt
make bench-compare      # compare previous.txt vs current.txt (requires benchstat)
```

Or with `go test` directly:

```bash
go test ./benchmarks -bench=. -benchmem
```

From the **benchmarks** directory:

```bash
cd benchmarks
go test -bench=. -benchmem
```

## Run by category

```bash
# Assertions (single equality assertion)
go test ./benchmarks -bench=BenchmarkAssertion -benchmem

# Matchers (Expect().To(BeTrue) style)
go test ./benchmarks -bench=BenchmarkMatcher -benchmem

# Runner execution (N specs, one assertion per spec)
go test ./benchmarks -bench=BenchmarkRunner -benchmem

# Hooks (before-each + assertion per spec)
go test ./benchmarks -bench=BenchmarkHooks -benchmem

# Large-scale suites (100, 1000, 10000, 50000 specs; execution time + allocs + scaling)
go test ./benchmarks -bench=BenchmarkSuite_ -benchmem
```

## Structure

| File | Description |
|------|-------------|
| `helpers.go` | Suite generation: `BuildSpecsProgram(n)`, `CreateGoSpecsSuite(n)`, `SuiteSize100/1000/10000/50000` |
| `assertion_bench_test.go` | `BenchmarkAssertion_GoSpecs_EqualTo`, `_ExpectToEqual`, `BenchmarkAssertion_Testify_Equal`, `BenchmarkAssertion_Gomega_ExpectToEqual` |
| `matcher_bench_test.go` | `BenchmarkMatcher_GoSpecs`, `BenchmarkMatcher_Gomega` |
| `comparison_bench_test.go` | `BenchmarkCompare_*_GoSpecs`, `_Testify`, `_Gomega` — see [COMPARISON.md](COMPARISON.md) |
| `runner_bench_test.go` | `BenchmarkRunner_GoSpecs`, `BenchmarkRunner_Testify`, `BenchmarkRunner_Gomega` |
| `hooks_bench_test.go` | `BenchmarkHooks_GoSpecs`, `BenchmarkHooks_Testify`, `BenchmarkHooks_Gomega` |
| `large_suite_bench_test.go` | `BenchmarkSuite_100`, `_1000`, `_10000`, `_50000` (large-scale; suite creation outside timed region) |
| `minimal_and_buildsuite_bench_test.go` | `BenchmarkRunner_GoSpecs_BuildSuite`, `BenchmarkRunner_Minimal`, `BenchmarkRunner_MinimalParallel_*`, `BenchmarkHooks_GoSpecs_Nested` (from former `bench/` package) |

## Requirements

- **Deterministic**: Same N produces the same program shape; no randomness.
- **Realistic**: Suite sizes 100, 1000, 10000 where applicable.
- **go-specs target**: Zero allocations in assertion/runner fast path where possible (`0 allocs/op`).
  Where that target is an actual guarantee it is pinned by `specs/allocation_contract_test.go`; see
  [BENCHMARKS.md](../BENCHMARKS.md#contractual-vs-observational-claims). A benchmark printing `0 allocs/op`
  never fails a build on its own — benchmarks measure, tests gate.
- **Isolate setup**: Build suite / create runner before `b.ResetTimer()`; only the measured loop runs after.
- **Avoid reflection** in go-specs benchmarks (use `EqualTo` / `ExpectT().ToEqual` for comparable types).

## Fair comparison

- **Assertion**: Context/setup created once; loop measures only the assertion. Same comparison (42 == 42) for all three.
- **Runner**: Same number of specs (1000) and one equality per spec.
- **Hooks**: Same hook depth (5) and spec count (100); one assertion per spec.
- **Large suite**: go-specs at 100, 1000, 10000 specs to measure scalability.

## Statistical comparison (benchstat)

Results are stored under `benchmarks/results/`. Scripts run from the **repository root** (the directory containing `benchmarks/` and `scripts/`).

### Run the suite and save results

```bash
make bench-report
```

This runs `go test ./benchmarks -bench=. -benchmem -count=10` and writes output to `benchmarks/results/current.txt`. Use `-count=10` for stable statistics.

### Compare previous vs current

1. Install benchstat (once):

   ```bash
   go install golang.org/x/perf/cmd/benchstat@latest
   ```

2. First time: run the suite and save a baseline as `previous.txt`:

   ```bash
   make bench-report
   cp benchmarks/results/current.txt benchmarks/results/previous.txt
   ```

3. After making changes, run the suite again, then compare:

   ```bash
   make bench-report
   make bench-compare
   ```

   `make bench-compare` runs `benchstat` on `benchmarks/results/previous.txt` and `current.txt` and prints a comparison table (delta and significance).

### Determinism

Run from the same directory (repo root) and the same OS. For more stable results, close other heavy processes and avoid changing `GOMAXPROCS` between runs.

### Charts (matplotlib)

Generate bar charts from `benchmarks/results/current.txt`:

```bash
pip install -r scripts/requirements-charts.txt   # or: pip install matplotlib
python3 scripts/bench_to_chart.py
```

Outputs (in `benchmarks/results/`): `assertion_chart.png`, `runner_chart.png`, `hooks_chart.png`. Each chart shows framework name, ns/op, and relative performance (× vs fastest). If a category has no data (e.g. missing benchmarks), that chart is skipped with a warning.

## Prerequisites

The `specs` package must build. From repo root: `go build ./specs/...`
