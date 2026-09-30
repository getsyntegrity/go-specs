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
  test with. CI installs it through the `.github/actions/go-setup` action (`go-version-file`),
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

Two long-lived branches, with one rule each. Neither is ever pushed to directly, by a person or by a bot: both change only through pull requests.

| Branch | What lands there | How |
|--------|------------------|-----|
| `develop` | Every feature, fix, refactor and doc change | PR targeting `develop`. This is the default branch, so a PR opened without choosing a base already points here. |
| `main` | Releases and hotfixes only | PR from `develop` when a release is due; a `hotfix/*` branch cut from `main` for an urgent fix. Nothing else may target `main`: the `flow` job of `ci.yml` fails any other head branch. |

Never open a feature PR against `main`. `main` exists to hold the commit a release is cut from.

Merge `develop` into `main` with a merge commit, not a squash. A squash creates a commit on `main` that does not exist on `develop`, so the branches diverge again the moment the release lands.

## Pull request checklist

* Base branch follows the model above. The [PR template](.github/PULL_REQUEST_TEMPLATE.md) repeats this.
* Set exactly one `kind/*` label (`kind/feature`, `kind/bug`, `kind/breaking`, `kind/deprecation`, `kind/deps`, `kind/chore`, `kind/docs` or `kind/flake`). The `pr-meta` check fails otherwise, and the label decides which section of the release notes your PR appears in.
* Do not edit `CHANGELOG/`: it is generated when a release is cut. Your PR title (without its `feat:`/`fix:` prefix) becomes the note; to word it differently, fill the optional `release-note` block of the PR description (see "Release notes" below). Write `NONE` in the block, or apply the `skip-changelog` label, when nobody who uses go-specs can see the change.
* Put a `release:major`, `release:minor` or `release:patch` label on the PR that goes to `main` only when you want to override the default bump.

## Releasing

A release *is* a merge into `main`. There is no release command to remember to run, and no step that must happen before the merge.

1. **Nothing to prepare.** The changelog is written by the release itself, from the pull requests merged since the previous tag. Check that each of them has its `kind/*` label (`pr-meta` enforces it) and, when the title is not a good note, its `release-note` block.
2. **Open the release PR.** Base `main`, head `develop`. The `flow` job of `ci.yml` prints the version that merging will publish and a preview of the release notes in the run's summary. A version that cannot be published (for example a `v2` without the `/v2` module path) fails there, before the merge.
3. **Merge with a merge commit, not a squash.**
4. **`release.yml` runs on the push to `main`.** It computes the version (below), pushes the tag, runs GoReleaser to create the GitHub Release with the notes, and checks that `go get <module>@<tag>` works from a clean consumer module. Re-running it on the same commit reuses the tag.
5. **Merge the sync pull request.** When the release finishes, the `sync-develop` job opens a pull request into `develop` from a disposable `sync/release-vX.Y.Z` branch. It contains one commit that adds the release to `CHANGELOG/CHANGELOG-X.Y.md` (every patch of a minor in one file, newest first) and to the index `CHANGELOG/README.md`. Merge it with a merge commit; `main` receives it with the next release. If you forget for a while nothing breaks: the notes are always computed from the tags, never from the files.

### Release notes

The GitHub Release body and the new section of `CHANGELOG/CHANGELOG-X.Y.md` are the same text, produced by `.github/scripts/changelog.sh` in the Kubernetes changelog layout. It lists the PRs merged between the previous tag and the new one (squash commits ending in `(#N)` and `Merge pull request #N` commits). For each PR the note is the ` ```release-note ` block of its description, or else its title without the Conventional Commit prefix; a note of `NONE` leaves the PR out. The PR's `kind/*` label picks the section under "Changes by Kind": `kind/deprecation` Deprecation, `kind/breaking` API Change, `kind/feature` Feature, `kind/bug` Bug or Regression, `kind/deps` Dependency, anything else Other. A `kind/breaking` PR, or a note containing "action required", is also listed under "Urgent Upgrade Notes". A "Dependencies" section lists the `go.mod` changes between the two tags. The release PR (`develop` to `main`), `sync/*` PRs and PRs labeled `skip-changelog` are never listed. If no PR qualifies, the release still happens and GoReleaser uses its generated git-log changelog.

`CHANGELOG.md` at the root is only a pointer to `CHANGELOG/README.md`. One file, `CHANGELOG/unreleased-legacy.md`, holds the hand-written `[Unreleased]` entries that existed when the changelog became generated; the first release folds them into its section under "Hand-written entries" and the sync PR deletes the file.

### Version rules

The version comes from `.github/scripts/next-version.sh`, from the pull request that was merged into `main`:

| Merged PR | Bump |
|---|---|
| Carries a `release:major`, `release:minor` or `release:patch` label | That bump (the label wins) |
| Head is `hotfix/*` | patch |
| Head is `develop` | minor |

go-specs is pre-1.0, so an ordinary release is a minor bump (`v0.3.2` to `v0.4.0`) unless you label the release PR `release:patch`. From `v2` on, the module path in `go.mod` must end in `/vN`; `next-version.sh` refuses to compute a version that could not be fetched with `go get`. The `api` job of `ci.yml` runs `apidiff` against `develop` (informational) and, on a PR to `main`, against the latest tag: it blocks an incompatible change without `release:major` once the latest tag is `v1` or later, and only warns while it is `v0.x`.

### Hotfixes

A hotfix branches from `main` and PRs back into `main`. It is an urgent fix, so it is always a patch: nothing in the flow forces that for you, so keep a hotfix free of new API.

1. **Branch and fix.** Branch `hotfix/<name>` from `main` and make the fix. Its PR needs a `kind/*` label (normally `kind/bug`) like any other.
2. **Open the PR.** Base `main`, head `hotfix/*`. `ci.yml` runs like for any PR, and the `flow` job shows the patch version it will publish.
3. **Merge with a merge commit.** `release.yml` tags and publishes.
4. **Merge the sync pull request.** This one is also the "merged down into `develop`" step: `sync/release-vX.Y.Z` carries the hotfix commits and the changelog update into `develop`. `CHANGELOG/` files are generated, so on a conflict (for example `CHANGELOG/README.md`) keep both sides' lines. The head is deliberately a `sync/*` branch and never `main`: `.github/settings.yml` sets `delete_branch_on_merge: true`, and a pull request headed `main` would make GitHub delete `main` when it merges.

If the hotfix should also ship as part of a larger release later, nothing further is needed: it is already tagged and published, and `develop` carries it once the sync pull request is merged.

### Tokens

No workflow pushes to `develop` or `main`, so nothing needs a ruleset bypass and there is no release GitHub App. Two jobs open pull requests (`sync-develop` in `release.yml` and the rolling chart PR of `benchmark-charts.yml`); both use `secrets.ORG_CHECKOUT_TOKEN`, an organization token, when it exists and `github.token` otherwise. GitHub does not start workflows for a pull request opened with `github.token`, so without the secret those pull requests get no `ci-ok` until someone closes and reopens them.

### Benchmark charts

`benchmark-charts.yml` regenerates `benchmarks/results/*.png` on every push to `develop` (and via manual dispatch) and rolls the result into a single pull request from the fixed branch `chore/benchmark-charts` into `develop`, labeled `skip-changelog`. It never pushes to `develop` or `main` directly. The workflow ignores a push whose only change is those chart files (`paths-ignore`), so merging the rolling PR does not immediately retrigger it and reopen a fresh one: benchmark numbers differ slightly on every run, so without that rule the PR would never stay closed. `chore/benchmark-charts` is deleted once its PR merges (`delete_branch_on_merge`); the next push to `develop` recreates it.

---

# Pull Requests

PRs must include:

* a base branch that matches the Branching Model above
* tests
* benchmarks (if performance related)
* documentation updates if APIs change
* a `kind/*` label (and a `release-note` block when the title is not a good note)

A PR is validated by `ci.yml` (aggregated into the single required status check `ci-ok`) and `pr-meta.yml` (the required check `pr-meta`). CodeQL and the strict `govulncheck` run from `security.yml` on `develop` and nightly; they warn and are not PR checks. No workflow re-runs the PR's checks again after merge, except `ci.yml`'s `push` to `develop`, which validates the merge result and stores the test timings the next PRs use to shard. See [`docs/CI.md`](docs/CI.md) for the full pipeline reference: the workflow inventory, required checks, ruleset settings, and the SHA-pin policy every third-party action follows.

---

# Benchmark Validation

If modifying performance-sensitive code:

```
go test ./benchmarks -run='^$' -bench=. -benchmem
```

Results should not regress significantly.
