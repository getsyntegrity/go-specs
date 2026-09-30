#!/usr/bin/env bash
# Detects changes in the library's public API by comparing against BASE.
#
# Usage: api-check.sh <base_git_ref> [bump]
#   base_git_ref: tag or branch to compare against (e.g. v1.4.2, origin/develop)
#   bump:         major | minor | patch (what is going to be published). Empty = report only.
#
# Result:
#   - breaks the API and bump != major -> ERROR (except in v0.x, where it is a warning)
#   - adds API and bump == patch       -> warning (semver asks for a minor)
#   - no bump                          -> report only (PRs to develop)
# Requires apidiff in the PATH:  go install golang.org/x/exp/cmd/apidiff@latest
set -euo pipefail

base="${1:?missing base_git_ref}"
bump="${2:-}"
summary="${GITHUB_STEP_SUMMARY:-/dev/null}"

module=$(awk '$1 == "module" { print $2; exit }' go.mod)
tmp=$(mktemp -d)
trap 'git worktree remove --force "$tmp/base" >/dev/null 2>&1 || true; rm -rf "$tmp"' EXIT

git worktree add --detach --quiet "$tmp/base" "$base"
# apidiff warns about every internal/ package (not public API): that noise is filtered out
quiet() { "$@" 2> >(grep -v '^Ignoring internal package' >&2 || true); }

base_module=$(awk '$1 == "module" { print $2; exit }' "$tmp/base/go.mod")

# Path change (e.g. /v2 -> /v3): already an explicit major, nothing to compare.
if [ "$base_module" != "$module" ]; then
  echo "::notice::The module path changed ($base_module -> $module): explicit major, skipping apidiff"
  echo "### API: module path change ($base_module → $module)" >> "$summary"
  exit 0
fi

(cd "$tmp/base" && quiet apidiff -m -w "$tmp/old.api" "$module")
quiet apidiff -m -w "$tmp/new.api" "$module"

report=$(quiet apidiff -m "$tmp/old.api" "$tmp/new.api" || true)
incompatible=$(quiet apidiff -m -incompatible "$tmp/old.api" "$tmp/new.api" || true)

if [ -z "$report" ]; then
  echo "No changes to the public API compared with $base"
  echo "### API: no changes compared with \`$base\`" >> "$summary"
  exit 0
fi

echo "$report"
{
  echo "### API changes compared with \`$base\`"
  echo '```'
  echo "$report"
  echo '```'
} >> "$summary"

if [ -n "$incompatible" ]; then
  if [ -z "$bump" ]; then
    echo "::warning::This PR breaks the public API. When it reaches main it will require release:major"
  elif [ "$bump" = "major" ]; then
    echo "::notice::Incompatible changes covered by release:major"
  elif [[ "$base" == v0.* ]]; then
    echo "::warning::Incompatible changes in v0.x (allowed by semver, but tell the consumers)"
  else
    echo "::error::The public API has incompatible changes and the bump is '$bump'. Add the release:major label (and update the /vN path in go.mod) or revert the changes."
    exit 1
  fi
elif [ "$bump" = "patch" ]; then
  echo "::warning::New API is added in a patch; semver suggests release:minor"
fi
