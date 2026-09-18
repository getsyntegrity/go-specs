# Benchmarks

Benchmark methodology, environment, and results for go-specs compared with Testify and Gomega.

## Hardware and environment

| Item | Value |
| ---- | ----- |
| **Hardware** | Apple M4 Max |
| **Go version** | Go 1.26+ (see [go.mod](../go.mod)) |

## Benchmark command

From the repository root:

```bash
go test ./benchmarks -bench=. -benchmem
```

Setup (building the suite, creating the runner) is done before `b.ResetTimer()` so the reported time and allocations reflect only the measured loop.

## Assertions

Single equality assertion (same comparison across frameworks):

| Assertion                 | ns/op   | allocs |
| ------------------------- | ------- | ------ |
| GoSpecs EqualTo           | ~1 ns   | 0      |
| GoSpecs Expect().ToEqual  | ~7 ns   | 0      |
| Testify assert.Equal      | ~86 ns  | 0      |
| Gomega Expect().To(Equal) | ~238 ns | 3      |

### Assertion execution cost

Execution paths differ by framework. go-specs avoids matcher allocation and reflection on the hot path.

```mermaid
flowchart LR
    A[Test Assertion] --> B[go-specs]
    B --> C[Direct Comparison]
    C --> D[Return]

    A --> E[Testify]
    E --> F[Helper + Assertion]
    F --> G[Comparison]
    G --> H[Return]

    A --> I[Gomega]
    I --> J[Create Matcher]
    J --> K[Reflection]
    K --> L[Compare]
    L --> M[Return]
```

go-specs takes a direct comparison path; Testify adds helper and assertion layers; Gomega allocates matchers and uses reflection. See [PERFORMANCE.md](PERFORMANCE.md) for details.

## Runner

Full suite run (1000 specs, one assertion per spec). Time per run:

| Framework | Time    |
| --------- | ------- |
| GoSpecs   | ~1.7 µs |
| Testify   | ~86 µs  |
| Gomega    | ~238 µs |

## Why go-specs is faster

- **No reflection** — Assertions use generics and direct comparison. The fast path does not use `reflect.DeepEqual` or runtime type switches.
- **Compiled execution plan** — Suites are compiled once into a flat list of steps. The runner does not resolve hooks or look up specs at run time.
- **Zero allocations on the typed equality path** — the Context is pooled and the handle never escapes the assertion that consumes it, so it is stack-allocated; `EqualTo` and `ExpectT(...).ToEqual(...)` also hold the value at its own type, so they allocate nothing for a value of any size. The runner loop allocates nothing per spec.
- **Simple runner loop** — The runner just iterates over steps and calls `step(ctx)`. No maps, no reflection, no per-spec allocation.

### Allocation comparison

On the typed equality path (`EqualTo`, `ExpectT(...).ToEqual(...)`) go-specs performs no allocations
during test execution on the success path, for values of any type or size; other frameworks incur
helper or matcher allocations. The matcher and untyped forms convert the value to an `any` and cost
one and two allocations respectively for values Go cannot convert for free — see the table in
[../README.md#allocations-by-value-shape-go-specs](../README.md).

```mermaid
flowchart LR
    GoSpecs[go-specs] --> ZeroAlloc[0 allocations]
    Testify[Testify] --> SomeAlloc[helper calls]
    Gomega[Gomega] --> ManyAlloc[matcher allocations]
```

See [PERFORMANCE.md](PERFORMANCE.md) for execution cost diagrams and design choices.

## Scaling and benchmark scaling visualization

Large-suite benchmarks measure how execution time scales with the number of specs (100, 1000, 10000, 50000). Suite creation is outside the timed region; only execution is measured.

```mermaid
graph LR
    Suite100[Suite 100] --> Suite1000[Suite 1000]
    Suite1000 --> Suite10000[Suite 10000]
    Suite10000 --> Suite50000[Suite 50000]
```

Runtime scales **linearly** with suite size because execution is a simple sequential loop over the compiled step list. Doubling the number of specs doubles the number of steps and thus roughly doubles run time; there is no extra per-spec overhead from maps, reflection, or allocation in the runner.

Run large-suite benchmarks:

```bash
go test ./benchmarks -bench=BenchmarkSuite_ -benchmem
```

## Further reading

- [PERFORMANCE.md](PERFORMANCE.md) — Why go-specs is fast: assertion cost, runner loop, allocation behavior, scaling, and design choices.
- [benchmarks/README.md](../benchmarks/README.md) — Full benchmark suite layout, categories, and scripts (benchstat, charts).
- [ARCHITECTURE.md](ARCHITECTURE.md) — How the DSL compiles to a program and how the runner executes it.
