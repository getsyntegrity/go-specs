# Focus, skip and pending on `*Spec` (issue #245, spec 1 of 2)

## Objective

Give the canonical `specs.Describe` + `*Spec` surface the same `FIt`, `SkipIt` and `PendingIt`
that today only exist on `Builder`, with the same observable results, so users of the documented
entry point can focus, skip and park specs without switching engines.

## Problem

Issue #245: `FIt`, `SkipIt`, `PendingIt` and `ItParallel` exist only on `Builder`
(`specs/builder.go:223-262`), while `BeforeAll`/`AfterAll` exist only on `Spec`
(`specs/spec.go:359,375`). The README presents `Describe`/`*Spec` as the API, so a user following
the docs cannot focus or skip a spec at all.

`docs/EXECUTION_ENGINES.md` (decision from #176) already classifies the `Describe` engine
(`ExecutionPlan` + `CompiledSuite`) as canonical and `Builder`/`Program`/`Runner` as a
compatibility surface. Its Stage 4 says the gap is closed by moving these capabilities onto `Spec`,
not the other way round. This change is item 1 of that stage.

## What changes

Three new methods on `*Spec`, with the exact signatures `Builder` already uses:

```go
func (s *Spec) FIt(name string, fn func(*Context))
func (s *Spec) SkipIt(name string, fn func(*Context))
func (s *Spec) PendingIt(name string, fn func(*Context))
```

Semantics copy `Builder.finalize` (`specs/builder.go:306`), because the point is one behavior with
two entry points:

- `SkipIt` and `PendingIt` never compile or run `fn` (it may be nil). The spec's name is kept and
  reported as skipped or pending respectively, using the statuses that already exist in
  `report/model.go` (`StatusSkipped`, `StatusPending`).
- `FIt` with a nil `fn` is a no-op. If a `Describe` tree contains at least one `FIt`, only focused
  specs are compiled; every other `It`, `SkipIt` and `PendingIt` in that tree is dropped, the same as
  `Builder`. Focus is scoped to one `Describe` call, exactly as it is scoped to one `Builder` today.
  Hooks (`BeforeEach`/`AfterEach`, `BeforeAll`/`AfterAll`) around a focused spec still run.

Rejected alternative: reporting unfocused specs as `StatusFiltered` instead of dropping them. It
is arguably more informative, but it would make the two engines disagree on the same tree, which is
the divergence this issue exists to remove. If that behavior is wanted, it should change on both
engines in its own issue.

## Constraints

- Public DSL stays stable (`AGENTS.md`); this is additive only.
- Zero cost when unused: the flat path must stay allocation-free per spec
  (`TestDescribeEngineLoopAllocatesNothingPerSpecOnTheFlatPath`,
  `specs/allocation_contract_test.go`), and the opt-in-cost invariant H10 in
  `docs/SUITE_HOOKS_CONTRACT.md` applies to any new per-node metadata.
- Both `Spec` build paths (the bytecode compiler used by `describeWithCompiler` and the
  `Analyze`/registry path) must agree.
- No `-race` and no workbench runs locally (user rule); CI runs `-race`.

## Scope

In: `FIt`, `SkipIt`, `PendingIt` on `*Spec`; equivalence tests against `Builder`; allocation and
benchmark evidence; docs.

Out, and where it goes:

- `ItParallel` on `*Spec`: follow-up spec 2 (separate PR). It carries the determinism and
  concurrency risk, so it is reviewed on its own. **Spec 2's PR closes #245; this PR only
  references it.**
- `FailFast` and `RunShard` are also `Builder`-only, but #245 does not mention them. Proposed as a
  separate issue (not opened without the user's confirmation).

## Tasks

TDD mode: strict (source: user global config `~/.claude/CLAUDE.md`, "Strict TDD Mode: enabled").
Runner: `go test ./...` (targeted runs with `go test -count=1 -run <Name> ./specs/`).

- [x] **T1 — `SkipIt` and `PendingIt` on `*Spec`.** Skipped/pending specs never run their body,
  appear in `go test` output as skipped, and emit `StatusSkipped`/`StatusPending` report events
  like `Builder`. Check: new tests RED then GREEN; `go test ./...`.
  Done: both build paths (bytecode compiler and Analyze/registry) implemented; marks (`specMark`)
  live behind the existing lazily allocated `planGroups` pointer (H10), reported once per suite run
  via `reportMarks`, independent of whether the suite has any `BeforeAll`/`AfterAll` group.
  `CompiledSuite.Run`'s early-return guard and `specCounter`/`SuiteEndEvent.PendingSpecs` were also
  fixed to account for a suite made only of marks and for pending tallying, which the
  ExecutionPlan/CompiledSuite engine never did before (report/model.go's `StatusPending` existed
  since #208 but only Builder populated it).
- [x] **T2 — `FIt` on `*Spec`.** Focus filters the whole `Describe` tree as `Builder.finalize`
  does; nil `fn` is a no-op; hooks around focused specs run. Check: new tests RED then GREEN;
  `go test ./...`.
  Done. Note: `FIt`'s implementation (`applyFocusFilter`, `EmitFocusedIt`, `arenaHasFocus`, the
  `itKind`/`Kind` plumbing) landed together with T1's commit because skip/pending/focus share the
  same infrastructure (`planGroups`, the arena `Kind` field, `remapHookGroups`) end to end — "one
  behavior, two entry points" made the three hard to build as fully separate slices without
  duplicating that plumbing. To still get a genuine T2 RED, `Spec.FIt` was temporarily stubbed to a
  no-op, the new focus tests were run and observed failing for the real reason (unfocused specs ran,
  focused ones didn't filter anything), then the stub was reverted and the tests re-run GREEN. See
  Progress row below for the exact commands.
  **Semantics decision (BeforeAll/AfterAll of a group emptied by focus):** a group whose only specs
  are filtered out by a focus elsewhere in the suite is never entered — neither its `BeforeAll` nor
  its `AfterAll` runs. This is not a new rule: it is exactly H3
  (`docs/SUITE_HOOKS_CONTRACT.md`), "a group with zero runnable specs is never entered", already
  applied to "declares no `It`" — focus filtering is just another way a group's runnable-spec count
  can reach zero before it is entered, so the same test that decides entry (does this group's spec
  range contain at least one plan entry) answers both cases uniformly. Pinned by
  `TestSpecFItGroupWithZeroSpecsAfterFocusIsNeverEntered` and its converse,
  `TestSpecFItGroupSurvivingFocusIsStillEntered` (a group with at least one focused spec left is
  still entered, hooks run once around it).
- [x] **T3 — Equivalence with `Builder`.** Table of identical trees declared through both engines
  (mixed `It`/`FIt`/`SkipIt`/`PendingIt`, nested `Describe`/`When`) asserting the same set of
  (full name, status) outcomes and the same executed bodies. Check: `go test -count=1 -run
  Equivalence ./specs/`.
  Done: 3 tree shapes (flat mix, focused, nested with hooks) × 3 build paths (compiler, registry,
  Builder) in `spec_builder_equivalence_test.go`. Deliberately compares the *set* of (full name,
  status) outcomes, not event order: Builder's own `finalize` attaches a buffered skip/pending mark
  to whichever coalesced group closes next, so even Builder-only reporting order does not always
  match declaration order — see the feature doc's semantics section and the test file's header
  comment. `BeforeAll`/`AfterAll` are out of this table: they exist only on `*Spec`
  (`docs/EXECUTION_ENGINES.md`), so Builder has nothing to compare them against.
- [ ] **T4 — Performance evidence.** Allocation contract tests pass unchanged; benchmark of the
  flat `Describe` path on `origin/develop` vs this branch recorded here. Check:
  `go test -count=1 -run Alloc ./specs/` and the recorded benchmark.
- [ ] **T5 — Docs.** README DSL section, `docs/EXECUTION_ENGINES.md` Stage 4 item 1 marked done,
  CHANGELOG entry referencing #245. Check: `make fmt-check`, `go vet ./...`.

## Acceptance criteria

1. A user of `specs.Describe` can call `s.FIt`, `s.SkipIt`, `s.PendingIt`.
2. For the same tree, `Spec` and `Builder` produce the same outcomes (T3 is the proof).
3. A suite that uses none of them allocates and performs as before (T4 is the proof).

## Delivery

Strategy: `ask-on-risk`. Base `develop`, branch `feat/245-spec-focus-skip-pending`, one PR that
references (does not close) #245. Push and PR are the user's decision.

## Progress

Worktree `.claude/worktrees/issue-245-spec-focus-skip-pending`, based on `origin/develop` `c810233`.

| Task | Route | Trigger evidence | Commit | Checks |
| --- | --- | --- | --- | --- |
| T1 | delegated writer | 2+ non-trivial files (spec.go, compiler.go, execution_plan.go, group_hooks.go, arena.go, registry.go, tests) | d0ff646 | RED: compile failure (`s.SkipIt undefined`) on `spec_skip_pending_test.go`. GREEN: `go test -count=1 -run 'TestSpecSkipIt\|TestSpecPendingIt' ./specs/` all PASS. `make fmt-check`: clean. `go vet ./...`: clean. `go test ./...`: all packages ok. |
| T2 | delegated writer | same files (FIt's implementation shipped with T1; see note above), + spec_focus_test.go | 4caa637 | RED (via temporary `Spec.FIt` stub): `go test -count=1 -run TestSpecFIt -v ./specs/` — 7 of 9 new tests FAIL, e.g. `TestSpecFItOnlyFocusedRuns_CompilerPath: ran = [plain], want only [focused]`. GREEN (stub reverted): `go test -count=1 -run TestSpecFIt -v ./specs/` all PASS. `go vet ./specs/`: clean. `go test ./...`: all packages ok. |
| T3 | delegated writer | spec_builder_equivalence_test.go (new, 2 non-trivial trees across 3 engines) | (pending commit) | RED (test-authoring bug, not implementation): first draft's `want` maps used bare leaf names instead of full breadcrumbs (`"a"` instead of `"suite/a"`); `go test -count=1 -run Equivalence -v ./specs/` failed identically on all 3 engines with "missing outcome for a, want passed" / "unexpected outcome for suite/a". GREEN: fixed `want` keys, same command all PASS. `make fmt-check` (after `make fmt`): clean. `go vet ./...`: clean. `go test ./...`: all packages ok. |
| T4–T5 | delegated writer | same files, continuing | — | pending |

## Next step

T1, T2, T3 done. Continue with T4 (performance evidence).
