# Changelog

All notable changes to this project are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project is pre-1.0, so any release may include breaking changes (see [README.md#project-status](README.md#project-status)).

Entries for `v0.0.1`–`v0.0.9` predate this file — see [GitHub Releases](https://github.com/getsyntegrity/go-specs/releases) for their auto-generated notes.

## [Unreleased]

### Removed

- **Breaking.** Removed the exported legacy pointer-based declaration tree from `specs`: the `Node`
  struct, `PrintTree(*Node, int, io.Writer)` and `Walk(*Node, func(*Node))`. The registry has built
  into `NodeArena` — a flat, index-based arena — since the arena migration, and nothing in the module
  ever constructed a `Node`: the type survived only as the parameter of those two functions, which
  had no callers anywhere, so the three symbols formed a closed island unreachable from any live
  execution path. Code that printed a tree should call `PrintTreeArena(arena, rootID, depth, w)` or
  `SuiteTree.Tree()`; code that walked one should call `SuiteTree.Walk(func(id int))`, which visits
  arena node indices. Note that `PrintTreeArena` prints the node at `rootID` itself, so passing `0`
  emits the synthetic `suite` root line, whereas `SuiteTree.Tree()` omits it. `specs.Walk` was a free
  function over `*Node` and is unrelated to the retained `SuiteTree.Walk` method, which is unchanged.
  ([#156](https://github.com/getsyntegrity/go-specs/issues/156))
- Removed the internal package `specs/internal/registry`, a second, self-contained registry
  implementation (its own `Node`, `NodeType`, `ScopeMeta` and `Registry` with `Push`/`Pop`/`Attach`)
  that no package in the module imported. The live registry lives in `specs/registry.go` and builds
  into `NodeArena`; the internal copy never participated in execution and had diverged from it. This
  is an internal package, so no public API changes. ([#156](https://github.com/getsyntegrity/go-specs/issues/156))
- Removed `Context`'s per-context RNG: the unexported `rng` field and `randomInt64` method. The field
  was seeded only in `NewContext`, and only from `time.Now().UnixNano()` — a wall-clock seed, which
  the framework's deterministic-execution contract does not allow — while every runner acquires
  contexts from `contextPool` via `acquireContext`, whose `Reset` set `rng` to `nil`. The RNG was
  therefore nil for the entire life of every spec the runners execute, `randomInt64` had no
  production caller, and the `TestContextRNGDeterminism` test that appeared to guarantee its
  determinism was vacuous: its spec body never ran, so it compared two untouched zero-filled slices
  and would have passed against any implementation. `Spec.RandomSeed` is unaffected and remains the
  single seam for seeded behaviour — it seeds `Paths()` generation (`Sample` draws and the
  `Explore`/`ExploreCoverage`/`ExploreSmart` candidate streams), which it always did; its doc comment
  claimed it also seeded the context RNG, which was never true. A spec body that needs randomness
  brings its own generator and seeds it explicitly. ([#156](https://github.com/getsyntegrity/go-specs/issues/156))
- Removed the unexported `newSpec` constructor, which that deleted test was the only caller of
  anywhere in the module. It returned a `Spec` carrying neither a compiler nor a registry nor an
  arena, so `Describe` on the result had nothing to build into and silently did nothing — which is
  precisely why the RNG test's spec body never ran. No production code path used it: the exported
  entry points build their `Spec` directly. Removing it deletes the seam that let a test look like it
  exercised the framework while executing none of it. Internal only, so no public API change.
  ([#156](https://github.com/getsyntegrity/go-specs/issues/156))

### Added

- Direct tests for the retained exported registry helpers — `PrintTreeArena`, `CurrentArena`,
  `CurrentSuite`, `AppendBeforeHook`, `AppendAfterHook` and `SetPathGen` — which previously had none.
  They pin the documented behaviour of each, including that the read-only accessors and
  `PrintTreeArena` are no-ops rather than panics when no registry is active or when the arena, root
  id or writer is absent, and that `PrintTreeArena` includes its `rootID` node in the output. (The
  three mutating helpers were no-ops too when this landed; see the `Changed` entry for
  [#151](https://github.com/getsyntegrity/go-specs/issues/151) below, which made them fail closed.) Each helper's doc comment now states the
  purpose it is retained for: together they are the supported surface for building into the registry
  that `Analyze` or `Describe` pushed, without access to the unexported registry type.
  ([#156](https://github.com/getsyntegrity/go-specs/issues/156))

### Changed

- **Breaking.** Replaced the exported `CaptureCallerLocation bool` with the concurrency-safe pair
  `SetCaptureCallerLocation(enabled bool)` and `CaptureCallerLocationEnabled() bool`, backed by an
  `atomic.Bool`. Suite construction is documented as safe across concurrent goroutines, so any
  consumer writing the exported variable while another goroutine declared specs produced an
  unsynchronized read/write data race on the `callerLocation` read — a race the variable's type made
  unavoidable, not a misuse callers could code around. Behaviour is unchanged: capture is still off
  by default, and when off `callerLocation` still returns `"", 0` without calling `runtime.Caller`.
  Replace `specs.CaptureCallerLocation = true` with `specs.SetCaptureCallerLocation(true)` and any
  read of the variable with `specs.CaptureCallerLocationEnabled()`. The setter also restores the name
  the pre-rewrite `main` branch exported, closing the one parity gap `docs/LEGACY_PARITY.md` recorded
  as having no equivalent. ([#153](https://github.com/getsyntegrity/go-specs/issues/153))

- **Breaking.** `AppendBeforeHook`, `AppendAfterHook` and `SetPathGen` now panic when called with no
  active registry, instead of silently returning. Each one exists to write into the registry that
  `Analyze` or `Describe` pushed; with no registry there is no destination, so returning quietly
  discarded the caller's hook or path generator and reported success. A suite built that way
  registers nothing and still passes. The panic names the helper and states the requirement
  (`call it inside Analyze(fn) on the same goroutine`), because the call site is the only place that
  can fix it. The read-only accessors `CurrentSuite` and `CurrentArena` are unchanged and still
  return `nil` outside `Analyze`: "no suite is being built" is a legitimate answer to a question, not
  a lost write. The registry stack is keyed per goroutine, so a helper called from a goroutine
  started inside `Analyze` also panics — it would otherwise write into a registry nobody reads.
  ([#151](https://github.com/getsyntegrity/go-specs/issues/151))
- **Breaking.** `Spec.It`, `Spec.Describe`, `Spec.When`, `Spec.BeforeEach`, `Spec.AfterEach` and the
  `Paths()` registration now panic when called on a `Spec` that carries neither a compiler nor a
  registry. `Spec` is exported with unexported fields, so external code can write `&specs.Spec{}`;
  such a `Spec` has no build target, and every registration made against it was previously accepted
  and dropped — a suite could declare specs and hooks, register none of them, and report green.
  Obtain a `*Spec` from `Describe`, `DescribeFlat`, `DescribeWithReporter`,
  `DescribeFlatWithReporter` or `BuildSuite`, which set the build target and thread it into every
  nested block. A `nil` `*Spec` remains a tolerated no-op: there is no `Spec` there to have
  registered anything into. No supported construction path is affected — every internal entry point
  already set one of the two targets — so this breaks only code that constructed a `Spec` directly.
  ([#151](https://github.com/getsyntegrity/go-specs/issues/151))
- The intended status of the `Analyze` extension surface is now documented rather than inferred:
  `Analyze`, `CurrentSuite`, `CurrentArena`, `AppendBeforeHook`, `AppendAfterHook` and `SetPathGen`
  are a deliberate, supported API for building a `SuiteTree` without going through `Describe`, not
  legacy residue. `docs/DSL.md` gains "Where a `*Spec` comes from" and "Analyze and the registry
  extension surface", stating the valid construction context, the per-goroutine scoping rule, and why
  the read-only and mutating helpers behave differently outside it.
  ([#151](https://github.com/getsyntegrity/go-specs/issues/151))
- The registry's node-stack invariant — `stack` is never empty, because `newRegistry` seeds it with
  the suite root and the pop closure only shrinks it while `len(stack) > 1` — is now stated once and
  enforced consistently. `enterNode` indexed the stack top unguarded while `appendBeforeHook`,
  `appendAfterHook` and `setPathGen` each returned silently on an empty stack: three dead branches
  that could only mask a broken invariant by discarding a registration. All four now go through
  `currentNodeIDLocked`, which panics if the invariant is ever violated. Internal only, so no public
  API change. ([#151](https://github.com/getsyntegrity/go-specs/issues/151))

### Fixed

- **Breaking.** Invalid sharding configuration degraded silently into "run the whole suite". A
  `total <= 0`, a negative index or an index at or above the total made `ShardSpecs` and
  `ShardBCProgram` return every spec, and made `RunShard`/`RunShardWithReporter` run every group;
  `ParseShardEnv` and `ParseShardFlag` reduced any malformed value to `ok=false`, which
  `ShardFromArgsOrEnv` then reported as "no sharding requested". A CI typo as small as
  `SHARD_TOTAL=0` therefore made every worker run 100% of the suite, N times over, and still report
  green — the suite looked sharded and proved nothing about the partition. Sharding now has three
  distinct states instead of two: not configured (run everything — legitimate), configured and valid,
  and configured but unusable (a hard failure). The parsers return `error` instead of `ok bool`:
  `ErrShardNotConfigured` when nothing requested sharding, testable with `errors.Is`, and a
  `*ShardConfigError` when something did but the value cannot be used. `ShardSpecs` and
  `ShardBCProgram` hold no `testing.TB`, so they panic with an actionable message the way a reused
  `Expectation` does; `RunShard` and `RunShardWithReporter` do hold one, so they report through
  `tb.Fatalf` and their signatures are unchanged. Invalid configuration therefore never means "run
  everything", and never means "run nothing successfully".

  Migration — the `ok bool` return of `ParseShardString`, `ParseShardFlag`, `ParseShardEnv` and
  `ShardFromArgsOrEnv` becomes an `error`:

  ```go
  // before
  if shard, total, ok := specs.ShardFromArgsOrEnv(); ok {
      list = specs.ShardSpecs(list, shard, total)
  }

  // after
  shard, total, err := specs.ShardFromArgsOrEnv()
  switch {
  case errors.Is(err, specs.ErrShardNotConfigured): // run the whole suite
  case err != nil:
      fmt.Fprintln(os.Stderr, err)
      os.Exit(2)
  default:
      list = specs.ShardSpecs(list, shard, total)
  }
  ```

  Precedence between args and environment is now defined and fail-closed. The `-shard` flag, once
  present, is authoritative: a malformed value is an error and does **not** fall back to the
  environment, because running a different partition than CI asked for is the same class of silent
  degradation as running all of them. An absent flag defers to the environment, where `SHARD` takes
  precedence over `SHARD_INDEX`/`SHARD_TOTAL` on the same terms. Setting exactly one of
  `SHARD_INDEX`/`SHARD_TOTAL` is now a configuration error rather than "not configured" — a
  half-configured pair is a typo, not a request to run everything. `-shard=2/10` and `--shard` are
  accepted alongside `-shard 2/10`, since Go's `flag` package accepts all four spellings and a form
  this parser skipped silently meant "no sharding". Each diagnostic names the exact flag or variable
  at fault, the value it carried, why it is unusable, that no specs ran, and the remedy including how
  to opt out of sharding entirely.

  Valid sharding and the partition contract are unchanged: shards stay disjoint, their union is still
  the whole suite, and a shard that legitimately draws nothing — more shards than specs or groups —
  remains a valid empty partition rather than an error.
  ([#174](https://github.com/getsyntegrity/go-specs/issues/174))
- **Breaking.** An assertion handle from `ctx.Expect(x)` or `specs.ExpectT(ctx, x)` could be used
  more than once, and the second use was a false green. The first `To`/`ToEqual` call returned the
  `Expectation` to `expectationPool`, so a retained handle either silently returned (its `ctx` had
  been cleared) or — once the pool had handed that same object to another spec — reported the
  assertion against *that* spec's backend. `e := ctx.Expect(1); e.ToEqual(1); e.ToEqual(999)` passed.
  In a testing framework a silently-passing assertion is the worst possible defect, so the handle is
  now single-use: it is spent by its first `To`/`ToEqual`, and a second one panics with an actionable
  message. The runner recovers the panic and reports the spec as failed, so the misuse is visible
  rather than green. The handle is claimed with `atomic.Bool.CompareAndSwap`, not a plain flag: a
  `if spent { panic }` followed by a later write is check-then-act, so several goroutines sharing one
  fresh handle would all read `false` and all assert — a second silent assertion again, plus an
  unsynchronized read/write on the handle's `ctx` and `actual`. The swap makes single-use a real
  property rather than a merely sequential one, and gives the losing callers a happens-before edge
  so `-race` has nothing to report. Removing `expectationPool` is the other half: a flag cannot stop
  a recycled object from being handed to another spec while the original caller still holds it.
  Expectations are now stack-allocated instead — they never escape the assertion that consumes them,
  so the fast path still allocates zero. Net effect on the assertion path, measured on go1.26.6 with
  `-count=6`: `BenchmarkAssertion_GoSpecs_ExpectToEqual` 13.5 ns/op → 9.7 ns/op and
  `BenchmarkMatcher_GoSpecs` 13.0 ns/op → ~11 ns/op, both still at 0 allocs/op. Dropping the pool's
  `Get`/`Put` is worth more than the atomic claim costs, so the path is faster than before despite
  gaining the guarantee. Specs that assert once per `Expect` call — every documented usage — are
  unaffected. ([#170](https://github.com/getsyntegrity/go-specs/issues/170))
- Snapshot comparison decoded both the stored and the newly marshaled JSON into `any`, where
  `encoding/json` represents every number as a `float64`. Integers above 2^53 lose their last digits
  there, so adjacent 64-bit IDs such as `9007199254740992` and `9007199254740993` collapsed onto one
  value and a genuinely changed snapshot reported a false-positive match. Both sides are now decoded
  with `json.Decoder.UseNumber()` and each number literal is reduced to an exact canonical form, so
  precision is preserved at any magnitude. Comparison semantics are unchanged otherwise and are now
  stated explicitly: two snapshots are equal when they denote the same JSON value, so object key
  order and whitespace remain irrelevant, and numbers compare by exact numeric value rather than by
  literal text — `1`, `1.0`, `1e0` and `100e-2` are the same snapshot, as are `0` and `-0`. No stored
  snapshot needs regeneration. ([#154](https://github.com/getsyntegrity/go-specs/issues/154))
- **Breaking for suites that called it.** Calling Go's `ctx.T.Parallel()` from inside a sequential
  spec body is now defined as unsupported and fails the run immediately with a diagnostic naming the
  operation and pointing at `ItParallel`/`RunParallel`. It previously corrupted the run in two ways:
  `testing.T.Run` returns as soon as the subtest parks, so the runner resumed with its shared
  `*Context` still bound to a spec that had not executed — the next spec opened its subtest *under*
  the previous one (`suite/bravo/suite/charlie`), and, worse, the runner released that Context back
  to the pool before the parked body ran, so every assertion the body later made hit the
  `backend == nil` guards, returned silently, and left the suite reporting `PASS` having proven
  nothing. A parked body's `*Context` is now poisoned rather than recycled, so if the body does
  resume, its assertions still report against its own subtest instead of vanishing or landing on an
  unrelated spec. Both sequential models (`Describe`/`ExecutionPlan` and `Builder`/`Runner`) and the
  generated-case path are covered. Suites that relied on the previously green — and meaningless —
  behaviour will now fail; move those specs to `ItParallel`, which gives each spec its own `Context`
  and never exposes a live `*testing.T`.
  ([#172](https://github.com/getsyntegrity/go-specs/issues/172))
- `go.mod` declared `module github.com/pablogore/go-specs` while the repository is hosted at `github.com/getsyntegrity/go-specs`, so `go get github.com/getsyntegrity/go-specs@<version>` failed with a module-path mismatch for every external consumer. Corrected the module path and every internal import, doc reference, and CI/script reference to `github.com/getsyntegrity/go-specs`. ([#139](https://github.com/getsyntegrity/go-specs/issues/139))

### ⚠️ v0.1.0 is broken — do not use

`v0.1.0` was tagged and published with the `go.mod` mismatch above: it cannot be `go get`-installed from `github.com/getsyntegrity/go-specs`. The tag and GitHub Release are kept as-is (immutable — public tags are not rewritten), but the fix above ships as the next patch release instead. Use that release or later.

## [0.1.0] - 2026-09-16

### Breaking Changes / Migration from v0.0.9

`v0.0.9` was published from a branch that had reorganized the module into `specs/compiler`, `specs/dsl`, `specs/ctx`, `specs/property`, and `specs/runner` subpackages (plus `cmd/specs-ci` and `tools/coverageheatmap`), with five `specs/*_reexport.go` files aliasing every subpackage symbol back into the root `specs` package so `specs.Program`, `specs.Describe`, `specs.Context`, and friends kept compiling. This release reverts that reorganization: the module returns to the single flat `specs` package that predates it, and the subpackages, the reexport files, `cmd/specs-ci`, and `tools/coverageheatmap` are all gone. See [#127](https://github.com/getsyntegrity/go-specs/issues/127) for the full diagnosis of how the two layouts diverged.

**If you only imported the root `specs` package** (`import "github.com/getsyntegrity/go-specs/specs"`, using `specs.Program`, `specs.Describe`, `specs.Context`, `specs.PathValues`, `specs.Runner`, etc.) — **nothing breaks.** Every one of those identifiers still exists in `specs`, now as a native declaration instead of a type alias / forwarding var.

**If you imported a subpackage directly**, update the import path and drop the package qualifier — the identifiers themselves are unchanged:

| v0.0.9 path | What it exposed | Now | Migration |
|---|---|---|---|
| `specs/compiler` | `Program`, `Builder`, `ExecutionPlan`, `CompiledSuite`, `Instruction`, `OpCode`, `NodeArena`, `SpecFn`, `NewBuilder`, `BuildPlanFromArena`, `ShardProgram` | Same names, native in `specs` | Import `specs` instead; drop the `compiler.` prefix |
| `specs/dsl` | `Describe`, `DescribeFlat`/`DescribeFast` (+`WithReporter`), `BuildSuite`, `BuildProgram`, `Focus`, `Skip`, `Analyze`, `Spec`, `SuiteTree`, `PathBuilder` | Same names, native in `specs` | Import `specs` instead; drop the `dsl.` prefix |
| `specs/ctx` | `Context`, `Expectation`, `Fixture`, `TestBackend`, `NewContext`, `EqualTo`, `ExpectT`, matchers (`Equal`, `BeNil`, `BeTrue`, `BeFalse`, `Contain`, `NotEqual`) | Same names, native in `specs` | Import `specs` instead; drop the `ctx.` prefix |
| `specs/property` | `PathValues`, `PathGenerator`, `Coverage`, `Corpus`, `Mutator`, `Shrinker`, `CoverageExplorer`, `SmartExplorer`, `New*` constructors | Same names, native in `specs` | Import `specs` instead; drop the `property.` prefix |
| `specs/runner` | `Runner`, `MinimalRunner`, `BlockRunner`, `BytecodeRunner`, `NewRunner`, `RunShard`, `RunCompiledSuite`, sharding helpers | Same names, native in `specs` | Import `specs` instead; drop the `runner.` prefix |
| `specs/*_reexport.go` (5 files) | Only aliasing/forwarding — no behavior of its own | Removed as files; every symbol they forwarded still exists, natively, in `specs` | Nothing to migrate if you used `specs.X` |
| `specs/parallel` | `scheduler.go`/`scheduler_batch.go`, documented as an internal implementation detail (never imported the root `specs` or `specs/runner`, and never re-exported into `specs`) | Same code, now unexported inside `specs` | Was never part of the supported public surface — a direct import of `specs/parallel` was already unsupported in `v0.0.9`. `specs.ItParallel` (on `Builder`) is unaffected |
| `cmd/specs-ci` | Standalone CLI binary (`bench`/`coverage`/`doctor`/`generate`/`migrate`/`testcmd` subcommands, backed by `internal/cli/*`) — the target `main`'s `.goreleaser.yaml` pointed at, with no buildable source on either branch | Removed, no replacement | Never documented in README or `docs/` as a supported entrypoint. Flagged here only because it was physically present in the published `v0.0.9` module, not because it was ever advertised |
| `tools/coverageheatmap` | Standalone report generator; produced the committed `docs/COVERAGE.md` ("Generated automatically by tools/coverageheatmap") | Removed, along with `docs/COVERAGE.md` | Development-only tool, not a library import — no `specs.*` symbol was implicated. No replacement is offered |

### Changed

- Generated `Paths()` candidates now run under a subtest name that identifies them — `<spec breadcrumb>/case-<n>[-seed<s>][-<values>]` — instead of the shared literal `generated`, which Go disambiguated as `generated#01`. `go test -v` failure output names the candidate and the value that failed, and a Cartesian or `Sample` candidate can be re-run on its own with `go test -run` by pasting the name back in; the candidate part of the name carries no regexp metacharacter other than `.`. Selecting a single `Explore`/`ExploreCoverage`/`ExploreSmart` candidate with `-run` is not supported, because the explorer needs the feedback of the candidates `-run` skips — the values embedded in the name mean a diverging candidate does not match the pattern rather than silently running under it. Names are bounded (16 runes per value, 64 per values block, plus a `~<hash>` suffix when truncated); values that would render a pointer address, or that a bounded reflective walk could not fully rule out as one (depth/element budget exhausted, or a map — whose entry order is randomized per process — holding an unstable value), collapse to a stable kind word, and a value type redacts itself from test names by implementing `fmt.Stringer`. See [docs/EXECUTION_MODEL.md](docs/EXECUTION_MODEL.md#generated-candidate-identity). ([#103](https://github.com/getsyntegrity/go-specs/issues/103))
- Reporter events for generated candidates now carry the candidate's own name — `includes tier [tier=pro] #2` — instead of the bare spec name repeated per candidate, so each executed candidate is distinguishable in report output and the ordinal links it to its `go test -v` subtest. ([#103](https://github.com/getsyntegrity/go-specs/issues/103))
- Sequential specs now run as a subtest named by their full `Describe`/`When`/`It` breadcrumb
  instead of the leaf `It` name alone, in both sequential execution models (`Describe`/`Spec` and
  `Builder`/`Runner`). Two specs sharing an `It` name under different scopes are no longer told
  apart by `testing`'s incidental `#01` suffix, and `go test -run 'TestX/Suite/when_b/does_a_thing'`
  selects one behaviour by its declared context. The breadcrumb is joined with `/` and never
  escaped, so the guarantee is stated over the name `testing` actually uses: two specs are
  independently identifiable whenever their *normalized* breadcrumbs differ. Breadcrumbs that
  normalize to the same string — `When("a/b")` against `Describe("a")`/`When("b")`, or `When("when a")`
  against `When("when_a")` — stay ambiguous and keep `testing`'s `#01` numbering. That is a
  consequence of go-specs flattening the declared tree into a single `t.Run` per spec, not a
  limitation `testing` imposes: the whole breadcrumb has to fit in one subtest name. The discarded
  alternative is nesting a real `t.Run` per scope, which would cost the allocation-free runner; it is
  not escaping, which would break `-run` patterns typed from the declared names. The mapping is an
  internal detail and is not exported.

  Reporter `Name` and `Path` values are unchanged by *this* entry: `specEventPath` is byte-identical
  before and after it. `Name` stays the declared leaf name verbatim. `Path` was separately broken —
  the `Describe`/`Spec` model rebuilt it by splitting the joined breadcrumb on `/`, so
  `It("slash/inside")` reported four segments whose last one was not the `Name`; that is fixed below
  ([#113](https://github.com/getsyntegrity/go-specs/issues/113)). The `Builder`/`Runner` model
  reported no `Path` at all, also fixed below
  ([#112](https://github.com/getsyntegrity/go-specs/issues/112)).

  This applies to every run whose backend is a real `*testing.T`, including `DescribeFlat` and
  `DescribeFast`: despite their names, both create one subtest per spec exactly like `Describe`, so
  both are renamed by this change too. Only a `*testing.B` backend runs without subtests and is
  genuinely unaffected.

  **Migration.** A `-run` pattern that named a spec by its `It` name alone no longer matches; it now
  needs the full breadcrumb. `go test -run 'TestCart/has_no_items'` becomes
  `go test -run 'TestCart/Cart/empty/has_no_items'`. This affects CI scripts that pin specific
  specs, and saved per-test re-run buttons in IDEs, which will silently select nothing until the
  stored pattern is regenerated.

  Two pre-existing behaviours became easier to meet once `-run` was documented above as a way to
  select specs — both are now fixed. The `Builder`/`Runner` model used to still execute a discarded
  spec's group `BeforeEach`/`AfterEach` hooks, because those hooks ran outside the spec's own subtest;
  `runSpecWithHooks` now runs each spec's before hooks, body, and after hooks as one unit inside its
  own subtest, so a `-run` filter discards a spec's hooks along with it, matching `Describe`/`Spec`
  ([#109](https://github.com/getsyntegrity/go-specs/issues/109)). And a spec whose subtest the filter
  discarded no longer gets reported to an attached reporter as passed — see `Filtered: true` below
  ([#111](https://github.com/getsyntegrity/go-specs/issues/111)).
  ([#102](https://github.com/getsyntegrity/go-specs/issues/102))

### Fixed

- A failing assertion on a `Context` with no backend now returns quietly instead of panicking.
  `ctx.Expect(x).To(m)`, `specs.ExpectT(ctx, x).To(m)` and `ctx.Expect(x).ToEqual(y)` checked the
  context but not the backend behind it, so once the matcher or comparison rejected the value the
  reporting tail dereferenced a nil `testBackend` and crashed at `reportMatcherFailure` rather than
  reporting the assertion the user wrote. A backend-less `Context` is a reachable state —
  `specs.NewContext(nil)` produces one, and so does a `Context` returned to the pool, which
  `Reset(nil)` leaves with a nil backend. `EqualTo`, `ExpectT(...).ToEqual` and `Snapshot` already
  guarded it and returned without reporting; all six assertion entry points now degrade the same
  way, and the guarded paths still release their pooled `Expectation`. The passing fast path is
  unchanged — the guard is one nil comparison on the failure-side branch, `BenchmarkMatcher_GoSpecs`
  shows no regression, and all assertion benchmarks stay at zero allocations.
  ([#150](https://github.com/getsyntegrity/go-specs/issues/150))
- `snapshots.Save` no longer truncates the destination snapshot file before writing its replacement.
  It renders the new content into a temporary file in the same directory, flushes it, sets the
  published `0644` mode, and swaps it over the destination with a single `os.Rename`. An
  interruption, timeout, or disk error part-way through a `GO_SPECS_UPDATE_SNAPSHOTS=1` run therefore
  leaves the previous `.snap.json` intact and readable instead of a half-written file that fails to
  parse on the next run; the staging file is removed on every failure path. Staging beside the
  destination rather than in `os.TempDir()` is what keeps the rename atomic, because a rename across
  filesystems is not. The `fileLocks` comment no longer implies more than it delivers: that mutex
  serializes the load-mutate-save cycle *within one process* and does not coordinate independent
  `go test` package processes — crash safety comes from the rename, not the lock.
  ([#152](https://github.com/getsyntegrity/go-specs/issues/152))

- A failing typed matcher assertion — `specs.ExpectT(ctx, x).To(m)` — now marks the `Context` as
  failed, as the untyped `ctx.Expect(x).To(m)` already did. `expectT[T].To` reported the failure
  straight to the backend without calling `recordFailure`, so `go test` still exited red but the
  spec was reported to `report.EventReporter` with `Failed: false`, `SuiteEndEvent.FailedSpecs`
  undercounted it, and `Runner{FailFast: true}` kept running the groups after it. This is the same
  defect class as the snapshot path in [#115](https://github.com/pablogore/go-specs/issues/115);
  only specs whose failing assertion used the typed `.To(matcher)` form were affected — the typed
  `.ToEqual` and every untyped form already recorded it. The passing fast path is unchanged
  (`BenchmarkMatcher_GoSpecs` stays at 0 allocs/op); `recordFailure` runs only on the failure
  branch.

- A failing `ctx.Snapshot` was reported to a `report.EventReporter` as a **passing** spec
  (`SpecResultEvent.Failed` stayed `false`, and `SuiteEndEvent.FailedSpecs` did not count it), even
  though `go test` exited non-zero. #125 attempted to fix this by having `Context.Snapshot` call
  `c.recordFailure()` after `runSnapshot` returned `false` — but `snapshots.RunFromFile` reports a
  mismatch by calling `backend.Fatalf` directly, and on a real `*testing.T`, `Fatalf` ends in
  `FailNow()` → `runtime.Goexit()`, which unwinds the calling goroutine right there and never
  returns to `RunFromFile` or `Context.Snapshot`. `recordFailure()` was therefore unreachable in
  production; #125's own test used a fake backend whose `Fatalf` records the message and returns
  normally, so it never exercised the Goexit path it was meant to fix. `snapshots.Evaluate` now
  separates comparison from reporting: it returns a `Result{Passed, Message}` without calling
  `Fatalf`, so `Context.Snapshot` can call `c.recordFailure()` *before* triggering the backend's
  `Fatalf` — the same before-Fatalf ordering every other assertion in this package already follows.
  `snapshots.RunFromFile`'s existing bool-returning, Fatalf-calling contract is preserved as a thin
  wrapper over `Evaluate`, so it and its own tests are unaffected. Pinned by
  `TestSnapshotMismatchRealProcessRecordsFailure`, a subprocess test against a real `*testing.T` —
  a fake-backend test cannot catch this class of bug, since a fake `Fatalf` doesn't `Goexit`.
  ([#115](https://github.com/getsyntegrity/go-specs/issues/115))

- A spec whose subtest is discarded by `-run` (e.g. `go test -run 'TestX/suite/when_b'` against a
  suite with a sibling `when_a` spec) is no longer reported to `report.EventReporter` as passed.
  `runSpecIsolated` (`Builder`/`Runner`) and `runSpecProgramIsolated` (`Describe`/`ExecutionPlan`)
  both run the spec's body inside a `testing.T.Run` call, but neither its return value nor
  `ctx.failed` can tell a filtered-out spec apart from one that ran and passed: `testing.T.Run`
  itself returns `true` for a subtest `-run` never invoked — a subtest that never ran is treated as
  vacuously passing — and `ctx.failed` is simply never touched when the body doesn't execute. Both
  functions now set a flag from inside the `t.Run` closure itself, which only runs when the filter
  accepts the subtest, and report the spec as `report.SpecResultEvent{Filtered: true}` instead of a
  bare pass. `Failed` stays `false` and `Duration` stays `0`, the same shape a compile-time `Skipped`
  spec has, but `Skipped` itself stays `false`: the cause is external test selection, not a declared
  `XIt`/`Skip`, and a consumer that needs to tell the two apart still can. `report.SuiteEndEvent`
  gains a matching `FilteredSpecs` field, and `TotalSpecs` now counts it too.
  ([#111](https://github.com/getsyntegrity/go-specs/issues/111))

- `Spec.flat` — written by `DescribeFlat`/`DescribeFast` but never read by anything downstream, since
  `CompiledSuite` never carried it — has been removed, along with the `flat` parameter threaded
  through the internal `describeWithCompiler`/`describeWithCompilerContext` helpers. `DescribeFlat`
  and `DescribeFast` are now documented aliases for `Describe`: this was already their exact runtime
  behavior (both create one subtest per spec against a `*testing.T`, exactly like `Describe`, per
  `TestDescribeFlatSubtestIdentityRealProcess`/`TestDescribeFastSubtestIdentityRealProcess`), so no
  caller-visible behavior changes. Wiring the flag up instead — skipping the per-spec subtest for
  these two entry points — was the alternative; it was rejected because it would have reintroduced
  the `Fatalf`/`FailNow` cross-spec failure #74 fixed, only for `DescribeFlat`/`DescribeFast` callers.
  ([#110](https://github.com/getsyntegrity/go-specs/issues/110))

- Failing built-in assertions now report the user's own assertion file and line instead of a
  go-specs internal frame such as `testing_backend.go` or `context.go`. Terminal and IDE output
  jump straight to the behaviour that broke. Covers `EqualTo`, `ExpectT(...).ToEqual`/`.To`,
  `Context.Expect(...).ToEqual`/`.To`, and `ctx.Snapshot`, across `Describe`, `DescribeFlat`,
  `DescribeFast`, and the Builder/Runner path. The passing fast path is unchanged: caller discovery
  still happens only on failure, and assertions still allocate nothing when they pass.
  ([#101](https://github.com/getsyntegrity/go-specs/issues/101))

- `ItParallel`/`RunParallel`/`RunParallelBatched` failures now embed the user's own assertion file
  and line in the reported message text, e.g. `spec[0]: /path/to/spec_test.go:42: expected 1 to
  equal 2`. The worker goroutine that ran the assertion has already exited by the time the failure
  is reported, so `testing.T.Helper()` — the mechanism #101 above uses — has no live frame left to
  mark; the location is instead captured while the goroutine's frame is still live and carried as
  data (file, line) until the failure is formatted into text at the one place a plain `go test` run
  can show it. Go's own primary-decorated location (the line `go test` prints right after
  `--- FAIL:`) still names an internal go-specs frame — that part of #101's fix does not extend to
  the parallel path, and is not expected to. `report.EventReporter`'s `SpecResultEvent.Message` is
  unchanged: it still receives the failure string verbatim, with no location text mixed in.
  ([#108](https://github.com/getsyntegrity/go-specs/issues/108))

- `report.SpecStartEvent.Path` no longer splits a declared name that contains `/`. It was rebuilt by
  splitting the slash-joined breadcrumb, so `It("slash/inside")` arrived as two segments: the event
  reported more scopes than were declared and its last segment was not `Name`, which made a reporter
  rendering a tree, or deriving a JUnit `classname` from all but the last segment, invent a scope
  nobody wrote. The compiler now carries the declared segments through to the event
  ([#113](https://github.com/getsyntegrity/go-specs/issues/113)).

- Running a suite without a `report.EventReporter` no longer builds a `Path` per spec. It was built
  unconditionally and then dropped, costing one allocation per spec per run on the reporter-less
  path this package advertises as allocation-free. Over 2000 specs that is half of the run's total
  allocations: `allocs/op` 4.004k → 2.003k, `B/op` −14%.

- `report.SpecStartEvent.Path` is now reported by the `Builder`/`Runner` execution model too — it
  previously stayed `nil` for every spec, unlike the `Describe`/`Spec` model, which leaves a reporter
  (a JUnit `classname`, a tree renderer, grouped CI output) unable to tell a suite's structure apart
  when a program was built through `Builder`. `Path` is built from the enclosing `Describe` names
  captured at registration time, never by splitting the joined breadcrumb — the same defect #113
  fixed for the other model would otherwise resurface for a declared name containing `/`. Covers
  sequential specs, `SkipIt` specs, and `ItParallel` specs alike.
  ([#112](https://github.com/getsyntegrity/go-specs/issues/112))

- `PathValues.Hash()` is now sensitive to the content of `string`, `float64`, and `float32` values
  instead of falling through to the position-only fallback used for types the hash doesn't recognize.
  Strings are hashed byte-by-byte with an FNV-like multiply; floats use their IEEE bit pattern
  (`math.Float32bits`/`math.Float64bits`). Two `PathValues` differing only in a string or float value
  no longer collide on the same hash. Pointer/map/func/etc. values keep the conservative position-only
  fallback unchanged.
  ([#122](https://github.com/getsyntegrity/go-specs/issues/122))

### Added

- `benchmarks/paths_bench_test.go`: path-exploration benchmarks for a large Cartesian run and a sampled run, covering the third cost center `BENCHMARKS.md` names.

- `ExecutionPlan.PathScopes`, `ExecutionPlan.PathScopeStart` and `ExecutionPlan.PathScopeLen` hold
  the declared scopes enclosing each spec, laid out like `Instructions`/`ProgramStart`/`ProgramLen`.
  The spec's own name is not repeated — `Names[i]` already holds it, and `Path` is the window
  followed by `Names[i]`. Specs sharing enclosing scopes share one window, so this grows with the
  shape of the suite rather than with the spec count. `FullNames` is unchanged and still carries the
  subtest identity.

- `ExecutionPlan` is exported, and `Path` is now derived from the fields above rather than from
  `FullNames`. A hand-built plan that populates only `FullNames` therefore reports `Path: nil` where
  it previously reported the split breadcrumb; populate `PathScopes`/`PathScopeStart`/`PathScopeLen`
  to report a path. Plans built through `Describe`/`BuildSuite` are unaffected.

- `assert.EqualValues(t testing.TB, a, b any) bool` compares two values for use as a test helper:
  wrapped errors compare via `errors.Is` (either direction), everything else via `reflect.DeepEqual`.
  Moved from the `matchers` package; `assert` is now the single source of truth for equality helpers.
  ([#128](https://github.com/getsyntegrity/go-specs/issues/128))

### Known issues

- Narrowing `go test -run` to a single `ExploreCoverage`/`ExploreSmart` generated candidate's
  subtest — the natural way to isolate a failure and re-run it — does not isolate it from the
  strategy's corpus/coverage state. `-run` only gates the `t.Run` call around a candidate's
  execution; the surrounding Propose/Execute/AdmitFeedback loop still iterates every candidate up to
  it, and each one `-run` discards never populates its `Coverage`, so the corpus these two strategies
  draw from can end up smaller than it was on the run that produced the failure. The candidate
  actually generated for the target attempt index can then differ from the one that failed, even
  with the same seed. With an ordinal/prefix `-run` pattern, that divergence can execute the
  different `PathValues` in place of the one that failed; with the exact copied candidate name
  (values and hash included), it can instead make the regenerated candidate stop matching the
  pattern, so no candidate executes at all. `Cartesian`, `Sample`, and plain `Explore` are not
  exposed — see `docs/EXECUTION_MODEL.md`'s "Adaptive strategies" section for the full mechanism.
  Documentation only; no code change. Tracked in
  [#124](https://github.com/getsyntegrity/go-specs/issues/124).
