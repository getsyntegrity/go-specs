# pr-gated-pipeline — make the pull request the only CI/CodeQL gate

Follow-up to `workflow-hardening.md` (PR #282), branched from its head (`ci/hotfix-release-and-charts`
after the rebase onto `develop`). Implements the maintainer's 2026-09-27 decision, "option A: the PR
is the only gate."

## Problem

Every commit that reaches `develop` or `main` today runs CI and CodeQL **twice** for the same tree,
because `ci.yml` and `codeql.yml` both trigger on `pull_request` *and* `push` to the same branches:

- A feature PR against `develop` runs `ci.yml`/`codeql.yml` once as its `pull_request` check. The
  moment it merges, GitHub fires a `push` to `develop`, and the exact commit that PR already
  validated runs `ci.yml`/`codeql.yml` a second time — with nothing left to catch, since nothing
  changed between the two runs.
- `v0.2.0`'s own release-prep commit (`a23f2ac`, `chore(release): prepare v0.2.0`) shows this
  concretely: `release-prep.yml` pushed that commit directly onto the develop → main release PR's
  head branch (which *is* `develop` for an ordinary release). That single push produced a `push`
  run on `develop` and a `pull_request: synchronize` run on the release PR, for the same commit —
  `ci.yml` ran twice, `codeql.yml` ran twice.
- A develop → main or hotfix/* → main merge fires a `push` to `main`. Before this change, that
  re-ran `ci.yml` and `codeql.yml` a third time against a tree that was already fully validated on
  `develop` (or the hotfix branch) and is not being modified by the merge itself.

None of these extra runs catch anything a human or a required check hasn't already seen; they only
spend runner minutes and add noise to the commit's check list.

## What changes

1. **`ci.yml` drops its `push` trigger entirely** (`pull_request` on `develop`/`main`, plus
   `merge_group` and `workflow_dispatch`). The pull request — or, once/if a merge queue is enabled,
   the merge-queue entry — is the only place `ci.yml` ever runs. `dependency-review.yml` folds into
   `ci.yml` as a `dependency-review` job (`if: github.event_name == 'pull_request'`), since it was
   already PR-only and a `push`-less `ci.yml` is exactly where the rest of this file's PR-only
   checks live. A new `ci-ok` job aggregates every other job in the file into the one required
   status check a branch ruleset names.
2. **`codeql.yml` keeps a narrow `push`, `develop` only, and drops `push` to `main`.** Unlike
   `ci.yml`, CodeQL's `push` trigger is not pure duplication: GitHub's code-scanning alert diffing
   needs a baseline analysis committed on the default branch to compare a PR's *new* alerts
   against. Losing `push: [develop]` would make every alert on every future PR show up as "new"
   forever. `push: [main]` has no equivalent justification — nothing reads a main-branch CodeQL
   baseline that the develop baseline doesn't already cover for the same tree — so it is dropped.
3. **`benchmarks.yml` (`ratio-guard`) drops `push: [main]`**, keeping `push: [develop]` and
   `workflow_dispatch`. It is a post-merge trend signal, not a merge gate: nothing requires it to
   pass before a PR merges, so it has no reason to re-run on the develop → main / hotfix/* → main
   merge that follows — that merge revalidates nothing new.
4. **`benchmark-charts.yml` is unchanged** (`push: [develop]` only, confirmed — see Decision 3) and
   **`release-prep.yml`/`release.yml` keep their triggers**, confirmed to have no trigger overlap
   with the above beyond the intentional case of a release PR getting both full CI and release prep
   (Decision 4). `release-prep.yml` gains one guard before its push: the prepare commit must change
   only `CHANGELOG.md` (Decision 5).
5. **`docs/CI.md`** is a new, repository-agnostic guide to the whole pipeline: workflow inventory,
   branch model, required checks, ruleset settings, the SHA-pin policy, and a porting checklist —
   linked from `CONTRIBUTING.md`.

## Decisions

1. **No `push` trigger on `ci.yml`, at all (maintainer decision 2026-09-27, "PR is the only
   gate").** Every commit that reaches `develop` or `main` arrives through a pull request merge or
   (once enabled) a merge-queue entry, both of which already run `ci.yml` via `pull_request` or
   `merge_group` — with one exception, release-prep's prepare commit, covered by Decision 5. A
   `push` trigger on either branch would therefore only re-run a tree `pull_request` (or
   `merge_group`) already ran, or, for the prepare commit, one the release PR's
   `pull_request: synchronize` run checks moments later anyway. Rejected: keeping `push` on
   `main` only (dropping it on `develop`) — main's `push` is exactly as redundant as develop's,
   since `release.yml`'s own `pull_request: closed` trigger is what actually gates a merge to
   `main`, and nothing runs `ci.yml`/`codeql.yml` as a merge *requirement* there anyway.
2. **`ci-ok` is the one required status check name (maintainer decision 2026-09-27).** Naming
   individual jobs (`verify`, `unit`, `lint`, ...) as a ruleset's required checks means every future
   job rename, split, or addition is also a ruleset edit, made in the GitHub UI/API, easy to forget
   and impossible to review in a diff. `ci-ok` `needs:` every other job, runs `if: always()` so a
   failed dependency does not skip it (a skipped required check blocks a merge exactly like a
   failed one, with a far less useful status line), and fails only on an actual `failure` or
   `cancelled` result — a `skipped` result is accepted generically, which in practice only ever
   happens for `dependency-review` on a non-PR event (`merge_group`/`workflow_dispatch`), since it
   is the only job in the file with a conditional `if:`. Rejected: `needs:` implicitly enforcing
   success (leaving `ci-ok` with no steps) — GitHub Actions skips a job outright when *any* of its
   `needs:` fails unless it declares its own `if: always()`, so a plain `needs:`-only job would
   itself report `skipped`, which a ruleset treats as blocking, not passing; the explicit `if:
   always()` plus the jq check above is what turns "one dependency failed" into an actual `failure`
   result on `ci-ok` instead of a `skipped` one.
3. **`merge_group` is added to `ci.yml`/`codeql.yml` even though this repository does not have a
   GitHub merge queue enabled today.** `protect-develop`'s "strict" (up-to-date) requirement, as
   specified for this change, comes from the ruleset's own "require branches to be up to date
   before merging" option, which needs no merge queue and fires no `merge_group` event — an
   out-of-date PR simply cannot merge until its author updates it, which re-triggers an ordinary
   `pull_request: synchronize` run. `merge_group` is included anyway so that turning a GitHub merge
   queue on later, if PR volume ever makes one worth the added complexity, needs no workflow edit;
   until then, `merge_group:` with no matching ruleset setting is simply inert. This is flagged as
   an open question at the end of this document rather than decided here, since enabling a merge
   queue is a repository-configuration decision, not a workflow-file one.
4. **CodeQL's remaining `push: [develop]` legitimately runs the same analysis twice around a
   release-prep push, and that is not a duplicate to eliminate.** When `release-prep.yml` pushes a
   `chore(release): prepare vX.Y.Z` commit onto a develop → main release PR's head (which *is*
   `develop`), that single push is simultaneously a push to `develop` (baseline scan) and a new
   commit on the release PR (diff scan) — two runs, but two different purposes on the same tree,
   not the same check running twice for no reason the way `ci.yml`'s old `push`+`pull_request` pair
   did. The event → workflow matrix below calls this out explicitly rather than folding it into the
   "zero duplicates" claim, which is about `ci.yml` (the actual gate), not every workflow in the
   repository.
5. **release-prep's prepare commit is the one commit that reaches `develop` before `ci.yml` runs on
   it; it is kept changelog-only rather than gated (review of PR #291 at `c1b6287`, 2026-09-27).**
   On a develop → main release PR the head branch *is* `develop`, so `release-prep.yml` pushes
   `chore(release): prepare vX.Y.Z` straight onto `develop` through the release App's ruleset
   bypass. With no `push` trigger on `ci.yml`, the first CI run to see that commit is the release
   PR's `pull_request: synchronize` run, after the commit is already on `develop`. This is accepted
   because the commit cannot change anything CI checks: its parent is a `develop` tip that a pull
   request already validated, it changes only `CHANGELOG.md`, and no build, test, lint, or
   GoReleaser step reads that file (`.goreleaser.yaml` builds its changelog from git history, and
   `tools/release` reads the file only in `release-prep.yml`/`release.yml` themselves). The
   release PR still needs `ci-ok` on that commit before it can merge, so nothing reaches `main`
   unvalidated. `release-prep.yml` now enforces the changelog-only condition instead of assuming
   it: after committing, it diffs the prepare commit against its parent and refuses to push, with
   an `::error::`, if any path other than `CHANGELOG.md` changed. Rejected: (a) putting
   `push: [develop]` back on `ci.yml`, filtered to the App's commits — every PR merge would again
   start a `ci.yml` run (all jobs skipped at best), undoing Decision 1 for one commit per release;
   (b) running the full suite inside `release-prep.yml` before the push — duplicates `ci.yml` in a
   job that holds the App's installation token, only for the `synchronize` run to repeat it
   seconds later; (c) pushing the prepare commit somewhere other than `develop` — reverses Decision
   1 of `native-ci-pipeline.md` (version and changelog visible in the release PR itself) and is out
   of scope for this change.

## Event → workflow matrix

"Runs" means the workflow's trigger fires and its job(s) actually execute (not skipped by an `if:`
that always evaluates false for that event).

| Event | `ci.yml` | `codeql.yml` | `benchmarks.yml` (`ratio-guard`) | `benchmark-charts.yml` | `release-prep.yml` | `release.yml` |
|---|---|---|---|---|---|---|
| Feature/Dependabot PR opened or synced (base `develop`) | `pull_request` (incl. `dependency-review`) | `pull_request` | — | — | — | — |
| Merge to `develop` (PR merge → `push`) | — | `push` (baseline) | `push` (trend) | `push` (rolling chart PR) | — | — |
| [if a merge queue is later enabled] entry into `develop`'s merge queue | `merge_group` | `merge_group` | — | — | — | — |
| `develop` → `main` release PR opened or synced | `pull_request` (base `main`) | `pull_request` (base `main`) | — | — | `pull_request` | — |
| release-prep's own prepare-commit push (lands on `develop`, the PR's head) | `pull_request: synchronize` re-run only (no `push` run), after the commit is already on `develop` — Decision 5 | **both**: `push` (baseline) *and* `pull_request: synchronize` (diff) — Decision 4, not a bug | `push` (trend) | `push` (rolling chart PR) | `pull_request: synchronize` re-run | — |
| Merge to `main` (release or hotfix PR merge → `pull_request: closed`, plus a `push`) | — | — | — | — | — | `pull_request: closed` (`release` job, `+hotfix-sync` if hotfix) |
| Hotfix PR opened or synced (base `main`) | `pull_request` | `pull_request` | — | — | `pull_request` | — |
| Hotfix's sync PR opened (`sync/hotfix-*` → `develop`, from `hotfix-sync`) | `pull_request` | `pull_request` | — | — | — | — |
| Weekly schedule | — | `schedule` | — | — | — | — |

Zero duplicate `ci.yml` runs anywhere in this table — the whole point of Decision 1. The one cell
that runs a workflow twice for the same tree (`codeql.yml` on release-prep's own push) is Decision
4's deliberate exception, not an oversight: it is two different CodeQL purposes (baseline vs. diff),
not the same check re-run for nothing.

## Tasks

- [x] T1 — `ci.yml`: drop `push`; add `merge_group`, `workflow_dispatch`; fold in
  `dependency-review` as a job gated `if: github.event_name == 'pull_request'`; delete
  `dependency-review.yml`; add the `ci-ok` aggregator job; re-key `concurrency` on the PR number
  (falling back to `github.ref` for `merge_group`/`workflow_dispatch`). Check: actionlint, YAML
  parse, `rg -n 'uses: [^.].*@v[0-9]' .github` empty.
- [x] T2 — `codeql.yml`: add `merge_group`; narrow `push` to `develop` only (drop `main`), with the
  header explaining why `develop`'s baseline push stays while `main`'s does not. Check: actionlint,
  YAML parse.
- [x] T3 — `benchmarks.yml`: narrow `push` to `develop` only (drop `main`); confirm
  `benchmark-charts.yml`, `release-prep.yml`, and `release.yml` need no trigger change (no diff to
  those three files). Check: actionlint, YAML parse; the event matrix above as the proof of zero
  `ci.yml` duplication.
- [x] T4 — `docs/CI.md`: new transplantable pipeline guide (goals, workflow inventory, branch model,
  required checks and ruleset settings, release/hotfix flow, secrets, repo settings, SHA-pin
  policy, porting checklist); linked from `CONTRIBUTING.md`'s Pull Requests section. Check:
  markdown reads correctly, link resolves to a real file.
- [x] T5 — Full verification pass (see Verification below) and this document's own Progress/event
  matrix kept in sync with what was actually implemented.

## Verification

- `go run github.com/rhysd/actionlint/cmd/actionlint@latest` — 0 findings, whole repo.
- `rg -n 'uses: [^.].*@v[0-9]' .github` — empty (no pin regressed to a bare tag).
- A case-insensitive repo-wide search for the retired container-based CI runner's old name
  (native-ci-pipeline.md's T1) — empty.
- `python3 -c "import yaml; yaml.safe_load(open(f))"` on every changed/added workflow file — parses
  clean.
- `make fmt-check`, `make lint`, `go build ./...`, `go test ./...` — unaffected by a workflow-only
  change, run anyway as the standing checks for this worktree.

## Progress

- Branch `ci/pr-gated-pipeline` from `ci/hotfix-release-and-charts` (rebased onto `develop`) at
  `513b6c6`.
- T1 done. `ci.yml`'s `on:` is now `pull_request: [develop, main]` + `merge_group:` +
  `workflow_dispatch:`, no `push:`. Header comment rewritten to explain the missing `push` trigger
  and the new `ci-ok`/`dependency-review` jobs. `dependency-review` copied in verbatim from the
  deleted `dependency-review.yml`, with `if: github.event_name == 'pull_request'` added (it has no
  PR diff to review on `merge_group`/`workflow_dispatch`) and its own job-scoped
  `pull-requests: write` kept (the workflow-level default stays `contents: read`).
  `dependency-review.yml` deleted. `ci-ok` added: `needs:` all nine other jobs
  (`verify, unit, race, bench-smoke, report-cli, lint, govulncheck, goreleaser,
  dependency-review`), `if: always()`, `timeout-minutes: 5`; its one step reads `toJSON(needs)`
  into `$NEEDS_JSON`, filters with `jq` for any need whose `result` is neither `success` nor
  `skipped`, and fails with `::error::` naming the offending jobs if any are found. `concurrency`
  re-keyed to `${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}` so
  every push to the same PR branch shares one group regardless of ref spelling, falling back to
  `github.ref` for the two non-PR triggers.
- T2 done. `codeql.yml` gained `merge_group:` and `push.branches` narrowed from `[develop, main]`
  to `[develop]`. Header rewritten to explain both the missing `main` push (nothing reads a
  main-only baseline the develop baseline doesn't already cover) and the kept `develop` push
  (GitHub's alert-diffing needs a default-branch baseline to diff a PR's new alerts against).
- T3 done. `benchmarks.yml`'s `ratio-guard` job's `push.branches` narrowed from `[main, develop]`
  to `[develop]`; its own comment rewritten to say this is a post-merge trend signal, not a gate,
  with no reason to re-run on the develop → main / hotfix/* → main merge that follows a develop
  push. `benchmark-charts.yml` read and confirmed already `push: [develop]` only, no change.
  `release-prep.yml` (`pull_request` on `main` only) and `release.yml` (`pull_request: closed` on
  `main` only) read and confirmed to have no `push` trigger and no overlap with the changes above
  beyond the intentional develop → main PR case (Decision 4).
- T4 done. Added `docs/CI.md`; linked from `CONTRIBUTING.md`'s Pull Requests section.
- T5 done. Ran the full Verification list above after T4: `actionlint` — 0 findings (whole repo).
  `rg -n 'uses: [^.].*@v[0-9]' .github` — empty. The retired CI runner's old-name search — empty
  (this document's own first draft of that line accidentally quoted the literal search term inside
  its own description, tripping the check on itself; reworded, twice, to describe the check instead
  of quoting the command it runs — the same class of self-reference `workflow-hardening.md`'s own
  verification note needed fixing for during the Part 1 rebase, and worth remembering: never quote
  this particular search term literally in a document this search term's own check will scan).
  `python3` YAML parse of every workflow file — all OK. `make fmt-check` — exit 0.
  `make lint` — 0 issues. `go build ./...` — exit 0. `go test ./...` — all packages `ok` (no
  failures, no skips beyond the pre-existing packages with no test files).
- Review of PR #291 at `c1b6287` (changes requested). P1: release-prep's prepare commit reaches
  `develop` through the App's bypass before any `ci.yml` run, contradicting `docs/CI.md`'s claim
  that every commit on that branch was validated first. Resolved as a documented, enforced
  exception (Decision 5): `release-prep.yml` refuses to push a prepare commit that changes anything
  but `CHANGELOG.md`; `ci.yml`'s header, `docs/CI.md` (Goals and inventory), `CONTRIBUTING.md`, and
  Decision 1 now state the exception instead of the absolute claim. Minor: the matrix row for
  release-prep's push showed `benchmarks.yml` as "—", but `ratio-guard` keeps `push: [develop]`
  and does run on that push; the cell now reads `push` (trend).
