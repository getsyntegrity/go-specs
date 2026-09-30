#!/usr/bin/env bash
# Tests for the release scripts in .github/scripts (next-version.sh, release-changelog.sh).
# Each case builds a throwaway git repository, so nothing here touches the real checkout.
# Usage: bash .github/scripts/test/run.sh        (needs git and go)
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

# ── release-changelog.sh ─────────────────────────────────────────
release_bin="$tmp/release-bin"
(cd "$repo_root" && go build -o "$release_bin" ./tools/release) || { echo "cannot build tools/release"; exit 1; }
export RELEASE_TOOL="$release_bin" RELEASE_DATE=2026-01-02

changelog_header=$'# Changelog\n\n## [Unreleased]\n'

# rc_repo <dir>: tag v0.1.0 holds a changelog whose [Unreleased] has entry A (two lines long).
rc_repo() {
  new_repo "$1" example.com/x v0.0.9
  printf '%s\n### Added\n\n- Entry A, first line\n  and its continuation line.\n\n## [v0.0.9] - 2025-01-01\n\n### Added\n\n- Entry Z, older.\n\n[Unreleased]: https://example.com/compare/v0.0.9...HEAD\n' \
    "$changelog_header" > CHANGELOG.md
  git_quiet add -A && git_quiet commit -m "changelog A" && git_quiet tag -a v0.1.0 -m v0.1.0
}

# Case 1: main is unstamped after v0.1.0, a hotfix adds entry H. Only H may be released.
rc_repo "$tmp/rc1"
printf '%s\n### Added\n\n- Entry A, first line\n  and its continuation line.\n\n### Fixed\n\n- Entry H, the hotfix.\n\n## [v0.0.9] - 2025-01-01\n\n### Added\n\n- Entry Z, older.\n\n[Unreleased]: https://example.com/compare/v0.0.9...HEAD\n' \
  "$changelog_header" > CHANGELOG.md
out="$tmp/rc1.out"; notes="$tmp/rc1.notes"
(cd "$tmp/rc1" && "$scripts/release-changelog.sh" v0.1.1 v0.1.0 "$out" "$notes" >/dev/null 2>&1)
status=$?
expect_eq "release-changelog: hotfix with stale entries exits 0" 0 "$status"
if grep -q 'Entry H' "$notes" && ! grep -q 'Entry A' "$notes"; then pass "release-changelog: notes hold only the new entry"; else fail "release-changelog: notes hold only the new entry" "$(cat "$notes" 2>/dev/null)"; fi
if grep -q '^## \[v0.1.1\] - 2026-01-02' "$out"; then pass "release-changelog: output is stamped with version and date"; else fail "release-changelog: output is stamped with version and date"; fi

# Case 2: a multi-line entry that is already released is dropped as a whole block.
if grep -q 'continuation line' "$out"; then fail "release-changelog: continuation lines of a released entry are dropped"; else pass "release-changelog: continuation lines of a released entry are dropped"; fi

# Case 3: nothing new since the previous tag -> exit 3, no notes.
rc_repo "$tmp/rc3"
(cd "$tmp/rc3" && "$scripts/release-changelog.sh" v0.1.1 v0.1.0 "$tmp/rc3.out" "$tmp/rc3.notes" >/dev/null 2>&1)
expect_eq "release-changelog: no new entries exits 3" 3 "$?"

# Case 4: no previous tag at all -> everything in [Unreleased] is released.
new_repo "$tmp/rc4" example.com/x v0.0.9
printf '%s\n### Added\n\n- Entry A.\n\n' "$changelog_header" > "$tmp/rc4/CHANGELOG.md"
(cd "$tmp/rc4" && "$scripts/release-changelog.sh" v0.1.0 - "$tmp/rc4.out" "$tmp/rc4.notes" >/dev/null 2>&1)
expect_eq "release-changelog: no previous tag releases everything" 0 "$?"
if grep -q 'Entry A' "$tmp/rc4.notes"; then pass "release-changelog: no previous tag keeps entry"; else fail "release-changelog: no previous tag keeps entry"; fi

echo
if [ "$failures" -gt 0 ]; then echo "$failures failure(s)"; exit 1; fi
echo "all script tests passed"
