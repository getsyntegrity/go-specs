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
  merge commit and publishes, then `hotfix-sync.yml` opens the `main` → `develop` sync PR as today.
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

## Tasks

- [x] T1 — `tools/release next-version -patch-only`: bump patch; fail on breaking/`feat` with a
  clear message. Strict TDD. Check: `go test ./tools/release/...`.
- [ ] T2 — `release-prep.yml` and `release.yml` also handle `hotfix/*` → `main` (prepare on the
  hotfix branch; publish on merge); `hotfix-sync.yml` runs after the release and mentions the
  possible CHANGELOG conflict. Check: actionlint, YAML parse, dry run of the tool.
- [ ] T3 — `benchmarks.yml`: charts through a rolling PR into `develop`, with no direct push and
  loop-safe path filtering. CONTRIBUTING.md release and hotfix section updated. Check: actionlint.
- [ ] T4 — Stacked PR against `ci/native-pipeline`, with CI green. Do not merge.

## Limits

As in #280, this cannot run end to end before `RELEASE_TOKEN` exists and a real hotfix PR is
opened. The logic is proven with unit tests and a dry run.

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
