# Matcher composition: Not, All, Any (issue #209)

## Objective

Let users combine existing matchers logically instead of writing a new matcher type for every
combination, and make the resulting failure message name the sub-matcher actually responsible.

## Problem

`assert` ships a fixed set of matchers (`Equal`, `NotEqual`, `BeNil`, `BeTrue`, `BeFalse`,
`Contain`, `MatchError`, `MatchErrorAs`). Logical combination only exists where someone hand-wrote
it: `NotEqual` is `Equal` negated by hand, in its own type, with its own message. There is no way to
say "matches A and B" or "matches A or B" at a call site, and every new combination costs a new
exported matcher.

The `Matcher` interface (`assert/matcher.go:15`) is already the right seam — `Match(any) bool` plus
`FailureMessage(any) string` — so combinators are additive. Nothing in the public DSL changes.

The hard part is the message. A composite that reports "combined matcher failed" throws away the
only thing the user needs. #149/#200 taught this repo that matcher messages are the product and must
be pinned by tests, not inferred from a green boolean.

## What changes

Three new combinators in `assert`, re-exported from `specs`:

- `Not(m Matcher) Matcher`
- `All(ms ...Matcher) Matcher`
- `Any(ms ...Matcher) Matcher`

`All` and `Any` build their failure message from the sub-matchers' own `FailureMessage`, indexed, so
the reader sees exactly which entries failed and why.

`Not` cannot do that: when `Not` fails, its sub-matcher *succeeded*, so calling
`sub.FailureMessage(actual)` would print a message describing a comparison that did not fail
("expected 5 to equal 5"). `Not` therefore needs a way to *name* its sub-matcher rather than quote
its failure. That is the one new piece of surface:

```go
// Describer is an optional interface a Matcher may implement to name itself in composite failures.
type Describer interface{ Description() string }
```

Every built-in matcher implements it (`Equal(43)` → `equal to 43`, `BeNil()` → `nil`, and so on),
and the combinators implement it too, so nesting reads as English. A third-party matcher that does
not implement it falls back to its `%T`. Rejected alternative: printing `%T` unconditionally, which
leaks unexported type names (`*assert.equalMatcher`) into user-facing output.

## Nil and empty semantics (acceptance criterion 6)

| Form | Match | Why |
| --- | --- | --- |
| `All()` — no matchers | `true` | Identity of AND. "All of nothing holds" is the only composable answer. |
| `Any()` — no matchers | `false` | Identity of OR. Nothing offered, nothing satisfied. |
| `Not(nil)` | `false` | A nil sub-matcher is a caller mistake, not a logical value. |
| `All(..., nil, ...)` | `false` | Same. |
| `Any(..., nil, ...)` | `false` for the whole composite | Same — a nil entry must not be masked by a sibling that happens to match, or the mistake ships green. |

None of these panic. A testing framework reports; it does not take the suite down. This matches the
existing `MatchErrorAs` precedent (`assert/error_matchers.go:99`), which turns an unusable target
into a failed match with an explanatory message.

Each nil case names its position in the message.

## Scope

In scope: `assert/composite_matchers.go` and its tests, `Description()` on the existing matchers,
the `specs` re-exports, `docs/DSL.md`, `README.md`, `CHANGELOG.md`.

Out of scope: changing `Matcher`, any existing matcher's `Match` behaviour or message wording,
generics on `Matcher`, and `ExpectT`'s typed path.

## Delivery

Strategy `single-pr` (forecast well under the ~400 authored-line budget). TDD: enabled (repo
standing rule), runner `go test ./...` — never `-race` locally, never the workbench.

## Tasks

- [x] **T1** — Pin the semantics with failing tests: `assert/composite_matchers_test.go` covering
  `Match` truth tables, every message branch (which sub-matcher failed, indices, nesting), and the
  nil/empty table above. Check: `go test ./assert/...` fails to compile / fails RED for the right
  reason. Route: delegated (writer).
- [x] **T2** — Implement `Not`, `All`, `Any` and the `Describer` fallback in
  `assert/composite_matchers.go`; add `Description()` to the existing matchers. Check:
  `go test ./assert/...` green. Route: delegated (same writer).
- [x] **T3** — Re-export `Not`, `All`, `Any` from `specs/matcher.go` with a DSL-level test proving
  a composite failure reaches the reporter intact. Check: `go test ./...` green. Route: delegated
  (same writer).
- [x] **T4** — Document: matcher-composition section in `docs/DSL.md`, README matcher list,
  `CHANGELOG.md` Unreleased/Added entry. Check: doc examples compile as written. Route: delegated
  (docs writer).

- [x] **T5** — Review fallout from PR #225: typed-nil panic guard and the re-evaluation diagnostics.
  Check: regression tests RED first, then `go test -count=1 ./...` green. Route: delegated (writer),
  parent reproduced both defects first and verified both fixes independently.

## Acceptance criteria (from #209)

Generic negation, all/AND, any/OR combinators; messages identify the failing sub-matcher(s); message
branches have focused tests; nil/empty semantics explicitly defined.

## Progress

- Branch `feat/209-matcher-composition`, worktree
  `.claude/worktrees/issue-209-matcher-composition`, based on `develop` at `2af992d`.
- **T1–T3 done**, commit `84b6f34` (`feat(assert): add Not, All and Any matcher combinators (#209)`).
  Observed: `go build ./...` clean, `go vet ./assert/... ./specs/...` clean, `gofmt -l` empty,
  `go test -count=1 ./assert/... ./specs/...` → `ok assert 0.005s`, `ok specs 1.034s`,
  `go test ./...` → every package `ok`.
- One parent correction on top of the writer's output: `anyMatcher.Match` evaluated every entry on
  every call so it could detect a later nil. Split into a nil scan followed by a short-circuiting
  match loop — same semantics, and the common nil-free case stops at the first hit again.
- Settled message formats (pinned by tests):
  - `All: #1: "expected 2 to equal 1"; #2: "expected true, got 2 (int)"` — only failing entries listed.
  - `Any: #1: "expected 3 to equal 1"; #2: "expected 3 to equal 2"`.
  - `expected 5 not to be equal to 43` — `Not` names its sub-matcher via `Description()`.
  - `Not: sub-matcher at position 1 is nil`; `All: #2: nil matcher (never matches)`;
    `Any: no matchers were given, so nothing could match`; and, when a nil masks a sibling,
    `Any: #1: would have matched (equal to 1), but a nil entry forces this Any to fail; #2: nil matcher (never matches)`.
- **T4 done.** Added a "Matcher composition: `Not`, `All`, `Any`" section to `docs/DSL.md` (under
  `## Expect and EqualTo`, right after "Errors compare by identity, and the comparison is oriented"):
  runnable examples of `Not`/`All`/`Any`, the indexed `All`/`Any` failure-message format, why `Not`
  names its sub-matcher via `Description()` instead of quoting a `FailureMessage` that would describe
  a comparison that succeeded, the `Describer` interface and its `%T` fallback, and the full nil/empty
  table with reasoning. Added `Not`/`All`/`Any` to the two places README.md already lists matchers
  ("Rich assertions" bullet, "assert" package bullet in Architecture overview) without restructuring
  either section. Added one `### Added` entry under `## [Unreleased]` in `CHANGELOG.md`, in the style
  of the existing entries, linking issue #209. Every Go snippet was checked against
  `assert/composite_matchers_test.go`'s pinned wording and against the real `specs`/`assert`
  signatures, not invented. No `.go` file touched.
  Observed: `~/sdk/go1.26.6/bin/go build ./...` clean; `~/sdk/go1.26.6/bin/go test ./...` → every
  package `ok` (no `-race`, no workbench).

## Review round 1 (PR #225, changes requested)

Two defects were reported and **both reproduced before any fix**, in a throwaway package:

1. **Typed nil panicked.** A declared-but-unassigned pointer matcher (`var m *customMatcher`) is not
   `== nil` once inside the `Matcher` interface, so all three combinators called into a nil receiver:
   `runtime error: invalid memory address or nil pointer dereference` for `Not`, `All` and `Any`.
   That contradicted the documented guarantee that no nil case panics. The repo already had
   `assert.IsNilValue` (`assert/matcher.go:268`) doing exactly this reflect-based check for `BeNil`;
   the fix is `isNilMatcher(m) = m == nil || IsNilValue(m)`, applied at every nil comparison in the
   file. Missing it the first time was carelessness, not an exotic edge case.
2. **`FailureMessage` re-ran sub-matchers.** `Expectation.To` calls `Match` and then, on failure,
   `FailureMessage`, so composites evaluate entries twice. Observed against a matcher returning false
   then true: `All.FailureMessage` emitted `All: no sub-matcher explains the failure` — the
   "unreachable" placeholder, reachable — and `Any.FailureMessage` ran a sibling that `Match` never
   reached (calls 0 → 1) because of the nil scan.

Fixes: `Any` with a nil entry now evaluates nothing and only describes its siblings; `All`/`Any` name
an observed non-determinism instead of a placeholder that reads like a framework bug; the `Matcher`
interface now documents that implementations must be deterministic and side-effect-free.

**Rejected alternative: memoizing per-entry results in the composite.** It removes the second pass,
but makes a composite stateful — one shared across parallel specs could build its message from
another goroutine's `actual`. Trading a rare misleading message for a data race, in a repo whose CI
runs the race detector, is a bad trade.

Post-fix verification (after rebasing onto `develop` at `7ceeb68`, which carries #224):
check-go-version OK, `make fmt-check` no drift, `go build ./...` clean, `go vet ./...` clean,
`go test -count=1 ./...` every package ok, `make bench-smoke` PASS. The six new regression tests pass.
Independent parent re-reproduction after the fix: no panic in any combinator, and `Any` sibling call
count unchanged at 0 during `FailureMessage`.
