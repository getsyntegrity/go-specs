<!--
Thanks for the PR! Before submitting it:

1. It targets `develop`. Only `develop` itself (a release) and `hotfix/*` branches target `main`.
2. Set ONE `kind/*` label: it tells reviewers what kind of change this is.
3. Add or run the relevant tests (coverage shows up in the summary of the CI run).
4. Add an entry under `## [Unreleased]` in CHANGELOG.md, or apply the `skip-changelog` label if the
   change is invisible to people who use go-specs. The pr-meta check fails otherwise.
5. If it is not finished yet, open it as a Draft.
-->

#### What kind of PR is this?

<!--
Label (one):
kind/feature · kind/bug · kind/breaking · kind/deprecation · kind/deps · kind/chore · kind/docs
-->

#### What does this PR do and why is it needed?

#### Which issue(s) does it resolve?

<!--
"Fixes #123" closes the issue on merge. Do not use "Fixes" in kind/flake PRs.
If there is no issue, write N/A.
-->

#### Impact on the public API

<!-- The `api` CI job checks this with apidiff; this is so the reviewer knows up front. -->

- [ ] Does not change the public API
- [ ] Adds compatible API (new functions, types or fields)
- [ ] **Breaks compatibility** → label `kind/breaking`, and say so in the CHANGELOG.md entry (go-specs is pre-1.0)
- [ ] Deprecates API → `// Deprecated:` in the godoc, with the alternative

#### CHANGELOG.md

<!--
The entry under `## [Unreleased]` becomes the GitHub Release notes. Write it for whoever uses the
library. Put each entry under the right heading (Added, Changed, Deprecated, Removed, Fixed,
Security); `go run ./tools/release validate -file CHANGELOG.md` checks the structure.
-->

- [ ] Entry added under `## [Unreleased]`
- [ ] Not needed: `skip-changelog` label applied

#### Special notes for the reviewer

#### AI usage

<!--
YES or NO. If YES, briefly describe how it was used.
Whoever opens the PR is responsible for all the code they submit.
-->
