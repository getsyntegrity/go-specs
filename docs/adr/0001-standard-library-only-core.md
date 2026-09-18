# ADR-0001: Standard-Library-Only Core, Third-Party Confined to Benchmarks

## Status

Accepted.

## Date

2026-09-18 (recorded). The boundary predates this record; it was hardened with supply-chain tooling
on 2026-09-09 and is enforced continuously.

## Deciders

go-specs maintainers, via #89 (supply-chain security tooling).

## Context

go-specs is a testing library. Every dependency it declares is a dependency every downstream
consumer inherits in their module graph — not only at test time, because `go.mod` requirements are
transitive regardless of which files import them.

A testing framework is also, uncomfortably, a good supply-chain target: it is present in almost every
repository that uses it, it runs on developer machines and in CI, and nobody reads its transitive
dependency tree.

The repository nonetheless benchmarks itself against Testify and Gomega, and those comparisons are
load-bearing — they are the evidence behind every performance claim in
[08 · Performance](../08-PERFORMANCE.md). Removing them would remove the evidence.

## Decision

The execution core and every surface package import **nothing outside the Go standard library**:
`specs`, `assert`, `mock`, `snapshots`, `report`, `gen/generators`.

`github.com/onsi/gomega` and `github.com/stretchr/testify` appear in `go.mod` because `benchmarks/`
is part of the same module and benchmarks go-specs *against* them. Their reachable set is exactly
`benchmarks/`: `helpers.go`, `assertion_bench_test.go`, `hooks_bench_test.go`,
`matcher_bench_test.go`.

A consumer who imports `github.com/getsyntegrity/go-specs/specs` links no third-party code.

## Decision classification

### Accepted now

- No third-party import in `specs`, `assert`, `mock`, `snapshots`, `report` or `gen/generators`, in
  production or test files.
- Third-party imports permitted in `benchmarks/` only, and only to construct the comparison harness.
- `govulncheck` and Dependabot cover the small surface that does exist.

### Deferred

- Whether to move `benchmarks/` into its own module to remove the two requirements from consumers'
  module graphs entirely. It would work, and it would also mean a second `go.mod` to keep in step and
  a benchmark suite that can silently stop compiling against the core.

### Open hypotheses

- That the standard library remains sufficient. The place this would break first is reporting:
  richer coverage handling would be easier with `golang.org/x/tools/cover`, and the known
  `-coverpkg` deduplication gap ([ADR-0008](0008-coverage-aggregates-statements.md)) is exactly the
  shape of problem that library solves.

## Consequences

**Benefits:** a consumer's module graph does not grow; the vulnerability surface is the standard
library plus two benchmark-only packages; no dependency upgrade can change test-execution semantics;
the framework cannot acquire a transitive dependency by accident.

**Costs:** functionality that a library would provide has to be written and maintained here — the
coverage profile parser is the clearest case, and it is *already* known to be less correct than
`x/tools/cover` on overlapping `-coverpkg` blocks. The rule buys independence and pays in scope.

## Rejected / deferred alternatives

- **Allow a small, well-known dependency in the core** (`go-cmp` for diffs, `x/tools/cover` for
  coverage) — rejected. There is no principled place to stop after the first one, and a diff library
  in the assertion path would sit on the hot path this framework exists to keep clear.
- **Drop the comparison benchmarks to reach a zero-requirement `go.mod`** — rejected. The
  performance claims are the reason to choose this framework; deleting the evidence to tidy a file
  is the wrong trade.
- **Vendor Testify and Gomega** — rejected. Vendoring a comparison target means maintaining it, and
  a stale comparison target is a dishonest benchmark.

## Related work

- #89 — supply-chain security tooling (`govulncheck`, Dependabot, `SECURITY.md`).
- [ADR-0008](0008-coverage-aggregates-statements.md) — the coverage gap this rule contributes to.
- [01 · Architecture](../01-ARCHITECTURE.md) — the package boundaries this rule sits inside.
