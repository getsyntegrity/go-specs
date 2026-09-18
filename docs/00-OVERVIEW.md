# 00 · Overview

> **Audience:** everyone · **Reading time:** ~8 minutes

## 1. What go-specs is

go-specs is a BDD-style testing framework for Go that compiles a declarative suite into a flat
execution plan and runs it on top of `testing.TB`. It does not replace `go test` — it runs inside it.
Every spec becomes a Go subtest, every failure reaches the terminal through the ordinary
`*testing.T` reporting path, and `go test -run`, `-race`, `-timeout` and caching all keep working.

```go
specs.Describe(t, "math", func(s *specs.Spec) {
    s.It("adds numbers", func(ctx *specs.Context) {
        ctx.Expect(1 + 1).ToEqual(2)
    })
})
```

## 2. The problem it addresses

Go's own `testing` package is deliberately minimal. Teams that want nested scopes, shared setup,
readable failure messages and structured reports reach for a framework, and the popular ones charge
for it in two currencies:

- **Dependency weight.** A test framework that pulls a dependency tree into `go.mod` puts that tree
  in every downstream consumer's module graph.
- **Runtime cost.** Assertion and runner overhead is paid once per assertion and once per spec. On a
  suite of tens of thousands of specs that overhead stops being a rounding error.

go-specs takes the position that a framework can offer the expressive surface **without** either
cost: the execution core imports nothing outside the standard library
([ADR-0001](adr/0001-standard-library-only-core.md)), and the hot paths are written to allocate
nothing ([ADR-0003](adr/0003-compiled-execution-plan.md), [08 · Performance](08-PERFORMANCE.md)).

## 3. What it guarantees

| Guarantee | Where it comes from |
| --- | --- |
| **Deterministic execution** — specs run in declaration order; no map iteration, no scheduler-dependent ordering, no ambient randomness | [ADR-0002](adr/0002-deterministic-execution.md) |
| **Per-spec isolation** — a panic in one spec is recorded and the run continues; a `Fatal` in one spec never skips another spec's cleanup | [ADR-0004](adr/0004-subtest-isolation-and-goexit-safe-hooks.md) |
| **Fail closed** — a declaration with nowhere to register panics instead of producing an empty green suite | [ADR-0005](adr/0005-fail-closed-dsl-registration.md) |
| **Observational reporting** — attaching a reporter never changes execution semantics | [ADR-0007](adr/0007-observational-constructor-injected-reporting.md) |
| **Zero third-party runtime dependencies** | [ADR-0001](adr/0001-standard-library-only-core.md) |
| **A stable public DSL** | [ADR-0011](adr/0011-public-dsl-stability-contract.md) |

## 4. The packages

| Package | Role | Imports `specs`? |
| --- | --- | --- |
| `specs/` | The execution core: DSL, compiler, runners, scheduler, sharding | — |
| `assert/` | Matchers and equality primitives | No |
| `mock/` | Spies and call-recording test doubles | No |
| `snapshots/` | JSON snapshot storage and comparison | No |
| `report/` | Event model, normalized report, four renderers, coverage parsing | No |
| `gen/generators/` | Adversarial value generators — **present but not wired into the public DSL** | No |
| `tools/perfcheck/` | Benchmark regression CLI — **exists as tooling, not enforced in CI** | No |
| `benchmarks/` | Comparative benchmarks against Testify and Gomega | Yes |
| `examples/` | Executable documentation | Yes |

The dependency arrows only ever point **into** `specs`, never out of it into a sibling surface, and
never between siblings. [01 · Architecture](01-ARCHITECTURE.md) states the rule and its enforcement.

## 5. What it is not

- **Not a `go test` replacement.** There is no `go-specs` binary. `.goreleaser.yaml` ships a library
  and nothing else ([ADR-0015](adr/0015-library-only-release-artifact.md)).
- **Not a mocking framework with code generation.** `mock/` is a spy recorder, no generator, no
  interface synthesis.
- **Not a module-wide reporter yet.** `report/` produces a complete report for one package binary.
  Coordinating `go test ./...` across independently-executing package binaries is specified in the
  [coordination contract](144-report-coordination-contract.md) and is **not implemented**
  ([ADR-0009](adr/0009-contract-first-report-coordination.md)).
- **Not stable.** Pre-1.0. The DSL forms in [ADR-0011](adr/0011-public-dsl-stability-contract.md) are
  the part that is held stable; everything else may move.

## 6. Honest gaps

Documented here rather than buried, because a reader deciding whether to adopt deserves them up front:

- `gen/generators` is unreferenced by any other package in the module. It is staged infrastructure,
  not a feature.
- `tools/perfcheck` implements a ns/op regression gate but no workflow invokes it. Performance
  regressions are visible in the benchmark charts, not blocked at the gate.
- CI generates `coverage.out` and prints the table. There is **no** enforced numeric coverage
  threshold.
- `report.ParseCoverageProfile` does not deduplicate overlapping `-coverpkg` blocks, so a dependency
  instrumented by two test binaries is double-counted. This is finding F7 of the
  [coordination contract](144-report-coordination-contract.md) and is open.
- The Shipwright CI job (lint + `govulncheck` via Dagger) is disabled pending stability
  ([ADR-0016](adr/0016-native-ci-single-toolchain.md)). `make lint` still runs locally.

## 7. Where to go next

- Writing tests → [02 · The DSL](02-DSL.md)
- Understanding how a suite actually runs → [03 · Execution model](03-EXECUTION-MODEL.md)
- Wiring reports into CI → [07 · Reporting](07-REPORTING.md)
- Contributing → [`CONTRIBUTING.md`](../CONTRIBUTING.md) and [10 · Workflow](10-WORKFLOW.md)
- Why something is the way it is → [adr/](adr/README.md)
