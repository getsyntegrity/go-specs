# native-ci-pipeline — GitHub Actions + GoReleaser pipeline, automatic release on develop → main

## Problem

CI has three gaps. First, lint (`golangci-lint`) and `govulncheck` only ran inside a job of
`.github/workflows/ci.yml` that has since been disabled (`if: false`) because that container-based
CI runner was unstable. Neither check runs anywhere today. Second,
there is no code scanning. Third, releases are a manual `workflow_dispatch`, a rule inherited from
#127 when `main` and `develop` had diverged. They are reconciled now, and the maintainer wants a
release to happen automatically when `develop` is merged into `main`.

## What changes

- Drop the disabled container-based CI runner (its config directory and its CI job). Its checks
  come back as native jobs: `lint` and `govulncheck`.
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
   write).** *(Superseded 2026-09-27 — see below; kept for history.)* Commits and PRs made with
   `GITHUB_TOKEN` do not trigger other workflows, so required checks would never run on the
   changelog commit or the hotfix sync PR, and the PR could not be merged. The maintainer creates
   the token; the workflows fail with a clear error while it is missing. Rejected for now: a
   GitHub App (cleaner, more setup for a single maintainer).

   **Superseded (2026-09-27): a GitHub App replaces the PAT.** The active ruleset
   `protect-main-develop` (PR required on `main`/`develop`, no deletion, no force push) must let
   exactly one actor bypass it for release-prep's changelog push to `develop`. With the PAT from
   the decision above, that bypass actor would be the maintainer's own GitHub user — who could
   then push directly to `main`/`develop` too, defeating the ruleset the ruleset exists to
   enforce. A GitHub App installed only on `getsyntegrity/go-specs` is a narrower actor: only the
   App can bypass, never a human, including the maintainer. Rejected alternative: keeping the PAT
   and accepting the maintainer as the bypass actor — rejected because it reopens exactly the
   direct-push hole the ruleset was created to close. New secrets `RELEASE_APP_ID` and
   `RELEASE_APP_PRIVATE_KEY` replace `RELEASE_TOKEN`; `.github/workflows/release-prep.yml` and
   `.github/workflows/hotfix-sync.yml` mint a short-lived installation token per run via
   `actions/create-github-app-token@v2` instead of reading a long-lived PAT, and the prepare
   commit is attributed to the App's own bot identity rather than `github-actions[bot]` or a
   human, since the App is the actor the ruleset now names.
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
   a ruleset, created with explicit authorization in T5, with the release GitHub App's actor (see
   Decision 3's supersession) allowed to push the changelog commit.
7. **CI split into staged jobs (maintainer request 2026-09-27).** `ci.yml`'s `test` job had grown
   into a monolith: version pin, module path, gofmt, build, vet, `go test`, `go test -race`,
   benchmark-path execution, and benchmark-path racing, all as one job that reports one PR status
   line. A failure anywhere inside it only says "test failed" -- finding out *which* of the eight
   checks broke means opening the log. It also serialized everything: `go test -race` never started
   until gofmt, build, and vet had each already finished, even though nothing about `-race` depends
   on them running first in the same job.

   The fix models `ci.yml`'s jobs on a CircleCI-style `requires` pipeline: a job graph with real
   `needs` edges instead of one job with many steps. Stage 1 runs in parallel with no dependencies --
   `verify` (Go version pin via the new `.github/actions/setup-go` composite action, module path
   check, gofmt, `go vet ./...`, `go build ./...`), plus the existing `lint`, `govulncheck`, and
   `goreleaser` jobs. Stage 2 -- `unit` (`go test ./...`), `race` (`go test -race ./...`), and
   `bench-smoke` (the benchmark-execution and benchmark-race steps, unchanged) -- each declare
   `needs: verify`, so none of them spends a runner until the fast, deterministic checks are green.
   Each stage now has its own PR status line, and `lint`/`govulncheck`/`goreleaser` already ran in
   parallel with `test` before this change, so splitting `test` only adds parallelism inside what
   used to be serial.

   Rejected: keeping one job with many steps. It was the status quo, and it fails on exactly the two
   things this change fixes -- a failure is only as visible as "open the log and scroll", and nothing
   inside that one job can run concurrently with anything else inside it, so a fast, unrelated
   failure (gofmt) still waits behind nothing, but a would-be-parallel check (`-race` vs. the
   benchmark steps) pays full serial cost for no correctness reason. Also considered: moving the
   Set-up-Go step's options inline into every job instead of a composite action -- rejected because
   `verify`, `unit`, `race`, and `bench-smoke` all need the exact same toolchain pin and drift check,
   and four copies of the same options is the same duplication problem the split is trying to remove.

## Tasks

- [x] T1 — CI hygiene: drop the disabled container-based CI runner; native `lint` and `govulncheck` jobs; least-privilege
  permissions; `concurrency`; `goreleaser check` on PRs. Check: YAML parses, `actionlint` if
  available, CI green on the PR.
- [x] T2 — CodeQL workflow; dependabot `target-branch: develop` plus groups. Check: YAML parses, CodeQL
  runs on the PR.
- [x] T3 — `tools/release`: next version from commits, and the CHANGELOG rewrite. Behaviour tests
  (strict TDD). Plus the `release-prep` workflow on `develop` → `main` PRs. Check:
  `go test ./tools/release/...`, YAML parses.
- [x] T4 — `release` workflow on a merged `develop` → `main` PR (tag, GoReleaser, installability);
  `hotfix-sync` workflow; update CONTRIBUTING.md's release section. Check: YAML parses, dry-run of
  the tool on the real history.
- [ ] T5 — Ruleset for `main`/`develop` (only with explicit authorization); open the PR. Check: CI
  green. Do not merge.

## Limits

The release workflows cannot run end to end before the release GitHub App exists (`RELEASE_APP_ID`
and `RELEASE_APP_PRIVATE_KEY` provisioned) and a real `develop` → `main` PR is opened. T3 and T4
prove the logic with unit tests and a dry run on the real tag and commit history, not with a
published release.

## TDD

Strict TDD is on (user global config), with runner `go test`.

## Progress

- Branch `ci/native-pipeline` from `develop`.
- T1 done. The runner's own config directory and its disabled CI job removed from
  `.github/workflows/ci.yml`;
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
  before checkout if `RELEASE_APP_ID`/`RELEASE_APP_PRIVATE_KEY` is empty; mints a short-lived
  installation token from the release GitHub App (`actions/create-github-app-token@v2`); checks
  out `develop` with `fetch-depth: 0` and that token; computes the last tag (`git describe --tags
  --abbrev=0 origin/main`, falling back to the latest local `v*` tag, then `v0.0.0`); runs
  `tools/release next-version`, treating exit 3 as a green no-op (`::notice::` + job summary,
  every later step skipped); runs `tools/release changelog`; commits
  `chore(release): prepare vX.Y.Z` as the App's own bot identity (looked up via `gh api
  /users/<slug>[bot]`) and pushes to `develop` only when `git diff` shows a change (the tool's own
  idempotency, exercised by
  `TestRewriteChangelog_Idempotent`/`TestRun_Changelog_IdempotentSecondRunExitsZero`, is what
  makes the retriggered run after that push a no-op: same last tag, same computed next version,
  changelog already carries that heading, no diff, no second push — verified by reasoning through
  the retrigger with the actual regex/classification rules, not by running the workflow itself,
  since that needs a real PR and the App installed, see Limits). `permissions: contents: read`
  (write goes through the App's installation token); `concurrency` keyed on the PR number,
  `cancel-in-progress: false`.

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
- T4 done. Added `tools/release/info.go` (+ `info_test.go`, strict TDD: `go test ./tools/release/...`
  failed to build first — `undefined: LatestHeadingVersion` etc. — then implemented) with two more
  read-only subcommands wired into `main.go`: `latest-heading -file CHANGELOG.md` (the version
  from the top heading below `## [Unreleased]`, normalized to `vX.Y.Z`, falling back to the first
  heading in the file when there is no `Unreleased` section at all) and `notes -version vX.Y.Z
  -file CHANGELOG.md` (that version's own section body, for GoReleaser's `--release-notes`).
  `ReleaseNotes` matches the heading with or without a leading `v`, so it finds this repository's
  existing `## [0.1.0]` heading (no `v`) as well as every new `## [vX.Y.Z]` heading this tool
  writes going forward.

  Rewrote `.github/workflows/release.yml`: trigger is now `pull_request: types: [closed]` on
  `main` (the `workflow_dispatch` input is gone — Decision 2, single path); job gated on
  `merged == true && head.ref == 'develop' && head.repo.full_name == github.repository`; checks
  out `github.event.pull_request.merge_commit_sha` (not main's moving HEAD, so a second PR merging
  into `main` between job start and the version-determination step can't shift which commit this
  run is about); a new "Determine the version to release" step recomputes the version from
  commits (`tools/release next-version`, same last-tag lookup as release-prep.yml) *and* reads it
  from `tools/release latest-heading`, and fails with `::error::` on any disagreement (this is the
  safety net for "release-prep.yml never ran, or a later commit invalidated what it wrote");
  idempotency guard unchanged in spirit (skip if the computed version's tag already exists); tags
  the merge commit directly (not `HEAD`) and pushes; a new "Extract release notes" step tries
  `tools/release notes`, passing `--release-notes <file>` to GoReleaser only when that section is
  non-empty, otherwise logging an `::notice::` and falling back to GoReleaser's own git-log
  changelog exactly as before; the "Verify external installability" step is unchanged, just
  re-pointed at the computed version output. `permissions: contents: write` moved from
  workflow-level to job-level only (no other job in this file needs it).

  Added `.github/workflows/hotfix-sync.yml`: `pull_request: types: [closed]` on `main`, gated on
  `merged == true && startsWith(head.ref, 'hotfix/') && head.repo.full_name == github.repository`
  (disjoint from `release.yml`'s condition, since a hotfix branch is never named `develop`); mints
  a release GitHub App installation token the same way as release-prep.yml, then opens a
  `main` → `develop` PR via `gh pr create` using that token (the App is reused rather than
  provisioning a second credential with the same Pull-requests-write scope), or comments on an
  already-open one if a second hotfix lands before the first sync PR is merged; fails fast with
  `::error::` if `RELEASE_APP_ID`/`RELEASE_APP_PRIVATE_KEY` is missing, same message shape as
  release-prep.yml's check. No release step — by design, per the task.

  Rewrote CONTRIBUTING.md's Releasing section (previously the `gh workflow run release.yml --ref
  main` manual-dispatch instructions) into: the new develop→main-PR-is-the-release model in 4
  steps, a "Prerequisite: `RELEASE_TOKEN`" subsection (what scopes the fine-grained PAT needs and
  why GITHUB_TOKEN-authored pushes don't work — Decision 3), a "Version rules" table restating
  Decision 4 for contributors, and a "Hotfixes" subsection describing the hotfix→main→(sync
  PR)→develop flow. Left every other CONTRIBUTING.md section untouched. *(2026-09-27: that
  Prerequisite subsection was later replaced by a GitHub App setup subsection — see Decision 3's
  supersession above and the Progress entry below.)*

  Dry run on real history, continuing from T3's dry-run artifacts: `go run ./tools/release
  latest-heading -file CHANGELOG.md` (the real, unmodified file on this branch) → `v0.1.0`
  (correctly falls back to the pre-existing `## [0.1.0]` heading — no release has been prepared on
  this branch); on T3's `/tmp/dryrun-CHANGELOG.md` (already rewritten to `v0.2.0`) →
  `v0.2.0`. Cross-check: `next-version -last v0.1.2 -commits <same 69-commit range as T3>` →
  `v0.2.0`, `latest-heading -file /tmp/dryrun-CHANGELOG.md` → `v0.2.0` — **MATCH**, confirming
  `release.yml`'s consistency check would pass on this exact prepared state. `go run
  ./tools/release notes -version v0.2.0 -file /tmp/dryrun-CHANGELOG.md` → 68741 bytes, the entire
  real (very large) Unreleased section this repository currently carries, starting `### Removed`
  and ending with the `v0.1.0 is broken` notice right before `## [0.1.0]` — correct given the
  actual file content, and confirms `ReleaseNotes` stops exactly at the next `## ` heading.
- CI split into staged jobs, per Decision 7 (maintainer request 2026-09-27). `ci.yml`'s `test`
  monolith is now `verify` (Go version pin, module path, gofmt, `go vet ./...`, `go build ./...`)
  plus `lint`/`govulncheck`/`goreleaser`, all Stage 1 with no `needs`; and `unit` (`go test ./...`),
  `race` (`go test -race ./...`), `bench-smoke` (unchanged benchmark-execution and
  benchmark-race steps), each `needs: verify`, as Stage 2. Added `.github/actions/setup-go`, a
  local composite action (`actions/setup-go@v7` with the same `go-version-file: .go-version` +
  `cache: false` options the old `test` job used, then `check-go-version.sh`), used by `verify`,
  `unit`, `race`, and `bench-smoke`; `lint` and `goreleaser` keep their own inline `actions/setup-go`
  step unchanged, since the task scoped them as existing/unchanged. Every job keeps
  `env: GOTOOLCHAIN: local`, and Checkout still runs before the composite action in every job that
  uses it (documented in the action's own header — a local action resolves from the checked-out
  tree). Top-level `permissions: contents: read` and the existing `concurrency` block are
  unchanged. Checked for anything else referencing the old `test` job name: no `workflow_run`
  trigger in any other workflow names it, and `.github/settings.yml` deliberately declares no
  branch protection (`develop` has none today, and the one ruleset is disabled) — so nothing
  needed updating there; not touched.
  Verified: `python3 -c 'import yaml; yaml.safe_load(open(f))'` on both new/changed files parses
  clean; `go run github.com/rhysd/actionlint/cmd/actionlint@latest` — 0 findings; `make fmt-check`,
  `go vet ./...`, `go build ./...`, `go test ./...` all pass unchanged (no Go source touched).
- CI parallelism (maintainer request 2026-09-27): measured `race` at 71s, mostly race-instrumented
  compilation, with `vet` at 24s. Dropped `needs: verify` from `unit`/`race`/`bench-smoke`, so all
  jobs start together, and added a GOCACHE-only `actions/cache` in `.github/actions/setup-go`, one
  key per compile flavour (vet/test/race/bench). setup-go's own cache stays off (tar "File exists").
- Ruleset `protect-main-develop` (id 24078173) was created live on 2026-09-27 with deletion
  protection, non_fast_forward protection, and a pull_request rule (0 required approvals,
  merge+squash allowed), no bypass actor configured yet, required status checks to be added after
  PR #280 merges. The old, already-disabled ruleset `gospecs` was left untouched because it
  requires linear history, which is incompatible with merge-commit releases.
- `RELEASE_TOKEN` (Decision 3) replaced with the release GitHub App (Decision 3's supersession),
  per the maintainer's 2026-09-27 decision that the ruleset's bypass actor must be narrower than
  the maintainer's own user: `.github/workflows/release-prep.yml` and
  `.github/workflows/hotfix-sync.yml` now verify `RELEASE_APP_ID`/`RELEASE_APP_PRIVATE_KEY` and
  mint a short-lived installation token via `actions/create-github-app-token@v2` instead of
  reading `secrets.RELEASE_TOKEN`; the release-prep changelog commit is attributed to the App's
  own bot identity (`<slug>[bot]` / `<id>+<slug>[bot]@users.noreply.github.com`, looked up via
  `gh api /users/<slug>[bot]`) instead of `github-actions[bot]`. `release.yml`'s tag step is
  unaffected — it already used the default `GITHUB_TOKEN` and stays `github-actions[bot]`.
  CONTRIBUTING.md's "Prerequisite: `RELEASE_TOKEN`" subsection was replaced with GitHub App setup
  instructions (create the App, webhook off, Contents RW + Pull requests RW, install only on
  go-specs, generate a private key, add `RELEASE_APP_ID`/`RELEASE_APP_PRIVATE_KEY`, add the App as
  the ruleset's sole bypass actor); the WHY (retriggering required checks that a default
  `GITHUB_TOKEN`-authored commit/PR would not retrigger) is unchanged, only the mechanism moved.
- 2026-09-27: GitHub App `go-specs-release` (app id 5098738; Contents RW, Pull requests RW,
  selected repositories) created by the maintainer, secrets `RELEASE_APP_ID` /
  `RELEASE_APP_PRIVATE_KEY` set. The App was added live as the sole bypass actor
  (`Integration`, `always`) of ruleset `protect-main-develop` (id 24078173); rules and branches
  unchanged. Not yet verified by a real run: that happens on the first `develop` → `main` PR.
