# Contributing

Thank you for considering contributing to go-specs. This document covers development setup, testing, and the principles we follow.

## Development setup

1. Clone the repository and ensure you have a supported Go version (see [go.mod](../go.mod)).

2. Install dependencies:

   ```bash
   go mod tidy
   ```

3. Run the full test suite:

   ```bash
   go test ./...
   ```

4. Run tests with the race detector:

   ```bash
   go test -race ./...
   ```

5. Run the linter:

   ```bash
   go vet ./...
   ```

All of the above should pass before submitting a change.

## Benchmark testing

When changing performance-sensitive code (runner, assertions, context, compiler), run the benchmark suite and ensure you do not regress allocations or throughput:

```bash
go test ./benchmarks -bench=. -benchmem
```

Prefer:

- **Zero allocations** in the hot path (assertion success, runner loop).
- **Deterministic execution** — same input produces the same order of execution.
- **Minimal reflection** — use typed APIs and generics where possible.

For statistical comparison between two runs (e.g. before and after a change), use [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) and the scripts in [benchmarks/README.md](../benchmarks/README.md).

## Architecture (brief)

go-specs follows a simple pipeline: **DSL → Builder → Program → Runner**.

- The **DSL** (`Describe`, `BeforeEach`, `AfterEach`, `It`) is the user-facing API.
- The **Builder** (or bytecode compiler) compiles the DSL into a flat execution plan (steps or groups of steps).
- The **Program** is the compiled plan—no tree, no hook resolution at run time.
- The **Runner** executes the plan by iterating over steps and calling each with a pooled Context.

For more detail, see [ARCHITECTURE.md](ARCHITECTURE.md) and [EXECUTION_MODEL.md](EXECUTION_MODEL.md).

## What we encourage

- **Zero allocations in hot paths** — Use sync.Pool for the Context; avoid allocating in the assertion success path and in the runner loop. Do **not** pool objects that are handed to user code: an expectation the caller still holds must never be handed to another spec (see issue #170). Rely on escape analysis instead — keep the object from escaping the call that consumes it and it costs nothing.
- **Deterministic execution** — Specs run in a fixed order; no map iteration or nondeterministic scheduling that could change outcome order.
- **Minimal reflection** — Prefer generics and direct comparison; avoid `reflect.DeepEqual` and runtime type switches on the hot path.

If you are unsure about a change, open an issue or discussion first so we can align on approach.
