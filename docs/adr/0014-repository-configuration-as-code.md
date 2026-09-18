# ADR-0014: Repository Configuration as Code, Branch Protection Deliberately Excluded

## Status

Accepted.

## Date

2026-09-18

## Deciders

go-specs maintainers, via #164.

## Context

Repository settings — default branch, merge methods, branch deletion on merge — are configuration
that governs how work lands, and they lived only in the GitHub web UI. A change there has no diff, no
review, no history and no author. "Who turned that off, and why?" has no answer.

Declaring them in `.github/settings.yml` makes them reviewable like anything else, and the Probot
Settings app applies them where it is installed.

The same file format can declare **branch protection**, and that is where care is needed. Protection
rules decide what can merge and who can approve. Adding them to a configuration file means that
installing the Settings app — a tooling change — would create or replace protection rules as a side
effect. The rules would take effect because an app was installed, not because anyone decided they
should.

## Decision

Repository configuration is declared in `.github/settings.yml` and versioned.

Declared:

- repository name, description and visibility,
- `default_branch: develop`,
- all three merge methods enabled,
- `delete_branch_on_merge: true`,
- `allow_auto_merge: false`.

**Branch protection rules are deliberately not declared.** That omission is a decision, not an
oversight, and this record is where it is written down.

Two of these are load-bearing rather than cosmetic:

- **All three merge methods stay enabled** because the release flow requires a real merge commit when
  syncing `develop` into `main`; disabling merge commits would break
  [ADR-0013](0013-two-branch-model-manual-releases.md).
- **`allow_auto_merge: false`** because auto-merge lands a PR the moment its checks pass, and the
  checks that pass today do not cover lint or vulnerabilities — Shipwright is disabled
  ([ADR-0016](0016-native-ci-single-toolchain.md)). Auto-merge would treat an incomplete gate as a
  complete one.

The file is **inert without the app**. Where the Settings app is not installed, it documents intent
and nothing more, which is stated here so nobody assumes the settings are enforced.

## Decision classification

### Accepted now

- The declared settings above, and the deliberate exclusion of branch protection.
- The file's status as documentation-plus-automation, not as an enforcement guarantee.

### Deferred

- Declaring branch protection. Worth doing once the rules themselves are decided on their own merits
  — required checks, required reviews, linear history — rather than inherited from a template. Also
  worth doing *after* the CI gates are complete, so the required-checks list is not a list of the
  checks that happen to be green.
- Declaring labels, milestones and issue templates in the same file.

### Open hypotheses

- That the Settings app is the right applier. It is the conventional one; a GitHub Actions workflow
  calling the REST API would keep the same declarative property without a third-party app
  installation, at the cost of writing and maintaining it.

## Consequences

**Benefits:** a settings change is a reviewable diff with an author and a reason; the configuration
survives a maintainer change; the reasoning behind non-obvious choices — three merge methods,
auto-merge off — lives next to the setting instead of in someone's memory.

**Costs:** a file that is inert without the app is a file that can quietly drift from the live
settings, and nothing detects that drift. A reader may reasonably assume declaration means
enforcement; this record exists partly to prevent that. And excluding branch protection means the
base-branch rule from [ADR-0013](0013-two-branch-model-manual-releases.md) is enforced by convention
and review, not by the platform.

## Rejected / deferred alternatives

- **Leave settings in the UI** — rejected. No diff, no review, no history, no author.
- **Declare branch protection along with everything else** — rejected for now. It would make an app
  installation the cause of a governance change, and it would freeze a required-checks list that is
  currently incomplete.
- **Disable squash and rebase merging to enforce merge commits** — rejected. It would enforce
  [ADR-0013](0013-two-branch-model-manual-releases.md)'s sync rule mechanically, but it would also
  forbid squashing a noisy feature branch into `develop`, which is normal and useful.
- **Enable auto-merge** — rejected while the CI gate set is incomplete.

## Related work

- #164 — the implementing change.
- [ADR-0013](0013-two-branch-model-manual-releases.md) — why all three merge methods are needed.
- [ADR-0016](0016-native-ci-single-toolchain.md) — the incomplete gate set behind `allow_auto_merge: false`.
- [10 · Workflow](../10-WORKFLOW.md#7-repository-configuration).
