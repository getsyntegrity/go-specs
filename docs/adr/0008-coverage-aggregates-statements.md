# ADR-0008: Coverage Aggregates Statement Counts, Never Averages Percentages

## Status

Accepted. One known gap, stated below.

## Date

2026-09-16

## Deciders

go-specs maintainers, via #141 / #142.

## Context

Given per-package coverage, there are two ways to produce a module-wide number, and they are not the
same:

1. **Average the package percentages** — `mean(p₁, p₂, … pₙ)`.
2. **Sum the raw statement counts** — `Σ covered / Σ total`.

They coincide only when every package has the same number of statements, which no real module
satisfies. The averaging form lets a 3-statement package at 100% offset a 3,000-statement package at
40%, producing a headline number that describes nothing anyone cares about.

There is also an edge case: a package with zero executable statements. Its percentage is either 0%
or undefined depending on how you divide, and both answers are wrong because the question does not
apply.

## Decision

**Module-wide coverage is `Σ covered / Σ total` across packages.** Percentages are never averaged.

A package with zero executable statements **never appears** in the output. It contributes nothing to
either sum, and listing it would show a number that means nothing.

Percentages are computed from the counts at render time and rounded to two decimal places in the
JSON output. The counts, not the percentage, are the stored value — so a consumer can re-aggregate.

This is asserted by tests in `report/coverage_test.go` and `report/render_json_test.go`.

## Decision classification

### Accepted now

- Statement-count aggregation for the module total, and for any subset a consumer computes.
- Exclusion of statement-free packages.
- Counts as the stored representation; percentages as a rendered view.

### Deferred

- Per-package coverage thresholds or a failing gate. No numeric coverage gate is enforced anywhere
  today — `make coverage` and CI produce and print `coverage.out` and nothing blocks on it. Adding a
  gate is a separate decision with its own cost.

### Open hypotheses

- That statement coverage remains the metric worth aggregating. Branch coverage would be more
  informative and is not something `go test -coverprofile` provides.

## Consequences

**Benefits:** the module number answers the question it appears to answer — "what fraction of this
module's statements were executed". It cannot be gamed by adding tiny fully-covered packages, and it
is stable under package splits and merges, since neither changes the statement totals.

**Costs:** a large well-tested package dominates the headline, which can mask a small badly-tested
one. That is a real loss of signal, and the mitigation is per-package numbers — which the report
carries — rather than a different aggregation.

## The known gap

`ParseCoverageProfile` does **not** deduplicate overlapping `-coverpkg` blocks the way
`golang.org/x/tools/cover` does.

When two test binaries are both instrumented over a shared dependency, that dependency's blocks
appear in the combined profile more than once. Both sums then include it repeatedly, and the total is
wrong — inflated or deflated depending on whether the duplicate blocks agree on coverage.

This is finding **F7** of the [coordination contract](../144-report-coordination-contract.md) and is
open. It is a direct cost of [ADR-0001](0001-standard-library-only-core.md): the library that solves
it correctly is exactly the third-party dependency the core does not take. Deduplication is specified
as part of the contract's finalize step; implementing it here is tracked with that work.

Until then: a report generated with overlapping `-coverpkg` sets carries a coverage total that should
not be quoted.

## Rejected / deferred alternatives

- **Average package percentages** — rejected. It answers a question nobody asked, and it is the form
  that flatters a module with many small packages.
- **Weight packages by some notion of importance** — rejected. Any weighting is a judgement call
  embedded in a number that will be read as a measurement.
- **Emit 0% for statement-free packages** — rejected. It is a false negative that drags the visible
  per-package list down without changing the total.
- **Depend on `golang.org/x/tools/cover`** — deferred, and constrained by
  [ADR-0001](0001-standard-library-only-core.md). It would fix F7 directly; it would also be the
  first third-party import in the core.

## Related work

- #141, #142 — the reporting feature this is part of.
- [ADR-0001](0001-standard-library-only-core.md) — why the correct library is not simply imported.
- [ADR-0009](0009-contract-first-report-coordination.md) — where deduplication is specified to live.
- [07 · Reporting](../07-REPORTING.md#5-coverage).
