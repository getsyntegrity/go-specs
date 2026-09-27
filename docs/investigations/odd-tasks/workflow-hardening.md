# workflow-hardening — split and harden every GitHub Actions workflow

Follow-up to `hotfix-release-and-charts.md` (PR #281), stacked on `ci/hotfix-release-and-charts`.

## Problem

`ci.yml` already runs one visible job per concern, all in parallel. The other workflows do not,
and an audit of `.github/` (2026-09-27) found gaps across all of them:

- No job sets `timeout-minutes`, so a hang runs for GitHub's 6-hour default.
- Every action is pinned by a mutable major tag (`@v7`). A re-pointed tag would run inside jobs that
  can read the release App's private key.
- No checkout sets `persist-credentials: false`, so the job token stays in `.git/config` for any
  later tool to read.
- `release.yml` has no workflow-level `permissions:` block, unlike every sibling file.
- `codeql.yml` and `dependency-review.yml` have no `concurrency`.

The audit also confirmed a strength. No `run:` block interpolates an attacker-controlled value:
branch names go through `env:`.

## What changes

1. **Baseline hardening in every workflow.** `timeout-minutes` on every job. `persist-credentials:
   false` on every checkout that does not push. A workflow-level `permissions:` in `release.yml`.
   `concurrency` on `codeql.yml` and `dependency-review.yml`.
2. **CodeQL as a language matrix** `[go, actions]`, two parallel checks. The `actions` leg
   analyzes the workflow files themselves, including the script-injection class.
3. **Explode the two workflows whose single job mixes concerns.** `release.yml` splits into
   `release` (tag, GoReleaser, installability) and `hotfix-sync` (`needs: release`, hotfix only).
   `benchmark-charts.yml` splits into `bench` → `chart` → `publish`, passing results as artifacts.
   Neither split saves wall time, because both chains are inherently sequential. What they buy is
   visibility, plus the release App token living only in the one job that uses it.
4. **Pin every action by commit SHA**, with a trailing `# vX.Y.Z` comment. Dependabot's
   `github-actions` ecosystem, already configured, keeps the SHAs current through PRs.
5. **Open the PR.** Update the ruleset's required checks in lockstep with the new check names:
   the matrix renames `analyze` to `analyze (go)` / `analyze (actions)`.

## Decisions

1. **`release-prep.yml` stays one job.** Each step consumes the previous one's output (token →
   checkout → version → changelog → commit → push). Splitting it adds re-checkouts and a
   stale-checkout risk for zero parallelism.
2. **`report-cli`, `goreleaser`, `dependency-review` and `benchmarks` stay as they are.** Each is
   already one job with one concern, or shares runner state that jobs would have to ship as
   artifacts for no gain.
3. **CodeQL `init`/`build`/`analyze` stay in one job per language.** The CodeQL database must be
   built and analyzed on the same runner.
4. **Pin by SHA plus Dependabot** instead of tags. Rejected: keeping major tags, because a tag
   re-point is exactly how past action compromises spread.
5. **Dependabot and fork PRs** may get a read-only `GITHUB_TOKEN`. That could break CodeQL's SARIF
   upload and dependency-review's failure comment on those PRs. This is unverified here; the first
   real Dependabot PR shows it. Accepted and documented rather than working around it with a
   `workflow_run` relay, which adds complexity and trust surface for a single-maintainer repo.
6. **Optional additions are out of this cut.** They are the go-specs-report test summary,
   coverage, OpenSSF Scorecard and harden-runner, each pending a maintainer choice. Rejected
   outright: path filters for docs-only changes (skipped required checks can block merges),
   merge queue, release-drafter (`tools/release` covers it), and provenance/SBOM (no binaries are
   published).

## Tasks

- [x] T1 — Baseline hardening (timeouts, persist-credentials, permissions, concurrency). Check:
  actionlint, YAML parse.
- [x] T2 — CodeQL language matrix `[go, actions]`. Check: actionlint; both legs green on the PR.
- [x] T3 — Split `release.yml` and `benchmark-charts.yml`. Check: actionlint; job outputs and
  artifacts wired; `if:` conditions preserved.
- [ ] T4 — Pin every `uses:` by SHA with a version comment. Check: every SHA resolves to the tag it
  claims (`git ls-remote`); actionlint.
- [ ] T5 — PR, CI green, ruleset required checks updated. Do not merge.

## Progress

- Branch `ci/workflow-hardening` from `ci/hotfix-release-and-charts` @ `b9b75ce`.
- T1 done. Added `timeout-minutes` to all 14 jobs across the 7 workflow files (10-25 for CI/lint/
  vuln/dependency-review, 20 for race and codeql, 20-30 for release/release-prep, 30 for the two
  benchmark jobs), with the rationale written once per file rather than repeated per job.
  `persist-credentials: false` added to every `actions/checkout` except two, each with a comment
  explaining why: release-prep.yml's checkout (the App-token push depends on it) and release.yml's
  "Checkout merge commit" (the tag push uses plain `git push origin`, which needs the GITHUB_TOKEN
  credential actions/checkout persists by default; the hotfix sync push right after it still
  clears that credential explicitly and authenticates with the App token instead — see the
  T3 note below for why that workaround could not be removed yet at T1). Added workflow-level
  `permissions: contents: read` to release.yml (the `release` job keeps its own `contents:
  write`). Added `concurrency` (group per workflow+ref, cancel-in-progress for pull_request only)
  to codeql.yml and dependency-review.yml, which had none.
  Check: `python3 -c "import yaml,glob; [yaml.safe_load(open(f)) for f in glob.glob('.github/workflows/*.yml')]"` — all 7 files parse.
  `go run github.com/rhysd/actionlint/cmd/actionlint@latest` — 0 findings.
  Commit: `ci: add timeouts, drop persisted credentials and tighten permissions in every workflow`.
- T2 done. `analyze` in codeql.yml is now a `strategy: { fail-fast: false, matrix: { language:
  [go, actions] } }` job. `Set up Go` and the manual `go build ./...` step are gated on
  `matrix.language == 'go'`; `Initialize CodeQL` passes `languages: ${{ matrix.language }}` and
  `build-mode: manual` for go / `none` for actions; `Perform CodeQL analysis` passes `category:
  "/language:${{ matrix.language }}"`. GitHub Actions renders the two matrix jobs as `analyze
  (go)` and `analyze (actions)` automatically, no explicit `name:` needed. Whether both legs
  actually go green is only verifiable once this runs on GitHub (open question for T5, not
  reproducible locally).
  Check: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/codeql.yml'))"` — OK.
  `go run github.com/rhysd/actionlint/cmd/actionlint@latest` — 0 findings.
  Commit: `ci: analyze workflows with CodeQL alongside Go`.
- T3 done.
  - `release.yml`: `release` (Determine release kind -> Checkout merge commit -> Check Go version
    pin -> Set up Go -> Determine the version to release -> Skip if already tagged -> Create and
    push tag -> Extract release notes -> Run GoReleaser -> Verify external installability) now
    exposes outputs `version`, `is_hotfix`, `already_tagged`. New job `hotfix-sync` (`needs:
    release`, `if: needs.release.outputs.is_hotfix == 'true' && needs.release.outputs.already_tagged != 'true'`)
    verifies the App secrets, mints the App token, checks out the just-pushed tag
    (`ref: ${{ needs.release.outputs.version }}`, `fetch-depth: 0`), pushes `sync/hotfix-<version>`
    and opens/reuses the develop PR -- unchanged behavior, moved wholesale out of `release`. The
    App token is no longer minted in `release` at all. `hotfix-sync`'s checkout sets
    `persist-credentials: false` (it has no tag push to protect, unlike `release`'s own checkout),
    which let the `-c http.https://github.com/.extraheader=` workaround be removed from the
    sync-branch push -- its comment now explains why it is no longer needed there. `release` keeps
    `contents: write`; `hotfix-sync` gets `contents: read` (its writes go through the App token).
  - `benchmark-charts.yml`: `bench` (checkout, Go, run the suite, upload `current.txt`) -> `chart`
    (`needs: bench`; download `current.txt`, Python + matplotlib, generate the PNGs, upload them)
    -> `publish` (`needs: chart`; verify App secrets, mint the App token, checkout, download the
    PNGs into `benchmarks/results/`, `peter-evans/create-pull-request`). The App token now exists
    only in `publish`; `bench` and `chart` touch no secret. All three use
    `actions/upload-artifact@v4`/`actions/download-artifact@v4` with `retention-days: 1`. The
    `paths-ignore` loop guard and the workflow-level `concurrency` block are unchanged, both still
    apply to the whole workflow (all three jobs).
  - No `requirements.txt` exists for `pip install matplotlib` (checked with `fd -HI 'requirements*.txt'`),
    so `chart`'s `actions/setup-python` step has no dependency file to key a pip cache on --
    skipped rather than added without one, per T3's "if simple" qualifier.
  Check: `python3 -c "import yaml; yaml.safe_load(open(f))"` on both files -- OK.
  `go run github.com/rhysd/actionlint/cmd/actionlint@latest` -- 0 findings.
  Commit: `ci: split release and benchmark chart workflows into single-purpose jobs`.
