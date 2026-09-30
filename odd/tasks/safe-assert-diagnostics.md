# Safe assertion diagnostics (PR A: shared safe rendering infrastructure)

Branch: `feat/safe-assert-diagnostics` (base: merge of #364 and #366; merge order #364 -> #366 -> this PR).
Workflow: ODD, one writer, strict TDD. TDD mode = strict (source: user global config), runner = `go test ./...`.
Route: delegated direct (single writer; the work touches 2+ non-trivial files and 4+ files were needed to understand it).

## Problem

Assertion failure messages render user values with `fmt` verbs (`%v`, `%#v`). `fmt` has no cycle
protection: a map or slice that contains itself overflows the stack, and no `recover` can catch that.
#364 and #366 fixed this for `Equal`'s header and for `Satisfy` / poll results, but every other matcher
(`HaveLen`, `HaveKey`, `Contain*`, `BeOneOf`, `BeBetween`, `Not`, `BeNil`, error matchers, ...) still
prints its operands with raw `%v`. The two "safe" renderers also duplicate their reference identity
code, have no size budget in the fallback, and can call user `String` / `Error` methods.

## What changes

A new file `assert/render_safe.go` holds one reference identity, one safety pre-check (cycle, NaN key
and node budget) and `formatUserValue(v, verb)`: ordinary values pass the pre-check and are printed by
`fmt` exactly as before; anything else goes to a bounded fallback that never calls user methods and
prints an explicit truncation marker when it runs out of budget. `structural_diff.go` and
`poll_render.go` keep their own limits and formats but use the shared identity and pre-check. Every
matcher message that renders a user value goes through `formatUserValue`.

## Why

Rendering a diagnostic must never crash the test binary or run user code. Doing it once, in one
place, is the only way to keep the ~40 message sites consistent. Alternative rejected: guard each
matcher separately (duplicated logic, easy to miss a site).

## Scope and constraints

- Only `assert/`, `CHANGELOG.md`, `docs/DSL.md`. `specs/` sites are a follow-up (typed `EqualTo`, `failf`, panic reports).
- No change to verdicts, number of `Match` evaluations, or matcher semantics.
- Ordinary messages stay byte-identical. Passing paths stay at 0 allocations (renderer runs on failure only).
- Deterministic ordering of NaN/tie-break keys beyond what sharing identity needs is PR B.
- About 400 authored changed lines per task is a planning heuristic only.

## Checklist

- [x] T1 Centralize reference identity and the safety pre-check. Check: identity unit tests RED (missing API) then GREEN; `go test ./assert/` unchanged-green.
- [x] T2 Bound both fallback renderers: node budget with truncation marker, no user methods, `MapRange` selection, deterministic poll key order. Check: effective-limit and no-method tests RED then GREEN.
- [x] T3 Render every matcher diagnostic through `formatUserValue` (direct and composite). Check: subprocess cycle regressions RED then GREEN, compatibility tests pin ordinary text.
- [x] T4 Docs and CHANGELOG (format changes, limits). Check: `go run ./tools/release validate -file CHANGELOG.md`.
- [x] T5 Full checks, push, draft PR. Check: test, vet, lint, gofmt, allocation contracts.

## Evidence

### T1
- Route: delegated direct (single writer), inline for the refactor.
- RED: `go test ./assert/ -run 'RefIdentity|RefPair|SafeForFmt'` -> `assert/render_safe_test.go:12:13: undefined: refIdentity`, `undefined: refKey`, `undefined: refPairOf` (build failed).
- GREEN: same command passes; full `go test ./assert/` passes unchanged (existing structural and poll tests pin the refactor).
- Removed duplicates: `diffVisit` (diffWalker.seen, deepComparer.revisit), `cycleWalk`/`needsBoundedRender`, poll `visit`/`refVisit`/`walker`.
- Commit: 500746f

### T2
- RED (`go test ./assert -run 'PollFallback|Fallback|DiffRenderer|SmallestMap'`, new API stubbed to compile): poll fallback of a shared 16-way DAG printed 21041422 bytes and no marker; struct fields uncapped (`F17` printed); NaN keys printed in iteration order (`NaN:5, NaN:1 ...` vs `NaN:2, NaN:3 ...`); `the safe fallback called user String/Error methods 20 times`; `rendering allocated 263777032 bytes for a huge string`; wide map diff render allocated 21602520 bytes; poll wide map 96362440 bytes; diff render with budget 6 had no marker.
- GREEN: all of the above pass; full `go test ./...` passes; existing structural-diff and poll tests are unchanged and pass.
- Design: `renderLimits` and `nodeBudget` are shared; the two renderers keep their formats. `smallestMapEntries` (one `MapRange` pass, window of k) replaces the full sort. Poll map keys sort by rendered text, then by the shared `compareMapEntries`. Maps over 1024 entries print their 16 smallest keys by `compareMapKeys`.
- Commit: 44675d6 (T1 commit: 500746f)

### T3
- Compatibility first: `TestOrdinaryMatcherMessagesKeepTheirText` pins 80 ordinary messages and descriptions (39 matcher cases); it passed before and after the change.
- RED (`formatUserValue` stubbed as raw `fmt.Sprintf`): `TestMatcherMessagesTerminateOnCyclicValues/{map,slice,view}` -> `child did not finish (exit status 2); last case started: "CASE Equal"` with `fatal error: stack overflow` (the pointer kind does not overflow because fmt prints nested pointers as addresses); `TestMatcherFallbackNeverCallsUserMethods` -> `a cyclic value's String or Error method was called 26 times`; `TestMatcherMessagesBoundHugeAcyclicValues` -> `BeNil: message is 320829 bytes`, `BeOneOf: message is 641627 bytes`, and the same for Contain, Not, HaveLen.
- GREEN: all pass; `go test ./...`, `go vet ./...`, `golangci-lint run ./...` clean.
- Route: `formatUserValue` / `userValue` applied at every `%v` of a user value in matcher.go, general, length, map, collection, order, composite (Not), error matchers, and PollResult.Err/Panic/Last. `renderBounded` was folded into `formatUserValue`.
- Commit: 2d3efd9

### T4
- `docs/DSL.md`: new section "Failure messages render values safely"; `CHANGELOG.md` [Unreleased] > Fixed.
- Check: `go run ./tools/release validate -file CHANGELOG.md` -> `release validate: CHANGELOG.md [Unreleased] structure is valid`.
- Commit: aa32a1f

### T5
- Checks: `go test ./...` (27 packages ok, no -race), `go test ./assert -count=5` ok, `go vet ./...`, `golangci-lint run ./...` (0 issues), `gofmt -l .` empty, `release validate` ok; allocation contract tests in `specs/` (13 Alloc tests) pass at 0 allocs.
- Follow-ups (not in this PR): `specs/context.go` typed `EqualTo`/`ToEqual`, `specs/failure.go` failf, `specs/panic_report.go`, `specs/context_go.go`, `specs/context_cleanup.go` panic values; deterministic order when the tie-break budget runs out (PR B).
- Next step: review and merge in order #364 -> #366 -> this PR.
