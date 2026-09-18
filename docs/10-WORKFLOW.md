# 10 · Engineering workflow

> **Audience:** contributors · **Reading time:** ~10 minutes

The binding contribution contract lives in [`CONTRIBUTING.md`](../CONTRIBUTING.md) at the repository
root, where GitHub and contributors expect it. This document is the operational companion: what the
tooling does, what CI actually enforces, and what it does not.

---

## 1. Toolchain

Two files, two different jobs — do not collapse them
([ADR-0012](adr/0012-two-file-go-toolchain-pin.md)):

| File | Meaning |
| --- | --- |
| `go.mod`'s `go` directive | the **minimum** language version, and the floor every downstream consumer inherits. Stays conservative. |
| `.go-version` | the **exact** toolchain CI installs and contributors run. Tracks a current patch release so security fixes land without moving the consumer floor. |

They may differ on patch and normally do. They **must** agree on `<major>.<minor>`.

```bash
make check-go-version
```

CI runs the same script (`.github/scripts/check-go-version.sh`) in the `test` and `release` jobs, so
drift fails the build rather than silently changing the toolchain a release is built with.

Version managers — `asdf`, `mise`, `goenv`, `gvm` — read `.go-version` automatically.

## 2. Make targets

| Target | What it does |
| --- | --- |
| `help` | lists every target (the default) |
| `check-go-version` | fails on `go.mod` ↔ `.go-version` minor drift |
| `test` | `go test ./...` |
| `test-race` | `go test -race ./...` |
| `coverage` | writes `coverage.out` and prints the function table |
| `lint` | `golangci-lint run` over the package set, falling back to `go vet` if it is not installed |
| `build` | `go build ./...` |
| `tidy` | `go mod tidy` |
| `bench` | quick benchmark pass to the terminal |
| `bench-report` | `-count=10`, written to `benchmarks/results/current.txt` |
| `bench-compare` | `benchstat` over previous vs current |
| `clean` | removes coverage output and benchmark artifacts |

> `make lint` covers `assert`, `specs`, `report`, `mock`, `gen`, `snapshots`, `benchmarks` and
> `examples`. **`tools/` is not in that list** — `tools/perfcheck` is currently unlinted.

## 3. Minimum validation before a pull request

```bash
make test
make test-race
```

If you touched performance-sensitive code, add a benchmark comparison
([08 · Performance](08-PERFORMANCE.md#7-working-on-a-hot-path)) and put the numbers in the PR.

## 4. What CI actually runs

### `ci.yml` — push and PR to `develop` and `main`

| Job | Steps | Status |
| --- | --- | --- |
| `test` | pin toolchain from `.go-version` with `GOTOOLCHAIN: local`, run the version-drift check, assert `go list -m` matches the repository, then `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...` | active |
| `goreleaser` | `goreleaser check` plus a `--snapshot` dry-run release | active |
| `shipwright` | lint and `govulncheck` through Dagger | **disabled** (`if: false`) |

The module-path assertion exists because a wrong module path once shipped (#139). It is cheap and it
catches a class of bug that is expensive after release.

The GoReleaser dry run exists because a release config once pointed at a build entrypoint that never
existed on this branch (#130, #131). It turns a release-day failure into a PR-time one.

Shipwright is off after intermittent Dagger daemon failures made merges depend on luck rather than on
the code ([ADR-0016](adr/0016-native-ci-single-toolchain.md)). `test` and `goreleaser` are
independent jobs precisely so they are not blocked by it.

**There is no Go-version or OS matrix.** One toolchain, one OS. No OS-specific code path has ever
justified the multiplier, and `go build`/`go vet` cannot move into Shipwright because no provider for
them exists and Dagger is Linux-only.

### `benchmarks.yml` — push to `main`, or manual

Runs the benchmark suite, regenerates the charts and commits them back with `[skip ci]`. **It has no
failure condition.** It visualizes performance; it does not gate on it.

### `release.yml` — `workflow_dispatch` only

Never on push or merge ([ADR-0013](adr/0013-two-branch-model-manual-releases.md)):

```bash
gh workflow run release.yml --ref main
```

Checks the version pin, skips if `HEAD` is already tagged, auto-bumps the patch version or accepts an
explicit strict-SemVer `vX.Y.Z`, pushes the tag, runs GoReleaser, and then performs a black-box
`go get <module>@<tag>` from a scratch module to prove the released version is actually installable
by a stranger.

## 5. Gates that exist, and gates that do not

| Gate | Status |
| --- | --- |
| build, vet, test, race | **enforced** in `ci.yml` |
| toolchain drift | **enforced** in `test` and `release` |
| module path correctness | **enforced** in `test` |
| release config validity | **enforced** via GoReleaser dry run |
| post-release installability | **enforced** in `release.yml` |
| lint, `govulncheck` | **not enforced** — Shipwright is disabled; `make lint` is local only |
| coverage threshold | **not enforced** — `coverage.out` is produced and printed, never thresholded |
| performance regression | **not enforced** — `tools/perfcheck` exists but no workflow calls it |

Nothing in this table is aspirational in either direction. If a row says "not enforced", assume it is
not enforced.

## 6. Branching

Two long-lived branches, one rule each
([ADR-0013](adr/0013-two-branch-model-manual-releases.md)):

| Branch | What lands there |
| --- | --- |
| `develop` (default) | every feature, fix, refactor and doc change |
| `main` | releases and hotfixes only |

Never open a feature PR against `main`. When syncing `develop` into `main` for a release,
**merge — do not squash**: a squash creates a commit on `main` that does not exist on `develop`, and
the branches diverge again the moment the release lands.

## 7. Repository configuration

Repository settings are declared in `.github/settings.yml` and applied by the Probot Settings app
where it is installed ([ADR-0014](adr/0014-repository-configuration-as-code.md)). Branch-protection
rules are **deliberately excluded** so that installing the app cannot create protection as a side
effect of a tooling change rather than as a reviewed decision.

`.github/dependabot.yml` updates the `gomod` and `github-actions` ecosystems weekly.

## 8. Documentation is part of the change

A behavioural change lands with its documentation in the same work unit. Concretely:

- A new or changed public API updates [02 · The DSL](02-DSL.md).
- A change to how specs run updates [03 · Execution model](03-EXECUTION-MODEL.md).
- A decision that constrains future work gets an [ADR](adr/README.md). A decision that merely
  implements an existing ADR does not.
- A performance-relevant change updates the numbers in [08 · Performance](08-PERFORMANCE.md) from a
  real `make bench-report`, not from memory.

If a document and the code disagree, the code wins and the document is a bug — report it as one.
