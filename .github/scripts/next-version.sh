#!/usr/bin/env bash
# Computes the library's next semver version and validates that it can be published.
#
# Usage: next-version.sh <head_ref> [labels_csv]
#   head_ref: source branch of the PR to main (develop, hotfix/xxx)
#   labels:   PR labels separated by commas (optional)
#
# Bump rules (in order):
#   1. label release:major | release:minor | release:patch -> that bump
#   2. hotfix/*  -> patch
#   3. develop   -> minor
#   4. other     -> patch (with a warning)
#
# Go validation (semantic import versioning): from v2 onwards the module path in
# go.mod MUST end in /vN. If it does not match, it exits with an error: a v2.0.0
# tag on "module github.com/org/ego" cannot be consumed with go get.
#
# Output (GITHUB_OUTPUT format): version=, previous=, bump=, source=
set -euo pipefail

head_ref="${1:-}"
labels=",${2:-},"

case "$labels" in
  *,release:major,*) bump="major" ;;
  *,release:minor,*) bump="minor" ;;
  *,release:patch,*) bump="patch" ;;
  *)
    case "$head_ref" in
      hotfix/*) bump="patch" ;;
      develop)  bump="minor" ;;
      *) bump="patch"; echo "::warning::Source '${head_ref:-unknown}' is outside the flow, using patch" >&2 ;;
    esac ;;
esac

case "$head_ref" in
  hotfix/*) source=hotfix ;;
  develop)  source=release ;;
  *)        source=other ;;
esac

# HIGHEST stable semver tag reachable from HEAD (not the nearest one: with
# main<->develop sync merges, `git describe` may return an old one).
previous=$(git tag --merged HEAD --list 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1 || true)
previous="${previous:-v0.0.0}"
IFS=. read -r major minor patch <<<"${previous#v}"

case "$bump" in
  major) major=$((major + 1)); minor=0; patch=0 ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  patch) patch=$((patch + 1)) ;;
esac
version="v${major}.${minor}.${patch}"

# ── Module path validation ──
if [ -f go.mod ]; then
  module=$(awk '$1 == "module" { print $2; exit }' go.mod)
  if [[ "$module" =~ /v([0-9]+)$ ]]; then mod_major="${BASH_REMATCH[1]}"; else mod_major=""; fi
  if [ "$major" -ge 2 ] && [ "$mod_major" != "$major" ]; then
    echo "::error::${version} requires 'module ${module%/v*}/v${major}' in go.mod (today: ${module})" >&2
    exit 1
  fi
  if [ "$major" -le 1 ] && [ -n "$mod_major" ]; then
    echo "::error::go.mod declares /v${mod_major} but the computed version is ${version}" >&2
    exit 1
  fi
fi

echo "version=${version}"
echo "previous=${previous}"
echo "bump=${bump}"
echo "source=${source}"
