# describe-shard (go-specs #251, spec 2 of 2)

Branch `feat/251-describe-shard`, base `origin/develop` `5825435` (spec 1, `CompiledSuite.SetFailFast`,
merged as #264).

## Problem

`RunShard`/`RunShardWithReporter` (`specs/scheduler.go`) split a suite across CI jobs, but only for
a `*Program` built with `Builder`. A user of the canonical `specs.Describe` + `*Spec` engine
(`CompiledSuite` over `ExecutionPlan`) cannot shard. `docs/EXECUTION_ENGINES.md` lists this as
Stage 4 item 4, the last capability that still requires `Builder`. `Builder`/`Runner` and their
API stay exactly as they are.

## Decision

1. **Public API.** `func (s *CompiledSuite) RunShard(tb testing.TB, shardIndex, shardCount int)`.
   It reports through the suite's existing `Reporter` field exactly as `Run` does, so one method
   covers both the plain and the reporter case (`BuildSuite` + set `Reporter`, or build through the
   reporter-aware entry points). Nothing is stored on the suite: the shard's selection is computed
   inside `RunShard` and dropped when it returns. `Run` is unchanged. Callers pick the shard with
   the existing `ShardFromArgsOrEnv`/`ParseShardEnv`/`ParseShardFlag`.
2. **Assignment unit.** A unit is, in declaration order: a whole top-level `BeforeAll`/`AfterAll`
   group (with every nested group inside it), a whole `ItParallel` batch outside such a group, or a
   single spec otherwise. Hook groups stay together because their `BeforeAll`/`AfterAll` run once for
   all their specs (H1-H6). An `ItParallel` batch stays together because it is one concurrent unit
   and H9's "the batch finishes before a stop" is defined over it; a batch never crosses a hook
   group boundary (`planGroups.parallel` doc comment), so this is always well-formed. A spec outside
   both has its own `BeforeEach`/`AfterEach` compiled into its own instruction range and shares no
   state, so it is sharded on its own. That is finer than `RunShard` on `Builder`, which can only
   shard coalesced hook groups, and it balances shards better. Rejected: whole-top-level-`Describe`
   units (one big `Describe` would land on one shard).
3. **Deterministic rule.** Units are numbered `u = 0..U-1` in declaration order; unit `u` runs on
   shard `u % shardCount`. The same declared tree always yields the same units and the same
   assignment, on both compile paths. This is the round-robin rule `ShardSpecs`, `ShardBCProgram`
   and `RunShard` already use. Rejected: hashing spec paths, which keeps assignments stable when a
   spec is inserted but balances poorly on small suites and must break ties for duplicate names.
4. **FailFast.** `SetFailFast(true)` still applies, and a failure stops only the shard that is
   running: other shards are other processes and never see it.
5. **Reporting.** Specs of this shard run and report exactly as under `Run`, including
   `Filtered` when `-run` discards one. Specs of other shards are never started and emit no event,
   so `SuiteEndEvent.TotalSpecs` counts only this shard's specs, the same as
   `RunShardWithReporter`. Compile-time skipped/pending marks (`SkipIt`/`PendingIt`) have no plan
   index; shard 0 reports them and no other shard does, so the union of all shards reports each
   one exactly once. An invalid `shardIndex`/`shardCount` fails the test through `tb.Fatalf` with
   the shared `validateShardPartition` diagnostic (#174); a valid shard that draws nothing runs
   nothing, which is not a failure.
6. **Report coordination (#143/#145/#146).** Its "shard" is one `go test` package process's
   result file, not a subset of specs; the two meanings are unrelated. Nothing here changes it.
   Known limit: when CI runs the same package on several `RunShard` jobs, each job publishes its own
   package result, and merging those per-job results is not handled by `report/coordination`
   today. That is documented, not implemented, in this change.

## Constraints

- Additive only; public DSL and `Builder`/`Runner` API unchanged.
- Zero cost when unused: `TestDescribeEngineLoopAllocatesNothingPerSpecOnTheFlatPath` stays green,
  `ExecutionPlan` stays 192 bytes, `CompiledSuite` stays within 64; no shard metadata on the
  common path.
- TDD: strict (source: user `~/.claude/CLAUDE.md`), runner `go test ./specs/...`. RED must fail by
  behavior: a stub `RunShard` that runs the whole suite is committed only together with GREEN.
- `-race`: run once at the end, explicitly requested by the user for this change.
- Delivery: one PR against `develop`, closing #251 (spec 1 merged as #264).

## Tasks

- [x] T1 Contract tests + implementation, flat path: exact coverage across shards, no duplicates,
      stable assignment, reporter totals, skip/pending marks only on shard 0, invalid config fails,
      empty shard, `-run` filtering, `FailFast` per shard. RED against a stub, then GREEN. Check:
      `go test ./specs/...` plus size and allocation pins. Route: delegated writer.
      Evidence: a stub `RunShard` (validates config, then ignores the computed selection and runs
      the whole suite like `Run`) was committed nowhere — it only ever existed in the working tree
      between commits — and every new test in `specs/compiled_suite_shard_test.go` failed against
      it by observed behavior (duplicated specs across shards, wrong per-shard totals, marks
      reported on every shard, an "empty" shard still emitting suite events, `FailFast` on shard 0
      leaking into shard 1). After restoring the real `s.buildShardSelection(...)` call, the same
      tests are GREEN, plus `go vet`, `make fmt-check`, and the full `go test ./...`.
      Commit: `658ab98` `feat(specs): add RunShard to CompiledSuite for the Describe engine (#251)`.
- [x] T2 Group path: hook-group integrity (a `BeforeAll`/`AfterAll` group, including nested ones,
      lands whole on one shard with its hooks run once), `ItParallel` batch integrity, reporter and
      `FailFast` on this path. RED then GREEN. Route: same writer.
      Evidence: the group-path implementation (`runTopRange`, `buildShardSelection`'s group branch)
      landed together with T1, so this task is tests-only. `specs/group_hooks_shard_test.go`'s tests
      were run against the same RED stub as T1 before it was replaced — hook groups split across
      shards, `BeforeAll`/`AfterAll` running once per shard instead of once total, `ItParallel`
      batch members scattered across shards — then GREEN once the stub was removed.
      Commit: `195208d` `test(specs): cover the group path of CompiledSuite.RunShard (#251)`.
- [x] T3 Docs: `docs/DSL.md` sentence (a sequential failure stops before the next spec; an
      `ItParallel` batch finishes before the stop), `docs/EXECUTION_ENGINES.md` capability table
      and Stage 4, sharding rule in `docs/SUITE_HOOKS_CONTRACT.md`, `CHANGELOG.md` Unreleased.
      Check: `make fmt-check`, `go vet ./...`.
      Commit: `a451c3b` `docs: document sharding on the Describe engine (#251)`.

## Verification (foreground, run after T3)

- `make fmt-check`: clean (no output).
- `go build ./...`: clean.
- `go vet ./...`: clean.
- `go test -count=1 ./...`: all packages `ok`.
- `go test ./specs/ -run 'TestDescribeEngineLoopAllocatesNothingPerSpecOnTheFlatPath|TestGroupHookStorageAddsNoBytesToAlwaysAllocatedStructs|TestSuiteWithoutGroupHooksAllocatesNoGroupStorage|Shard' -count=1 -v`: all PASS.
- `go test -race ./...`: all packages `ok`, run once as explicitly requested for this change.

## Progress

- Engram mirror `odd/describe-shard/tasks`: via `engram save` CLI (MCP session ambiguous).

## Next step

Done. Ready for review/PR against `develop` (spec 1 already merged as #264).
