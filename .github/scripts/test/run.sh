#!/usr/bin/env bash
# Tests for the release scripts in .github/scripts (next-version.sh, changelog.sh).
# Each case builds a throwaway git repository, so nothing here touches the real checkout.
# Usage: bash .github/scripts/test/run.sh        (needs git, jq, perl and go)
set -uo pipefail

repo_root=$(cd "$(dirname "$0")/../../.." && pwd)
scripts="$repo_root/.github/scripts"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

failures=0
pass() { echo "ok   - $1"; }
fail() { echo "FAIL - $1"; shift; printf '       %s\n' "$@"; failures=$((failures + 1)); }

# expect_eq <name> <want> <got>
expect_eq() {
  if [ "$2" = "$3" ]; then pass "$1"; else fail "$1" "want: $2" "got:  $3"; fi
}

git_quiet() { git -c user.name=t -c user.email=t@example.com -c commit.gpgsign=false -c tag.gpgsign=false "$@" >/dev/null 2>&1; }

# new_repo <dir> <module> <tag>: a repository with one commit tagged <tag>.
new_repo() {
  mkdir -p "$1" && cd "$1" || exit 1
  git init -q -b main .
  printf 'module %s\n\ngo 1.25.0\n' "$2" > go.mod
  git_quiet add -A && git_quiet commit -m init && git_quiet tag -a "$3" -m "$3"
}

# ── next-version.sh ──────────────────────────────────────────────
new_repo "$tmp/nv" example.com/x v0.3.2
nv() { (cd "$tmp/nv" && "$scripts/next-version.sh" "$@" 2>/dev/null); }

expect_eq "next-version: develop is a minor" $'version=v0.4.0\nprevious=v0.3.2\nbump=minor\nsource=release' "$(nv develop)"
expect_eq "next-version: hotfix is a patch" $'version=v0.3.3\nprevious=v0.3.2\nbump=patch\nsource=hotfix' "$(nv hotfix/fix-x)"
expect_eq "next-version: release:patch overrides develop" "v0.3.3" "$(nv develop release:patch | sed -n 's/^version=//p')"
expect_eq "next-version: release:major from v0 gives v1.0.0" "v1.0.0" "$(nv develop release:major | sed -n 's/^version=//p')"
expect_eq "next-version: labels are matched exactly" "v0.4.0" "$(nv develop kind/bug,release:minor | sed -n 's/^version=//p')"

new_repo "$tmp/nv1" example.com/x v1.2.0
if (cd "$tmp/nv1" && "$scripts/next-version.sh" develop release:major >/dev/null 2>&1); then
  fail "next-version: v2 without /v2 in go.mod is rejected" "exit status was 0"
else
  pass "next-version: v2 without /v2 in go.mod is rejected"
fi

# ── changelog.sh ─────────────────────────────────────────────────
export GITHUB_REPOSITORY=o/r GITHUB_SERVER_URL=https://github.com

# commit <message>: an empty commit.
commit() { git_quiet commit --allow-empty -m "$1"; }

# pr <dir> <n> <title> <head> <base> <labels,csv> [body]: writes the GitHub API answer of a PR.
pr() {
  mkdir -p "$1"
  jq -n --argjson n "$2" --arg t "$3" --arg h "$4" --arg b "$5" --arg l "$6" --arg body "${7:-}" '
    { number: $n, title: $t, body: $body, html_url: "https://github.com/o/r/pull/\($n)",
      user: { login: "dev" }, head: { ref: $h }, base: { ref: $b },
      labels: ($l | split(",") | map(select(. != "") | { name: . })) }' > "$1/$2.json"
}

link() { echo "([#$1](https://github.com/o/r/pull/$1), [@dev](https://github.com/dev))"; }

# A repository with the previous tag v0.0.10 and thirteen PRs merged after it. Between the two
# tags go.mod gains one requirement, and CHANGELOG/unreleased-legacy.md holds hand-written entries
# (the transition from the old model).
cl_repo="$tmp/cl"; fx="$tmp/cl-fixtures"
new_repo "$cl_repo" example.com/x v0.0.9
git_quiet tag -a v0.0.10 -m v0.0.10
note=$'Text before.\n```release-note\nBoom note\n```\nText after.'
none=$'```release-note\nNONE\n```'
chore_note=$'```release-note\nNote on chore\n```'
commit "feat: add thing (#1)";            pr "$fx" 1 "feat: add thing" feat/a develop kind/feature
commit "fix: repair (#2)";                pr "$fx" 2 "fix: repair" fix/b develop kind/bug
commit "chore: tidy (#3)";                pr "$fx" 3 "chore: tidy" chore/c develop kind/chore
commit "Merge pull request #4 from o/develop"; pr "$fx" 4 "release" develop main ""
commit "docs: skipped (#5)";              pr "$fx" 5 "docs: skipped" docs/d develop kind/feature,skip-changelog
commit "feat: hidden (#6)";               pr "$fx" 6 "feat: hidden" feat/e develop kind/feature "$none"
commit "feat!: boom (#7)";                pr "$fx" 7 "feat!: boom" feat/f develop kind/breaking "$note"
commit "chore(deps): bump deps (#8)";     pr "$fx" 8 "chore(deps): bump deps" dependabot/x develop kind/deps
commit "fix: no label fix (#9)";          pr "$fx" 9 "fix: no label fix" fix/g develop ""
commit "chore: sync (#10)";               pr "$fx" 10 "chore: sync" sync/release-v0.0.10 develop ""
commit "chore: with note (#11)";          pr "$fx" 11 "chore: with note" chore/h develop kind/chore "$chore_note"
commit "refactor: no label, no note (#12)"; pr "$fx" 12 "refactor: no label, no note" refactor/i develop ""
commit "deprecate: old api (#13)";        pr "$fx" 13 "deprecate: old api" dep/j develop kind/deprecation
commit "docs: mention an issue (#99)"     # #99 is an issue, not a PR: the API answers 404, no fixture
printf 'module example.com/x\n\ngo 1.25.0\n\nrequire example.org/dep v1.0.0\n' > go.mod
mkdir -p CHANGELOG
printf '### Added\n\n- Legacy A, first line\n  and its continuation line.\n\n### Fixed\n\n- Legacy F.\n' > CHANGELOG/unreleased-legacy.md
git_quiet add -A && git_quiet commit -m "legacy and dependency"
cd "$repo_root" || exit 1

want_notes="## Changelog since v0.0.10

## Urgent Upgrade Notes

### (No, really, you MUST read this before you upgrade)

- Boom note $(link 7)

## Changes by Kind

### Deprecation

- Old api $(link 13)

### API Change

- Boom note $(link 7)

### Feature

- Add thing $(link 1)

### Bug or Regression

- Repair $(link 2)

### Other (Cleanup or Flake)

- Tidy $(link 3)
- No label fix $(link 9)
- Note on chore $(link 11)
- No label, no note $(link 12)

### Dependency

- Bump deps $(link 8)

## Hand-written entries

#### Added

- Legacy A, first line
  and its continuation line.

#### Fixed

- Legacy F.

## Dependencies

### Added
- example.org/dep: v1.0.0

### Changed
_No changes._

### Removed
_No changes._"

cl() { (cd "$cl_repo" && PR_FIXTURES="$fx" "$scripts/changelog.sh" "$@" 2>/dev/null); }

# Preview: the tag does not exist yet, --ref and --previous describe the range.
cl v0.1.0 --ref HEAD --previous v0.0.10 --notes "$tmp/preview.md" >/dev/null; status=$?
expect_eq "changelog: preview without a tag exits 0" 0 "$status"
expect_eq "changelog: notes are the Kubernetes layout" "$want_notes" "$(cat "$tmp/preview.md" 2>/dev/null)"

(cd "$cl_repo" && git_quiet tag -a v0.1.0 -m v0.1.0)
cl v0.1.0 --notes "$tmp/notes.md" >/dev/null; status=$?
expect_eq "changelog: tagged version exits 0" 0 "$status"
expect_eq "changelog: the default range ends at the tag" "$want_notes" "$(cat "$tmp/notes.md" 2>/dev/null)"

# --write, new minor: creates CHANGELOG/CHANGELOG-0.1.md, indexes it and consumes the legacy file.
cl v0.1.0 --write >/dev/null; status=$?
expect_eq "changelog: --write for a new minor exits 0" 0 "$status"
minor="$cl_repo/CHANGELOG/CHANGELOG-0.1.md"
if rg -q '^# v0.1.0$' "$minor" && rg -q '^## Changelog since v0.0.10$' "$minor" && rg -qF -- '- [v0.1.0](#v010)' "$minor"; then pass "changelog: --write creates CHANGELOG-0.1.md with the section and its table of contents"; else fail "changelog: --write creates CHANGELOG-0.1.md with the section and its table of contents" "$(cat "$minor" 2>/dev/null | head -20)"; fi
expect_eq "changelog: README indexes the new file" $'# CHANGELOGs\n\n- [CHANGELOG-0.1.md](./CHANGELOG-0.1.md)' "$(cat "$cl_repo/CHANGELOG/README.md" 2>/dev/null)"
if [ -e "$cl_repo/CHANGELOG/unreleased-legacy.md" ]; then fail "changelog: --write consumes the legacy file"; else pass "changelog: --write consumes the legacy file"; fi
(cd "$cl_repo" && git_quiet add -A && git_quiet commit -m "sync 0.1.0")

# --write, patch of an existing minor: the new section goes above the old one, same file.
cd "$cl_repo" || exit 1
commit "fix: later fix (#40)"; pr "$fx" 40 "fix: later fix" fix/k develop kind/bug
git_quiet tag -a v0.1.1 -m v0.1.1
cd "$repo_root" || exit 1
cl v0.1.1 --write >/dev/null; status=$?
expect_eq "changelog: --write for a patch exits 0" 0 "$status"
new_at=$(rg -n '^# v0.1.1$' "$minor" | cut -d: -f1); old_at=$(rg -n '^# v0.1.0$' "$minor" | cut -d: -f1)
if [ -n "$new_at" ] && [ -n "$old_at" ] && [ "$new_at" -lt "$old_at" ]; then pass "changelog: --write puts the patch above the earlier release of its minor"; else fail "changelog: --write puts the patch above the earlier release of its minor" "v0.1.1 at '$new_at', v0.1.0 at '$old_at'"; fi
if rg -qF -- '- Later fix' "$minor"; then pass "changelog: the patch section holds its PR"; else fail "changelog: the patch section holds its PR"; fi
cl v0.1.1 --write >/dev/null
expect_eq "changelog: --write twice does not repeat the section" 1 "$(rg -c '^# v0.1.1$' "$minor")"
expect_eq "changelog: a patch adds no README line" $'# CHANGELOGs\n\n- [CHANGELOG-0.1.md](./CHANGELOG-0.1.md)' "$(cat "$cl_repo/CHANGELOG/README.md")"
(cd "$cl_repo" && git_quiet add -A && git_quiet commit -m "sync 0.1.1")

# --write, next minor: a second file, listed above the first.
cd "$cl_repo" || exit 1
commit "feat: new minor (#41)"; pr "$fx" 41 "feat: new minor" feat/l develop kind/feature
git_quiet tag -a v0.2.0 -m v0.2.0
cd "$repo_root" || exit 1
cl v0.2.0 --write >/dev/null; status=$?
expect_eq "changelog: --write for the next minor exits 0" 0 "$status"
expect_eq "changelog: README lists the newest minor first" $'# CHANGELOGs\n\n- [CHANGELOG-0.2.md](./CHANGELOG-0.2.md)\n- [CHANGELOG-0.1.md](./CHANGELOG-0.1.md)' "$(cat "$cl_repo/CHANGELOG/README.md")"
if rg -q '^## Changelog since v0.1.1$' "$cl_repo/CHANGELOG/CHANGELOG-0.2.md"; then pass "changelog: the next minor starts at the previous patch"; else fail "changelog: the next minor starts at the previous patch"; fi
if rg -q '^# v0.1.0$' "$minor"; then pass "changelog: earlier minors are untouched"; else fail "changelog: earlier minors are untouched"; fi

# Nothing to release: no entries and no legacy ones -> exit 3, nothing written.
nr="$tmp/nr"; new_repo "$nr" example.com/x v0.0.9
commit "chore: tidy (#20)"; pr "$tmp/nr-fx" 20 "chore: tidy" chore/x develop kind/chore "$none"
git_quiet tag -a v0.0.10 -m v0.0.10
(cd "$nr" && PR_FIXTURES="$tmp/nr-fx" "$scripts/changelog.sh" v0.0.10 --notes "$tmp/nr.md" --write >/dev/null 2>&1); status=$?
expect_eq "changelog: nothing to release exits 3" 3 "$status"
if [ -e "$tmp/nr.md" ] || [ -e "$nr/CHANGELOG" ]; then fail "changelog: nothing to release writes nothing"; else pass "changelog: nothing to release writes nothing"; fi
cd "$repo_root" || exit 1

# No previous tag: every PR of the history counts.
np="$tmp/np"; new_repo "$np" example.com/x v0.0.1
git tag -d v0.0.1 >/dev/null 2>&1
commit "feat: first (#30)"; pr "$tmp/np-fx" 30 "feat: first" feat/y develop kind/feature
git_quiet tag -a v0.1.0 -m v0.1.0
(cd "$np" && PR_FIXTURES="$tmp/np-fx" "$scripts/changelog.sh" v0.1.0 --notes "$tmp/np.md" >/dev/null 2>&1); status=$?
expect_eq "changelog: no previous tag exits 0" 0 "$status"
if rg -q '^## Changelog \(first release\)$' "$tmp/np.md" && rg -qF -- "- First $(link 30)" "$tmp/np.md"; then pass "changelog: no previous tag releases every PR"; else fail "changelog: no previous tag releases every PR" "$(cat "$tmp/np.md" 2>/dev/null)"; fi
cd "$repo_root" || exit 1

echo
if [ "$failures" -gt 0 ]; then echo "$failures failure(s)"; exit 1; fi
echo "all script tests passed"
