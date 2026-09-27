# hotfix-release-and-charts — patch release on hotfix merge, benchmark charts through a PR

Follow-up to `native-ci-pipeline.md` (PR #280), split out to keep each spec within 5 tasks.
Stacked on `ci/native-pipeline`.

## Problem

Two gaps remain after #280.

First, a hotfix does not reach users. #280 releases only on a `develop` → `main` merge, so a
`hotfix/*` → `main` PR lands the fix on `main` without ever tagging it. Routing the hotfix through
`develop` instead would drag every unreleased change into the release. Today that means the v0.2.0
breaking removals (#204, #207, #224) would ship to someone who only needed a fix on v0.1.2.

Second, `benchmarks.yml` commits the benchmark charts directly to `main`. That breaks the rule
that `main` and `develop` only change through pull requests.

## What changes

- **Hotfix releases a patch automatically.** `release-prep.yml` also runs for a `hotfix/*` → `main`
  PR. It prepares the version and changelog on the hotfix branch, with the version forced to the
  next patch of the last tag. `release.yml` also runs when a `hotfix/*` PR is merged. It tags the
  merge commit and publishes, then, as the final step of that same job, pushes a disposable
  `sync/hotfix-vX.Y.Z` branch and opens the `develop` sync PR from it (Decisions 5-6) -- replacing
  the separate `hotfix-sync.yml` workflow.
- **Charts go to `develop` through one rolling PR.** On a push to `develop`, the chart job
  regenerates the charts and opens or updates a single PR from the fixed branch
  `chore/benchmark-charts` into `develop`. It never pushes to `main` or `develop` itself.

## Decisions

1. **Hotfix = patch only (maintainer decision 2026-09-27).** A hotfix whose commits contain a
   breaking change (`!` or `BREAKING CHANGE`) or a `feat` fails `release-prep`, because a hotfix
   must not change the API. Rejected: routing hotfixes through `develop`, which would release every
   unreleased change along with the fix.
2. **A hotfix needs a changelog entry.** `main`'s `[Unreleased]` is empty right after a release,
   so an empty `[Unreleased]` on the hotfix branch fails with a message asking for the entry,
   instead of publishing a release with no notes.
3. **Charts: one rolling PR into `develop` (maintainer decision 2026-09-27).** Rejected: one PR per
   push, which is noise for a single maintainer. The chart workflow ignores pushes that only change
   chart files. Benchmark numbers differ on every run, so without that rule merging the chart PR
   would regenerate the charts and reopen the PR forever.
4. **The sync PR can conflict on `CHANGELOG.md`.** `main` gains a `## [vX.Y.Z]` section while
   `develop`'s `[Unreleased]` keeps growing, and both edit the top of the file. The sync PR is
   opened anyway, and its body says so. A human resolves it by keeping `develop`'s `[Unreleased]`
   above the hotfix's released section.
5. **The sync PR's head must never be `main` (maintainer decision 2026-09-27).**
   `.github/settings.yml` sets `delete_branch_on_merge: true`, and `main` is neither this
   repository's default branch (`develop` is) nor protected today, so a merged PR whose head is
   `main` would make GitHub delete `main` itself. `release.yml`'s hotfix path instead pushes the
   just-tagged commit to a disposable branch named from the release, `sync/hotfix-vX.Y.Z`, and
   opens the sync PR from that branch into `develop`. That branch existing only to be deleted on
   merge is exactly what `delete_branch_on_merge` is for.
6. **Sync folded into `release.yml`, not a separate `hotfix-sync.yml` triggered by `workflow_run`
   (maintainer decision 2026-09-27).** The sync PR must open *after* the release (so it carries the
   tagged state), and a `workflow_run` trigger on `release.yml`'s completion would need to
   re-derive "was this a hotfix merge" from `github.event.workflow_run.pull_requests` -- a second,
   less direct read of the same fact `release.yml` already computed once. Doing the sync as the
   last step of `release.yml`'s own hotfix path guarantees the ordering by construction (it is
   later in the same job) instead of by two workflows' triggers racing or a `workflow_run`
   completion filter. `hotfix-sync.yml` is removed; its logic moved into `release.yml`.
7. **Dependency review added alongside `govulncheck`/CodeQL/Dependabot (maintainer decision
   2026-09-27).** `govulncheck` (native-ci-pipeline.md's T1) only flags a vulnerability that is
   actually *reachable* from this repository's own code; CodeQL scans this repository's own code
   for flaws, not its dependencies' advisories; Dependabot (enabled 2026-09-27) opens fix PRs after
   a vulnerable dependency is already in `go.mod`. None of the three blocks a PR from introducing a
   dependency version that already carries a known advisory, which is what
   `actions/dependency-review-action` does, on the PR that adds the dependency, before it merges.

## Tasks

- [x] T1 — `tools/release next-version -patch-only`: bump patch; fail on breaking/`feat` with a
  clear message. Strict TDD. Check: `go test ./tools/release/...`.
- [x] T2 — `release-prep.yml` and `release.yml` also handle `hotfix/*` → `main` (prepare on the
  hotfix branch; publish on merge, then open the `sync/hotfix-vX.Y.Z` → `develop` sync PR as the
  last step of the hotfix path, mentioning the possible CHANGELOG conflict); `hotfix-sync.yml`
  removed (Decision 6). Check: actionlint, YAML parse, dry run of the tool.
- [ ] T3 — `benchmarks.yml`: charts through a rolling PR into `develop`, with no direct push and
  loop-safe path filtering. CONTRIBUTING.md release and hotfix section updated, including the
  `delete_branch_on_merge` hazard and why sync uses a temporary branch. Check: actionlint.
- [ ] T4 — `.github/workflows/dependency-review.yml` (Decision 7); `.github/settings.yml` comment
  on the security settings enabled live 2026-09-27. Check: actionlint, YAML parse.
- [ ] T5 — Stacked PR against `ci/native-pipeline`, with CI green. Do not merge.

## Limits

As in #280, this cannot run end to end before the release GitHub App exists (`RELEASE_APP_ID` and
`RELEASE_APP_PRIVATE_KEY` provisioned -- see native-ci-pipeline.md's Decision 3 supersession) and a
real hotfix PR is opened. The logic is proven with unit tests and a dry run.

## Progress

- Branch `ci/hotfix-release-and-charts` from `ci/native-pipeline` @ `1aff547`.
- T1 done, strict TDD. Wrote `tools/release/version_test.go` (`TestNextVersionPatchOnly_*`),
  `tools/release/main_test.go` (`TestRun_NextVersion_PatchOnly_*`), and
  `tools/release/changelog_test.go` (`TestErrEmptyUnreleased_MessageIsActionable`) against a
  not-yet-existing `NextVersionPatchOnly`/`ErrPatchOnlyDisallowedCommit` first; `go test
  ./tools/release/...` failed to build (RED): `tools/release/version_test.go:283:16: undefined:
  NextVersionPatchOnly` (and 5 more `undefined:` errors) before any implementation existed.
  Implemented `NextVersionPatchOnly` in `version.go` (extracted `parseLastTag` out of `NextVersion`
  so both share the same `-last` validation; added `commitSubject` to name offending commits): it
  always bumps `patch` on `lastTag` when at least one valid, non-ignored commit is present and none
  of them are breaking/`feat`; returns `ErrPatchOnlyDisallowedCommit` (wrapped with every offending
  commit's subject, `; `-joined) if any is; returns `ErrNothingReleasable` if none of the commits
  are valid at all -- exactly `main.go`'s existing exit-3 mapping, unchanged. Wired `-patch-only`
  into `runNextVersion` in `main.go`. Reworded `ErrEmptyUnreleased` in `changelog.go` to add
  `-- add an entry under "## [Unreleased]" describing the fix, then run this again` (a hotfix
  branch's `[Unreleased]` is always empty right after a release, so this is the first thing a
  hotfix author sees). All GREEN: `go test ./tools/release/...` -- every existing subtest still
  passes plus 4 new `TestNextVersionPatchOnly_*` groups (18 subtests), 3 new `TestRun_*` CLI cases,
  and 1 new changelog message test. `go vet ./tools/release/...`, `go build
  ./tools/release/...`, `make lint` all clean.

  Dry run: `go run ./tools/release next-version -last v0.1.2 -commits <fix-only> -patch-only` ->
  `v0.1.3`, exit 0. `go run ./tools/release next-version -last v0.1.2 -commits <fix+feat>
  -patch-only` -> `release next-version: release: hotfix commits must not contain a breaking
  change or a feat: feat: add something`, exit 1. Real-history dry run skipped as not meaningful:
  `git log v0.1.2..origin/main` is empty (`origin/main`'s tip is the `v0.1.2` tag itself, no
  hotfix has ever landed there), matching this feature doc's own Limits section.
- T2 done. Rewrote `.github/workflows/release-prep.yml`: job `if` now accepts
  `head.ref == 'develop'` OR `startsWith(head.ref, 'hotfix/')` (same-repo check kept unchanged); a
  new `Determine release kind` step (no checkout needed) sets `is_hotfix` from a job-level
  `HEAD_REF` env var (never `${{ github.event.pull_request.head.ref }}` interpolated directly into
  a `run:` script -- a branch name is attacker-controlled, and GitHub Actions pastes an expression
  into the script's source text before bash runs it, so a branch cleverly named with shell
  metacharacters would execute as code; `$HEAD_REF` expanding inside a quoted shell string carries
  no such risk). Checkout's `ref:` now follows the PR's own head (`develop` or the hotfix branch)
  instead of the hardcoded `develop` -- develop's behavior is unchanged since its head literally is
  `develop`. `Compute last tag and next version` adds `-patch-only` when `is_hotfix`. The push at
  the end targets `"HEAD:${HEAD_REF}"` (the env var, not a literal `develop`) so a hotfix's prepare
  commit lands on the hotfix branch. Job summary now also names which kind of release it was.

  Rewrote `.github/workflows/release.yml`: job `if` accepts a merged PR from `develop` or
  `hotfix/*` (same-repo check kept); the same `Determine release kind`/`HEAD_REF` pattern as
  release-prep.yml; a new `Verify the release GitHub App is configured` step gated
  `is_hotfix == 'true'` only (develop's publish still uses `GITHUB_TOKEN` exactly as before, so it
  needs no new secret), followed by a `Mint a release GitHub App installation token` step (same
  `actions/create-github-app-token` shape as release-prep.yml), also hotfix-only;
  `Determine the version to release`'s cross-check adds `-patch-only` for a hotfix. Everything
  from `Skip if already tagged` through `Verify external installability` is unchanged for both
  paths. Two new steps at the end, both gated
  `is_hotfix == 'true' && already_tagged != 'true'`, fold what `hotfix-sync.yml` used to do
  (Decision 6, chosen over a `workflow_run`-triggered separate workflow because it guarantees the
  sync runs strictly after the tag/publish steps *by construction* -- later in the same job --
  instead of by a second trigger racing this one or re-deriving "was this a hotfix merge" from
  `github.event.workflow_run.pull_requests`): `Push the hotfix sync branch` pushes the just-tagged
  commit to `sync/hotfix-${VERSION}` (never `main` itself -- Decision 5: `.github/settings.yml`'s
  `delete_branch_on_merge: true` plus `main` being unprotected and non-default means a PR headed
  `main` would get `main` deleted by GitHub on merge; the branch name comes from the
  tool-validated `vX.Y.Z` version, never the attacker-controlled PR head ref, so it needs no extra
  sanitizing), authenticated as the release App's installation token via an explicit URL with
  `-c http.https://github.com/.extraheader=` clearing the `GITHUB_TOKEN` header `actions/checkout`
  persisted (confirmed this matters: without the override, git applies the persisted header by URL
  prefix regardless of the literal push URL, silently pushing as `GITHUB_TOKEN` instead). `Open or
  reuse the hotfix sync PR` mirrors the old `hotfix-sync.yml` `gh pr list`/`gh pr create` logic
  against `sync/hotfix-${VERSION}` -> `develop`, with the PR body naming the possible
  `CHANGELOG.md` conflict and its resolution (Decision 4) plus why the head branch isn't `main`.
  Deleted `.github/workflows/hotfix-sync.yml` -- its logic now lives at the end of `release.yml`'s
  hotfix path, so keeping it as a second workflow reacting to the same `pull_request: closed` event
  would just be dead/duplicate code.

  *(2026-09-27, rebase onto develop: the token mechanism above was written against the original
  `RELEASE_TOKEN` fine-grained PAT design. `develop` had since superseded that PAT with the release
  GitHub App (native-ci-pipeline.md's Decision 3 supersession); rebasing this branch carried that
  App-based token forward into these same two steps rather than reintroducing the PAT, so the
  mechanism described above now reads `RELEASE_APP_ID`/`RELEASE_APP_PRIVATE_KEY` and mints a
  short-lived installation token exactly like release-prep.yml, instead of reading a
  `RELEASE_TOKEN` secret directly.)*

  Verified: `go run github.com/rhysd/actionlint/cmd/actionlint@latest` (both changed files, then
  the whole repo) -- 0 findings. `python3 -c 'import yaml; yaml.safe_load(open(f))'` on both files
  -- parses clean. Every `run:` block in both files additionally checked with `bash -n` (extracted
  via a small PyYAML script) -- no syntax errors. One real YAML bug caught this way before
  actionlint even ran: the multi-paragraph `gh pr create --body` heredoc's continuation lines
  started at column 0, which is *below* the enclosing `run: |` block scalar's indentation, so YAML
  read it as ending the block scalar early (`could not find expected ':'`) -- fixed by indenting
  those lines to the block's own indentation.
