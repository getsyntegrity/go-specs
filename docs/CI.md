# CI/CD pipeline guide

How this repository's GitHub Actions pipeline works. It is modeled on the pipeline of the `ego`
repository, so both are operated the same way, and it keeps the go-specs-only pieces (GoReleaser,
the `go-specs-report` flow check, benchmarks, fuzzing). It assumes no prior context beyond
CONTRIBUTING.md's Branching Model. The design notes and the decisions behind it are in
`docs/investigations/odd-tasks/ego-style-pipeline.md`.

## Goals

- **Two required checks, one of them aggregating.** Rulesets name `ci-ok` (every job of `ci.yml`)
  and `pr-meta` (`pr-meta.yml`), never an individual job, so adding, renaming or splitting a job
  never means editing a ruleset.
- **`develop` and `main` only change through pull requests, for bots too.** No workflow pushes to
  either branch. `release.yml` pushes a tag and a `sync/*` branch; `benchmark-charts.yml` pushes a
  `chore/*` branch; both open ordinary pull requests.
- **Releasing is a merge.** Merging `develop` (or a `hotfix/*` branch) into `main` tags and
  publishes; there is no separate release command and no "prepare" step that must run first.
- **Every third-party action is pinned to a commit SHA**, not a mutable tag.
- **Heavy scanning stays off the PR path** (`security.yml`).

## Workflow inventory

| File | Trigger(s) | Job(s) | Purpose |
|---|---|---|---|
| `ci.yml` | `pull_request` (`develop`, `main`), `push` (`develop`, `main`), `workflow_dispatch` | `flow`, `verify`, `lint`, `plan`, `test (shard N)`, `test-report`, `race` (merges only), `modules (property)`, `bench-smoke`, `report-cli`, `tidy`, `api`, `vuln`, `goreleaser`, `dependency-review` (PR-only), `ci-ok` | The gate. See "What `ci.yml` checks". |
| `pr-meta.yml` | `pull_request` (`develop`, `main`) | `pr-meta` | A PR must change `CHANGELOG.md` unless exempt (`skip-changelog` or `kind/deps` label, a `sync/*`, `dependabot/*` or `chore/benchmark-charts` branch, or the `develop` → `main` release PR). |
| `release.yml` | `push` (`main`), `workflow_dispatch` (re-run) | `release`, `sync-develop`, `notify` | Tag, GitHub Release, installability check, then a pull request into `develop`. See "Release flow". |
| `security.yml` | `push` (`develop`), nightly `schedule`, `workflow_dispatch` | `codeql (go)`, `codeql (actions)`, `govulncheck-strict`, `notify` | Static analysis and strict vulnerability scan. Warns, never blocks. The `actions` leg scans the workflow YAML itself for script injection. |
| `fuzz.yml` | weekly `schedule`, `workflow_dispatch` (`duration`, default `5m`) | `fuzz` (matrix: one job per fuzz target) | Coverage-guided fuzzing of the assertion diagnostics. Not a gate; pull requests already replay the seed corpus through `go test ./...`. Run it with `gh workflow run fuzz.yml -f duration=10m`. |
| `benchmarks.yml` | `push` (`develop`), `workflow_dispatch` | `ratio-guard` | Post-merge trend signal: Runner/Describe cost against a hand-written no-framework loop in the same process. Not a gate. |
| `benchmark-charts.yml` | `push` (`develop`, `paths-ignore` on its own chart output), `workflow_dispatch` | `bench` → `chart` → `publish` | Regenerates `benchmarks/results/*.png` and rolls the result into one reused pull request (`chore/benchmark-charts` → `develop`). |

Composite actions: `.github/actions/go-setup` (toolchain from `.go-version`, the `go.mod` drift
check, the Go build cache; every Go job uses it) and `.github/actions/notify` (Slack message; does
nothing unless `SLACK_BOT_TOKEN` and the `SLACK_CHANNEL` variable exist).

Scripts in `.github/scripts`: `next-version.sh`, `release-changelog.sh`, `test-matrix.sh`,
`api-check.sh`, `check-go-version.sh`, `labels.sh` (creates the labels). `make test-ci-scripts`
runs `.github/scripts/test/run.sh`, which tests `next-version.sh` and `release-changelog.sh` in
throwaway git repositories (`ci.yml`'s `verify` job runs it too).

## What `ci.yml` checks

- **`flow`**: only a PR from `develop` or `hotfix/*` may target `main`. For a PR to `main` it also
  prints the version the merge would publish and a preview of the release notes, and fails early if
  the version cannot be published.
- **`verify`**: module path equals the repository path, `gofmt` (`make fmt-check`), `go vet`,
  `go build`, `go run ./tools/release validate -file CHANGELOG.md`, and the release script tests.
- **`plan`, `test`, `test-report`**: tests run in shards planned from the timings of previous runs
  (`test-matrix.sh`; timings are saved by `test-report` and cached, and `push` to `develop` keeps
  them fresh). `plan` skips the compile-heavy jobs for a PR that only touches top-level Markdown,
  `docs/` or the issue/PR templates; `ci-ok` still reports, because a skipped job counts as
  success.
- **Race detector, merges only.** The `race` job, the race step of `modules` and the race-checked
  benchmark step of `bench-smoke` run on `push` to `develop` or `main` (the merge of a PR) and on
  `workflow_dispatch`, never on pull requests. `-race` instruments the whole build and is the
  slowest part of the pipeline, so feature and hotfix PRs skip it to keep feedback fast; a race is
  caught on the merge instead. Need it on a PR? Run `make test-race` locally or trigger the
  workflow manually on the branch (`gh workflow run ci.yml --ref <branch>`).
- **`race`**, **`modules`** (the nested `property` module: build, vet, test; race on merges),
  **`bench-smoke`** (every benchmark once; on merges also a race-checked run of the
  goroutine-spawning ones), **`report-cli`**
  (runs the `go-specs-report` flow documented in `docs/REPORTING.md` for real, against the
  `report/coordination` fixtures; green only when `go test` is red and `finalize` succeeds with the
  expected totals), **`tidy`**, **`vuln`** (`govulncheck`), **`goreleaser`** (`goreleaser check`
  plus a `--snapshot` release), **`dependency-review`** (moderate and above fails; PR-only), **`lint`**
  (golangci-lint, pinned), **`api`** (`apidiff`: informational against `develop`; against the latest
  tag on a PR to `main` it blocks an incompatible change without `release:major` once the tag is
  `v1`+, and only warns on `v0.x`).

## Branch model

- `develop` is the default branch. Every feature, fix, refactor and doc change targets it.
- `main` holds releases and hotfixes only: `develop` → `main` for an ordinary release, or
  `hotfix/*` → `main` for a hotfix. The follow-up to either goes back into `develop` through the
  `sync/release-vX.Y.Z` pull request that `release.yml` opens.
- Never open a feature PR against `main`.

Full detail, including the version-bump rules and the changelog conflict a hotfix sync can carry,
is in CONTRIBUTING.md's Branching Model and Releasing sections.

## Required checks and ruleset settings

Two GitHub rulesets, one per protected branch:

- **`protect-develop`**: pull request required; 0 required approvals (single-maintainer
  repository); "require branches to be up to date before merging".
- **`protect-main`**: pull request required, **merge commit only** (squash disallowed, because a
  squash creates a commit on `main` that does not exist on `develop`).
- **Both rulesets**: no branch deletion, no force push, **no bypass actor**. Nothing in the pipeline
  needs one any more.

Required status checks on both: **`ci-ok`** and **`pr-meta`**. `analyze (go)` and
`analyze (actions)` are no longer PR checks (CodeQL moved to `security.yml`) and must be removed
from the required list.

## Release flow

**Ordinary release:**

1. Open a `develop` → `main` PR. `flow` shows the version and the release notes preview. Add a
   `release:*` label only to override the default (minor).
2. Merge with a merge commit. `release.yml` runs on the push: version from `next-version.sh`, tag
   `vX.Y.Z` pushed with the workflow token, notes from `CHANGELOG.md`, GoReleaser creates the
   GitHub Release, and `go get <module>@<tag>` is verified from a clean module. Re-running the
   workflow on the same commit reuses the tag and skips a release that already exists.
3. `sync-develop` opens the pull request `sync/release-vX.Y.Z` → `develop` that stamps
   `CHANGELOG.md`. Merge it with a merge commit.

**Hotfix:** branch `hotfix/<name>` from `main`, add a `CHANGELOG.md` entry, PR into `main`, merge.
Same release job (a patch by default), and `sync-develop` opens the main → develop pull request that
carries the fix and the stamp. The release notes exclude any entry the previous tag already
published, which is what makes a hotfix cut from an unstamped `main` safe.

## Secrets and variables

- `ORG_CHECKOUT_TOKEN` (optional): an organization token, used by `release.yml`'s `sync-develop` and
  `benchmark-charts.yml`'s `publish` to open pull requests that trigger CI. Without it they fall back
  to `github.token`, and the pull request has to be closed and reopened to run CI.
- `SLACK_BOT_TOKEN` (secret) and `SLACK_CHANNEL` (variable), both optional: enable `notify`.
- Nothing else. There is no release GitHub App any more: `RELEASE_APP_ID`, `RELEASE_APP_PRIVATE_KEY`
  and the App's bypass entries on the rulesets can be deleted.

## Repository settings

Declared as code in `.github/settings.yml` (applied by the Probot Settings App; see that file's own
header for what happens if the app is not installed):

- `default_branch: develop`.
- `delete_branch_on_merge: true`: every disposable branch this pipeline creates
  (`chore/benchmark-charts`, `sync/release-vX.Y.Z`) is meant to be deleted once its PR merges.
- `allow_squash_merge`, `allow_merge_commit` and `allow_rebase_merge` all stay available at the
  repository level; the per-branch rulesets restrict `main`.
- `enable_vulnerability_alerts` and `enable_automated_security_fixes` (Dependabot alerts and
  security updates).
- Secret scanning and push protection are not declared there (the Probot schema does not cover
  them); enable them in Settings, Code security and analysis.

Labels used by the pipeline (`kind/*`, `skip-changelog`, `release:major|minor|patch`,
`needs-triage`) are created by `.github/scripts/labels.sh`; run it once with an authenticated `gh`.

## SHA-pin policy

Every `uses: owner/repo@...` in `.github/workflows` and `.github/actions` is pinned to a 40-character
commit SHA with a trailing `# vX.Y.Z` comment, never a bare `@vN` tag. Each SHA is resolved with:

```
git ls-remote --tags https://github.com/<owner>/<repo> 'refs/tags/vN*'
```

taken to the action's most specific current release tag within the major already in use, with the
peeled `^{}` commit used for an annotated tag. `.github/dependabot.yml`'s `github-actions`
ecosystem keeps the pins current by opening a PR that bumps the SHA and its version comment
together. Verify that no unpinned action is left with:

```
rg -n 'uses: [^.].*@' .github | rg -v '@[0-9a-f]{40}'
```

which must print nothing. The same rule covers tools fetched with `go install` / `go run`: they are
pinned to a version (`gotestsum`, `govulncheck`, `apidiff`).

## Porting to another repository

- **The module path check** (`ci.yml`'s `verify` job) assumes `go list -m` equals
  `github.com/${GITHUB_REPOSITORY}`.
- **The Go version pin file**: every job reads `.go-version` through
  `.github/actions/go-setup/action.yml`, the one place the pin is read from.
- **`report-cli`, `bench-smoke`, `benchmarks.yml`, `benchmark-charts.yml` and `fuzz.yml` are
  go-specs-specific**: they assume this repository's fixtures, benchmark suite and fuzz targets.
  Drop them elsewhere.
- **`release-changelog.sh` only matters where `CHANGELOG.md` is hand-written** with a Keep a
  Changelog `## [Unreleased]` section and `tools/release`. A repository that generates notes from
  PRs (as `ego` does) does not need it.
- **What is not expressible in a workflow file**: the two rulesets, the required check names, the
  optional `ORG_CHECKOUT_TOKEN` and Slack secrets, and the labels.
