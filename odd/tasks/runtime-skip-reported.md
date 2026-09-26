# runtime-skip-reported (go-specs #254)

Branch `fix/254-runtime-skip-reported`, base `origin/develop` `1047c56` (after #255 and #257).

## Problem

A spec whose body calls `ctx.T.Skip`, `ctx.T.Skipf` or `ctx.T.SkipNow` shows `--- SKIP` in
`go test`, but the `EventReporter` receives it as passed: `SpecResultEvent.Skipped` is `false` and
`SuiteEndEvent.SkippedSpecs` does not count it. JSON, JUnit and report coordination then list a
skipped spec as passed. This is the skip counterpart of #253 (failures through `ctx.T`).

## What changes

`runSubtestGuardingParallel` (`specs/spec_body_parallel.go`) already reads `subT.Failed()` after
`t.Run` returns. It will also read `subT.Skipped()` at the same point and return it. Every engine
that runs a spec in its own subtest folds it into the reported outcome: the flat plan path
(`specs/execution_plan.go`), the `BeforeAll`/`AfterAll` group path (`specs/group_hooks.go`),
`ItParallel` (which shares the group path since #255) and the `Runner` (`specs/runner.go`).

A spec is reported `Skipped` only when its subtest skipped **and** it did not fail. A failure
followed by `SkipNow` stays `Failed`, which matches Go: that test still reports FAIL.

## Decision on the issue's open question

`SpecResultEvent.Skipped` covers both causes: a compile-time `SkipIt`/`Skip` and a runtime
`ctx.T.Skip`. Its doc comment is widened: for a runtime skip the body did start, so `Duration` may
be non-zero. Rejected: a separate `RuntimeSkipped` marker. It would add public API and a new
status, while JUnit (`<skipped>`) and `report.Status` could not show the difference anyway, and
no consumer enforces `Duration == 0` for skipped cases.

## Constraints

- Public DSL unchanged. No allocation added on the hot path (the helper's struct already holds `sub`).
- No `-race`, no workbench locally.
- TDD: strict (user CLAUDE.md), runner `go test ./specs/...`.

## Tasks

- [x] T1 RED: reporter test with one spec per mechanism (`Skip`, `Skipf`, `SkipNow`) on the flat
      path, the group path, `ItParallel` and `Runner`, plus fail-then-skip staying `Failed`,
      asserting `Skipped` and `SkippedSpecs`. Route: delegated writer (4+ files touched).
- [x] T2 GREEN: return `skipped` from `runSubtestGuardingParallel` and fold it in on every engine.
- [x] T3 Docs: `report/events.go` and `report/model.go` doc comments, CHANGELOG entry.

## Checks

- `go test ./...` (no `-race`) — PASS.
- `make fmt-check` after `git add -A` — clean, no output.
- `go vet ./...` — clean, no output.
- `go test ./specs/... -run 'Alloc|alloc' -count=1` — PASS, no new allocations.

## Progress

- Done. RED confirmed on all four engines (describe, describe-with-before-all, runner, it-parallel)
  before the fix: every runtime-skip mechanism reported `skipped=false` and
  `SUITE_FINISHED ... skipped=0`, while `fails-then-skips` already correctly reported `failed=true`.
  GREEN: `runSubtestGuardingParallel` (`specs/spec_body_parallel.go`) now also returns `subT.Skipped()`,
  threaded through `runSpecProgramIsolated`/`runSpecProgram` (`specs/execution_plan.go`), `runSpec` and
  `runParallelSpec`/`emitParallelOutcome` (`specs/group_hooks.go`), and
  `runSpecIsolated`/`runSpecRecovered`/`runSpecsRecovered` (`specs/runner.go`); each caller folds it as
  `skipped && !failed` against its own final `failed` (Context + subtest), matching go test's own
  FAIL-over-SKIP precedence for a fail-then-SkipNow spec. `specResult` (`specs/program.go`) gained a
  `Skipped` field. New test: `specs/ctx_t_skip_report_test.go`
  (`TestCtxTSkipsAreReportedAsSkippedRealProcess`, `TestRunnerFailFastDoesNotStopOnCtxTSkip`), modeled
  on #253's `ctx_t_failure_report_test.go`. Compile-time `SkipIt` regression already pinned by the
  existing `TestSpecSkipItReportsStatusSkipped` (`specs/spec_skip_pending_test.go`), left untouched and
  still green. `ItParallel` finding: `Spec.ItParallel` (issue #245/#255) runs through the group engine
  (`group_hooks.go`'s `runParallelGroup`/`runParallelSpec`), which already shares
  `runSubtestGuardingParallel` — confirmed by testing and by the existing #253 code comments; the
  older `Builder.ItParallel`/`parallelStep` path (`program.go`) uses `parallelBackend` with a nil
  `ctx.T` and was correctly left untouched (no real subtest to skip).

