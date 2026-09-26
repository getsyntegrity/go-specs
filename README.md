# go-specs

[![Go Reference](https://pkg.go.dev/badge/github.com/getsyntegrity/go-specs.svg)](https://pkg.go.dev/github.com/getsyntegrity/go-specs)
[![Go Report Card](https://goreportcard.com/badge/github.com/getsyntegrity/go-specs)](https://goreportcard.com/report/github.com/getsyntegrity/go-specs)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## Project status

go-specs is pre-1.0 (`v0.x`). The public API (`Describe`, `It`, `Context`, `Expectation`, etc.) is still settling and may change without notice between releases. Pin an exact version and check [CHANGELOG.md](CHANGELOG.md) before upgrading.

## Description

**go-specs** is a fast, deterministic BDD-style testing framework for Go. It provides an expressive DSL for writing readable tests while staying close to the standard library and avoiding reflection and allocation overhead. By default, suites run sequentially in declaration order with no hidden concurrency, so test results are stable and reproducible; opt-in parallel execution (`ItParallel`, `RunParallel`, `RunParallelBatched`) is available where throughput matters.

## Key features

- **BDD-style API** — `Describe`, `When`, `It`, `BeforeEach`/`AfterEach` (per spec), and
  `BeforeAll`/`AfterAll` (once per group) for structured specs
- **Deterministic execution** — Specs run in declaration order; no map iteration or nondeterministic scheduling
- **Low overhead** — Zero allocations on the typed assertion path, for values of any size; compiled execution plan
- **Rich assertions** — `Expect(x).ToEqual(y)`, matchers (`BeTrue`, `Equal`, `BeNil`, etc.), composable with `Not`/`All`/`Any`, and snapshot testing
- **Lightweight mocking** — Spies and argument matchers without heavy code generation

## Installation

```bash
go get github.com/getsyntegrity/go-specs
```

## Basic example

```go
package math_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestMath(t *testing.T) {
	specs.Describe(t, "math", func(s *specs.Spec) {
		s.It("adds numbers", func(ctx *specs.Context) {
			ctx.Expect(1 + 1).ToEqual(2)
		})
	})
}
```

## Hooks example

Use `BeforeEach` and `AfterEach` for per-spec setup and teardown:

```go
func TestMathWithHooks(t *testing.T) {
	specs.Describe(t, "math", func(s *specs.Spec) {
		s.BeforeEach(func(ctx *specs.Context) {
			// runs before each It
		})

		s.It("adds numbers", func(ctx *specs.Context) {
			ctx.Expect(add(1, 2)).ToEqual(3)
		})
	})
}
```

`BeforeAll`/`AfterAll` run once per `Describe`/`When` group instead of once per spec — useful for
an expensive fixture (a test database, a server) shared across several specs. A hooked group runs
as its own Go subtest, so it needs an explicit, non-empty name that no sibling spec or other hooked
group shares; a suite that breaks this is rejected while it is built. See
[docs/DSL.md](docs/DSL.md#beforeall--afterall) and
[docs/SUITE_HOOKS_CONTRACT.md](docs/SUITE_HOOKS_CONTRACT.md) for the full contract.

See [examples/basic](examples/basic), [examples/hooks](examples/hooks) and
[examples/suite_hooks](examples/suite_hooks) for runnable examples.

## Benchmarks

go-specs is built for low latency and zero allocations on its own hot path. The honest summary
comes first, because the micro-benchmarks below are easy to over-read:

> **In a normal `go test` run, every spec is a `t.Run` subtest, and the subtest dominates.** Measured
> end to end, go-specs costs about the same as Testify and a little less than Gomega. Its
> nanosecond-level advantage matters where assertions run in tight loops (property, mutation or
> exploration testing), not in an ordinary suite. See [BENCHMARKS.md](BENCHMARKS.md) for which
> claims CI enforces and which are observational.

All figures: Go 1.25.14 (the pinned toolchain), linux/amd64, one Intel Xeon core at 2.1 GHz, medians
of repeated runs. Absolute numbers vary by machine; compare rows within a table, not across machines.

### End to end: what a suite actually costs (`*testing.T`, 1000 specs)

Every framework runs the same 1000 specs, one passing equality assertion each, and every spec is its
own subtest, as `go test -v` reports it. The baseline is a hand-written `t.Run` loop with an `if`,
so the last column shows what each framework adds on top of the testing package itself.

| Framework                                     | per spec | vs `t.Run` baseline | allocs/spec |
| --------------------------------------------- | -------- | ------------------- | ----------- |
| baseline: `t.Run` + `if`                      | ~10.1 µs | 1.00×               | 25          |
| go-specs, suite built once (`BuildSuite`)     | ~12.9 µs | ~1.27×              | 30          |
| Testify: `t.Run` + `assert.Equal`             | ~13.8 µs | ~1.37×              | 27          |
| go-specs, `specs.Describe` (declare + run)    | ~14.3 µs | ~1.41×              | 31          |
| Gomega: `t.Run` + `NewWithT`                  | ~15.0 µs | ~1.48×              | 33          |

Differences under ~10% between rows are within run-to-run noise. Reproduce with `make bench-e2e`
(source: [`benchmarks/e2e_test.go`](benchmarks/e2e_test.go)).

### Assertions (single assertion, no subtest)

| Framework                 | ns/op   | allocs |
| ------------------------- | ------- | ------ |
| go-specs EqualTo          | ~1.9 ns | 0      |
| go-specs Expect().ToEqual | ~12.8 ns| 0      |
| Testify Equal             | ~195 ns | 0      |
| Gomega Expect().To(Equal) | ~510 ns | 3      |

Those rows compare `42` against `42`, which is what the other frameworks' own benchmarks use. It
is also the case least able to allocate: Go serves interface conversions of integers below 256 from
a static table, so a framework that boxes the value still reports 0 allocs/op there. The table below
therefore states the allocation guarantee over values a real spec asserts on instead.

### Allocations by value shape (go-specs)

Measured on the pinned toolchain (Go 1.25.14) with values built at run time, so neither constant
folding nor the static small-integer table can flatter the result.

| Assertion                     | small int | large int | string | struct |
| ----------------------------- | --------- | --------- | ------ | ------ |
| `EqualTo(ctx, a, b)`          | 0         | 0         | 0      | 0      |
| `ExpectT(ctx, a).ToEqual(b)`  | 0         | 0         | 0      | 0      |
| `ExpectT(ctx, a).To(matcher)` | 0         | 1         | 1      | 1      |
| `ctx.Expect(a).ToEqual(b)`    | 0         | 2         | 2      | 2      |

The typed equality path holds the value at its own type, so it allocates nothing whatever `T` is.
The other two rows convert the value to an `any` — `Matcher` is `Match(any)` and `ctx.Expect` takes
an `any` — and for a value outside the static small-integer table that conversion costs an
allocation. Use `EqualTo` or `ExpectT(...).ToEqual(...)` where the comparison is equality; the
numbers above are pinned by tests in `specs/assertion_allocations_test.go`, not only by benchmarks.

> **Semantics differ, not only cost.** `EqualTo`/`ExpectT(...).ToEqual` compare with `==`;
> `ctx.Expect(...).ToEqual` uses `errors.Is` for errors and `reflect.DeepEqual` for everything else.
> Switching an error assertion to the typed path for speed changes what it accepts. See
> [Equality semantics](docs/DSL.md#equality-semantics-differ-between-the-three-toequal-shaped-apis--by-design) in docs/DSL.md.

### Matcher (Expect().To(Equal) style)

| Framework | ns/op    | allocs |
| --------- | -------- | ------ |
| go-specs  | ~14.5 ns | 0      |
| Gomega    | ~488 ns  | 3      |

Both rows assert on a `bool`, which Go converts to an interface without allocating. For a value
that does allocate on conversion, go-specs' matcher path costs one allocation — see the table above.

### Flat execution: framework overhead with the subtest removed

These benchmarks run on a `*testing.B`, the one backend on which the go-specs runner deliberately
skips its per-spec subtest. They isolate the framework's own per-spec overhead and are what the
allocation contracts guard. **They are not what a `go test` run costs** — see the end-to-end table.
The Testify and Gomega rows are plain loops of 1000 assertions with no runner at all.

| 1000 specs, one assertion each                   | time     | per spec | allocs |
| ------------------------------------------------ | -------- | -------- | ------ |
| go-specs, Builder → Program → Runner engine      | ~24.5 µs | ~25 ns   | 0      |
| go-specs, `Describe` engine (`BuildSuite`)¹      | ~70 µs   | ~70 ns   | 0      |
| Testify, 1000 × `assert.Equal` (no runner)       | ~175 µs  | ~175 ns  | 0      |
| Gomega, 1000 × `Expect().To(Equal)` (no runner)  | ~492 µs  | ~492 ns  | 3003   |

¹ Measured on 2000 specs (`BenchmarkRunner_GoSpecs_BuildSuite`) and scaled. `specs.Describe` runs
on this engine, not on the Builder one; see [docs/EXECUTION_ENGINES.md](docs/EXECUTION_ENGINES.md).

| Hooks: 100 specs × 5 nested BeforeEach | time    |
| -------------------------------------- | ------- |
| go-specs (Builder engine)              | ~3.3 µs |
| Testify (hand-written loop)            | ~18 µs  |
| Gomega (hand-written loop)             | ~49 µs  |

| Suite scaling (Builder engine) | Time     |
| ------------------------------ | -------- |
| 100                            | ~2.5 µs  |
| 1000                           | ~24.6 µs |
| 10000                          | ~251 µs  |
| 50000                          | ~1.27 ms |

Earlier versions of this README reported ~1.7 µs for 1000 specs and ~199 ns for the hooks case.
Those figures date from March 2026, before per-spec panic recovery, failure isolation and reporting
were added; each of those correctness fixes added per-spec cost, and the table was never re-measured.

### Why go-specs is fast (where it is)

- **Zero allocations on the typed equality path** — `EqualTo` and `ExpectT(...).ToEqual(...)` allocate nothing on success for a value of any type or size, and every runner loop allocates nothing per spec on the flat path.
The untyped `ctx.Expect(...)` and matcher forms convert the value to an `any`, which costs an allocation for values Go cannot convert for free; the table above gives the exact counts.
This is a contract, not just a measurement: it is pinned by allocation tests that run under `go test ./...`, so a regression fails a PR.
[BENCHMARKS.md](BENCHMARKS.md#contractual-vs-observational-claims) lists exactly which claims are enforced and which are observational — every ns/op figure above is the latter.
- **Compiled execution plan** — Suites are compiled once into a fixed program; the runner executes steps via direct function dispatch instead of per-spec lookups or reflection.
- **No reflection on the typed path** — Typed assertions use generics and `==`; the fast path avoids `reflect.DeepEqual` and runtime type switches.
- **Sequential runner loop** — The default runner invokes spec and hook functions in a simple loop with direct calls. Opt-in parallel paths (`ItParallel`, `RunParallel`, `RunParallelBatched`) trade this loop for a worker pool when a suite benefits from concurrency.

Reproducible benchmark suite: [benchmarks/](benchmarks). From the repository root run `make bench` (quick), `make bench-report` (10 runs, written to `benchmarks/results/current.txt`) or `make bench-e2e` (the end-to-end `*testing.T` table).

## Architecture overview

go-specs compiles a spec tree (from `Describe` / `It` / `BeforeEach` / etc.) into an execution plan once. The runner then executes that plan in order: for each spec it runs before hooks, the spec body, and after hooks (LIFO). No maps or reflection are used at run time; the plan is a flat sequence of steps with direct function pointers. Parallel specs (`ItParallel` via the Builder) are grouped into a single step and run concurrently, then execution continues sequentially. `MinimalRunner.RunParallel`/`RunParallelBatched` offer an additional opt-in worker-pool execution path, distributing specs across goroutines instead of the default sequential loop. The repository is a single Go module; packages include:

- **specs** — Core DSL, runner, context, and execution plan
- **assert** — Matcher implementations (Equal, BeTrue, BeNil, etc.) and composition (Not, All, Any)
- **benchmarks** — Benchmark suite (go-specs vs Testify vs Gomega)
- **mock** — Spies and argument matchers
- **snapshots** — Snapshot testing support
- **examples** — Example tests (basic, hooks, parallel, and more)

## Running benchmarks

From the repository root:

| Target | Description |
|--------|-------------|
| `make bench` | Run benchmarks once (output to terminal). |
| `make bench-report` | Run benchmarks 10 times and write report to `benchmarks/results/current.txt`. |
| `make bench-e2e` | Measure the real `go test` path: 1000 specs as `*testing.T` subtests, go-specs vs Testify vs Gomega vs a bare `t.Run` baseline. |
| `make bench-compare` | Compare `benchmarks/results/previous.txt` vs `current.txt` with [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) (install: `go install golang.org/x/perf/cmd/benchstat@latest`). |

**Generate a report for the README or CI:**

```bash
make bench-report
```

**Compare before/after a change:**

```bash
make bench-report
cp benchmarks/results/current.txt benchmarks/results/previous.txt
# ... make your changes ...
make bench-report
make bench-compare
```

Run by category (direct `go test`):

```bash
go test ./benchmarks -bench=BenchmarkAssertion -benchmem
go test ./benchmarks -bench=BenchmarkRunner -benchmem
go test ./benchmarks -bench=BenchmarkSuite_ -benchmem
```

See [benchmarks/README.md](benchmarks/README.md) for full benchmark layout and options.

## Contributing

Contributions are welcome. Before submitting changes:

1. Run the test suite: `go test ./...` and `go test -race ./...`
2. Run the linter: `go vet ./...`
3. If you change performance-sensitive code, run: `go test ./benchmarks -bench=. -benchmem`

Please open an issue or discussion for larger changes so we can align on direction.

## License

MIT License. See [LICENSE](LICENSE) for details.

---

Maintained by [GetSyntegrity](https://github.com/getsyntegrity).
Created and maintained by Pablo Gore ([@pablogore](https://github.com/pablogore)).
