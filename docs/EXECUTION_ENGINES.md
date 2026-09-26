# Execution engines: inventory, classification and consolidation plan

Status: decision proposed, migration not yet executed. Tracking issue: [#176](https://github.com/getsyntegrity/go-specs/issues/176).

This document answers one question: **which execution engines are product surface for v1, and where does
each correctness invariant live?** It exists because correctness semantics were duplicated across five
execution implementations, which is why #62, #63, #64 and #65 were four separate issues for a single
missing invariant — per-spec panic recovery — and why #109 and #111 each had to be reasoned about per
engine.

The non-goal from #176 holds here: nothing is classified by line count.

---

## Correction to the existing architecture docs

`docs/ARCHITECTURE.md` and `docs/EXECUTION_MODEL.md` both open with:

> **DSL → Builder → Program → Runner**

That is **not** the default path. `Describe` never touches `Builder`, `Program` or `Runner`:

```
specs.Describe(tb, name, fn)                         specs/spec.go:50
  └─ describeWithCompilerContext                     specs/spec.go:81
       ├─ newBytecodeCompiler()                      specs/compiler.go:35
       ├─ c.TakePlan()  → *ExecutionPlan             specs/compiler.go:160
       └─ s.suite.run(tb, runCtx)  → CompiledSuite   specs/execution_plan.go:199
```

The `Analyze`/registry path differs only in how the plan is built (`arena` →
`buildExecutionPlanFromArena`, `specs/spec.go:246-263`); it executes through the same `CompiledSuite`.

`Builder → Program → Runner` is a **second, parallel public path**, reachable only when a caller
constructs it by hand via `NewBuilder`/`BuildProgram` + `NewRunner`. Both doc headers should be corrected
as part of this work — a reader currently learns the wrong engine.

---

## 1. Inventory

### 1.1 `ExecutionPlan` + `CompiledSuite` — the default engine

| | |
|---|---|
| Files | `specs/execution_plan.go`, `specs/compiler.go`, `specs/instruction.go`, `specs/spec.go` |
| Public surface | `ExecutionPlan`, `CompiledSuite`, `Instruction`, `OpCode`, `Spec`, `Describe`, `DescribeFlat`, `DescribeFast`, `*WithReporter`, `BuildSuite` |
| Reachable from `Describe` | **Yes — this is the documented entry point.** |
| Non-test consumers | `specs/spec.go`, `specs/path_builder.go` |
| Tests | `execution_plan_test.go`, `_isolation_test.go`, `_recovery_test.go`, `_reporter_test.go`, `compiled_runner_test.go`, `lifecycle_hooks_test.go`, `spec_event_path_test.go`, `subtest_identity_test.go` |
| Benchmarks | `minimal_and_buildsuite_bench_test.go`, `describe_variants_bench_test.go`, `e2e_test.go` (`*testing.T` path) |
| Docs | README, `ARCHITECTURE.md`, `EXECUTION_MODEL.md`, every `examples/*` |
| Reason to exist | It is the product. |

### 1.2 `Builder` / `Program` / `Runner`

| | |
|---|---|
| Files | `specs/builder.go`, `specs/program.go`, `specs/runner.go`, `specs/dsl.go` |
| Public surface | `Builder`, `NewBuilder`, `BuildProgram`, `Program`, `Runner`, `NewRunner`, `NewRunnerWithReporter` |
| Reachable from `Describe` | **No** — caller-constructed only |
| Non-test consumers | `RunShard`/`RunShardWithReporter` (`specs/scheduler.go:265,280`), `benchmarks/helpers.go` |
| Docs | README:120,126; `DSL.md:56-62`; `EXECUTION_MODEL.md:20,45,105,171,203-213`; `ARCHITECTURE.md:146`; `examples/parallel/parallel_test.go` |
| **Unique capabilities** | **`FailFast` and `RunShard` exist here and nowhere else.** `Focus`/`Skip`/`Pending` (the `SpecFn`/`ItWith` wrapper style) are also Builder-only, but `Spec` has the same underlying capability directly as `FIt`/`SkipIt`/`PendingIt` since [#245](https://github.com/getsyntegrity/go-specs/issues/245) — see Stage 4 item 1 below. `ItParallel` is likewise Builder-only as a `SpecFn`-free direct method, but `Spec.ItParallel` has the same capability with a stronger guarantee — a real per-spec `*testing.T` and `-run` addressing — since #245's second spec; see Stage 4 item 2 below. |

This is the load-bearing fact for v1: removing this engine removes `FailFast` and
`RunShard` from the library. Focus/skip/pending and `ItParallel` no longer depend on it (#245).

### 1.3 `MinimalRunner`

| | |
|---|---|
| File | `specs/minimal_runner.go` |
| Public surface | `RunSpec`, `MinimalRunner`, `NewMinimalRunner`, `NewMinimalRunnerFromSpecs`, `Add`, `RunBatchSize`, `Run`, `RunParallel`, `RunParallelBatched` |
| Reachable from `Describe` | **No** |
| Non-test consumers | **None.** `ShardSpecs` (`specs/sharding.go:162`) consumes `[]RunSpec`, and the file's doc comment points at `NewMinimalRunnerFromSpecs`, but nothing calls it. |
| Docs | README:13,120,126 — `RunParallel`/`RunParallelBatched` named as "an additional opt-in worker-pool execution path" |
| Benchmarks | Six in `minimal_and_buildsuite_bench_test.go` |
| Reason to exist | The only public worker-pool execution of a flat spec list, and the documented partner of `ShardSpecs`. |

### 1.4 `BytecodeRunner` (`BCProgram` / `BCBuilder`)

| | |
|---|---|
| Files | `specs/runner_bytecode.go`, `specs/bytecode_impl.go`, `specs/bytecode.go` |
| Public surface | `BytecodeRunner`, `NewBytecodeRunner`, `Run`, `RunParallel`, `BCProgram`, `BCLen`, `NumSpecs`, `BCBuilder`, `NewBCBuilder`, `AddBefore`, `AddAfter`, `AddSpec`, `BuildBC`, `ShardBCProgram` |
| Reachable from `Describe` | **No** |
| Non-test consumers | `ShardBCProgram` only (`specs/sharding.go:180`) |
| Benchmarks | **None** |
| Docs | **None** in README, `docs/`, or `examples/` — only a `CHANGELOG.md:182` migration-table mention |
| Reason to exist | It flattens hooks at build time like the canonical compiler does, but reports nothing and isolates nothing. It duplicates `ExecutionPlan`'s model with strictly fewer capabilities. **No performance claim backs it: it has no benchmark.** |

### 1.5 `BlockRunner`

| | |
|---|---|
| File | `specs/block_runner.go` |
| Public surface | `DefaultBlockSize`, `CompileBlocks`, `BlockRunner`, `NewBlockRunner`, `Run`, `NumSpecs`, `NumBlocks` |
| Reachable from `Describe` | **No** |
| Non-test consumers | **None anywhere in the repository** |
| Benchmarks | **None** |
| Docs | **None** |
| Defect | `specBlock` is unexported yet appears in the signatures of the exported `CompileBlocks` and `NewBlockRunner`. An external caller can pass the value through but cannot name the type, declare a variable of it, or build blocks itself. |
| Reason to exist | Its stated premise — "fewer outer-loop iterations" — is unmeasured. Its inner loop is `for i := 0; i < n; i++ { runBlockSpecRecovered(...) }`, the same per-spec call `MinimalRunner` makes. |

### 1.6 Parallel scheduler — substrate, not an engine

| | |
|---|---|
| Files | `specs/scheduler.go`, `specs/scheduler_batch.go` |
| Public surface | `RunShard`, `RunShardWithReporter`, `DefaultChunkSize` — core is unexported |
| Role | The shared worker pool behind `MinimalRunner.RunParallel*`, `BytecodeRunner.RunParallel`, and (via `parallelStep`) Builder's `ItParallel`. `RunShard` is unrelated: it shards a `*Program` and delegates to `Runner`. |
| Note | `CHANGELOG.md:184` already states the worker pool was "never part of the supported public surface". |

**Conclusion:** there are **two** engines with full semantics (ExecutionPlan, Builder/Runner), **three**
flat spec-list executors (Minimal, Block, Bytecode), and **one** shared parallel substrate. The three flat
executors share exactly one invariant with the real engines — per-spec panic recovery — and implement none
of the others.

---

## 2. Shared invariants and who implements them

✅ implemented · ❌ absent · ⚠️ partial

| Invariant | ExecutionPlan | Builder/Runner | MinimalRunner | BytecodeRunner | BlockRunner | Worker pool |
|---|---|---|---|---|---|---|
| Hook ordering (before outer→inner, after LIFO) | ✅ | ✅ | ❌ | ✅ (flattened at build) | ❌ | ❌ |
| Per-spec panic recovery | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Failure recording | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ (indexed) |
| Focus / skip / pending filtering | ✅ ([#245](https://github.com/getsyntegrity/go-specs/issues/245)) | ✅ | ❌ | ❌ | ❌ | ❌ |
| `-run` filtering (`Filtered`) | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| Subtest identity (`t.Run`) | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| Reporting (`report.EventReporter`) | ✅ | ✅ | ❌ | ❌ | ❌ | ⚠️ `parallelStep` only |
| FailFast | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ |
| Isolation (spec survives `Fatalf`) | ✅ | ✅ | ❌ | ❌ | ❌ | ✅ (sentinel) |
| `BeforeAll`/`AfterAll` group hooks ([#207](https://github.com/getsyntegrity/go-specs/issues/207), `docs/SUITE_HOOKS_CONTRACT.md`) | ✅ | ❌ (follow-up) | ❌ | ❌ | ❌ | ❌ |
| `ItParallel` ([#245](https://github.com/getsyntegrity/go-specs/issues/245), Stage 4 item 2) | ✅ (real per-spec subtest and `ctx.T`; H9 with `BeforeAll`/`AfterAll`) | ✅ (shared `ctx.T == nil`, no subtest) | ❌ | ❌ | ❌ | ❌ |

Two asymmetries are **deliberate** and must not be "consolidated" away:

- **Failure-record scope.** `Runner` clears `ctx.failure` at the top of every spec (`runner.go:221`)
  because `FailFast` asks *did this spec fail*. Every other engine leaves it cumulative — *did any spec
  fail*. Read through `ctx.hasFailed()`, the single accessor (#175). Pinned by
  `TestEngineContractFailedFlagScopeIsNotShared`.
- **Recovery dialect.** Sequential engines report straight to `ctx.backend`; worker goroutines write into
  an indexed `[]failureRecord` so output stays deterministic regardless of completion order. These are
  two rules, not one rule implemented twice.

---

## 3. Classification

| Engine | Class | Rationale |
|---|---|---|
| `ExecutionPlan` + `CompiledSuite` | **Canonical** | The `Describe` path. Every invariant lands here first. |
| `Builder` / `Program` / `Runner` | **Compatibility surface** | Documented, exercised by `examples/parallel`, and the sole home of `FailFast` and `RunShard`. Focus/skip/pending and `ItParallel` have equivalents on the canonical engine since #245; cannot be removed until `FailFast` and `RunShard` do too. |
| `MinimalRunner` | **Compatibility surface (narrow)** | Named in README, benchmarked, and the documented partner of `ShardSpecs`. Keep `Run`/`RunParallel`/`RunParallelBatched`; do not grow it. |
| Parallel scheduler | **Internal substrate** | Already declared non-public in `CHANGELOG.md:184`. Keep unexported. |
| `BytecodeRunner` / `BCProgram` / `BCBuilder` | **Experimental → deprecate** | Zero documentation, zero benchmarks, zero consumers beyond `ShardBCProgram`. A strictly weaker duplicate of the canonical model with no measured justification. |
| `BlockRunner` | **Removable** | Zero documentation, zero benchmarks, zero callers, and an unusable exported signature (`specBlock`). |

---

## 4. Consolidation and removal plan

### Stage 0 — done in this change (no public behavior removed)

This builds directly on #185, which already made `reportRecoveredPanic` (`specs/panic_report.go`) the one
path a recovered panic takes to a backend, so that a missing, released or defective backend can no longer
turn a recoverable spec panic into a dead process (#171).

1. **`specs/panic_report.go`** gains the two engine-facing wrappers that decide *whether* a recovered
   value is a failure at all: `recoverSpecFailure` (sequential) and `recoverParallelSpecFailure` (worker).
   They live in that file, beside `reportRecoveredPanic`, specifically so no second reporting authority
   can grow beside it. `recoverSpecFailure` builds the message/output pair and **delegates delivery to
   `reportRecoveredPanic`**; it never touches a backend itself, so #171's guarantees extend unchanged to
   every engine and to any engine added later. `recoverParallelSpecFailure` stays separate because worker
   goroutines record into an indexed `[]failureRecord` and touch no backend at all — safe against #171's
   failure mode by construction, which is precisely why the sequential rule cannot be reused there.
2. All five sequential recovery sites (`runner.go`, `execution_plan.go` ×2, `block_runner.go`,
   `minimal_runner.go`, `runner_bytecode.go`) now call the one wrapper with the same wire format. As a
   result `fmt` and `runtime/debug` dropped out of all five engine files entirely: message formatting and
   stack capture are no longer an engine concern.
3. **`specs/execution_engine_contract_test.go`** asserts the shared invariants over a table of engines
   instead of one private test per engine. A new engine must be added to the table; a new rule applies to
   every engine at once.
4. **Defect fixed.** `runStepRecovered` (`runner.go`) was the only recovery site without the
   `isExpectedAbort` guard, so the Builder/Runner path double-reported a controlled backend's
   `FailNow`/`Fatal`/`Fatalf` sentinel as `panic: {}`. Every other engine already had a private test for
   this rule; Runner never grew one. The contract table found it on its first run. Routing every site
   through one wrapper means the guard can no longer be present in four places and missing in a fifth.

One intentional behavioral difference: the reported stack trace now carries `recoverSpecFailure` as its
top frame, because the trace is captured inside the shared helper.

### Stage 1 — documentation (no code risk)

Two documentation defects were found while compiling this inventory. Both teach a reader something the
code does not do:

- Correct the `DSL → Builder → Program → Runner` pipeline header in `ARCHITECTURE.md:9` and
  `EXECUTION_MODEL.md:9` to describe the compiler/`ExecutionPlan` path, with Builder/Runner presented as
  the alternative surface it is.
- Correct `EXECUTION_MODEL.md:200-206`, which still states that `Builder`/`Runner` runs `BeforeEach`
  "1 time — once per group" and describes #109 as an open divergence. #109 is closed and the code runs
  hooks per spec: a probe with three specs sharing one `BeforeEach` invokes it three times. The table and
  the paragraph beneath it are stale.
- Publish the section 2 matrix so a user can see, before choosing an engine, that `MinimalRunner` has no
  hooks and `BytecodeRunner` has no reporting.

### Stage 2 — deprecate, do not yet delete

- Mark `BlockRunner`, `CompileBlocks`, `NewBlockRunner`, `DefaultBlockSize` as `Deprecated:` with
  `MinimalRunner` named as the replacement — same shape, same contract, actually benchmarked.
- Mark `BytecodeRunner`, `BCProgram`, `BCBuilder`, `NewBCBuilder`, `ShardBCProgram` as `Deprecated:` with
  `BuildSuite` + `CompiledSuite` named as the replacement.
- **Migration impact:** both are exported from `specs`, so this is a public-API change even though no code
  in this repository calls them. The library is pre-1.0 (`v0.x`), so a deprecation cycle followed by
  removal in a minor release is within its stated compatibility promise. `BlockRunner` additionally cannot
  be fully driven from outside the package today (`specBlock` is unexported), which bounds the realistic
  external blast radius to callers who only ever pass `CompileBlocks`' results straight into
  `NewBlockRunner`.

### Stage 3 — remove after one deprecation release

- Delete `specs/block_runner.go` and its two test files.
- Delete `specs/runner_bytecode.go`, `specs/bytecode_impl.go`, `specs/bytecode.go`, `ShardBCProgram`, and
  their tests.
- Remove both rows from the contract table. If a row cannot be removed because something still depends on
  it, that dependency is the argument for keeping the engine — record it here instead.

### Stage 4 — close the capability gap, then reconsider Builder/Runner

Builder/Runner stays until the canonical engine grows what only it has:

1. ~~Focus/skip on `Spec` (`s.FIt` / `s.XIt`), compiled into `ExecutionPlan` the way `Builder.finalize`
   compiles them into groups.~~ **Done ([#245](https://github.com/getsyntegrity/go-specs/issues/245)).**
   `Spec.FIt`/`SkipIt`/`PendingIt` exist on both build paths (the bytecode compiler and the
   `Analyze`/registry path) with the same observable behavior as `Builder.FIt`/`SkipIt`/`PendingIt`
   — see `docs/DSL.md`'s "Spec.FIt, SkipIt and PendingIt" and `specs/spec_builder_equivalence_test.go`.
   `ItParallel` is not part of this item; it was item 2 below, tracked separately as its own
   follow-up change (it carries the determinism and concurrency risk `#245`'s proposal deliberately
   kept out).
2. ~~`ItParallel` equivalent on the plan model, reusing the existing worker substrate.~~ **Done
   ([#245](https://github.com/getsyntegrity/go-specs/issues/245)).** `Spec.ItParallel` exists on
   both build paths, but not by literally reusing `parallelStep`/the worker substrate's shared-`ctx`
   model: it launches each parallel spec as its own real Go subtest (concurrent `testing.T.Run`
   calls, never `t.Parallel()`), so every spec keeps this engine's per-spec guarantees — a real
   `ctx.T`, `-run` addressing, `BeforeAll`/`AfterAll` interaction (H9) — that `Builder`'s
   shared-context model cannot give without breaking them. See `docs/DSL.md`'s "ItParallel" section
   and `specs/spec_builder_equivalence_test.go`.
3. `FailFast` on `CompiledSuite`.
4. `RunShard` re-expressed over `ExecutionPlan`.

Only once items 3-4 also land — each with a contract-table row — does removing Builder/Runner become
a migration question rather than a feature regression. **That decision is explicitly out of scope
for #176.**

---

## 5. What was deliberately *not* consolidated

- **Hook execution** across `ExecutionPlan` and `Builder/Runner`. Both now run hooks at the same
  frequency — once per spec, verified by probe: three specs sharing one `BeforeEach` under
  `Builder`/`Runner` invoke it three times — so the remaining difference is only the compiled shape
  (instruction stream with `OpBeforeHook`/`OpAfterHook` vs. `group{before, specs, after}`). Unifying those
  shapes would couple the engines far more tightly than the duplication costs, for no correctness gain.
- **Reporting.** `reporterObserver` (Runner) and `specCounter`/`reportSpecFinished` (plan) already emit the
  same `report.EventReporter` events; the duplication is in plumbing, not in the contract. Worth revisiting
  only after Stage 4.
- **The failure-record and recovery-dialect asymmetries** in section 2 — both are load-bearing, and both are
  now pinned by tests so they cannot drift into accidental divergence.
- **Merging the two recovery dialects into one.** It is tempting, and it would be wrong: the sequential
  rule reports through a backend and therefore needs every `reportRecoveredPanic` guarantee from #171,
  while the worker rule writes into an indexed slice and touches no backend. Collapsing them would either
  drag backend fragility into the worker path or weaken the sequential one. Two wrappers over one delivery
  authority is the correct shape.
