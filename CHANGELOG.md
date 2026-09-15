# Changelog

All notable changes to this project are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project is pre-1.0, so any release may include breaking changes (see [README.md#project-status](README.md#project-status)).

Entries for `v0.0.1`–`v0.0.9` predate this file — see [GitHub Releases](https://github.com/pablogore/go-specs/releases) for their auto-generated notes.

## [Unreleased]

### Changed

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
  ([#113](https://github.com/getsyntegrity/go-specs/issues/113)). The `Builder`/`Runner` model still
  reports no `Path` at all, tracked in
  [#112](https://github.com/getsyntegrity/go-specs/issues/112).

  This applies to every run whose backend is a real `*testing.T`, including `DescribeFlat` and
  `DescribeFast`: despite their names, both create one subtest per spec exactly like `Describe`, so
  both are renamed by this change too. Only a `*testing.B` backend runs without subtests and is
  genuinely unaffected.

  **Migration.** A `-run` pattern that named a spec by its `It` name alone no longer matches; it now
  needs the full breadcrumb. `go test -run 'TestCart/has_no_items'` becomes
  `go test -run 'TestCart/Cart/empty/has_no_items'`. This affects CI scripts that pin specific
  specs, and saved per-test re-run buttons in IDEs, which will silently select nothing until the
  stored pattern is regenerated.

  Two pre-existing behaviours become easier to meet now that `-run` is a documented way to select
  specs. Under a narrow `-run` pattern the `Builder`/`Runner` model still executes the group
  `BeforeEach`/`AfterEach` hooks of specs the pattern discarded, because those hooks run outside the
  subtest; the `Describe`/`Spec` model does not. And in either model a spec whose subtest the filter
  discarded is still reported to an attached reporter as started and finished without failing, so it
  appears as passed although its body never ran. Both are tracked separately.
  ([#102](https://github.com/getsyntegrity/go-specs/issues/102))

### Fixed

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

### Added

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

### Known issues

- A failing `ctx.Snapshot` is reported to a `report.EventReporter` as a **passing** spec
  (`SpecResultEvent.Failed` is `false`, and `SuiteEndEvent.FailedSpecs` does not count it), even
  though `go test` exits non-zero. Pre-existing and unrelated to the attribution fix above; every
  other assertion records the failure correctly. Pinned by
  `TestSnapshotFailureLeavesContextUnfailed`.
  ([#115](https://github.com/getsyntegrity/go-specs/issues/115))
