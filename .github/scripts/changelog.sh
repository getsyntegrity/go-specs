#!/usr/bin/env bash
# Generates the notes of a version in the Kubernetes CHANGELOG format (ported from the ego
# repository's .github/scripts/changelog.sh).
#
# Usage: changelog.sh <version> [--ref REF] [--previous TAG|-] [--notes FILE] [--write]
#   --ref REF       end of the PR range and source of go.mod and of the legacy file. Default: the
#                   tag <version>, which must exist. The PR preview of a release passes HEAD, since
#                   the tag does not exist yet.
#   --previous TAG  start of the range ("-" for none). Default: the latest vX.Y.Z tag merged into
#                   REF, other than <version>.
#   --notes FILE    writes only the section of this version (for the GitHub Release)
#   --write         updates CHANGELOG/CHANGELOG-X.Y.md (every patch of a minor in one file, newest
#                   first) and CHANGELOG/README.md (the index, newest minor first)
#
# Where each thing comes from:
#   - PRs: the ones merged between the previous tag and REF ("Merge pull request #N" commits or
#     squash "... (#N)"). For each PR the ```release-note block of the body is used; if there is
#     none, the title without its Conventional Commit prefix. "NONE" excludes it.
#   - Type: the PR's kind/* label. kind/breaking or "action required" in the note also goes to
#     "Urgent Upgrade Notes".
#   - Dependencies: go.mod diff between the two tags.
#   - Hand-written entries: CHANGELOG/unreleased-legacy.md at REF. It holds the notes written by
#     hand before the changelog was generated; the first release folds them into its section, and
#     --write deletes the file so that the sync PR removes it from develop.
# Excluded: the develop->main release PR, sync/* PRs and PRs with the skip-changelog label.
#
# Exit codes: 0 done, 3 nothing to release (no PR entry and no hand-written entry: nothing is
# written), 1 error.
#
# Needs: git (all tags), jq, perl, go. GH_TOKEN for the GitHub API.
# For tests: PR_FIXTURES=dir reads dir/<N>.json instead of calling the API.
set -euo pipefail

die() { echo "::error::$*" >&2; exit 1; }
usage="Usage: changelog.sh vX.Y.Z [--ref REF] [--previous TAG|-] [--notes FILE] [--write]"
version="${1:-}"; shift || true
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "$usage"
notes=""; write=false; ref=""; previous_arg=""
while [ $# -gt 0 ]; do
  case "$1" in
    --notes) notes="${2:?--notes needs a file}"; shift 2 ;;
    --ref) ref="${2:?--ref needs a ref}"; shift 2 ;;
    --previous) previous_arg="${2:?--previous needs a tag or -}"; shift 2 ;;
    --write) write=true; shift ;;
    *) die "Unknown option: $1. $usage" ;;
  esac
done
[ -n "$notes" ] || $write || die "Pass --notes FILE and/or --write"

repo="${GITHUB_REPOSITORY:?missing GITHUB_REPOSITORY}"
api="${GITHUB_API_URL:-https://api.github.com}"
server="${GITHUB_SERVER_URL:-https://github.com}"
cd "$(git rev-parse --show-toplevel)"
if [ -z "$ref" ]; then
  git rev-parse -q --verify "refs/tags/$version" >/dev/null || die "Tag $version does not exist"
  ref="$version"
fi

if [ -n "$previous_arg" ]; then
  previous="$previous_arg"; [ "$previous" != "-" ] || previous=""
else
  previous=$(git tag --merged "$ref" --list 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
             | grep -vxF "$version" | sort -V | tail -1 || true)
fi
range="${previous:+$previous..}$ref"
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT

# ── Included PRs ────────────────────────────────────────────────
# Prints the PR as JSON, or nothing when the number is an issue (a commit subject can end in an
# issue number; the API answers 404 for it). Any other API failure stops the script.
pr_json() {
  if [ -n "${PR_FIXTURES:-}" ]; then
    if [ -f "$PR_FIXTURES/$1.json" ]; then cat "$PR_FIXTURES/$1.json"; fi
    return 0
  fi
  local code
  code=$(curl -sS -o "$tmp/pr.json" -w '%{http_code}' -H "Authorization: Bearer ${GH_TOKEN:?missing GH_TOKEN}" \
           -H "Accept: application/vnd.github+json" "$api/repos/$repo/pulls/$1")
  case "$code" in
    200) cat "$tmp/pr.json" ;;
    404) echo "::notice::#$1 is not a pull request: skipped" >&2 ;;
    *) die "GitHub API answered $code for PR #$1" ;;
  esac
}

git log --format=%s "$range" \
  | grep -oE '^Merge pull request #[0-9]+|\(#[0-9]+\)$' | grep -oE '[0-9]+' | sort -un > "$tmp/prs" || true

: > "$tmp/entries.jsonl"
while read -r n; do
  [ -n "$n" ] || continue
  pr_json "$n" | jq -c '
    def strip: sub("^\\s+"; "") | sub("\\s+$"; "");
    # A title is a Conventional Commit line: the type and scope are dropped, the rest capitalised.
    def clean_title: sub("^[A-Za-z]+(\\([^)]*\\))?!?:\\s*"; "") | (.[0:1] | ascii_upcase) + .[1:];
    ([.labels[]?.name]) as $l
    | if ($l | index("skip-changelog")) or (.head.ref == "develop" and .base.ref == "main")
         or (.head.ref | startswith("sync/")) then empty else
      ((.body // "") | gsub("\r"; "")
        | [match("```release-note[^\\n]*\\n([\\s\\S]*?)```"; "g").captures[0].string] | .[0] // "" | strip) as $note
      | (if $note == "" then (.title | clean_title) else $note end | strip) as $text
      | if ($text | ascii_downcase | IN("none", "n/a", "na")) then empty else
        { kind: (first(("kind/breaking", "kind/deprecation", "kind/feature", "kind/bug", "kind/deps")
                        as $k | select($l | index($k)) | $k) // "other"),
          urgent: (($l | index("kind/breaking")) != null or ($text | test("action required"; "i"))),
          text: $text, number: .number, user: .user.login, url: .html_url }
        end
      end' >> "$tmp/entries.jsonl"
done < "$tmp/prs"

# ── Hand-written entries (transition from the hand-maintained CHANGELOG.md) ──
# Headings go one level down so that they do not compete with the generated ones.
legacy_path=CHANGELOG/unreleased-legacy.md
git show "$ref:$legacy_path" 2>/dev/null \
  | awk '/^(```|~~~)/ { fence = !fence } !fence && /^### / { sub(/^### /, "#### ") } { print }' > "$tmp/legacy.md" || : > "$tmp/legacy.md"

if [ ! -s "$tmp/entries.jsonl" ] && ! grep -q '[^[:space:]]' "$tmp/legacy.md"; then
  echo "::notice::No PR entry and no hand-written entry between ${previous:-the beginning} and $ref: nothing to release" >&2
  exit 3
fi

# ── Dependencies (go.mod diff between tags) ─────────────────────
modjson() {  # $1 = ref (empty = does not exist)
  if [ -n "$1" ] && git show "$1:go.mod" > "$tmp/go.mod" 2>/dev/null; then
    (cd "$tmp" && GOTOOLCHAIN=local go mod edit -json go.mod)   # read-only: no toolchain downloads
  else echo '{}'; fi
}
modjson "$previous" > "$tmp/old.json"
modjson "$ref"      > "$tmp/new.json"

# ── Section rendering ─────────────────────────────────────────
jq -rn --arg v "$version" --arg prev "$previous" --arg server "$server" \
  --rawfile legacy "$tmp/legacy.md" \
  --slurpfile e <(jq -s . "$tmp/entries.jsonl") \
  --slurpfile old "$tmp/old.json" --slurpfile new "$tmp/new.json" '
  def userlink: if (.user | endswith("[bot]")) then "\($server)/apps/\(.user | rtrimstr("[bot]"))" else "\($server)/\(.user)" end;
  def item: "- " + (.text | split("\n") | join("\n  ")) + " ([#\(.number)](\(.url)), [@\(.user)](\(userlink)))";
  def nothing: "_No changes._";
  def tc($m): ($m.Toolchain // "") | if type == "object" then (.Name // "") else . end;
  def reqs($m): ($m.Require // []) | map({key: .Path, value: .Version}) | from_entries;
  ($e[0]) as $e | reqs($old[0]) as $o | reqs($new[0]) as $n
  | [ "# \($v)", "",
      (if $prev == "" then "## Changelog (first release)" else "## Changelog since \($prev)" end), "",
      ( ($e | map(select(.urgent))) as $u
        | if ($u | length) > 0 then
            "## Urgent Upgrade Notes", "", "### (No, really, you MUST read this before you upgrade)", "",
            ($u[] | item), ""
          else empty end ),
      "## Changes by Kind", "",
      ( [ ["kind/deprecation", "Deprecation"], ["kind/breaking", "API Change"], ["kind/feature", "Feature"],
          ["kind/bug", "Bug or Regression"], ["other", "Other (Cleanup or Flake)"], ["kind/deps", "Dependency"] ] as $kinds
        | if ($e | length) == 0 then nothing, "" else
            ($kinds[] as [$k, $title]
              | ($e | map(select(.kind == $k))) as $items
              | if ($items | length) > 0 then "### \($title)", "", ($items[] | item), "" else empty end)
          end ),
      ( ($legacy | sub("\\s+$"; "")) as $l
        | if $l != "" then "## Hand-written entries", "", $l, "" else empty end ),
      "## Dependencies", "",
      ( [ (if $old[0].Go != $new[0].Go and $new[0].Go != null then
             "- Minimum version: \($old[0].Go // "-") → \($new[0].Go)" else empty end),
          (if tc($old[0]) != tc($new[0]) and tc($new[0]) != "" then
             "- Toolchain: \(tc($old[0]) | if . == "" then "-" else . end) → \(tc($new[0]))" else empty end) ] as $go
        | if ($go | length) > 0 then "### Go", "", $go[], "" else empty end ),
      "### Added",
      ( [$n | to_entries[] | select($o[.key] == null) | "- \(.key): \(.value)"] | if length > 0 then .[] else nothing end ), "",
      "### Changed",
      ( [$n | to_entries[] | select($o[.key] != null and $o[.key] != .value) | "- \(.key): \($o[.key]) → \(.value)"]
        | if length > 0 then .[] else nothing end ), "",
      "### Removed",
      ( [$o | to_entries[] | select($n[.key] == null) | "- \(.key): \(.value)"] | if length > 0 then .[] else nothing end )
    ] | .[]' > "$tmp/section.md"

if [ -n "$notes" ]; then
  # In the GitHub Release the title is already the version: the leading "# vX.Y.Z" is omitted.
  tail -n +3 "$tmp/section.md" > "$notes"
  echo "Notes for $version -> $notes ($(wc -l < "$tmp/prs" | tr -d ' ') PRs reviewed)"
fi

# ── CHANGELOG/CHANGELOG-X.Y.md + README.md ───────────────────────
if $write; then
  IFS=. read -r major minor _ <<<"${version#v}"
  mkdir -p CHANGELOG
  file="CHANGELOG/CHANGELOG-${major}.${minor}.md"
  perl -CSD -Mutf8 -e '
    my ($file, $secfile, $ver) = @ARGV;
    local $/;
    my $new = do { open my $f, "<:utf8", $secfile or die; <$f> };
    my $old = -e $file ? do { open my $f, "<:utf8", $file or die; <$f> } : "";
    $old =~ s/<!-- BEGIN MUNGE: GENERATED_TOC -->.*?<!-- END MUNGE: GENERATED_TOC -->\n*//s;
    my @chunks = grep { /\S/ } split /<!-- NEW RELEASE NOTES ENTRY -->\n*/, $old;
    @chunks = grep { $_ !~ /^# \Q$ver\E[ \t]*$/m } @chunks;     # idempotent: replaces the version
    s/\s+\z/\n/ for ($new, @chunks);
    unshift @chunks, $new;
    my (%seen, @toc);
    for my $c (@chunks) {
      my $fence = 0;
      for my $line (split /\n/, $c) {
        $fence = !$fence if $line =~ /^```/;
        next if $fence or $line !~ /^(#{1,3}) (.+?)\s*$/;
        my ($lvl, $t) = (length $1, $2);
        (my $a = lc $t) =~ s/[^\p{L}\p{N}\s_-]//g;
        $a =~ s/ /-/g;
        my $n = $seen{$a}++;
        $a .= "-$n" if $n;
        push @toc, ("  " x ($lvl - 1)) . "- [$t](#$a)";
      }
    }
    open my $out, ">:utf8", $file or die;
    print $out "<!-- BEGIN MUNGE: GENERATED_TOC -->\n\n", join("\n", @toc),
               "\n\n<!-- END MUNGE: GENERATED_TOC -->\n\n",
               join("\n", map { "<!-- NEW RELEASE NOTES ENTRY -->\n\n$_" } @chunks);
  ' "$file" "$tmp/section.md" "$version"

  { echo "# CHANGELOGs"; echo
    printf '%s\n' CHANGELOG/CHANGELOG-*.md | sort -rV | while read -r f; do b=$(basename "$f"); echo "- [$b](./$b)"; done
  } > CHANGELOG/README.md
  rm -f "$legacy_path"
  echo "Updated $file and CHANGELOG/README.md"
fi
