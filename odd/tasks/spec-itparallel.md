# `ItParallel` on `*Spec` (issue #245, spec 2 of 2)

## Objective

Give the canonical `specs.Describe` + `*Spec` surface an `ItParallel`, so the last spec-declaration
capability that only `Builder` had is reachable from the documented entry point. This change closes
#245. Spec 1 (`FIt`/`SkipIt`/`PendingIt`, PR #250) is the base of this branch.

## Problem

`Builder.ItParallel` (`specs/builder.go:262`) runs consecutive parallel specs together on the
worker substrate (`parallelStep`, `specs/program.go:231`). `*Spec` has no way to do this.
`docs/EXECUTION_ENGINES.md` Stage 4 item 2 asks for "`ItParallel` equivalent on the plan model,
reusing the existing worker substrate".

The two engines treat a spec differently. On `*Spec` every spec is its own Go subtest
(`runSpecProgramIsolated`, `specs/execution_plan.go:427`): it has a subtest name, can be selected
with `-run`, is reported `Filtered` when `-run` excludes it, and its body gets a real `ctx.T`. On
`Builder` a parallel group runs under the parent test with `ctx.T == nil` and no per-spec subtest.

## Decision: one subtest per parallel spec

The user chose to keep `*Spec`'s per-spec guarantees inside a parallel group. Each `ItParallel`
spec runs in its own `t.Run`, started concurrently from the worker substrate, with its own pooled
`*Context`. This is valid without `t.Parallel()`: the `testing` package allows `t.Run` to be called
from several goroutines at once, provided all calls return before the parent test function returns.

Why: with the `Builder` shape, a parallel spec could not be selected or excluded with `-run`, IDE
"run test" buttons that rely on the subtest name would not work, and `ctx.T.TempDir()` or
`ctx.T.Cleanup()` would panic only in parallel specs. With a subtest per spec each failure is
reported from its own `t`, in its own goroutine, with the frame still alive, so `Helper()`
attribution works normally.

Rejected alternative: reusing `parallelStep` exactly as `Builder` does (no per-spec subtest,
`ctx.T == nil`, failures aggregated with `Errorf` on the parent). It is simpler and closer to
`Builder`, but it would make parallel specs a silent exception to `*Spec`'s contract, which is the
kind of divergence #245 exists to remove. The (full name, status) outcomes still match `Builder`;
the difference is that `ctx.T` is non-nil and `go test -v` line order may vary.

Also rejected: `t.Parallel()`. `specs/spec_body_parallel.go` explains why it breaks the runner's
`*Context` model (#172). The guard that rejects `ctx.T.Parallel()` inside a spec body stays active,
including inside `ItParallel`.

## Semantics

- `func (s *Spec) ItParallel(name string, fn func(*Context))`, same signature as `Builder`. A nil
  `fn` follows `It`'s existing nil handling on `*Spec`.
- Consecutive `ItParallel` specs form one parallel group. Any other kind (`It`, `FIt`, `SkipIt`,
  `PendingIt`) ends the group, as in `Builder`. A group also ends at a `BeforeAll`/`AfterAll` group
  boundary, which `Builder` does not have.
- `BeforeEach`/`AfterEach` run per parallel spec, on that spec's own `Context`, around its body.
- A `BeforeAll`/`AfterAll` group that contains parallel specs runs its `BeforeAll` once, before all
  of them start, and its `AfterAll` once, after all of them finish.
- With focus: an `ItParallel` is dropped when any `FIt` exists in the tree, like an unfocused `It`.
  No `FItParallel` is added (`Builder` has none).
- Every spec in a group runs to completion; there is no fail-fast inside a group (`FailFast` on
  `*Spec` is issue #251).

## Concurrency conditions (from the user)

1. **Reporter events are serialized.** `program.go` warns that not every `EventReporter` is safe for
   concurrent use. Events from parallel specs go through a mutex or a single goroutine, never
   directly from workers.
2. **Results are reported in declaration order.** The group buffers its per-spec results and emits
   `SpecResultEvent`s in declaration (index) order once the group finishes, so JUnit/JSON reports
   are deterministic and match `Builder`, even when `go test -v` lines appear in completion order.
3. **Documented limit.** `ctx.T.Setenv`, and anything else that mutates process-global state, is not
   safe inside `ItParallel`. Go only detects this when `t.Parallel()` is used, which it is not
   here, so the misuse would not be caught.

## Constraints

- Additive only; public DSL stays stable (`AGENTS.md`).
- The non-parallel flat path is unchanged: `TestDescribeEngineLoopAllocatesNothingPerSpecOnTheFlatPath`
  stays green, and H10 size pins (`docs/SUITE_HOOKS_CONTRACT.md`, `group_hook_cost_test.go`) hold.
  Any parallel bookkeeping lives behind a lazily allocated pointer. The parallel group itself may
  allocate (goroutines, `WaitGroup`), like `parallelStep`.
- Both `*Spec` build paths (bytecode compiler and `Analyze`/registry) agree.
- `-race`: not run locally by default (user rule), except the stability test below, which the user
  explicitly asked to run with `-race -count=20`. CI runs `go test -race ./...` on every PR.

## Tasks

TDD mode: strict (source: user global config `~/.claude/CLAUDE.md`). Runner: `go test`.

- [x] **T1 — Registration and execution.** `Spec.ItParallel` on both build paths (new `itParallel`
  kind next to `itFocus`/`itSkip`/`itPending`); grouping rules above; concurrent `t.Run` per spec
  from the worker substrate with its own `Context`; real `ctx.T`; per-spec
  `BeforeEach`/`AfterEach`; dropped under focus; `ctx.T.Parallel()` guard still fires. Tests
  prove the specs actually overlap in time. Check: RED then GREEN, `go test ./...`.
  Design: `planGroups.parallel []parallelRange{Start,End}` (group_hooks.go), the same lazily
  allocated pointer skip/pending/hook groups already share (H10). Both compile paths end an open
  run at *every* Describe/When scope transition (stricter than "ends at a BeforeAll/AfterAll
  boundary", never merges what that rule would keep apart) so a range never straddles a hookGroup.
  `groupRun.runRange` looks up `parallelByStart` alongside its existing hookGroup children; a match
  launches `runParallelGroup`, which spawns one goroutine per spec calling `t.Run` concurrently,
  waits, then emits buffered SpecStarted/SpecFinished pairs in declaration order (serialized, since
  emission happens on one goroutine after `wg.Wait()`). Without a real `*testing.T` it runs the
  range sequentially via the existing `runSpec`.
- [x] **T2 — Reporting and `-run`.** Serialized reporter events; `SpecResultEvent`s in declaration
  order after the group. Tests: `-run` selecting a single parallel spec; `-run` matching none
  reports them `Filtered`; reporter event order stable across runs. Check: RED then GREEN;
  `go test -race -count=20 -run <stability test> ./specs/` (explicitly requested by the user).
  `TestSpecItParallel_ReporterOrderStability`: `go test -race -count=20 -run
  TestSpecItParallel_ReporterOrderStability ./specs/` — 20/20 pass, no race.
- [x] **T3 — Hooks and attribution.** A `BeforeAll` wrapping parallel specs runs once, before all of
  them, and `AfterAll` once after all; a failing assertion inside `ItParallel` is attributed to the
  user's line (added to the attribution fixture, `specs/testdata/attribution`). Check: RED then
  GREEN.
- [x] **T4 — Equivalence and performance.** Extend `specs/spec_builder_equivalence_test.go` with
  `ItParallel` trees (same (full name, status) outcomes as `Builder`); allocation contracts pass
  unchanged; flat `Describe` benchmark on base vs branch recorded here. Check:
  `go test -count=1 -run 'Equivalence|Alloc' ./specs/` and the recorded benchmark.

  `TestItParallelEquivalence_MixedTree` (compiler path, registry path, Builder) — same
  `(full name, status)` outcomes for a mixed It/ItParallel/SkipIt/PendingIt tree nested in a `When`.
  RED (compile failure, checked out against base `3734d4f`) → GREEN.

  Benchmark: `go test ./benchmarks -run='^$' -bench='^BenchmarkDescribeVariant_Describe$' -benchmem
  -count=6`, base `3734d4f` (temporary detached worktree, removed after) vs this branch
  (`a1a22dc`), compared with `benchstat`:

  | | base (3734d4f) | branch | delta |
  | --- | --- | --- | --- |
  | B/op | 52.71Ki (53975-53978 B/op) | 52.71Ki (53975-53977 B/op) | ~0%, p=0.870 |
  | allocs/op | 226.0 | 226.0 | 0 (all samples equal) |
  | sec/op | 64.00µ ± 14% | 66.60µ ± 15% | ~ (p=0.589, noise) |

  A suite without `ItParallel` allocates and performs identically to base: H10 holds.
- [x] **T5 — Docs.** `docs/DSL.md` (`ItParallel` on `Spec`, the `ctx.T` difference from `Builder`,
  the `Setenv`/global-state limit), `docs/EXECUTION_ENGINES.md` Stage 4 item 2 done plus capability
  table, README (line ~211 says "`ItParallel` via the Builder"), CHANGELOG. Check: `make fmt-check`,
  `go vet ./...`.

## Acceptance criteria

1. `s.ItParallel` runs consecutive parallel specs concurrently, each as its own named subtest.
2. `-run` selects or filters individual parallel specs like any other `*Spec` spec.
3. Reporter output is deterministic (declaration order) and serialized.
4. Outcomes match `Builder` for the same tree (T4).
5. A suite without `ItParallel` allocates and performs as before (T4).

## Delivery

Branch `feat/245-spec-itparallel`, stacked on `feat/245-spec-focus-skip-pending` (PR #250, still
open). The PR targets that branch until #250 merges, then retargets `develop`. This PR's body
carries `Closes #245`. Push and PR are the user's decision.

## Progress

Worktree `.claude/worktrees/issue-245-spec-itparallel`, based on `3734d4f` (head of #250).

| Task | Route | Trigger evidence | Commit | Checks |
| --- | --- | --- | --- | --- |
| T1 | delegated writer | 2+ non-trivial files (arena.go, compiler.go, execution_plan.go, group_hooks.go, spec.go, tests) | 475642d | RED (compile failure) → GREEN; `go test ./...` all green; `make fmt-check`/`go vet ./...` clean |
| T2 | delegated writer | same | 0ebf9e5 | RED (compile failure) → GREEN; `go test -race -count=20 -run TestSpecItParallel_ReporterOrderStability ./specs/` 20/20 pass; `go test ./...` green |
| T3 | delegated writer | same | a1a22dc | RED (order-count bug caught by real run) → GREEN; `TestAssertionSourceAttribution` green with ItParallel added; `go test ./...` green |
| T4 | delegated writer | same | 51e9077 | RED (compile failure, checked out against base) → GREEN; `go test -count=1 -run 'Equivalence|Alloc' ./specs/` green; benchmark recorded (no regression) |
| T5 | delegated writer | docs only (DSL.md, EXECUTION_ENGINES.md, README.md, CHANGELOG.md) | 3966165 | `make fmt-check` clean, `go vet ./...` clean, `golangci-lint run ./specs/...` 0 issues, `go test ./...` green |

Deviation from the planned semantics, accepted as documented: a parallel run ends at *every*
`Describe`/`When` transition, not only at a `BeforeAll`/`AfterAll` boundary. It is stricter than
`Builder`, which coalesces consecutive `ItParallel` across `Describe` boundaries. Outcomes still
match `Builder`; only which specs overlap in time differs. It guarantees a parallel range never
straddles a hooked group. Pinned by `TestSpecItParallel_GroupingConsecutiveVsSeparated`.

Parent verification: `gentle-ai review assess --base-ref 3734d4f --committed-only` returned risk
`high` (`process_boundary`, subprocess tests); RDD is off for this clone, so no native review ran.
The writer self-verified. An independent read-only verifier returned PASS with no findings. It
checked concurrency, panic/`Fatal` containment (with throwaway reproducers, deleted), `-run`,
reporting order, focus, H10, the non-`*testing.T` fallback and docs. It also ran
`go test -race -count=3 -run TestSpecItParallel ./specs/`, which passed. The parent re-ran
`go test -count=1 ./...`: no FAIL.

Coverage gap noted by the verifier, now closed: `specs/spec_itparallel_failure_test.go` pins that a
panic or `ctx.T.Fatal` inside one `ItParallel` body fails only that spec's subtest. Siblings still
pass, `AfterEach` runs for all four specs, and the process does not crash. RED was observed through
a temporary mutation that replaced `runProgram` in `runParallelSpec` with an unrecovered loop.

Correction (after review): an earlier version of this note claimed that
`runSubtestGuardingParallel` also recovers the panic. That was wrong, because it has no `recover()`.
Under the mutation the test binary did crash (`panic: parallel-boom [recovered, repanicked]`). The
original test missed the crash because the `testing` package prints the whole `--- FAIL` hierarchy
before it re-panics; its first RED came only from `AfterEach ran 2 times, want 4`. The test now
asserts that Go's top-level `panic: ... [recovered` line is absent. Under the same mutation it fails
with `an ItParallel panic escaped its spec and crashed the test binary`. `runParallelSpec` now
carries a comment stating that `runProgram`'s deferred recover is the only containment on this
path, and that the same defer is what runs `AfterEach`.

Also renamed the four specs of `TestSpecItParallel_SpecsOverlapInTime` from a shared `"spec"`
(which Go de-duplicates as `spec#01`…) to `spec-0`…`spec-3`.

## Review fixes (Codex P1)

Two Codex P1 findings against PR #252 were fixed after the spec above was marked done.

### Fix 1 — direct `testing.T` failures were invisible to the reporter

`runParallelSpec` (`specs/group_hooks.go`) derived `Failed` only from `ctx.hasFailed()`. A body that
failed only through `ctx.T.Error`/`Errorf`/`Fail` or `ctx.T.Fatal`/`FailNow` — never touching `ctx` —
still failed the Go subtest, but `SpecFinished.Failed` stayed `false` and
`SuiteFinished.FailedSpecs` never counted it, so the reporter and `go test`'s own PASS/FAIL line
disagreed.

Fix: capture `subT.Failed()` with a `defer` inside the subtest closure (so it also runs on
Fatal/FailNow's `runtime.Goexit`) and OR it into the outcome's failed flag: `failed :=
ctx.hasFailed() || subTFailed`.

The sequential `*Spec` path (`runSpecProgramIsolated` in `specs/execution_plan.go`, via its caller in
`execution_plan.go:434`) has the **same gap**: `Failed: ctx.hasFailed()`, no `subT.Failed()` fold-in.
Per this task's scope, the sequential path was left unchanged — this is a pre-existing, separate
divergence, not something Fix 1 introduced or corrected.

RED (`specs/spec_itparallel_direct_failure_test.go`,
`TestSpecItParallel_DirectTestingTFailuresCountInOutcomes`, subprocess-based since a real
`ctx.T.Error`/`FailNow` would fail the outer test too):

```
expected SpecFinished for "bravo-errors" to report Failed=true:
    SPEC_FINISHED name=alpha-passes failed=false
    SPEC_FINISHED name=bravo-errors failed=false
    SPEC_FINISHED name=charlie-failnow failed=false
    SUITE_FINISHED failed=0
    --- FAIL: .../suite/charlie-failnow (0.00s)
    --- FAIL: .../suite/bravo-errors (0.00s)
```

GREEN: same test passes after the fix (`SUITE_FINISHED failed=2`).

Commit: `76f3183` — `fix(specs): count testing.T failures in Spec ItParallel outcomes (#245)`.

### Fix 2 — duplicate ItParallel subtest names raced

`runParallelGroup` launches one goroutine per spec, each calling `testing.T.Run(name, ...)`
concurrently. Two adjacent `ItParallel` specs sharing a (normalized) name passed the **same**
un-deduplicated name from every goroutine; whichever goroutine's call reached testing's internal
name-dedup mutex (`matcher.fullName`/`unique`, `testing/match.go`) first won the unsuffixed name, so
`-test.run` selecting that unsuffixed name picked a nondeterministic body. Observed RED was not even
flaky — it deterministically picked the second-declared spec every time on this machine, which is
itself evidence the "first declared wins" contract was accidental, not designed.

Investigation into `testing/match.go` (Go 1.26.6, `/home/pablog/sdk/go1.26.6/src/testing/match.go`):
`matcher.unique(parent, subname)` keys a `subNames map[string]int32` by `parent + "/" + subname`;
first occurrence of a base returns it unchanged, each further occurrence gets `#01`, `#02`, ...
Passing an explicitly pre-suffixed name (e.g. literally `"x#01"`) is stable and is **not**
re-suffixed by `unique`, provided the bare prefix's own count (`subNames["x"]`) has not itself
already advanced past that number — which holds by construction whenever a batch contains at most
one bare (unsuffixed) occurrence of that name, and does not depend on which goroutine's call
happens first (see the doc comments on `uniqueSubtestName`/`computeParallelSubtestNames`).

Chosen approach: precompute every ItParallel spec's exact subtest name **before** launching any
goroutine, on the single goroutine that already walks the whole plan (`buildTree`, called from
`runPlanWithGroups` before any parallel range runs). `computeParallelSubtestNames`
(`specs/group_hooks.go`) walks the plan exactly the way `runRange`/`runGroup` do at runtime (same
recursion over `children`/`top`, same per-scope prefix), maintaining a local `map[string]int32`
counter scoped fresh per Describe/When (or root) — mirroring `testing`'s own per-parent `subNames`
map — and feeding every item in that scope (nested group names, ordinary spec names, and
`ItParallel` spec names) through `uniqueSubtestName`, a direct behavioral port of
`matcher.unique`/`parseSubtestNumber` restricted to one parent (no path-prefix concatenation needed,
since the simulation is already scoped per parent). The result for every `ItParallel` spec is stored
in `groupRun.parallelNames map[int]string`; `runParallelSpec` passes that name straight to `t.Run`
instead of the raw (un-deduplicated) breadcrumb slice.

This is not an approximation restricted to the specs within one parallel range: it also accounts for
a same-named **sequential** sibling declared earlier in the same scope, because the walk processes
every item of the scope, in declaration order, before reaching the parallel range — so the local
counter already reflects that sibling's registration by the time it resolves the range's names.
Evidence this matches Go's own naming exactly, order-independent: `go test -race -count=20 -run
'ItParallel' ./specs/...` (20 iterations of every `ItParallel` test in the package) — `ok`, 0
`--- FAIL`, 0 `WARNING: DATA RACE`, 0 `panic:` lines.

RED (`specs/spec_itparallel_dup_name_test.go`,
`TestSpecItParallel_DuplicateNameSubtestSelectionIsDeterministic`; subprocess with
`-test.run=^TestX$/^suite$/^dup$ -test.count=20`, since the outcome depends on goroutine scheduling):

```
expected -test.run selecting the unsuffixed name "suite/dup" to run only the first declared
spec, all 20 times; got first=0 second=20
```

(All 20 iterations ran `RAN:second`, none ran `RAN:first` — the unsuffixed name deterministically,
if accidentally, picked the wrong spec on this build.)

GREEN: same test, `first=20 second=0`.

Commit: `<pending>` — `fix(specs): assign duplicate ItParallel subtest names in declaration order
(#245)`.

### Final verification (both fixes)

- `go test -count=1 ./...`: all packages `ok`, no FAIL.
- `make fmt-check`: clean.
- `go vet ./...`: clean.
- `golangci-lint run ./specs/...`: `0 issues`.
- `go test -race -count=20 -run 'ItParallel' ./specs/...`: `ok`, 0 `--- FAIL`, 0 `WARNING: DATA
  RACE`, 0 `panic:` (explicitly requested by the user; no other `-race` use).

## Next step

Spec 2 and both Codex P1 review fixes are done. Push and PR (`Closes #245`, base
`feat/245-spec-focus-skip-pending` until #250 merges) are the user's decision.
