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

- [ ] **T1** — Pin the semantics with failing tests: `assert/composite_matchers_test.go` covering
  `Match` truth tables, every message branch (which sub-matcher failed, indices, nesting), and the
  nil/empty table above. Check: `go test ./assert/...` fails to compile / fails RED for the right
  reason. Route: delegated (writer).
- [ ] **T2** — Implement `Not`, `All`, `Any` and the `Describer` fallback in
  `assert/composite_matchers.go`; add `Description()` to the existing matchers. Check:
  `go test ./assert/...` green. Route: delegated (same writer).
- [ ] **T3** — Re-export `Not`, `All`, `Any` from `specs/matcher.go` with a DSL-level test proving
  a composite failure reaches the reporter intact. Check: `go test ./...` green. Route: delegated
  (same writer).
- [ ] **T4** — Document: matcher-composition section in `docs/DSL.md`, README matcher list,
  `CHANGELOG.md` Unreleased/Added entry. Check: doc examples compile as written. Route: delegated
  (docs writer).

## Acceptance criteria (from #209)

Generic negation, all/AND, any/OR combinators; messages identify the failing sub-matcher(s); message
branches have focused tests; nil/empty semantics explicitly defined.

## Progress

- Branch `feat/209-matcher-composition`, worktree
  `.claude/worktrees/issue-209-matcher-composition`, based on `develop` at `2af992d`.
