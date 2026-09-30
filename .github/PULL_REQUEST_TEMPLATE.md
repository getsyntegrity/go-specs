<!--
Thanks for the PR! Before submitting it:

1. It targets `develop`. Only `develop` itself (a release) and `hotfix/*` branches target `main`.
2. Set ONE `kind/*` label: it tells reviewers what kind of change this is, and it decides which
   section of the release notes the PR appears in. The pr-meta check fails otherwise.
3. Add or run the relevant tests (coverage shows up in the summary of the CI run).
4. Do not edit CHANGELOG/: it is generated at release time from the merged PRs. Write the note in
   the `release-note` block below if the title is not enough.
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
- [ ] **Breaks compatibility** → label `kind/breaking`, and say so in the release note (go-specs is pre-1.0)
- [ ] Deprecates API → `// Deprecated:` in the godoc, with the alternative

#### Special notes for the reviewer

#### Release note (optional)

<!--
Optional. Without a block, the PR title (minus its `feat:`/`fix:` prefix) is the note.
Write the note as it should appear in the release notes, for whoever uses the library
("Adds the `WithTimeout` option.", not "fix timeout"). The `kind/*` label decides its section.
If people must do something when upgrading, include "action required": the note is also listed
under "Urgent Upgrade Notes" (`kind/breaking` does the same).
Write NONE, or apply the `skip-changelog` label, for a change nobody who uses go-specs can see.
-->

```release-note

```

#### AI usage

<!--
YES or NO. If YES, briefly describe how it was used.
Whoever opens the PR is responsible for all the code they submit.
-->
