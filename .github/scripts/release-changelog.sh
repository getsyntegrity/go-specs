#!/usr/bin/env bash
# Stamps CHANGELOG.md for a release WITHOUT modifying the checked-out file, and optionally
# extracts the release notes of that version.
#
# Usage: release-changelog.sh <version> <previous_tag|-> <out_file> [notes_file]
#   version        vX.Y.Z being released
#   previous_tag   the tag released before it, or "-" when there is none
#   out_file       receives the whole CHANGELOG.md with `## [Unreleased]` turned into
#                  `## [<version>] - <date>` (the existing `tools/release changelog` rewrite)
#   notes_file     optional; receives only the section of <version> (GitHub Release body)
#
# Which entries are released: the entries of `## [Unreleased]` in ./CHANGELOG.md minus every entry
# that the previous tag's CHANGELOG.md already contains. `main` does not get a stamped changelog
# before a release (the stamp arrives with the pull request that follows each release), so
# `## [Unreleased]` on `main` can still hold entries that an earlier tag already published. An entry
# is one `- ` list item with its continuation lines; it is compared as a whole. Category headings
# that end up empty are dropped.
#
# Exit codes: 0 done, 3 nothing new to release (out_file and notes_file are not written), 1 error.
#
# Environment:
#   RELEASE_TOOL   command that runs the release tool. Default: go run ./tools/release
#   RELEASE_DATE   YYYY-MM-DD. Default: today (UTC)
set -euo pipefail

version="${1:?missing version}"
previous="${2:?missing previous tag (use - for none)}"
out="${3:?missing out_file}"
notes="${4:-}"
release_tool="${RELEASE_TOOL:-go run ./tools/release}"
date="${RELEASE_DATE:-$(date -u +%Y-%m-%d)}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

: > "$tmp/previous.md"
if [ "$previous" != "-" ]; then
  git show "$previous:CHANGELOG.md" > "$tmp/previous.md" 2>/dev/null || echo "::notice::$previous has no CHANGELOG.md, every entry is new" >&2
fi

# Entry blocks, keyed by their full text. Two passes over two files: the first reads the previous
# changelog into a set, the second rewrites the current [Unreleased] section.
awk '
  function is_end(l) { return l == "" || l ~ /^#/ || l ~ /^\[[^]]+\]: / }
  function flush_block() {
    if (block == "") return
    if (!(pass == 2 && (block in seen))) {
      if (pass == 2) {
        if (heading != "") { print heading; print ""; heading = "" }
        print block; print ""
      }
    }
    if (pass == 1) seen[block] = 1
    block = ""
  }
  FNR == 1 { pass = (FILENAME == ARGV[1]) ? 1 : 2; block = ""; heading = ""; in_u = 0 }
  pass == 1 {
    if (is_end($0)) { flush_block(); next }
    if ($0 ~ /^- /) { flush_block(); block = $0; next }
    if (block != "") block = block "\n" $0
    next
  }
  # pass 2: only the [Unreleased] section is rewritten; everything else is copied verbatim.
  !in_u {
    print
    if ($0 == "## [Unreleased]") { in_u = 1; print "" }
    next
  }
  in_u {
    if ($0 ~ /^## / || $0 ~ /^\[[^]]+\]: /) { flush_block(); in_u = 0; print; next }
    if ($0 ~ /^### /) { flush_block(); heading = $0; next }
    if ($0 ~ /^- /) { flush_block(); block = $0; next }
    if ($0 == "") { flush_block(); next }
    if (block != "") { block = block "\n" $0; next }
    # Free text under [Unreleased] that is not a list item: keep it.
    if (heading != "") { print heading; print ""; heading = "" }
    print
  }
  END { flush_block() }
' "$tmp/previous.md" CHANGELOG.md > "$tmp/current.md"

cp "$tmp/current.md" "$tmp/stamped.md"
set +e
# shellcheck disable=SC2086  # RELEASE_TOOL is a command line, not a single word
$release_tool changelog -version "$version" -date "$date" -file "$tmp/stamped.md"
status=$?
set -e
if [ "$status" -eq 3 ]; then
  echo "::notice::No entries under [Unreleased] that $previous did not already publish" >&2
  exit 3
elif [ "$status" -ne 0 ]; then
  exit "$status"
fi

cp "$tmp/stamped.md" "$out"
if [ -n "$notes" ]; then
  # shellcheck disable=SC2086
  $release_tool notes -version "$version" -file "$tmp/stamped.md" > "$notes"
fi
