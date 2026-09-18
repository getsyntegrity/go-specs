# ADR-0007: Reporting Is Observational and Constructor-Injected

## Status

Accepted.

## Date

2026-09-16

## Deciders

go-specs maintainers, via #141 / #142.

## Context

Structured test reports — JUnit XML for a CI UI, JSON for tooling, HTML for a build artifact — are
table stakes for a framework used in CI. The question is how the reporter reaches the runner, and
what it is allowed to do once it gets there.

Two designs are common and both are traps:

- **A global registry.** `report.Register(myReporter)` in an `init` or a `TestMain`. It makes
  "which reporter is attached" depend on import order, and it makes two concurrently-building suites
  observable to each other — which conflicts directly with the per-goroutine build isolation the
  framework already maintains.
- **A reporter that can influence execution.** Once a reporter can skip, retry or reorder, a report
  stops being evidence about the unreported run. The thing you measured is not the thing that ships.

## Decision

### Four events, one interface

```go
type EventReporter interface {
    SuiteStarted(SuiteStartEvent)
    SpecStarted(SpecStartEvent)
    SpecFinished(SpecResultEvent)
    SuiteFinished(SuiteEndEvent)
}
```

### Constructor injection only

No global registry, no `init`-time registration, no environment-variable discovery. The reporter is
passed in:

`DescribeWithReporter`, `NewRunnerWithReporter`, `RunShardWithReporter`.

### Observation never changes execution

No control-flow branch in the runner reads the reporter. Failure state is reset per spec whether or
not an observer is attached. A run with a reporter and the same run without one execute identically.

### One normalized model, one classifier, four renderers

`NormalizedReport` is the single internal model. Status classification happens in exactly one place,
so no renderer recomputes it and the four outputs cannot disagree. Each renderer has its own DTO,
so a wire-format change never forces an internal-model change and vice versa.

`SchemaVersion` is emitted in the JSON output so a consumer can detect a change rather than guess.

### The model is copied out

`Collector.Report()` deep-copies every slice: a caller cannot mutate collector state through the
returned report.

### Explicitly not concurrent

`Collector` is not safe for concurrent use and is not suite-interleaving aware. The runner serializes
calls on its behalf, and this is documented rather than papered over with a mutex that would suggest
guarantees the type does not make.

## Decision classification

### Accepted now

- All of the above, for sequential, parallel and sharded execution alike.
- Five statuses — `passed`, `failed`, `error`, `skipped`, `filtered` — with `Total` always equal to
  their sum.

### Deferred

- A streaming reporter that writes incrementally rather than on `Flush`. Useful for very long runs;
  it needs a story for partial output when the process is killed, which is the same problem the
  [coordination contract](../144-report-coordination-contract.md) works through.
- Making `Collector` concurrency-safe. Only worth doing if a caller genuinely needs to drive it from
  several goroutines; today the runner's serialization is sufficient and cheaper.

### Open hypotheses

- That four formats are the right set. TAP has been asked about and is not implemented; nobody has
  needed it enough to build it.

## Consequences

**Benefits:** a report is trustworthy evidence about an unreported run; two suites building
concurrently cannot observe each other; the attached reporter is visible at the call site rather than
hidden in an `init`; adding a format means adding a renderer, not touching the model.

**Costs:** injection is more typing than a global — a user wanting reports must thread the reporter
through every entry point they use. The deep copy in `Report()` is real allocation, paid once per
report rather than per event, which is the right place but is not free. And `Collector`'s
documented non-concurrency is a sharp edge for anyone writing a custom reporter fed from multiple
goroutines.

## Rejected / deferred alternatives

- **Global reporter registry** — rejected. Import-order dependence and cross-suite observability, for
  a saving of one parameter.
- **Environment-variable-activated reporting inside the framework** — rejected *here*, and correctly
  so: it is the right mechanism for **cross-process** coordination and is specified as such in
  [ADR-0009](0009-contract-first-report-coordination.md). Within one process, the caller already has
  a place to put the reporter.
- **Let reporters filter or retry** — rejected. It would make the report a description of a different
  run than the one that shipped.
- **One renderer switching on a format enum** — rejected. The four formats have genuinely different
  shapes, and a single renderer with four branches is where subtle disagreements between formats are
  born.

## Related work

- #141, #142 — multi-format test and coverage reporting.
- [ADR-0008](0008-coverage-aggregates-statements.md) — how coverage is aggregated into this model.
- [ADR-0009](0009-contract-first-report-coordination.md) — the cross-process problem this does not
  solve.
- [07 · Reporting](../07-REPORTING.md).
