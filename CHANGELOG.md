# Changelog

All notable changes to this project are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project is pre-1.0, so any release may include breaking changes (see [README.md#project-status](README.md#project-status)).

Entries for `v0.0.1`–`v0.0.9` predate this file — see [GitHub Releases](https://github.com/pablogore/go-specs/releases) for their auto-generated notes.

## [Unreleased]

### Breaking Changes / Migration from v0.0.9

`v0.0.9` was published from a branch that had reorganized the module into `specs/compiler`, `specs/dsl`, `specs/ctx`, `specs/property`, and `specs/runner` subpackages (plus `cmd/specs-ci` and `tools/coverageheatmap`), with five `specs/*_reexport.go` files aliasing every subpackage symbol back into the root `specs` package so `specs.Program`, `specs.Describe`, `specs.Context`, and friends kept compiling. This release reverts that reorganization: the module returns to the single flat `specs` package that predates it, and the subpackages, the reexport files, `cmd/specs-ci`, and `tools/coverageheatmap` are all gone. See [#127](https://github.com/getsyntegrity/go-specs/issues/127) for the full diagnosis of how the two layouts diverged.

**If you only imported the root `specs` package** (`import "github.com/pablogore/go-specs/specs"`, using `specs.Program`, `specs.Describe`, `specs.Context`, `specs.PathValues`, `specs.Runner`, etc.) — **nothing breaks.** Every one of those identifiers still exists in `specs`, now as a native declaration instead of a type alias / forwarding var.

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

- Generated `Paths()` candidates now run under a subtest name that identifies them — `<spec breadcrumb>/case-<n>[-seed<s>][-<values>]` — instead of the shared literal `generated`, which Go disambiguated as `generated#01`. `go test -v` failure output names the candidate and the value that failed, and a Cartesian or `Sample` candidate can be re-run on its own with `go test -run` by pasting the name back in; the candidate part of the name carries no regexp metacharacter other than `.`. Selecting a single `Explore`/`ExploreCoverage`/`ExploreSmart` candidate with `-run` is not supported, because the explorer needs the feedback of the candidates `-run` skips — the values embedded in the name mean a diverging candidate does not match the pattern rather than silently running under it. Names are bounded (16 runes per value, 64 per values block, plus a `~<hash>` suffix when truncated); values that would render a pointer address, or that a bounded reflective walk could not fully rule out as one (depth/element budget exhausted, or a map — whose entry order is randomized per process — holding an unstable value), collapse to a stable kind word, and a value type redacts itself from test names by implementing `fmt.Stringer`. See [docs/EXECUTION_MODEL.md](docs/EXECUTION_MODEL.md#generated-candidate-identity). ([#103](https://github.com/pablogore/go-specs/issues/103))
- Reporter events for generated candidates now carry the candidate's own name — `includes tier [tier=pro] #2` — instead of the bare spec name repeated per candidate, so each executed candidate is distinguishable in report output and the ordinal links it to its `go test -v` subtest. ([#103](https://github.com/pablogore/go-specs/issues/103))
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

- A failing `ctx.Snapshot` is no longer reported to a `report.EventReporter` as a **passing** spec.
  `snapshots.RunFromFile` decided the pass/fail verdict and reported it straight to the backend (so
  `go test` still exited non-zero) but never touched `ctx.failed`, so `SpecResultEvent.Failed` stayed
  `false` and `SuiteEndEvent.FailedSpecs` didn't count it for any reporter-driven consumer (JUnit
  writer, CI summary, flake tracker). `Context.Snapshot` now calls `recordFailure()` on a mismatch,
  like every other assertion does.
  ([#115](https://github.com/getsyntegrity/go-specs/issues/115))

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
