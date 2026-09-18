#!/usr/bin/env bash
#
# check-go-version.sh -- fail when any go.mod's `go` directive differs from
# .go-version.
#
# Contract:
#   .go-version                  -- single source of truth for the Go version
#                                   (e.g. "1.25.14"). CI installs it via
#                                   actions/setup-go's `go-version-file`, and
#                                   asdf/mise/goenv/gvm pick it up for
#                                   contributors.
#   every go.mod `go` directive  -- must match .go-version exactly, patch
#                                   component included.
#
# Exact match is deliberate: one version, one place to bump. The cost is that
# the declared minimum language version -- the floor every downstream consumer
# of go-specs inherits -- moves with the toolchain patch, so consumers must
# install at least that patch release to build. Bump .go-version, then run
# this script's companion fix:
#
#   go mod edit -go="$(tr -d '[:space:]' <.go-version)"
#
# for every module, and commit both.
#
# All modules are checked, not just the root one, so this keeps working if the
# repository ever grows beyond a single module. Directories Go itself ignores
# (testdata, vendor) are skipped, as are .git and .claude worktrees, which hold
# copies of the same module rather than modules of this repository.
#
# Run from the repo root (CI) or anywhere (the script self-locates).
#
# Exits non-zero on any mismatch, with a GitHub Actions ::error annotation.
#
# Invocation:
#   ./.github/scripts/check-go-version.sh

set -euo pipefail

cd "$(dirname "$0")/../.."

GOVERSION_FILE=".go-version"

if [[ ! -f "$GOVERSION_FILE" ]]; then
    echo "::error::check-go-version.sh: $GOVERSION_FILE not found" >&2
    exit 1
fi

GOVERSION_RAW="$(tr -d '[:space:]' <"$GOVERSION_FILE")"

if [[ -z "$GOVERSION_RAW" ]]; then
    echo "::error::check-go-version.sh: $GOVERSION_FILE is empty" >&2
    exit 1
fi

# Reject anything that is not a plain version: .go-version is consumed
# verbatim by actions/setup-go and by version managers, so a stray "go"
# prefix or a range would only fail much further downstream.
if ! [[ "$GOVERSION_RAW" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]]; then
    echo "::error::check-go-version.sh: $GOVERSION_FILE contains an unexpected value: '$GOVERSION_RAW' (expected MAJOR.MINOR[.PATCH])" >&2
    exit 1
fi

# Collect every module manifest that belongs to this repository. `find` rather
# than a shell glob so nested modules are covered, pruned so that vendored
# dependencies, test fixtures and worktree checkouts never register as modules
# of their own. Sorted for deterministic output.
GOMOD_FILES=()
while IFS= read -r gomod; do
    GOMOD_FILES+=("${gomod#./}")
done < <(
    find . \
        \( -name .git -o -name .claude -o -name vendor -o -name testdata -o -name node_modules \) -prune \
        -o -type f -name go.mod -print |
        LC_ALL=C sort
)

if [[ ${#GOMOD_FILES[@]} -eq 0 ]]; then
    echo "::error::check-go-version.sh: no go.mod found under $(pwd)" >&2
    exit 1
fi

FAILED=0

for GOMOD_FILE in "${GOMOD_FILES[@]}"; do
    # Pull the version out of `go X.Y[.Z]` in go.mod. Anchored to the start of
    # a line so a `// go 1.99` comment elsewhere cannot satisfy the match.
    GOMOD_RAW="$(awk '/^go [0-9]/ {print $2; exit}' "$GOMOD_FILE")"

    if [[ -z "$GOMOD_RAW" ]]; then
        echo "::error::check-go-version.sh: could not parse 'go' directive from $GOMOD_FILE" >&2
        FAILED=1
        continue
    fi

    if [[ "$GOMOD_RAW" != "$GOVERSION_RAW" ]]; then
        cat >&2 <<MSG
::error file=$GOMOD_FILE::check-go-version.sh: Go version drift between $GOMOD_FILE and $GOVERSION_FILE.
  $GOMOD_FILE     : go $GOMOD_RAW
  $GOVERSION_FILE : $GOVERSION_RAW

They must match exactly, patch component included. Fix with:
  go mod edit -go="$GOVERSION_RAW" $GOMOD_FILE
MSG
        FAILED=1
        continue
    fi

    echo "check-go-version.sh: OK $GOMOD_FILE (go $GOMOD_RAW == $GOVERSION_FILE $GOVERSION_RAW)"
done

if [[ $FAILED -ne 0 ]]; then
    exit 1
fi

echo "check-go-version.sh: OK (${#GOMOD_FILES[@]} module(s) match $GOVERSION_FILE $GOVERSION_RAW)"
