# CONTRIBUTING.md

Thanks for contributing to go-specs.

---

# Development Setup

Requirements:

* Go — install the exact version recorded in `.go-version`; that file is the
  source of truth for the toolchain CI builds and tests with, and version
  managers (`asdf`, `mise`, `goenv`, `gvm`) read it automatically.
* make (optional)

## Supported Go version policy

`.go-version` and every `go.mod`'s `go` directive answer two different
questions, so they deliberately hold two different values:

- `.go-version` is the exact toolchain patch CI and contributors build and
  test with. CI installs it through `actions/setup-go`'s `go-version-file`,
  and version managers (`asdf`, `mise`, `goenv`, `gvm`) read it automatically.
- every `go.mod`'s `go` directive is the minimum language version every
  downstream consumer of go-specs must have installed to build against it —
  the floor, not the pin.

go-specs supports **Go >= MAJOR.MINOR.0 of the pinned toolchain** — currently
**1.25.0**. The floor is the `.0` release of the pinned minor, never the
pinned patch and never a bare `MAJOR.MINOR`:

| File | Role | Current value |
| --- | --- | --- |
| `.go-version` | Exact toolchain CI builds and tests with. Bump this for every patch or minor update. | `1.25.14` |
| every `go.mod`'s `go` directive | Minimum Go version consumers need. Always `MAJOR.MINOR.0` of `.go-version`, generated from it, never edited by hand. | `1.25.0` |

The floor is not lower than the pinned minor because CI only ever builds on
that one toolchain (see `ci.yml`'s header for why a version matrix is
deliberately not run) — a lower floor would be an untested claim nothing
exercises. It is also usually the lowest floor actually achievable: dependencies commonly declare their own `go` directive at `MAJOR.MINOR.0` of a
recent minor, and `go mod tidy` on the pinned toolchain raises anything lower
to match. Patch releases within a minor are API-identical by Go's
compatibility policy, so testing on the pinned patch already validates every
consumer on that minor, whichever patch they run.

Bump procedure:

- **Patch bump** (e.g. `1.25.14` -> `1.25.20`): edit `.go-version` only. The
  floor (`MAJOR.MINOR.0`) is unchanged, so no `go.mod` edit is needed.
- **Minor bump** (e.g. `1.25.14` -> `1.26.1`): edit `.go-version`, then
  propagate the new floor to every module and verify:

```
go mod edit -go="<MAJOR>.<MINOR>.0" go.mod
make check-go-version
```

CI runs the same check (`.github/scripts/check-go-version.sh`) in the `test`
and `release` jobs, so drift fails the build instead of silently changing the
toolchain a release is built with.

Clone:

```
git clone https://github.com/getsyntegrity/go-specs
```

Run tests (from the repo root -- go-specs is a single module rooted there):

```
make test
```

`make test` is `go test ./...`; run that directly, or narrow it to a package
(`go test ./specs/...`) while iterating.

Race detector:

```
make test-race
```

## Formatting and linting

```
make fmt        # rewrite tracked Go files with gofmt
make fmt-check  # fail when any tracked Go file is not gofmt-clean
make lint       # golangci-lint ./... , or go vet ./... when it is not installed
```

CI runs `make fmt-check`, so formatting drift fails the build. `make lint`
exits non-zero whenever an installed `golangci-lint` does; the `go vet`
fallback is only for machines without it, never a second chance after a
failed lint.

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
