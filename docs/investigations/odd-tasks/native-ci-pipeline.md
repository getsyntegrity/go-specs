# native-ci-pipeline — GitHub Actions + GoReleaser pipeline, automatic release on develop → main

## Problem

CI has three gaps. First, lint (`golangci-lint`) and `govulncheck` only ran inside the `shipwright`
job of `.github/workflows/ci.yml`, and that job is disabled (`if: false`) because the Shipwright
runner (`pablogore/shipwright` v0.12.0) was unstable. Neither check runs anywhere today. Second,
there is no code scanning. Third, releases are a manual `workflow_dispatch`, a rule inherited from
#127 when `main` and `develop` had diverged. They are reconciled now, and the maintainer wants a
release to happen automatically when `develop` is merged into `main`.

## What changes

- Drop Shipwright (the `.shipwright/` directory and the `shipwright` job). Its checks come back as
  native jobs: `lint` and `govulncheck`.
- Add CodeQL (`security-and-quality` queries) on pull requests, on pushes to `develop`/`main`, and
  weekly. Dependabot targets `develop` and groups minor/patch updates.
- Harden the workflows: a least-privilege `permissions: contents: read` default, `concurrency` that
  cancels superseded pull-request runs, and a `goreleaser check` on pull requests so a broken
  `.goreleaser.yaml` fails before release day.
- Replace the manual release with an automatic one, **only** for a `develop` → `main` pull request:
  - **While the PR is open**, a workflow computes the next version from the Conventional Commits
    since the last tag. It rewrites `CHANGELOG.md`'s `## [Unreleased]` heading to
    `## [vX.Y.Z] - YYYY-MM-DD`, adds a fresh empty `[Unreleased]` and the compare links, and pushes
    that commit to `develop`. The version and changelog are visible in the PR before merging.
  - **When the PR is merged** (merge commit, per CONTRIBUTING.md), a workflow tags the merge commit,
    runs GoReleaser, and keeps the existing "installable with `go get`" check. `main` and `develop`
    stay identical, so no sync PR is needed.
- Hotfix: a `hotfix/*` → `main` PR does **not** release. When it is merged, a workflow opens a
  `main` → `develop` sync PR.

## Decisions

1. **Changelog updated inside the release PR (option A, chosen by the maintainer 2026-09-27).**
   Rejected: option B, updating the changelog after the merge on `main` and syncing back with a
   PR. It shows less before publishing and adds a sync PR to every release.
2. **Automatic release only for `develop` → `main`** (maintainer rule). Detected from the merged
   pull request's head ref, not from "any push to main". The manual `workflow_dispatch` release is
   removed so there is a single path.
3. **A `RELEASE_TOKEN` secret (fine-grained PAT, this repo only, contents and pull-requests
   write).** Commits and PRs made with `GITHUB_TOKEN` do not trigger other workflows, so required
   checks would never run on the changelog commit or the hotfix sync PR, and the PR could not be
   merged. The maintainer creates the token; the workflows fail with a clear error while it is
   missing. Rejected for now: a GitHub App (cleaner, more setup for a single maintainer).
4. **Version rule.** `!` or `BREAKING CHANGE` bumps minor while below v1.0.0 (the CHANGELOG
   already says pre-1.0 releases may break, and the pending breaking removal targets v0.2.0) and
   major from v1.0.0 on. `feat` bumps minor from v1.0.0 on and patch below it. Anything else bumps
   patch. With no releasable commits since the last tag, nothing is prepared. This was a judgement
   call. Rejected: `feat` bumping minor pre-1.0, which would make every feature release
   indistinguishable from a breaking one.
5. **Version and changelog logic is a small Go program** (`tools/release`), tested with `go test`.
   Rejected: inline shell in YAML, which cannot be unit-tested and is where the current bump logic
   hides.
6. **Branch protection is not changed by this PR's code.** Today neither `main` nor `develop` is
   protected and the only ruleset is disabled. The maintainer's rule (PR required, 0 approvals) needs
   a ruleset, created with explicit authorization in T5, with `RELEASE_TOKEN`'s actor allowed to
   push the changelog commit.

## Tasks

- [x] T1 — CI hygiene: drop Shipwright; native `lint` and `govulncheck` jobs; least-privilege
  permissions; `concurrency`; `goreleaser check` on PRs. Check: YAML parses, `actionlint` if
  available, CI green on the PR.
- [x] T2 — CodeQL workflow; dependabot `target-branch: develop` plus groups. Check: YAML parses, CodeQL
  runs on the PR.
- [x] T3 — `tools/release`: next version from commits, and the CHANGELOG rewrite. Behaviour tests
  (strict TDD). Plus the `release-prep` workflow on `develop` → `main` PRs. Check:
  `go test ./tools/release/...`, YAML parses.
- [ ] T4 — `release` workflow on a merged `develop` → `main` PR (tag, GoReleaser, installability);
  `hotfix-sync` workflow; update CONTRIBUTING.md's release section. Check: YAML parses, dry-run of
  the tool on the real history.
- [ ] T5 — Ruleset for `main`/`develop` (only with explicit authorization); open the PR. Check: CI
  green. Do not merge.

## Limits

The release workflows cannot run end to end before `RELEASE_TOKEN` exists and a real
`develop` → `main` PR is opened. T3 and T4 prove the logic with unit tests and a dry run on the real
tag and commit history, not with a published release.

## TDD

Strict TDD is on (user global config), with runner `go test`.

## Progress

- Branch `ci/native-pipeline` from `develop`.
- T1 done. `.shipwright/` and the disabled `shipwright` job removed from `.github/workflows/ci.yml`;
  added native `lint` (`golangci/golangci-lint-action@v7`, pinned to the locally installed
  `v2.13.1`) and `govulncheck` (`golang/govulncheck-action@v1`) jobs, both `GOTOOLCHAIN: local` +
  `.go-version` like `test`/`goreleaser`. Fixed the 3 pre-existing `make lint` findings so the new
  job starts green: `benchmarks/comparison_bench_test.go` renamed `cmpErr`/`cmpWrapped` to
  `errCmp`/`errCmpWrapped` (ST1012), `snapshots/snapshot.go:105` discards `fmt.Fprintf`'s error
  explicitly with a comment (errcheck) — both are diagnostic/benchmark-only, no behavior change.
  Added top-level `permissions: contents: read` and PR-cancelling `concurrency` to `ci.yml`;
  added top-level `permissions: contents: read` and non-cancelling `concurrency` to
  `benchmarks.yml` (`bench`'s job-level `contents: write` for the chart commit-back is
  unchanged). `release.yml` already declares `permissions: contents: write` at the top level, so
  it was left alone in T1 — T4 rewrites it per the design doc. `goreleaser check` already ran in
  the existing `goreleaser` job; no change needed there beyond inheriting the new
  `permissions`/`concurrency` blocks.
- T2 done. Added `.github/workflows/codeql.yml`: `github/codeql-action@v3`, language `go`,
  `security-and-quality` queries, triggers on pull_request/push to develop+main, a weekly cron,
  and workflow_dispatch; manual `go build ./...` (not autobuild) under `GOTOOLCHAIN: local` +
  `.go-version`, so the traced build matches every other workflow's pinned toolchain; job
  permissions `actions: read`, `contents: read`, `security-events: write` (its own block, since
  the ci.yml/benchmarks.yml `contents: read` default doesn't cover SARIF upload). Added
  `target-branch: develop` and a minor+patch update group to both `dependabot.yml` ecosystems
  (`gomod`, `github-actions`); major bumps stay ungrouped.
- T3 done, strict TDD. Wrote `tools/release/version_test.go` and `changelog_test.go` against
  not-yet-existing `NextVersion`/`classifyCommit`/`RewriteChangelog`/`ChangelogOptions` first;
  `go test ./tools/release/...` failed to build (RED):
  `tools/release/changelog_test.go:38:23: undefined: RewriteChangelog` (and 6 more `undefined:`
  errors) before any implementation existed. Implemented `version.go` (Conventional Commit
  classification + Decision 4's bump rules; commits are NUL-delimited `git log --format='%B%x00'`
  records) and `changelog.go` (Unreleased rewrite + Keep-a-Changelog compare links — this
  repository's CHANGELOG.md had no prior link-reference style, so the format follows Keep a
  Changelog directly), then `main.go` (CLI: `next-version`, `changelog`; exit 0/1/3). All GREEN:
  `go test ./tools/release/...` — 43 subtests across `TestNextVersion_BumpRules` (22),
  `TestNextVersion_NothingReleasable` (4), `TestNextVersion_StrictSemverLastTag` (7),
  `TestClassifyCommit` (12), 8 `TestRewriteChangelog_*` cases, and 8 `TestRun_*` CLI-level cases —
  all pass. `make lint` clean on the new package (fixed 6 errcheck findings on the CLI's own
  stderr/stdout writes via a `logf` helper that discards the write error deliberately, documented
  inline).

  Added `.github/workflows/release-prep.yml`: triggers on `pull_request`
  `[opened, synchronize, reopened, ready_for_review]` to `main`; job gated on
  `head.ref == 'develop' && head.repo.full_name == github.repository`; fails fast with `::error::`
  before checkout if `RELEASE_TOKEN` is empty; checks out `develop` with `fetch-depth: 0` and the
  PAT; computes the last tag (`git describe --tags --abbrev=0 origin/main`, falling back to the
  latest local `v*` tag, then `v0.0.0`); runs `tools/release next-version`, treating exit 3 as a
  green no-op (`::notice::` + job summary, every later step skipped); runs `tools/release
  changelog`; commits `chore(release): prepare vX.Y.Z` as `github-actions[bot]` and pushes to
  `develop` only when `git diff` shows a change (the tool's own idempotency, exercised by
  `TestRewriteChangelog_Idempotent`/`TestRun_Changelog_IdempotentSecondRunExitsZero`, is what
  makes the retriggered run after that push a no-op: same last tag, same computed next version,
  changelog already carries that heading, no diff, no second push — verified by reasoning through
  the retrigger with the actual regex/classification rules, not by running the workflow itself,
  since that needs a real PR and `RELEASE_TOKEN`, see Limits). `permissions: contents: read`
  (write goes through the PAT); `concurrency` keyed on the PR number, `cancel-in-progress: false`.

  Dry run on real history (`git fetch origin main develop --tags`, then run from this worktree):
  `origin/main` is at `v0.1.2`; `git log v0.1.2..origin/develop` has 69 commits, including two
  breaking changes (`feat(specs)!: add BeforeAll/AfterAll...` #207,
  `refactor(specs)!: replace interface{} DSL bodies...` #224) — `go run ./tools/release
  next-version -last v0.1.2 -commits <that range>` → `v0.2.0` (breaking, pre-1.0 → minor bump;
  correct per Decision 4). `go run ./tools/release changelog -version v0.2.0 -date 2026-09-27
  -file <temp copy of CHANGELOG.md>` → rewrote the heading, inserted a fresh `[Unreleased]`, and
  added `[Unreleased]: .../compare/v0.2.0...HEAD` and `[v0.2.0]: .../compare/v0.1.0...v0.2.0` (prev
  correctly derived from the existing `## [0.1.0] - 2026-09-16` heading, normalized to `v0.1.0`);
  re-running the same command against its own output was a no-op (`already has a heading for
  v0.2.0, nothing to do`, byte-identical file). Noted, not fixed (out of scope): `origin/main` and
  `origin/develop` have already diverged by one commit each — main carries a
  `chore: update benchmark charts [skip ci]` commit develop never received, and a `Merge pull
  request #160` merge commit — the exact main/develop drift this whole change is meant to prevent
  going forward once releases stop being ad hoc `workflow_dispatch` runs (T4); this pair predates
  T1-T4 and both are ignored by `next-version`'s classification (a bot commit exact-match and a
  merge-commit subject) regardless.
