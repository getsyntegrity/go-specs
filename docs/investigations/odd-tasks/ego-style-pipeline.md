# ego-style-pipeline — move go-specs to the ego repository's CI/release pipeline

Follow-up to `workflow-hardening.md`. Branch `ci/ego-style-pipeline`, based on `origin/develop`.

## Objective

Replace go-specs' GitHub Actions pipeline with one modeled on the ego repository's, so both
repositories are operated the same way: one required check (`ci-ok`), a release that happens by
merging into `main`, a version taken from PR labels, and no bot that pushes to a protected branch.
Keep every go-specs capability that ego lacks (GoReleaser, the changelog structure check, the
Go-version drift check, `gofmt`, benchmark smoke run, ratio guard and rolling chart PR, fuzzing,
CodeQL, the documented `go-specs-report` flow job) by porting it into ego's structure.

## Problem

Today go-specs releases through a chain that needs a GitHub App: `release-prep.yml` pushes a
`chore(release): prepare` commit straight onto `develop`, `release.yml` runs on the PR `closed`
event and cross-checks two version computations, and `hotfix-sync`, `benchmark-charts.yml` mint an
App token to open PRs. That App has to be a bypass actor of both branch rulesets, it is the only
thing allowed to write to `develop`, and it is a piece of setup that cannot live in the repository.
ego reaches the same outcomes with less: the version comes from `next-version.sh`, the tag and
GitHub Release are created on `push` to `main` with the plain workflow token, and the follow-up
changes go through ordinary pull requests.

## Why

One mental model for both repositories, fewer moving parts (two workflows and an App removed),
and the branching rule "develop and main only change through pull requests" becomes literally true
for bots as well, not true-with-one-exception. The cost is a behavior change in how versions are
chosen and how `CHANGELOG.md` is stamped; both are spelled out in Decisions.

## Scope

In scope: everything under `.github/`, the workflow sections of `CONTRIBUTING.md` and
`docs/CI.md`, one small Makefile target for the script tests.
Out of scope: any Go code (including `tools/release`), the rulesets themselves (they live in the
GitHub UI; the required-check names that change are listed under Acceptance criteria), repository
secrets.

## Constraints

- Third-party actions stay pinned to a 40-character commit SHA with a `# vX.Y.Z` comment.
- Least-privilege `permissions:` per job; `persist-credentials: false` on every checkout that does
  not push.
- No `-race` added anywhere new. The existing CI race runs (`race` job, `property` module, the
  benchmark race check) are kept because they are already CI configuration.
- No workbench invocations.
- Artifacts are written in English.
- TDD mode: strict (session configuration). Runner for the new shell scripts:
  `bash .github/scripts/test/run.sh` (new, wired into `make test-ci-scripts` and the `verify` job).
  Go tests are not touched.

## Decisions

### D1. Release model: ego's, with `CHANGELOG.md` stamped by a pull request after the release

What the repository does now: `release-prep.yml` rewrites `## [Unreleased]` into
`## [vX.Y.Z] - date` on `develop` while the release PR is open; `release.yml` later checks that
the commit-derived version equals that heading.

What it does after this change:

1. A merge into `main` (from `develop` or `hotfix/*`) triggers `release.yml` on `push`.
2. The version comes from `.github/scripts/next-version.sh`: a `release:major|minor|patch` label on
   the merged PR wins; otherwise `hotfix/*` is a patch and `develop` is a minor.
3. The release notes are the `## [Unreleased]` entries of the release commit. The script
   `.github/scripts/release-changelog.sh` works on a temporary copy of `CHANGELOG.md`, drops every
   entry that the previous tag's `CHANGELOG.md` already contains, runs the existing
   `go run ./tools/release changelog` to stamp the version, and then
   `go run ./tools/release notes` to extract that section. GoReleaser publishes it as the GitHub
   Release body. If there are no entries, GoReleaser's generated git-log changelog is used instead
   (the release is not blocked).
4. After the release, job `sync-develop` creates a disposable `sync/release-vX.Y.Z` branch from the
   tag, stamps `CHANGELOG.md` the same way, and opens a pull request into `develop`. For a hotfix
   this is also the main → develop sync (it carries the hotfix commits). It uses
   `secrets.ORG_CHECKOUT_TOKEN || github.token`, exactly like ego, so CI runs on that PR when the
   token is available.

Why entries of the previous tag are dropped: `main` no longer receives a stamped changelog before
the release. After a `develop` release, `main`'s `## [Unreleased]` still holds that release's
entries until the next `develop` → `main` merge brings the stamp from `develop`. A hotfix branched
from `main` in that window would otherwise publish those old entries again. Everything that is in
the previous tag's `CHANGELOG.md` has, by definition, already been released.

Rejected alternatives:

- Keep `release-prep.yml` (bot stamps `develop` before merging). Rejected: it is the reason the
  App exists and the only writer to a protected branch; removing it is the point of the migration.
- Use ego's `changelog.sh` (notes from PR `release-note` blocks, `CHANGELOG/` directory).
  Rejected: go-specs' `CHANGELOG.md` is a hand-written Keep a Changelog file with long, reviewed
  entries and a structure check (`tools/release validate`); replacing it with PR-body blocks would
  throw that content away and create two sources of truth.
- Stamp the changelog in a human-triggered "prepare release" workflow before the release PR.
  Rejected: it brings back a "did we remember to run it" step, which `native-ci-pipeline.md`
  Decision 2 removed on purpose.

### D2. The release GitHub App is removed

Nothing needs a ruleset bypass any more: no workflow pushes to `develop` or `main`. The three jobs
that open pull requests (`sync-develop` in `release.yml`, the rolling chart PR in
`benchmark-charts.yml`) use ego's `secrets.ORG_CHECKOUT_TOKEN || github.token`. Without the secret
they still work, but GitHub does not start workflows from a `github.token` pull request, so
`ci-ok` would not report on it; closing and reopening the PR by hand starts CI.

Rejected: keeping a minimal App only to mint a token for those PRs. Rejected because ego already
uses the org token convention and it keeps one mechanism across repositories. The trade-off is that
a personal access token is broader and longer-lived than an App installation token; if that
matters later, swapping the token expression in the two workflows back to an App token is a
two-line change per job.

`release-prep.yml` is deleted. `RELEASE_APP_ID`, `RELEASE_APP_PRIVATE_KEY` and the App's bypass
entries in `protect-develop` / `protect-main` can be deleted by a human.

### D3. Tests, security and the triggers follow ego

- `push` to `develop` is added to `ci.yml` (ego's design: it stores test timings that PRs use to
  shard, and validates the merge result). This reverses go-specs Decision 1 of
  `pr-gated-pipeline.md` ("no push trigger"), whose main motivation was the release-prep commit;
  that commit no longer exists.
- Tests run in shards (`plan` → `test (shard N)` → `test-report`) with
  `.github/scripts/test-matrix.sh` and `gotestsum`. The `race` job stays a plain
  `go test -race ./...`, as it is today.
- `merge_group` is dropped from `ci.yml`: the repository does not use a merge queue
  (`CI.md`), and ego does not have it.
- CodeQL moves into `security.yml` (push to `develop`, nightly, manual), keeping the `go` and
  `actions` legs. Consequence: it is no longer a PR check, so `analyze (go)` and
  `analyze (actions)` stop being required checks. Rejected alternative: leave `codeql.yml` as it
  is. It was rejected to keep the PR path at a single required check as in ego; the price is that
  a workflow-injection mistake is found after merge rather than on the PR.
- `govulncheck` on PRs keeps go-specs' SHA-pinned action (ego uses `go run ...@latest`, an
  unpinned execution). The strict nightly variant from ego is added to `security.yml` with a
  pinned `govulncheck` version.
- Dropped because they do not apply:
  `test (min)` — the `go` floor of `go.mod` is by policy the pinned minor (`check-go-version.sh`),
  so it would just repeat the main test run; `go-sdk-update.yml` / `go-sdk-update.sh` — go-specs has
  no Go SDK dependency to track; ego's nested-module list — replaced by go-specs' single nested
  module `property`; the `OWNERS`/`CODEOWNERS` match step in `pr-meta` — go-specs has no `OWNERS`.
- Added from ego because they fit: `tidy` (go.mod/go.sum are tidy, both modules), `api` (apidiff;
  informational on `develop`, blocking only from `v1` on without `release:major`, because the
  script already treats `v0.x` as a warning), `flow`, `pr-meta`, `notify`, `labels.sh`, issue and
  PR templates.
- Kept from go-specs: the module-path check, `gofmt` check, `go vet`, build, `go run ./tools/release
  validate -file CHANGELOG.md`, the Go-version drift check (inside the composite action), bench
  smoke run plus its race check, `report-cli`, golangci-lint (pinned, full not new-only),
  `dependency-review`, GoReleaser check plus snapshot, the external installability check after a
  release, `benchmarks.yml`, `benchmark-charts.yml`, `fuzz.yml`, `settings.yml`.

### D4. Version bumping changes

`tools/release next-version` (Conventional Commits) is no longer used by any workflow; ego's
label-based `next-version.sh` replaces it. Practical differences: before, `feat` commits before
`v1.0.0` bumped patch and a breaking change bumped minor; now a `develop` → `main` release is a
minor bump unless the PR carries `release:patch` or `release:major`, and a hotfix is a patch. The
"hotfix must not contain a feat or a breaking change" check of `-patch-only` is gone. The Go
code of `tools/release` (`next-version`, `latest-heading`) is left in place, unused by CI, so this
change touches no Go file; removing it can be a follow-up.

### D5. Things that do not change

- `.goreleaser.yaml` keeps `builds: - skip: true`. go-specs publishes no binary: `go-specs-report`
  is installed with `go install`, as `docs/REPORTING.md` documents. Only the GitHub Release is
  created.
- The nested module `property` has no tag of its own (it would need `property/vX.Y.Z` tags); that
  was true before and is not addressed here.

## Tasks

- [x] T1 — Composite action `go-setup` (renamed from `setup-go`), ego's scripts
  (`next-version.sh`, `test-matrix.sh`, `api-check.sh`, `labels.sh`), the new
  `release-changelog.sh`, the `notify` action, issue and PR templates, and script tests.
  Check: shellcheck on every script; `bash .github/scripts/test/run.sh`.
  Commit: `5d0fb5c`. RED observed first (release-changelog.sh missing: 6 failures), then GREEN
  (13/13). shellcheck clean after quoting the bump values in `next-version.sh` (SC2209).
- [x] T2 — `ci.yml` in ego's shape (`flow`, `plan`, sharded `test`, `test-report`, `modules`,
  `tidy`, `api`, `ci-ok`) with the go-specs jobs ported in. Check: actionlint.
  Commit: `2c1b83d`. actionlint clean; `go mod tidy` is clean in both modules and `api-check.sh`
  ran against `origin/develop` locally, so the new `tidy` and `api` jobs start green.
- [x] T3 — `release.yml` (push to `main`, notes from `CHANGELOG.md`, `sync-develop`, `notify`),
  `pr-meta.yml`; delete `release-prep.yml`. Check: actionlint; local dry run of the version and
  notes scripts.
  Commit: `7ed18a0`. actionlint clean. Dry runs in the real repo: `next-version.sh develop` gives
  v0.4.0, `hotfix/x` gives v0.3.2 (local tags stop at v0.3.1), `release:patch` gives a patch and
  `release:major` gives v1.0.0; `release-changelog.sh` against the real `CHANGELOG.md` produced a
  27-line notes file from the current `[Unreleased]` section.
- [x] T4 — `security.yml` (CodeQL folded in, strict govulncheck, notify); delete `codeql.yml`;
  token swap in `benchmark-charts.yml`; composite switch in `benchmarks.yml` and `fuzz.yml`;
  `dependabot.yml`. Check: actionlint; no unpinned `uses:`.
  Commit: `a1d5749`. actionlint clean; no unpinned `uses:`; no `RELEASE_APP` or App-token
  reference left in `.github/`.
- [x] T5 — Docs: `CONTRIBUTING.md` (Branching Model, Releasing, hotfix, benchmark charts, Pull
  Requests) and `docs/CI.md` rewritten for the new pipeline. Check: every workflow and script named
  in the docs exists; no reference to `release-prep` or the App remains outside investigation
  notes.
  Commit: `a239704`. Final run: actionlint, shellcheck (7 scripts), script tests (13/13),
  `make check-go-version`, `tools/release validate`, `go build ./...` and `goreleaser check` all
  pass.

## Acceptance criteria

- `actionlint` reports nothing; `shellcheck` is clean on every `.github/scripts/*.sh`.
- `make check-go-version`, `go run ./tools/release validate -file CHANGELOG.md` and
  `go build ./...` still pass.
- `next-version.sh` returns the documented version for develop, hotfix and label-override cases.
- Required checks after this change are `ci-ok` and `pr-meta`. Removed names: `analyze (go)`,
  `analyze (actions)`. The two rulesets must be updated by a human before merging.
- No `release-prep.yml`, no App token step, no `RELEASE_APP_*` reference in `.github/`.

## Checks

actionlint (latest), shellcheck, `bash .github/scripts/test/run.sh`, local runs of
`next-version.sh` and `test-matrix.sh`, `make check-go-version`,
`go run ./tools/release validate -file CHANGELOG.md`, `go build ./...`.

## Needs a human before merging

- Rulesets `protect-develop` and `protect-main`: required checks become `ci-ok` and `pr-meta`;
  remove `analyze (go)` and `analyze (actions)`; remove the App as bypass actor.
- Run `.github/scripts/labels.sh` once (creates `skip-changelog`, `kind/*`, `release:*`). This PR
  itself needs the `skip-changelog` label for `pr-meta`.
- Make sure `ORG_CHECKOUT_TOKEN` is available to this repository (org secret), otherwise the sync
  and chart pull requests need a close/reopen to run CI. `RELEASE_APP_ID` and
  `RELEASE_APP_PRIVATE_KEY` can be deleted.
- None of the workflows could be run here; only actionlint, shellcheck, the script tests and local
  dry runs of the scripts. The first real runs are the check.
- Comments in `tools/release/*.go` still mention `release-prep.yml`; Go files were out of scope.
  `next-version` and `latest-heading` of that tool are now unused by CI.
- The Engram mirror `odd/ego-style-pipeline/tasks` could not be written (the memory server asked
  for a session id); this file is the only copy.

## Progress

- Route: one delegated writer (this worktree), inline edits; no SDD artifacts.
- Drafted from a full read of both `.github` trees, `CONTRIBUTING.md`, `Makefile`,
  `.goreleaser.yaml`, `tools/release` and the three earlier investigation notes.
