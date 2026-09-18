# ADR-0009: Module-Wide Report Coordination Is Specified Before It Is Built

## Status

Accepted as a design gate. The contract is at v1.2.6; the implementation is **not started**.

## Date

2026-09-16, amended 2026-09-18.

## Deciders

go-specs maintainers, via #144, #147, #148.

## Context

`report` produces a complete report for **one** package binary
([ADR-0007](0007-observational-constructor-injected-reporting.md)). `go test ./...` compiles and runs
one binary per package, and no package binary can observe its siblings — only the invoking `go test`
process sees the whole run.

So "one report for the whole module" is not a feature that can be switched on. It is a distributed
problem with a hostile environment, and the obvious implementations are all wrong in ways that are
expensive to discover after they ship.

Eight findings were established experimentally before any design was proposed:

| | Finding |
| --- | --- |
| F1 | A custom test flag breaks any package binary that did not register it — so the activation signal cannot be a forwarded flag. |
| F2 | Environment variables cost non-participating packages nothing — the only zero-cost cross-process signal available. |
| F3 | No package binary can observe its siblings; only the invoking `go test` or CI step sees the whole run. |
| F4 | A cached test result skips `TestMain` entirely — reporting is incompatible with caching unless caching is forced off. |
| F5 | Abrupt termination (timeout, `SIGKILL`) bypasses anything after `m.Run()` — publication must be crash-safe. |
| F6 | The combined coverage profile is written once, by the `go` command itself, never per package. |
| F7 | `-coverpkg` overlap produces duplicate blocks that the current parser mishandles ([ADR-0008](0008-coverage-aggregates-statements.md)). |
| F8 | There is no free shared per-invocation identifier. |

## Decision

**No implementation lands until the contract specifies it.** The contract is
[`docs/144-report-coordination-contract.md`](../144-report-coordination-contract.md), a normative,
versioned document that gates issues #145 (shard emission) and #146 (merge and render).

The specified lifecycle:

1. **Preflight**, external to `go test`: generate a run id and a random token, **exclusively create**
   a `run.json` ownership marker, export the activation environment variables.
2. **Each participating package's `TestMain`**: verify token ownership, then atomically publish one
   shard file after its own `m.Run()` — create-no-replace, never a replacing rename (F5), with a
   SHA-256-derived collision-safe filename (F8).
3. **Finalize**, a separate explicit step that runs **even when `go test` went red**: verify
   ownership, merge the shards, deduplicate coverage blocks (F7), render the same four formats
   module-wide.

Constraints the contract binds itself to:

- The activation signal is an **environment variable**, never a forwarded flag (F1, F2).
- Merging is an **external step**, not something a package binary attempts (F3).
- Publication is **crash-safe and non-replacing** (F5).
- `NormalizedReport` and the four renderers are **unchanged** — this adds a coordination layer, it
  does not reshape the model.

## Decision classification

### Accepted now

- The contract as the design of record, and as a hard precondition for #145 and #146.
- The eight findings as established facts, each with the experiment that produced it recorded in the
  contract.
- Run ownership via token plus exclusively-created marker, and collision-safe shard filenames.

### Deferred

- Shard emission (#145) and merge/render (#146) — the implementation itself.
- Whether go-specs should ship the preflight and finalize steps as a binary. Shipping one conflicts
  with [ADR-0015](0015-library-only-release-artifact.md) and is a decision that record would have to
  be revisited to make.
- Behaviour under forced-off caching. F4 says reporting and caching are incompatible; making that a
  documented requirement rather than a surprise is part of the implementation.

### Open hypotheses

- That an environment variable remains viable across the CI runners people use. A runner that strips
  or rewrites the environment between the preflight step and `go test` would break the handshake at
  the first step.
- That `TestMain` participation is acceptable. It requires every participating package to have one,
  which is a real intrusion into user code that no alternative avoids.

## Consequences

**Benefits:** the eight findings are written down with their experiments, so the next person does not
rediscover them; the design cannot be cargo-culted from a framework in another ecosystem where
`go test`'s process model does not apply; the contract is versioned, so an amendment is visible as an
amendment. Writing it first has already produced value — F7 surfaced a real, open bug in shipped
code.

**Costs:** module-wide reporting does not exist today, and a user who needs it must aggregate the
per-package outputs themselves. A 1,400-line contract is also a document that can rot if the
implementation diverges from it, and it represents effort spent on specification rather than on
working code.

## Rejected / deferred alternatives

- **A custom test flag as the activation signal** — rejected by F1. It breaks every package binary
  that did not register the flag, including third-party packages in the same `go test ./...`.
- **Each package writing directly into one shared report file** — rejected by F3 and F5. Concurrent
  writers with no coordinator, and a crash mid-write destroys the artifact.
- **Implement first, document after** — rejected. The findings that constrain the design are
  non-obvious and experimentally established; a design that did not know them would have to be
  rebuilt, and its users migrated.
- **Wrap `go test` in a go-specs binary** — deferred, and in tension with
  [ADR-0015](0015-library-only-release-artifact.md).

## Related work

- #144 — the contract. #147, #148 — its authoring and amendment to v1.2.5/v1.2.6.
- #145 — shard emission. #146 — merge and render.
- [ADR-0007](0007-observational-constructor-injected-reporting.md),
  [ADR-0008](0008-coverage-aggregates-statements.md).
- [07 · Reporting](../07-REPORTING.md#6-the-limit-one-package-binary).
