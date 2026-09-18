# ADR-0015: go-specs Ships a Library, Not a Binary

## Status

Accepted.

## Date

2026-09-15

## Deciders

go-specs maintainers, via #130 and #131.

## Context

The GoReleaser configuration built and archived a CLI at `./tools/specs-cli` — **a path that has
never existed on this branch**. Every release attempt failed on an unresolvable build entrypoint, and
the failure only surfaced at release time, when it is most expensive and least convenient.

The phantom CLI had also propagated into documentation: `ARCHITECTURE.md` listed `tools/specs-cli/`
in the repository layout and gave `go build -o specs-cli ./tools/specs-cli` as a build command, and
the `Makefile` echoed messages about building and cleaning it. A reader following those instructions
got "no such file or directory" from a document that appeared authoritative.

The underlying question is whether go-specs should ship a binary at all. It is a testing library;
`go test` is the entrypoint, and there is no root `main` package.

## Decision

**go-specs is released as a Go module and nothing else.**

`.goreleaser.yaml` sets `builds: [skip: true]`. No binary is built, archived or published.

To catch this class of regression before a release attempt, `ci.yml` runs on every push and PR to
`develop` and `main`:

- `goreleaser check` — validates the configuration,
- `goreleaser release --snapshot --clean` — a full dry-run release.

A release configuration that cannot resolve its inputs now fails at PR time, not on release day.

The documentation that described the phantom CLI is corrected as part of this documentation
regeneration: [01 · Architecture](../01-ARCHITECTURE.md) states that there is no `internal/`
directory and no CLI.

> **Residue, stated rather than hidden:** the `Makefile` still echoes messages referring to
> `specs-cli` in its `build` and `clean` targets. They are harmless — `rm -f specs-cli` is a no-op —
> but they are wrong, and they are a leftover of the same phantom. Cleaning them is a small, separate
> change.

## Decision classification

### Accepted now

- Library-only release artifact.
- `goreleaser check` plus a snapshot dry-run on every push and PR.
- Documentation corrected to match.

### Deferred

- Whether a CLI should exist. The clearest candidate is the preflight/finalize tooling the
  [report coordination contract](../144-report-coordination-contract.md) specifies
  ([ADR-0009](0009-contract-first-report-coordination.md)) — an external step is exactly what a
  binary is for. Building one would revisit this record, which is the correct way to make that
  change.
- Cleaning the `Makefile`'s `specs-cli` echoes.

### Open hypotheses

- That `perfcheck` staying a `go run` target rather than a released binary is sufficient. It is run
  by contributors from the repository, so it has never needed distribution.

## Consequences

**Benefits:** releases succeed; the release configuration is validated on every PR rather than
discovered broken on release day; the repository layout in the documentation matches the repository;
consumers are not offered a binary that does nothing.

**Costs:** users wanting a CLI-driven workflow have none, and the report-coordination steps that
genuinely want to be a binary do not have one — which is a real constraint on
[ADR-0009](0009-contract-first-report-coordination.md)'s implementation. The snapshot dry-run also
adds time to every CI run, for a check that only matters at release time; it is worth it because the
failure it prevents is discovered at the worst possible moment.

## Rejected / deferred alternatives

- **Create `tools/specs-cli` to match the configuration** — rejected. It builds a binary because a
  config file mentioned one, which is writing code to satisfy a typo.
- **Fix the config and skip the CI validation** — rejected. The config was wrong for a long time and
  nothing noticed until a release ran. Validation is the part that makes the fix stay fixed.
- **Validate the release config only on the release workflow** — rejected. That is where it already
  failed; moving the check earlier is the entire improvement.

## Related work

- #130 — library-only GoReleaser configuration.
- #131 — the CI safeguard for unresolvable build entrypoints.
- #139 — the module-path bug; the same "validate the published artifact" instinct.
- [ADR-0009](0009-contract-first-report-coordination.md) — the deferred case for a binary.
- [ADR-0013](0013-two-branch-model-manual-releases.md) — how releases are triggered.
