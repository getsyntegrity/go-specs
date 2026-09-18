# ADR-0012: Two-File Go Toolchain Pin, Gated Against Drift

## Status

Accepted.

## Date

2026-09-18

## Deciders

go-specs maintainers, via #165.

## Context

Two different questions get confused into one "Go version":

1. **What is the oldest Go a consumer may use?** This is `go.mod`'s `go` directive, and every
   downstream consumer inherits it. Raising it can lock a consumer out of an upgrade.
2. **Which exact toolchain do CI and contributors build and test with?** This should track a current
   patch release so security and toolchain fixes land promptly.

Collapsing them forces a bad trade: either the consumer floor rises every time a patch release is
adopted, or the project builds with an old toolchain to keep the floor low.

Version managers — `asdf`, `mise`, `goenv`, `gvm` — already read a `.go-version` file automatically,
so the second question has a conventional home. What was missing was a guarantee that the two files
stay related, since nothing stops them silently diverging into a different major or minor version.

## Decision

Two files, two jobs, one check.

| File | Meaning |
| --- | --- |
| `go.mod`'s `go` directive | the **minimum** language version. The floor every consumer inherits. Stays conservative. |
| `.go-version` | the **exact** toolchain CI installs and contributors run. Tracks a current patch release. |

Rules:

- They **may** differ on the patch component, and normally do — currently `go 1.25.0` and `1.25.14`.
- They **must** agree on `<major>.<minor>`.
- A minor bump means bumping both.
- Drift fails the build. `.github/scripts/check-go-version.sh` runs in the `test` and `release` jobs;
  `make check-go-version` runs the identical script locally.
- CI sets `GOTOOLCHAIN: local`, so the pinned toolchain is the one actually used rather than a
  starting point Go may silently upgrade from.

Patch drift is allowed deliberately: it is the whole point. Minor drift is not, because a minor
difference means CI is validating a different language version than the one the floor promises.

## Decision classification

### Accepted now

- The two-file split, the patch-allowed / minor-must-match rule, and the CI gate in `test` and
  `release`.
- `GOTOOLCHAIN: local` in CI.

### Deferred

- A policy for when to raise the floor. Today it is a judgement call made per bump. A stated policy —
  "N-1 minor versions", say — would make it mechanical, and would also commit the project to
  something it has not needed yet.

### Open hypotheses

- That patch-level drift never changes behaviour that tests depend on. Go's compatibility promise
  makes this safe in principle; a patch release that changes runtime scheduling or `testing`
  internals would be where it stops being safe in practice.

## Consequences

**Benefits:** consumers keep a conservative floor while the project builds and tests on a current
toolchain; security fixes in the toolchain land without a consumer-visible change; the release
artifact is built with a known toolchain rather than whatever the runner defaulted to; drift fails
loudly at PR time instead of silently at release time.

**Costs:** two files to update on a minor bump, and one more thing to forget — mitigated by the gate,
which is the reason the gate exists. `GOTOOLCHAIN: local` also means a workflow cannot opportunistically
pick up a newer toolchain; that is the intent, but it does mean the pin must be maintained.

## Rejected / deferred alternatives

- **One file — pin only `go.mod`** — rejected. It couples the consumer floor to the CI toolchain, so
  adopting a patch release raises the floor for everyone.
- **One file — pin only `.go-version`** — rejected. `go.mod` needs a `go` directive regardless, and
  leaving it stale means it says something untrue.
- **Let the toolchain float in CI** — rejected. A release built with an unknown toolchain is a
  release you cannot reproduce, and a test failure that appears only after a runner image update is
  expensive to diagnose.
- **Allow minor drift with a warning** — rejected. A warning in CI output is noise that a green build
  overrides, which is the same reasoning as in
  [ADR-0005](0005-fail-closed-dsl-registration.md).

## Related work

- #165 — the implementing change.
- [`CONTRIBUTING.md`](../../CONTRIBUTING.md) — the contributor-facing statement of the same contract.
- [10 · Workflow](../10-WORKFLOW.md#1-toolchain).
