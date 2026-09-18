# ADR-0013: Two Long-Lived Branches, Merge-Not-Squash, Manual Releases

## Status

Accepted.

## Date

2026-09-15 (release gating, #127) and 2026-09-18 (branching model written down, #162).

## Deciders

go-specs maintainers, via #127 and #162.

## Context

A published Go module cannot unpublish a tag. The module proxy caches it, and `go get` will serve it
to anyone who asks. A release is therefore the one action in this repository that cannot be undone,
which makes automatic releasing the wrong default: a merge that looked routine becomes a permanent
artifact.

The release workflow did once run on every push to `main`, so merging anything there published.

Separately, the repository ran a two-branch model that existed only in people's heads. `CONTRIBUTING`
had no branching section at all, so a contributor opening a PR had nothing to read and the default
base was the only signal. And a squash-merge from `develop` into `main` creates a commit on `main`
that does not exist on `develop` — so the branches diverge again the moment a release lands, which
had already happened and required a manual reconciliation.

## Decision

### Two long-lived branches

| Branch | What lands there | How |
| --- | --- | --- |
| `develop` (default) | every feature, fix, refactor and doc change | PR targeting `develop`; opening a PR without choosing a base already points here |
| `main` | releases and hotfixes only | PR from `develop` when a release is due; a hotfix branches from `main`, PRs back into it, then merges down into `develop` |

Never open a feature PR against `main`. `main` exists to hold the commit a release is cut from; a
feature landing there directly is invisible to `develop` until someone notices.

### Merge, do not squash, when syncing for a release

A squash creates a `main` commit with no counterpart on `develop`, and the branches diverge again
immediately. This is why all three merge methods stay enabled in
[ADR-0014](0014-repository-configuration-as-code.md) — the release flow needs a real merge commit.

### Releases are manual

`release.yml` runs on `workflow_dispatch` **only**. A push or merge to `main` does not publish.

```bash
gh workflow run release.yml --ref main
```

It checks the toolchain pin, skips if `HEAD` is already tagged, auto-bumps the patch version or
accepts an explicit strict-SemVer `vX.Y.Z`, pushes the tag, runs GoReleaser, and then performs a
black-box `go get <module>@<tag>` from a scratch module — proving the released version is actually
installable by a stranger, which is the bug class #139 shipped.

## Decision classification

### Accepted now

- The two-branch model, the merge-not-squash rule, and `workflow_dispatch`-only releases.
- The post-release installability smoke test as part of the release, not as a follow-up check.

### Deferred

- Branch protection enforcing the base-branch rule. Deliberately not declared — see
  [ADR-0014](0014-repository-configuration-as-code.md).
- Release automation on a schedule or on a tag push. Both reintroduce the "merge publishes"
  property this record removes.

### Open hypotheses

- That two long-lived branches earn their cost. For a library with infrequent releases, trunk-based
  development with release tags is a real alternative. The reason `main` exists is to have a stable
  ref for hotfixes that does not carry in-flight `develop` work.

## Consequences

**Benefits:** publishing is always a deliberate act by a human; `main` always points at something
released; a hotfix has a clean base; the smoke test means an unusable release fails the workflow
rather than a user's `go get`; the model is written down, so a contributor can read it instead of
guessing.

**Costs:** two branches to keep in step, and the reconciliation work when they drift — which has
happened. Manual releases mean a release can be forgotten, and the merge-not-squash rule means
`main`'s history carries every `develop` commit rather than one commit per release, which some
readers will find noisier.

## Rejected / deferred alternatives

- **Release on push to `main`** — rejected by #127. It makes every merge a publication, and
  publication is irreversible.
- **Squash when merging `develop` into `main`** — rejected. It is the mechanism that made the
  branches diverge, and the divergence had to be repaired by hand.
- **Single branch with release tags** — deferred. Simpler, and it removes the sync problem entirely;
  it also removes the stable hotfix base, which is the reason `main` exists.
- **Publish from a tag push instead of a dispatch** — rejected. A tag push is easy to do by accident
  and just as irreversible.

## Related work

- #127 — `workflow_dispatch`-only publishing.
- #162 — the branching model written into `CONTRIBUTING.md`.
- #139 — the module-path bug that motivated the post-release smoke test.
- [ADR-0014](0014-repository-configuration-as-code.md) — why all three merge methods stay enabled.
- [ADR-0015](0015-library-only-release-artifact.md) — what the release actually ships.
