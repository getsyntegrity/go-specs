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

`.go-version` is the single source of truth for the Go version. CI installs it
through `actions/setup-go`'s `go-version-file`, version managers read it
automatically, and every `go.mod` in the repository must declare the **exact**
same version in its `go` directive — patch component included.

| File | Meaning |
| --- | --- |
| `.go-version` | The Go version. Bump this one. |
| every `go.mod`'s `go` directive | Must equal `.go-version` exactly. Generated from it, never edited by hand. |

The tradeoff is deliberate: one version, one place to bump. Because the `go`
directive is also the minimum language version downstream consumers inherit,
raising `.go-version` to a new patch release raises that floor too — anyone
importing go-specs must install at least that patch to build.

To bump, edit `.go-version`, propagate it to every module, and verify:

```
go mod edit -go="$(tr -d '[:space:]' <.go-version)"
make check-go-version
```

CI runs the same check (`.github/scripts/check-go-version.sh`) in the `test`
and `release` jobs, so drift fails the build instead of silently changing the
toolchain a release is built with.

Clone:

```
git clone https://github.com/getsyntegrity/go-specs
```

Run tests (from repo root; use `make test` because there is no root module):

```
make test
```

Or run tests per module, e.g. `go test ./specs/... ./gen/... ./snapshots/... ./benchmarks/... ./examples/...` (see `Makefile` for the full list).

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
