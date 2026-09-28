# CI/CD pipeline guide

This is a from-scratch guide to how this repository's GitHub Actions pipeline works, written so it
can also serve as a starting point for another repository (see "Porting to another repository" at
the end). It assumes no prior context beyond CONTRIBUTING.md's Branching Model.

## Goals

- **The pull request is the only CI/CodeQL gate.** A commit reaching `develop` or `main` has
  already been validated once, by the pull request (or merge-queue entry) that put it there; no
  workflow re-runs the same check against the same tree afterward just because a `push` event also
  fired. **One exception:** `release-prep.yml`'s `chore(release): prepare vX.Y.Z` commit is pushed
  straight onto `develop` by the release GitHub App (a ruleset bypass actor), so it lands there
  before `ci.yml` has run on it; the release PR's `pull_request: synchronize` run validates it
  afterward, and `ci-ok` must pass on it before it can reach `main`. The exception is kept narrow on
  purpose: `release-prep.yml` refuses to push unless that commit changes `CHANGELOG.md` and nothing
  else, on top of a `develop` tip a pull request already validated, and no build, test, lint, or
  GoReleaser step reads `CHANGELOG.md`. See `docs/investigations/odd-tasks/pr-gated-pipeline.md`
  (Decision 5) for why, and for the event-to-workflow matrix.
- **One required status check name.** A branch ruleset names `ci-ok`, never an individual job, so
  adding, renaming, or splitting a job inside `ci.yml` never also means editing the ruleset.
- **Every third-party action is pinned to a commit SHA**, not a mutable tag, so a re-pointed tag
  cannot silently start running inside a job that reads a secret (the release GitHub App's
  installation token, code-scanning's `security-events: write`).
- **Releasing is a pull request, not a command.** There is no `workflow_dispatch` release path;
  merging `develop` into `main` (or a `hotfix/*` branch into `main`) is the release.

## Workflow inventory

| File | Trigger(s) | Job(s) | Purpose |
|---|---|---|---|
| `ci.yml` | `pull_request` (`develop`, `main`), `merge_group`, `workflow_dispatch` | `verify`, `unit`, `race`, `bench-smoke`, `report-cli`, `lint`, `govulncheck`, `goreleaser`, `dependency-review` (PR-only), `ci-ok` | The gate. Static checks, the full test suite, the race detector, a smoke-run of every benchmark, a real run of the documented `go-specs-report` CLI flow, lint, `govulncheck`, a GoReleaser dry run, and a dependency-advisory diff — each its own job, each its own line in the PR's status list, aggregated by `ci-ok`. |
| `codeql.yml` | `pull_request` (`develop`, `main`), `merge_group`, `push` (`develop` only), weekly `schedule`, `workflow_dispatch` | `analyze` (matrix: `go`, `actions`) | Static security/quality analysis, two legs: `go` scans this repository's Go source, `actions` scans the workflow YAML itself for the script-injection class of problem. `push: [develop]` maintains the default-branch alert baseline GitHub's PR alert-diffing needs; it is a deliberate exception to "PR is the only gate" (see `pr-gated-pipeline.md` Decision 4). |
| `benchmarks.yml` | `push` (`develop` only), `workflow_dispatch` | `ratio-guard` | A post-merge trend signal (Runner/Describe cost vs. a hand-written no-framework loop, same process, same run), not a merge gate — nothing requires it to pass before a PR merges. |
| `benchmark-charts.yml` | `push` (`develop` only, `paths-ignore` on its own chart output), `workflow_dispatch` | `bench` → `chart` → `publish` | Regenerates `benchmarks/results/*.png` and rolls the result into a single reused pull request (`chore/benchmark-charts` → `develop`) instead of committing directly — `main`/`develop` only change through pull requests. Three jobs so the release App's installation token exists only in `publish`, the one job that pushes anything. |
| `release-prep.yml` | `pull_request` (`opened`, `synchronize`, `reopened`, `ready_for_review`; `main` only) | `prepare` | While a `develop` → `main` or `hotfix/*` → `main` PR is open, computes the next version from Conventional Commits and rewrites `CHANGELOG.md`'s `[Unreleased]` section into a dated release heading, pushed back onto the PR's own head branch — so the version and changelog are visible in the PR before it merges. For a release PR that head is `develop` itself, which makes this push the one exception to the PR-only gate; the push is refused unless the commit changes only `CHANGELOG.md` (see Goals). |
| `release.yml` | `pull_request` (`closed`; `main` only) | `release`, `hotfix-sync` (`needs: release`, hotfix-only) | On a genuine merge, re-derives the version, cross-checks it against what `release-prep.yml` wrote, tags the merge commit, runs GoReleaser, and verifies the published tag is externally installable. For a hotfix, `hotfix-sync` additionally pushes a disposable `sync/hotfix-vX.Y.Z` branch and opens (or reuses) a PR from it into `develop`. |

## Branch model

- `develop` is the default branch. Every feature, fix, refactor, and doc change targets it.
- `main` holds releases and hotfixes only, and only moves through a pull request: `develop` → `main`
  for an ordinary release, or `hotfix/*` → `main` for a hotfix. A hotfix is synced back down into
  `develop` automatically by `release.yml`'s `hotfix-sync` job.
- Never open a feature PR against `main`.

Full detail, including the version-bump rules and the changelog-conflict caveat on a hotfix's sync
PR, is in CONTRIBUTING.md's Branching Model and Releasing sections.

## Required checks and ruleset settings

Two GitHub rulesets, one per protected branch:

- **`protect-develop`**: pull request required to merge; 0 required approvals (single-maintainer
  repository); merge and squash allowed (not rebase, so history stays legible per commit); "require
  branches to be up to date before merging" (strict) — an out-of-date PR must be updated before it
  can merge, which re-triggers an ordinary `pull_request: synchronize` run rather than needing a
  merge queue.
- **`protect-main`**: pull request required, **merge only** — squash is disallowed on this branch
  specifically, because CONTRIBUTING.md requires a merge commit (not a squash) when syncing
  `develop` into `main`: a squash creates a commit on `main` that does not exist on `develop`, and
  the branches diverge again the moment the release lands.
- **Both rulesets**: no branch deletion, no force push.
- **Bypass actor**: the release GitHub App only, on both rulesets — never a human, including the
  maintainer. This is what lets `release-prep.yml` push its prepare commit and `hotfix-sync` push
  its sync branch directly, without opening a hole a human could also use to push straight to
  `develop`/`main`.

Required status checks:

- **`ci-ok`** — the single name `ci.yml`'s own `ci-ok` job reports; see Goals above for why nothing
  else in that file is named individually.
- **`analyze (go)`** and **`analyze (actions)`** — the two legs of `codeql.yml`'s `analyze` matrix;
  GitHub Actions names a matrix job `<job id> (<matrix value>)` automatically, so these are the
  literal required-check names, not a description.

## Release/hotfix flow

**Ordinary release:**

1. Open a `develop` → `main` PR. `release-prep.yml` runs immediately (and again on every later push
   to `develop` while the PR stays open), computing the next version and rewriting
   `CHANGELOG.md`.
2. Review the prepared commit — version and changelog are both visible in the PR diff.
3. Merge with a merge commit, not a squash.
4. `release.yml`'s `release` job runs automatically on the `closed` event, re-derives the version,
   cross-checks it against what was prepared, tags the merge commit, and runs GoReleaser. `main` and
   `develop` end up identical, so no sync PR is needed.

**Hotfix:**

1. Branch from `main`, open a PR back into `main` (never through `develop`).
2. `release-prep.yml` prepares the version with `-patch-only` — it fails if the commit range
   contains a breaking change or a `feat`, since a hotfix must not change the API.
3. On merge, `release.yml`'s `release` job tags and publishes exactly like an ordinary release, and
   `hotfix-sync` (gated `needs: release`, hotfix-only) pushes a disposable `sync/hotfix-vX.Y.Z`
   branch and opens a PR from it into `develop`, completing the "merged down into `develop`" step
   CONTRIBUTING.md's Branching Model requires.

Full step-by-step detail, including the exact version-bump table and the `CHANGELOG.md` conflict a
hotfix sync PR can carry, lives in CONTRIBUTING.md's Releasing section — this guide only covers the
pipeline mechanics, not the contributor-facing walkthrough.

## Secrets

- `RELEASE_APP_ID` / `RELEASE_APP_PRIVATE_KEY` — the release GitHub App's ID and private key.
  `release-prep.yml`, `release.yml`'s `hotfix-sync` job, and `benchmark-charts.yml`'s `publish` job
  each mint a short-lived installation token from these at the start of the one job that needs to
  push or open a PR, rather than reading a long-lived credential. A `GITHUB_TOKEN`-authored push or
  PR does not retrigger required checks, which is why the App exists at all instead of relying on
  the workflow's own default token.
- No other secret is used by any workflow in this repository. `GITHUB_TOKEN` (ephemeral, scoped by
  each job's `permissions:` block) covers everything else, including `release.yml`'s own tag push,
  which stays attributed to `github-actions[bot]` rather than the App.

## Repository settings

Declared as code in `.github/settings.yml` (applied by the Probot Settings App — see that file's
own header for what happens if the app is not installed):

- `default_branch: develop`.
- `delete_branch_on_merge: true` — every disposable branch this pipeline creates
  (`chore/benchmark-charts`, `sync/hotfix-vX.Y.Z`) is meant to be deleted once its PR merges.
- `allow_squash_merge`, `allow_merge_commit`, and `allow_rebase_merge` all stay available at the
  repository level; the per-branch ruleset (above) is what actually restricts `main` to merge-only.
- `enable_vulnerability_alerts` and `enable_automated_security_fixes` (Dependabot alerts and
  security updates) — enabled directly under `repository:`, since `probot/settings`' documented
  schema exposes these two as plain booleans there.
- Secret scanning and push protection are **not** declared in `settings.yml`: GitHub exposes them
  through the `security_and_analysis` object, which `probot/settings`' documented schema does not
  cover. They are enabled directly in Settings → Code security and analysis instead, and
  `settings.yml` documents the omission rather than guessing at an unverified key.

## SHA-pin policy

Every `uses: owner/repo@...` in `.github/workflows` and `.github/actions` is pinned to a 40-character
commit SHA with a trailing `# vX.Y.Z` comment, never a bare `@vN` tag — a re-pointed mutable tag
could otherwise start running inside a job that mints the release App's installation token or reads
code-scanning secrets. Each SHA is resolved with:

```
git ls-remote --tags https://github.com/<owner>/<repo> 'refs/tags/vN*'
```

taken to the action's most specific current release tag within the major already in use, with the
peeled `^{}` commit used for an annotated tag. `.github/dependabot.yml`'s `github-actions` ecosystem
keeps these pins current: Dependabot resolves a pinned action's new SHA itself and opens a PR that
bumps both the SHA and its version comment together, so a pin is never stale just because it is
never a moving tag.

Verify the whole repository has no unpinned action left with:

```
rg -n 'uses: [^.].*@v[0-9]' .github
```

which must return nothing.

## Porting to another repository

This pipeline is largely repository-agnostic, but not entirely. Before reusing it elsewhere, change:

- **The module path check** (`ci.yml`'s `verify` job, "Verify module path matches repository"):
  hardcodes the assumption that `go list -m` should equal `github.com/${GITHUB_REPOSITORY}`. Correct
  as a general Go-module check, but confirm the target repository is also hosted at the path its
  `go.mod` declares.
- **The Go version pin file**: this pipeline reads `.go-version` (via `actions/setup-go`'s
  `go-version-file` input and `.github/scripts/check-go-version.sh`'s drift check against `go.mod`).
  A repository without that file, or one on a different toolchain-pin convention, needs either that
  file added or every `go-version-file: .go-version` reference changed.
  `.github/actions/setup-go/action.yml` is the one place the pin is actually read from.
- **`ci.yml`'s `report-cli` job is go-specs-specific.** It runs this repository's own documented
  `go-specs-report` CLI flow against two fixture packages
  (`report/coordination/internal/e2efixture/producera`/`producerb`) that only exist here. Drop this
  job entirely in a repository that does not vendor `go-specs-report`.
- **`benchmark-charts.yml` and `benchmarks.yml` are optional.** They assume `benchmarks/results/*.png`
  and a `make bench-ratio-guard`/`make bench-smoke` target exist. A repository with no
  benchmark suite (or no interest in tracking one over time) can drop both files and `ci.yml`'s
  `bench-smoke` job without affecting anything else in this pipeline.
- **The release App and its two rulesets are the one piece every port must set up by hand** — a
  GitHub App is scoped per-installation, so a new repository needs its own App (or its own
  installation of a shared one), its own `RELEASE_APP_ID`/`RELEASE_APP_PRIVATE_KEY` secrets, and its
  own `protect-develop`/`protect-main` rulesets naming that App as the sole bypass actor. None of
  this is expressible in a workflow file.
