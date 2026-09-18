# ADR-0016: Native CI on a Single Toolchain; Shipwright Disabled Pending Stability

## Status

Accepted. The Shipwright part is explicitly provisional.

## Date

2026-09-10 (native split, #98 / #99) and 2026-09-18 (Shipwright disabled, #162).

## Deciders

go-specs maintainers, via #98, #99 and #162.

## Context

### The matrix

CI ran a 2×2 matrix over Go version and operating system. Nothing in the codebase is OS-specific: the
core is standard-library Go with no syscalls, no filesystem-layout assumptions beyond `path/filepath`,
and no build tags selecting per-platform code. The matrix multiplied CI time by four and had never
caught a platform-specific failure, because there was no platform-specific behaviour to catch.

### Shipwright

Lint and `govulncheck` were delegated to Shipwright, a Dagger-backed workflow runner. `go build` and
`go vet` could not move there — no provider exists for them — and Dagger is Linux-only, so the OS
axis could not move either. The delegation was therefore partial by construction.

Then the Dagger daemon began failing to connect intermittently. A red build caused by infrastructure
is worse than no build: it teaches everyone to re-run rather than to read. Merges were gated on luck
rather than on the code.

## Decision

### One toolchain, one OS

A single `test` job on the pinned toolchain ([ADR-0012](0012-two-file-go-toolchain-pin.md)) and
`ubuntu-latest`, running `go build ./...`, `go vet ./...`, `go test ./...` and `go test -race ./...`,
plus the toolchain-drift check and the module-path assertion.

### Build and vet stay native

They cannot be delegated — there is no provider — and inventing one to move two commands into another
runner is not a trade worth making.

### Shipwright is disabled

The job is `if: false`. It is retained, not deleted, so re-enabling is a one-line change once the
daemon is stable.

**The consequence is stated plainly: lint and `govulncheck` do not run in CI today.** `make lint`
runs `golangci-lint` locally, falling back to `go vet` when it is not installed. Dependabot still
runs, so dependency updates are still proposed — but nothing blocks a PR on a lint finding or a known
vulnerability.

### Jobs stay independent

`test` and `goreleaser` are separate jobs precisely so a failing or flapping `shipwright` cannot
block them. That independence is what made disabling it a one-line change rather than a rework.

## Decision classification

### Accepted now

- Single toolchain, single OS, native `build`/`vet`/`test`/`race`.
- Shipwright disabled, with its consequence documented rather than glossed.
- Independent jobs.

### Deferred

- **Re-enabling Shipwright** once the Dagger daemon is stable. This is the provisional part of this
  record.
- **Running lint and `govulncheck` natively in the meantime.** Both are ordinary GitHub Action steps
  and would restore the missing gates without waiting on Dagger. Not done yet; it is the obvious next
  move and is called out here so it is not forgotten.
- **Reintroducing an OS axis** if platform-specific code ever lands — snapshot file handling and the
  atomic-rename path in [ADR-0010](0010-snapshot-durability-and-semantics.md) are where Windows
  semantics would differ first.

### Open hypotheses

- That no OS-specific bug is being missed today. The atomic-rename write path is the most exposed:
  `rename`-over-existing behaves differently on Windows, and nothing currently tests it there.

## Consequences

**Benefits:** CI is fast and its failures mean something about the code; a flaky infrastructure
dependency cannot gate a merge; the toolchain under test is the pinned one, every time.

**Costs:** lint and vulnerability scanning are not enforced — a real regression in the gate set, and
the reason `allow_auto_merge` is off in
[ADR-0014](0014-repository-configuration-as-code.md). No Windows or macOS coverage, which is fine
until it is not, and the atomic-rename path is where it would stop being fine. A disabled job in the
workflow file is also a small trap: it appears in the workflow list and does nothing, and someone
will eventually wonder why.

## Rejected / deferred alternatives

- **Keep the 2×2 matrix** — rejected by #99. Four times the CI cost for a class of failure this
  codebase cannot currently produce.
- **Move `build` and `vet` into Shipwright** — rejected by #98. No provider exists, and Dagger is
  Linux-only, so the delegation could never be complete.
- **Delete the Shipwright job** — rejected. Retaining it makes re-enabling a one-line change and
  keeps the intent visible; deleting it would quietly drop the goal along with the job.
- **Let Shipwright keep failing and instruct people to re-run** — rejected. It trains everyone to
  ignore red, which costs far more than the checks are worth.

## Related work

- #98 — why `build`/`vet`/matrix stay native.
- #99 — dropping the Go-version and OS matrix.
- #162 — disabling Shipwright.
- [ADR-0012](0012-two-file-go-toolchain-pin.md) — the pinned toolchain CI uses.
- [ADR-0014](0014-repository-configuration-as-code.md) — `allow_auto_merge: false`, because of this.
- [10 · Workflow](../10-WORKFLOW.md#5-gates-that-exist-and-gates-that-do-not).
