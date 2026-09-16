# go-specs

[![Go Reference](https://pkg.go.dev/badge/github.com/pablogore/go-specs.svg)](https://pkg.go.dev/github.com/pablogore/go-specs)
[![Go Report Card](https://goreportcard.com/badge/github.com/pablogore/go-specs)](https://goreportcard.com/report/github.com/pablogore/go-specs)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## Project status

go-specs is pre-1.0 (`v0.x`). The public API (`Describe`, `It`, `Context`, `Expectation`, `Mutator`, etc.) is still settling and may change without notice between releases. Pin an exact version and check [CHANGELOG.md](CHANGELOG.md) before upgrading.

## Description

**go-specs** is a fast, deterministic BDD-style testing framework for Go. It provides an expressive DSL for writing readable tests while staying close to the standard library and avoiding reflection and allocation overhead. By default, suites run sequentially in declaration order with no hidden concurrency, so test results are stable and reproducible; opt-in parallel execution (`ItParallel`, `RunParallel`, `RunParallelBatched`) is available where throughput matters.

## Key features

- **BDD-style API** — `Describe`, `When`, `It`, `BeforeEach`, and `AfterEach` for structured specs
- **Deterministic execution** — Specs run in declaration order; no map iteration or nondeterministic scheduling
- **Low overhead** — Zero allocations on the assertion fast path; compiled execution plan
- **Rich assertions** — `Expect(x).ToEqual(y)`, matchers (`BeTrue`, `Equal`, `BeNil`, etc.), and snapshot testing
- **Combinatorial testing** — Path builder for deterministic exploration of parameter spaces
- **Lightweight mocking** — Spies and argument matchers without heavy code generation

## Installation

```bash
go get github.com/pablogore/go-specs
```

## Basic example

```go
package math_test

import (
	"testing"

	"github.com/pablogore/go-specs/specs"
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

See [examples/basic](examples/basic) and [examples/hooks](examples/hooks) for runnable examples.

## Benchmarks

go-specs is built for low latency and zero allocations on the hot path. The tables below compare single-assertion cost, runner execution (1000 specs), hooks, matchers, and suite scaling against Testify and Gomega. Values are averaged from `make bench-report` (10 runs).

### Assertions

| Framework                 | ns/op   | allocs |
| ------------------------- | ------- | ------ |
| go-specs EqualTo          | ~1 ns   | 0      |
| go-specs Expect().ToEqual  | ~7.4 ns | 0      |
| Testify Equal              | ~86 ns  | 0      |
| Gomega Expect().To(Equal)  | ~241 ns | 3      |

### Runner (1000 specs, one assertion per spec)

| Framework | ns/op   |
| --------- | ------- |
| go-specs  | ~1.7 µs |
| Testify   | ~85 µs  |
| Gomega    | ~244 µs |

### Hooks (100 specs, 5 nested BeforeEach + one assertion per spec)

| Framework | ns/op   |
| --------- | ------- |
| go-specs  | ~199 ns |
| Testify   | ~8.5 µs |
| Gomega    | ~24 µs  |

### Matcher (Expect().To(Equal) style)

| Framework | ns/op   | allocs |
| --------- | ------- | ------ |
| go-specs  | ~7.8 ns | 0      |
| Gomega    | ~227 ns | 3      |

### Suite scaling (go-specs)

| Specs | Time    |
| ----- | ------- |
| 100   | ~201 ns |
| 1000  | ~1.7 µs |
| 10000 | ~17 µs  |
| 50000 | ~85 µs  |

### Why go-specs is fast

- **Zero allocations** — The assertion and runner hot paths allocate nothing on success (0 allocs/op above), reducing GC pressure.
- **Compiled execution plan** — Suites are compiled once into a fixed program; the runner executes steps via direct function dispatch instead of per-spec lookups or reflection.
- **No reflection** — Assertions use generics and direct comparison; the fast path avoids `reflect.DeepEqual` and runtime type switches.
- **Sequential runner loop** — The default runner invokes spec and hook functions in a simple loop with direct calls; no matcher heap allocations or indirection on the hot path. Opt-in parallel paths (`ItParallel`, `RunParallel`, `RunParallelBatched`) trade this loop for a worker pool when a suite benefits from concurrency.

Reproducible benchmark suite: [benchmarks/](benchmarks/). From the repository root run `make bench` (quick) or `make bench-report` to generate a report in `benchmarks/results/current.txt`.

## Architecture overview

go-specs compiles a spec tree (from `Describe` / `It` / `BeforeEach` / etc.) into an execution plan once. The runner then executes that plan in order: for each spec it runs before hooks, the spec body, and after hooks (LIFO). No maps or reflection are used at run time; the plan is a flat sequence of steps with direct function pointers. Parallel specs (`ItParallel` via the Builder) are grouped into a single step and run concurrently, then execution continues sequentially. `MinimalRunner.RunParallel`/`RunParallelBatched` offer an additional opt-in worker-pool execution path, distributing specs across goroutines instead of the default sequential loop. The repository is a single Go module; packages include:

- **specs** — Core DSL, runner, context, and execution plan
- **assert** — Matcher implementations (Equal, BeTrue, BeNil, etc.)
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
