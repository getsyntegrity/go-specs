#!/usr/bin/env bash
# Creates (or updates) the labels used by the templates, the CI and the release flow.
# Idempotent. Requires an authenticated gh:  gh auth login
# Usage: .github/scripts/labels.sh [owner/repo]
set -euo pipefail
repo="${1:-$(gh repo view --json nameWithOwner -q .nameWithOwner)}"

while IFS='|' read -r name color desc; do
  [ -z "$name" ] && continue
  gh label create "$name" --repo "$repo" --color "$color" --description "$desc" --force >/dev/null
  echo "  ok $name"
done <<'LABELS'
kind/bug|d73a4a|Something is not working as it should
kind/feature|a2eeef|New functionality or API
kind/breaking|b60205|Breaks public API compatibility
kind/deprecation|fbca04|Deprecates existing API
kind/deps|0366d6|Dependency update
kind/chore|c5def5|Internal maintenance with no impact on users
kind/docs|0075ca|Documentation
kind/flake|f9d0c4|Flaky test or broken CI
needs-triage|ededed|Pending review by the maintainers
skip-changelog|eeeeee|No CHANGELOG.md entry needed (pr-meta exempts it)
release:major|b60205|Forces a major bump when this PR is released to main
release:minor|1d76db|Forces a minor bump when this PR is released to main
release:patch|0e8a16|Forces a patch bump when this PR is released to main
LABELS
echo "Labels ready in $repo"
