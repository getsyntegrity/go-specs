# CONTRIBUTING.md

Thanks for contributing to go-specs.

---

# Development Setup

Requirements:

* Go — install the exact version recorded in `.go-version`; that file is the
  source of truth for the toolchain CI builds and tests with, and version
  managers (`asdf`, `mise`, `goenv`, `gvm`) read it automatically.
* make (optional)

## Go version pinning

Two files, two different jobs — do not collapse them:

| File | Meaning |
| --- | --- |
| `go.mod`'s `go` directive | **Minimum** language version the code needs. This is the floor every downstream consumer of go-specs inherits, so it stays conservative. |
| `.go-version` | **Exact** toolchain CI installs and contributors run. Tracks a current patch release so security fixes land without moving the consumer floor. |

They are allowed to differ on the *patch* component, and normally do. They
must agree on `<major>.<minor>`.

Bumping either one means bumping both (minor bumps) and running:

```
make check-go-version
```

CI runs the same check (`.github/scripts/check-go-version.sh`) in the `test`
and `release` jobs, so drift fails the build instead of silently changing the
toolchain a release is built with.

Clone:

```
git clone https://github.com/getsyntegrity/go-specs
```

Run tests from the repository root:

```
make test
```

`make test` runs `go test ./...`. The repository is a **single Go module** with its `go.mod` at the
root — there is no `go.work` and no per-package module — so `go test ./...` covers everything and
running per-package (`go test ./specs/... ./snapshots/...`) is only useful to narrow a run.

Race detector:

```
make test-race
```

---

# Coding Guidelines

Follow standard Go practices.

Important rules:

* avoid reflection when possible
* avoid allocations in hot paths
* prefer simple APIs

---

# Branching Model

Two long-lived branches, with one rule each.

| Branch | What lands there | How |
|--------|------------------|-----|
| `develop` | Every feature, fix, refactor and doc change | PR targeting `develop`. This is the default branch, so a PR opened without choosing a base already points here. |
| `main` | Releases and hotfixes only | PR from `develop` to `main` when a release is due; a hotfix branches from `main` and PRs back into it, then is merged down into `develop`. |

Never open a feature PR against `main`. `main` exists to hold the commit a release is cut from, and a feature landing there directly is invisible to `develop` until someone notices and reconciles the two branches by hand.

When syncing `develop` into `main` for a release, merge — do not squash. A squash creates a commit on `main` that does not exist on `develop`, so the branches diverge again the moment the release lands.

## Releasing

Releases are manual and deliberate. The `Release` workflow runs on `workflow_dispatch` only: a push or merge to `main` does not publish by itself.

```bash
gh workflow run release.yml --ref main
```

Leave the `version` input empty to auto-bump the patch version from the latest tag, or pass an explicit `vX.Y.Z`. The workflow skips if `HEAD` is already tagged.

---

# Pull Requests

PRs must include:

* a base branch that matches the Branching Model above
* tests
* benchmarks (if performance related)
* documentation updates if APIs change

---

# Benchmark Validation

If modifying performance-sensitive code:

```
go test ./benchmarks -run='^$' -bench=. -benchmem
```

Results should not regress significantly.
