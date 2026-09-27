#!/usr/bin/env bash
#
# check-go-version.sh -- fail when any go.mod's `go` directive is not the
# MAJOR.MINOR.0 floor derived from .go-version.
#
# Contract -- two files, two different jobs:
#   .go-version                  -- the exact toolchain patch CI and
#                                   contributors build and test with (e.g.
#                                   "1.25.14"). CI installs it via
#                                   actions/setup-go's `go-version-file`, and
#                                   asdf/mise/goenv/gvm pick it up for
#                                   contributors. Must be a full
#                                   MAJOR.MINOR.PATCH; it is the pin, not a
#                                   floor, so a bare MAJOR.MINOR is rejected.
#   every go.mod `go` directive  -- the minimum language version every
#                                   downstream consumer of go-specs inherits.
#                                   Must equal MAJOR.MINOR.0 of .go-version --
#                                   the first release of the pinned minor --
#                                   never the pinned patch and never the bare
#                                   MAJOR.MINOR form.
#
# Why the floor is MAJOR.MINOR.0 and not lower: CI only ever builds on the
# single toolchain pinned in .go-version (see ci.yml's header for why a
# version matrix is deliberately not run), so a floor below the pinned minor
# would be an untested claim -- nothing exercises it. MAJOR.MINOR.0 is also
# usually the lowest floor actually achievable: dependencies commonly
# declare their own `go` directive at MAJOR.MINOR.0 of a recent minor, and
# `go mod tidy` on the pinned toolchain raises anything lower to match.
#
# Why the floor is not the pinned patch: patch releases within a minor are
# API-identical by Go's compatibility policy, so testing on the pinned patch
# already validates every consumer on that minor, including ones on an older
# or newer patch. Pinning the floor to the exact patch would force every
# downstream consumer onto that one patch release for no correctness reason.
#
# Bump procedure:
#   patch bump (e.g. 1.25.14 -> 1.25.20): edit .go-version only. The floor
#     (MAJOR.MINOR.0) is unchanged, so no go.mod edit is needed.
#   minor bump (e.g. 1.25.14 -> 1.26.1): edit .go-version, then run this
#     script's companion fix for every module:
#
#   go mod edit -go="<new MAJOR>.<new MINOR>.0" <go.mod>
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

# .go-version is the exact toolchain pin, consumed verbatim by
# actions/setup-go and by version managers, so it must be a full
# MAJOR.MINOR.PATCH -- a bare MAJOR.MINOR would leave the pinned patch
# ambiguous, and a stray "go" prefix or a range would only fail much further
# downstream.
if ! [[ "$GOVERSION_RAW" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "::error::check-go-version.sh: $GOVERSION_FILE contains an unexpected value: '$GOVERSION_RAW' (expected MAJOR.MINOR.PATCH)" >&2
    exit 1
fi

# The floor every go.mod must declare: MAJOR.MINOR.0 of the pinned patch.
GOFLOOR="${GOVERSION_RAW%.*}.0"

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

    if [[ "$GOMOD_RAW" != "$GOFLOOR" ]]; then
        cat >&2 <<MSG
::error file=$GOMOD_FILE::check-go-version.sh: Go version drift between $GOMOD_FILE and $GOVERSION_FILE.
  $GOMOD_FILE     : go $GOMOD_RAW
  $GOVERSION_FILE : $GOVERSION_RAW (floor: $GOFLOOR)

$GOMOD_FILE's 'go' directive must be the MAJOR.MINOR.0 floor derived from
$GOVERSION_FILE, not the pinned patch and not a bare MAJOR.MINOR. Fix with:
  go mod edit -go="$GOFLOOR" $GOMOD_FILE
MSG
        FAILED=1
        continue
    fi

    echo "check-go-version.sh: OK $GOMOD_FILE (go $GOMOD_RAW == $GOVERSION_FILE floor $GOFLOOR)"
done

if [[ $FAILED -ne 0 ]]; then
    exit 1
fi

echo "check-go-version.sh: OK (${#GOMOD_FILES[@]} module(s) match $GOVERSION_FILE floor $GOFLOOR)"
