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
- [ ] T3 — `tools/release`: next version from commits, and the CHANGELOG rewrite. Behaviour tests
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
