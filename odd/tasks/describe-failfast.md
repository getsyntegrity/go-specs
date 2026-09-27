# describe-failfast (go-specs #251, spec 1 of 2)

Branch `feat/251-describe-failfast`, base `origin/develop` `ff9e0f6`.

## Problem

`Runner.FailFast` (`specs/runner.go`) stops a run after the first step that records a failure, but
it exists only on the `Builder`/`Program`/`Runner` compatibility engine. A user of the canonical
`specs.Describe` + `*Spec` engine (`CompiledSuite` over `ExecutionPlan`) cannot opt into fail-fast.
`docs/EXECUTION_ENGINES.md` lists it as Stage 4 item 3, one of the last two reasons `Builder` cannot
be retired. Sharding (item 4) is spec 2 of this issue, a follow-up: `describe-shard`.

## What changes

`CompiledSuite` gets `SetFailFast(bool)`. `Describe` users opt in through
`BuildSuite(...)`, `suite.SetFailFast(true)`, `suite.Run(t)`. Semantics match `Runner.FailFast`:

- A sequential spec that fails still runs its own `AfterEach`; after it, no further spec or group
  starts. Specs that never start are not reported, the same as `Runner`.
- An `ItParallel` batch that already started runs every sibling to completion; the stop applies
  after the batch.
- A `BeforeAll` or `AfterAll` failure counts as a failure (H9). The `AfterAll` of every group that
  was already entered still runs (H9).
- Filtered (`-run`) and skipped specs are not failures.

## Why a method, not an exported field

`CompiledSuite` already fills its 64-byte allocation size class, pinned by
`TestGroupHookStorageAddsNoBytesToAlwaysAllocatedStructs` (H10). An exported `FailFast bool` field,
the shape `Runner` uses, would push every suite into the 80-byte class and cost 16 bytes per suite
whether or not it uses fail-fast. The flag instead lives behind the existing lazily allocated
`groups *planGroups` pointer, which already holds the optional skipped/pending/parallel bookkeeping.
A suite that never calls `SetFailFast` keeps that pointer nil. Rejected: an exported field (breaks
H10), and putting the flag on `ExecutionPlan` (pinned at exactly 192 bytes).

## Constraints

- Public DSL unchanged; the change is additive.
- Zero cost when unused: `TestDescribeEngineLoopAllocatesNothingPerSpecOnTheFlatPath` stays green,
  `ExecutionPlan` stays 192 bytes, `CompiledSuite` stays within 64.
- No `-race`, no workbench locally.
- TDD: strict (source: user `~/.claude/CLAUDE.md`), runner `go test ./specs/...`.
- Delivery strategy: `ask-on-risk`; forecast ~300-400 authored lines, one PR.

## Tasks

- [x] T1 `SetFailFast` + flat path: RED tests (failing spec stops later specs, its `AfterEach`
      runs, unreported rest, no stop without the option, filtered/skipped do not trigger), then
      GREEN. Check: `go test ./specs/...`, size and allocation pins. Route: delegated writer
      (2+ non-trivial files).
- [x] T2 group path: RED tests (later groups cut, entered `AfterAll` runs, `BeforeAll`/`AfterAll`
      failure triggers, `ItParallel` siblings finish then stop), then GREEN. Check: `go test
      ./specs/...`. Route: same delegated writer.
- [ ] T3 docs: `docs/EXECUTION_ENGINES.md` capability table and Stage 4, H9 wording in
      `docs/SUITE_HOOKS_CONTRACT.md`, `CHANGELOG.md` Unreleased. Check: `make fmt-check`, `go vet`.

## Progress

- Engram mirror `odd/describe-failfast/tasks`: pending (engram reported multiple active sessions).
- T1 done. `CompiledSuite.SetFailFast(bool)` added (specs/execution_plan.go), stored behind the
  existing lazily-allocated `groups *planGroups` pointer (new `planGroups.failFast` field, no new
  `CompiledSuite`/`ExecutionPlan` field — H10 intact). `runPlanSpecsInOrder`/`runExecution` now
  thread a `failFast bool` and `runExecution` returns whether the spec failed, so the flat path
  stops at the next spec after a failure without touching allocation shape. Tests:
  `specs/compiled_suite_failfast_test.go` (RED: compile failure, `suite.SetFailFast undefined`;
  GREEN after implementation). `go test ./specs/...` green;
  `TestDescribeEngineLoopAllocatesNothingPerSpecOnTheFlatPath`,
  `TestGroupHookStorageAddsNoBytesToAlwaysAllocatedStructs`,
  `TestSuiteWithoutGroupHooksAllocatesNoGroupStorage` all green.
- T2 done. `planGroups.failFast` (new bool field, no size pin on `planGroups` itself) plus
  `groupRun.failFastStopped`/`markFailFast()` (specs/group_hooks.go): a plain, non-atomic bool set
  only from the calling goroutine (after an `ItParallel` batch is `wg.Wait()`ed, never from a
  sibling's own goroutine), checked at the top of every `runRange` loop iteration so a stop set
  anywhere unwinds every enclosing scope without a `Goexit`/`FailNow` — a group already entered
  still runs its own `AfterAll` (defers), and a group never entered never starts. Wired into
  `runSpec` (both `r.real` and fake-backend paths), `runBeforeAlls`' actual-failure branch,
  `reportAfterAll`, the cleanup-only-failure branch in `runGroup`, and `runParallelGroup` (decided
  after the whole batch has finished). Tests: `specs/group_hooks_failfast_test.go` (RED: compile
  failure, `suite.SetFailFast undefined`; GREEN after implementation) — later
  groups/specs cut, entered `AfterAll` runs, `BeforeAll`/`AfterAll` failure triggers the stop, an
  `ItParallel` batch runs every sibling to completion before the stop applies (real-process
  subprocess test, since a real per-spec subtest failure would otherwise mark the outer test
  failed). `go test ./...` green.

## Next step

T3.
