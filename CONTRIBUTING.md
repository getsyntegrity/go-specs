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

A release *is* a `develop` → `main` pull request. There is no separate release command to remember to run.

1. **Open the PR.** Base `main`, head `develop`. As soon as it's open (and again on every later push to `develop` while it stays open), the `Release prep` workflow (`.github/workflows/release-prep.yml`) computes the next version from the Conventional Commits merged into `develop` since the last tag, rewrites `CHANGELOG.md`'s `## [Unreleased]` heading into a dated `## [vX.Y.Z] - YYYY-MM-DD` heading, and pushes that as a `chore(release): prepare vX.Y.Z` commit straight onto `develop` — so it shows up on the PR before anyone merges it. If nothing releasable landed since the last tag (no commit beyond the ones `next-version` ignores — see "Version rules" below), the workflow says so in the job summary and does nothing else; open the PR again once something releasable exists.
2. **Review the prepared commit.** The version and the changelog section are both visible in the PR diff. If more commits land on `develop` while the PR is open, `Release prep` re-runs and, if the computed version changed, updates the prepared commit again.
3. **Merge with a merge commit, not a squash.** Squashing creates a commit on `main` that does not exist on `develop`, so the branches diverge again the moment the release lands (see the Branching Model above).
4. **`Release` runs automatically on merge.** `.github/workflows/release.yml` triggers on the PR's `closed` event, checks `merged == true` and that the head was `develop`, re-derives the version from commit history, cross-checks it against what `Release prep` wrote into `CHANGELOG.md` (a mismatch means `Release prep` didn't run, or didn't get to re-run after a late commit — the job fails loudly instead of tagging the wrong version), tags the merge commit, and runs GoReleaser. `main` and `develop` end up identical, so no sync PR is needed for an ordinary release.

Before any of that, CI's `verify` job runs `go run ./tools/release validate -file CHANGELOG.md` on every PR. It is read-only and only checks structure: within `## [Unreleased]`, each of the `### Added`, `### Changed`, `### Deprecated`, `### Removed`, `### Fixed` and `### Security` headings may appear at most once, in that order (omitted sections are fine). It does not read entry prose, so it never decides what is breaking and never moves an entry; put a **Breaking.** item under whichever category fits it. Run the same command locally before opening a PR that touches `CHANGELOG.md`.

There is no `workflow_dispatch` for releases anymore, and no manual tagging step — a single path, matching Decision 2 of [`docs/investigations/odd-tasks/native-ci-pipeline.md`](docs/investigations/odd-tasks/native-ci-pipeline.md).

### Prerequisite: the release GitHub App

`Release prep` and `Hotfix sync` both push commits, and push or open PRs, in a way that needs to retrigger this repository's other required checks — a commit or PR authored by the default `GITHUB_TOKEN` does not retrigger anything, so a required check would never run on the prepare commit or the sync PR, and neither could be merged. Both workflows instead push using a short-lived installation token minted from a dedicated GitHub App.

A GitHub App is used instead of a personal access token because the rulesets `protect-develop` and `protect-main` need exactly one actor able to bypass them for the prepare commit. A personal access token's bypass actor is the maintainer's own GitHub user — who could then also push directly to `main`/`develop` themselves, defeating the ruleset. A GitHub App installed only on this repository is a narrower actor: only the App can bypass, never a human.

Set it up once:

1. Create the App at <https://github.com/organizations/getsyntegrity/settings/apps/new>.
2. Leave the webhook off.
3. Grant permissions **Contents (read and write)** and **Pull requests (read and write)** — nothing else.
4. Install it only on `getsyntegrity/go-specs`, not org-wide.
5. Generate a private key for the App.
6. Add two repository secrets: `RELEASE_APP_ID` (the App's ID) and `RELEASE_APP_PRIVATE_KEY` (the private key's contents).
7. Add the App as the sole bypass actor of both rulesets, `protect-develop` and `protect-main`.

Both workflows fail fast with a clear `::error::` while either secret is missing, rather than failing deep inside a git push with an opaque authentication error.

### Version rules

The next version is computed from [Conventional Commits](https://www.conventionalcommits.org/) merged into `develop` since the last tag (`tools/release next-version`; see its package doc comment for the exact input format and `docs/investigations/odd-tasks/native-ci-pipeline.md`'s Decision 4 for the reasoning):

| Commit carries | Before `v1.0.0` (major `0`) | From `v1.0.0` on |
|---|---|---|
| A breaking change (`!` after the type/scope, or a `BREAKING CHANGE:`/`BREAKING-CHANGE:` footer) | bumps **minor** | bumps **major** |
| `feat` | bumps **patch** | bumps **minor** |
| Any other valid type (`build`, `chore`, `ci`, `docs`, `fix`, `perf`, `refactor`, `revert`, `style`, `test`) | bumps **patch** | bumps **patch** |

The highest-ranked bump among all qualifying commits wins (breaking > `feat` > everything else). A merge commit, a commit whose subject isn't a valid Conventional Commit, and the benchmark chart bot's own `chore: update benchmark charts` commit (or its historical `[skip ci]`-suffixed form) are all ignored and never trigger a release on their own.

### Hotfixes

A hotfix branches from `main` and PRs back into `main` (never targets `main` from `develop` — that's an ordinary release). Unlike before, a hotfix *does* go through `Release prep` and `Release` now, the same as a `develop` → `main` release, with one difference: the version is always forced to the next patch of the last tag, and the workflow fails if any commit on the branch is a breaking change or a `feat` — a hotfix must not change the API (see "Version rules" above; `tools/release next-version -patch-only`).

1. **Branch and fix.** Branch from `main`, make the fix, and add a changelog entry under `## [Unreleased]` in `CHANGELOG.md` — this is required, not optional: `main`'s `[Unreleased]` is always empty right after the previous release, so `Release prep` fails with an actionable error (`add an entry under "## [Unreleased]" describing the fix, then run this again`) until one exists.
2. **Open the PR.** Base `main`, head `hotfix/*`. `Release prep` runs exactly as it does for a `develop` PR, except it computes the version with `-patch-only` and prepares the changelog on the hotfix branch itself.
3. **Merge with a merge commit.** `Release` runs automatically, tags the merge commit, and publishes — same as an ordinary release.
4. **The fix is synced back into `develop` automatically.** As the last step of that same `Release` run, a `sync/hotfix-vX.Y.Z` branch is pushed from the just-tagged commit and a pull request is opened from it into `develop`, titled `chore: sync hotfix <version> into develop`, using the release GitHub App's installation token, completing the "merged down into `develop`" step the Branching Model above requires. This sync PR's head is deliberately **not** `main` itself: `.github/settings.yml` sets `delete_branch_on_merge: true`, which deletes a merged PR's head branch (whether merged with a merge commit or a squash) once nothing is protecting it from deletion, and `main` is neither this repository's default branch nor protected — a PR headed `main` would make GitHub delete `main` the moment it's merged. The disposable `sync/hotfix-vX.Y.Z` branch exists only to carry that one PR and is expected to disappear once it's merged.
5. **Resolve the likely `CHANGELOG.md` conflict.** By the time the sync PR is reviewed, `main` has gained a dated `## [vX.Y.Z]` section for the hotfix while `develop`'s own `## [Unreleased]` has likely kept growing — both edit the top of the file, so the sync PR usually conflicts there. Resolve it by keeping `develop`'s `[Unreleased]` section above the hotfix's released section, not the other way around.

If the hotfix itself should also ship as part of a larger release later, nothing further is needed — it is already tagged and published; `develop` just also carries it once the sync PR is merged.

### Benchmark charts

`benchmark-charts.yml` regenerates `benchmarks/results/*.png` on every push to `develop` (and via manual dispatch) and rolls the result into a single pull request from the fixed branch `chore/benchmark-charts` into `develop`, opened or updated with the release GitHub App's installation token — it never pushes to `develop` (or `main`) directly, for the same reason a hotfix's sync PR never does. The workflow ignores a push whose only change is those chart files (`paths-ignore`), so merging the rolling PR does not immediately retrigger it and reopen a fresh one — benchmark numbers differ slightly on every run, so without that rule the PR would never stay closed. Like the hotfix sync branch above, `chore/benchmark-charts` is expected to be deleted once its PR merges (`delete_branch_on_merge`); the next push to `develop` recreates it fresh.

---

# Pull Requests

PRs must include:

* a base branch that matches the Branching Model above
* tests
* benchmarks (if performance related)
* documentation updates if APIs change

A PR is validated by `ci.yml` and `codeql.yml` running as its own `pull_request` checks, aggregated
into the single required status check `ci-ok` (plus CodeQL's own `analyze (go)`/`analyze
(actions)`) — no workflow re-runs the same checks again after merge. The one commit that reaches
`develop` before `ci.yml` has run on it is release prep's `chore(release): prepare` commit, which
may change only `CHANGELOG.md` and is validated by the release PR before it can reach `main`. See
[`docs/CI.md`](docs/CI.md) for the full pipeline reference: the workflow inventory, required
checks, ruleset settings, and the SHA-pin policy every third-party action follows.

---

# Benchmark Validation

If modifying performance-sensitive code:

```
go test ./benchmarks -run='^$' -bench=. -benchmem
```

Results should not regress significantly.
