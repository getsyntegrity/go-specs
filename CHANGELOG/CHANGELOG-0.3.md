<!-- BEGIN MUNGE: GENERATED_TOC -->

  - [[v0.3.1] - 2026-09-29](#v031---2026-09-29)
    - [Fixed](#fixed)
  - [[v0.3.0] - 2026-09-29](#v030---2026-09-29)
    - [Added](#added)
    - [Changed](#changed)
    - [Removed](#removed)
    - [Fixed](#fixed-1)

<!-- END MUNGE: GENERATED_TOC -->

<!-- NEW RELEASE NOTES ENTRY -->

## [v0.3.1] - 2026-09-29

### Fixed

- An empty `Describe`/`When` name no longer adds an empty segment to the Go subtest name. `Describe(t, "", fn)` used to run its specs as `TestX//case` (and a nested empty scope as `TestX/outer//case`), so `go test -run 'TestX/case'` selected nothing and passed green; they now run as `TestX/case` and `TestX/outer/case`. Report `Path` no longer contains an empty element for such a scope, on both the `Spec` and `Builder` engines, `ItParallel` included. Non-empty scope names, including a non-empty root `Describe` name, are unchanged. Two specs that now share a name (a root `It("a")` and an `It("a")` under `Describe("")`) get testing's usual `#01` suffix. A `BeforeAll`/`AfterAll` group still needs an explicit non-empty name. See `docs/DSL.md`.

[v0.3.1]: https://github.com/getsyntegrity/go-specs/compare/v0.3.0...v0.3.1

<!-- NEW RELEASE NOTES ENTRY -->

## [v0.3.0] - 2026-09-29

### Added

- `ctx.Go(func(*Context))` runs a task that is bound to its spec, the supported way to make concurrent assertions (#318). The spec waits for every task before its `AfterEach` hooks run and before it is reported or its `Context` is reused; assertion failures, `ctx.T` failures and panics inside a task are charged to the spec that started it (a panic is reported as an error), and tasks may start further tasks. Calling `ctx.Go` after its spec finished panics with an actionable `specs:` message, but only until that pooled `Context` is reused by a later spec; after reuse a `ctx.Go` issued through a stale handle does not panic and may be attributed to whichever spec owns the `Context` then (a task's own `*Context` is never pooled and always panics). Retaining a spec's `ctx` past the end of its spec, including in a goroutine launched directly with `go`, is unsupported and unprotected, because a pooled `Context` cannot tell a stale goroutine from the next spec. Specs that never call `ctx.Go` allocate nothing extra. See `docs/DSL.md`.
- **Breaking (report schema `"2"` → `"3"`).** A spec `CompiledSuite.SetFailFast(true)` or
  `Runner.FailFast` prevented from ever running — because an earlier spec in the same run already
  failed — is now reported as a new status, `report.StatusUnstarted` (`"unstarted"`), instead of
  simply never appearing in the report. `report.Totals` gains an `Unstarted` field and
  `report.Case` gains a `Declared` field (`"skip"`/`"pending"`/empty) that preserves a
  `SkipIt`/`PendingIt` spec's original declaration when the group containing it was never reached.
  `Totals.Total` keeps its pre-existing meaning — specs that entered execution, plus declared
  skip/pending that were actually processed — and an Unstarted spec never counts toward it, so a
  3-spec suite whose first spec fails now reports `Total: 1, Failed: 1, Unstarted: 2` rather than
  `Total: 1` alone. JUnit XML (`report.RenderXML`) renders an Unstarted case as
  `<skipped message="not run: fail-fast"/>`, folded into the `skipped` attribute like Pending and
  Filtered already are; its `tests` attribute becomes `total + unstarted` so a JUnit consumer still
  sees the full declared suite size. JSON, HTML and plain-text renderers gain matching
  `unstarted`/`Unstarted` fields. Applies to both execution engines (`CompiledSuite` and
  Builder/`Runner`), including hook groups, `ItParallel` batches and `RunShard` (shard-local: a
  shard reports only its own unstarted specs). See `docs/REPORTING.md`'s "Unstarted semantics and
  assumptions" for two flagged assumptions this decision does not itself settle.
  ([#274](https://github.com/getsyntegrity/go-specs/issues/274))
- `tools/release validate` is a read-only check that `CHANGELOG.md`'s `[Unreleased]` section lists each recognized Keep a Changelog category at most once and in canonical order (`Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, `Security`); CI's `verify` job runs it so a duplicated or misordered section is caught before release prep (#326).

### Changed

- **Breaking.** Nested `AfterEach` hooks registered through `Builder` now run in the documented order
  (`docs/DSL.md`, `docs/ARCHITECTURE.md`), the same as the `Spec` engine (`Spec.It`, `Spec.ItParallel`):
  innermost scope first, and last registered first within a scope. `Builder.It` used to run the outer
  scope's hooks before the inner scope's; `Builder.ItParallel` already ran the inner scope first but ran
  the hooks of one scope in registration order. A single scope's `AfterEach` order under `Builder.It` is
  unchanged. Suites whose teardown depends on the old order must reorder their hooks. Example:

  ```go
  b.Describe("outer", func() {
      b.AfterEach(afterOuter1)
      b.AfterEach(afterOuter2)
      b.Describe("inner", func() {
          b.AfterEach(afterInner1)
          b.AfterEach(afterInner2)
          b.It("spec", body)
      })
  })
  // Builder.It before:         body, afterOuter2, afterOuter1, afterInner2, afterInner1
  // Builder.ItParallel before: body, afterInner1, afterInner2, afterOuter1, afterOuter2
  // Now (every engine):        body, afterInner2, afterInner1, afterOuter2, afterOuter1
  ```

- **Breaking.** A committed `FIt` (or `Focus`/`ItWith(specs.Focus(...))`) is no longer allowed to
  silently turn a partial suite green: whenever at least one focused spec is active anywhere in a
  `Describe`/`BuildSuite`/`Analyze` call, the `Builder`/`Runner` program, or `RunShard`, the run now
  fails the enclosing test — always, locally and in CI, with no CI-only detection — via
  `tb.Errorf("go-specs: N focused spec(s) (FIt) are active, M spec(s) excluded; remove FIt or set
  GO_SPECS_ALLOW_FOCUS=1")`. The failure is reported through `Errorf`, not `Fatal`/`FailNow`: the
  focused specs still run and report their own pass/fail as before.
  - The only opt-out is the environment variable `GO_SPECS_ALLOW_FOCUS=1`, read once per suite
    build/run (never per spec). It disables only this failure; focus still filters exactly as
    before — only the focused specs execute.
  - In both modes, every spec a focus filter excludes — an ordinary `It`, and now also a
    `SkipIt`/`PendingIt` mark that focus previously dropped without a trace — is reported as
    `Filtered` (`report.SpecResultEvent.Filtered`/`report.StatusFiltered`), exactly once, including
    under `CompiledSuite.RunShard`/the package-level `RunShard` (deterministic shard-0 rule; see
    `docs/DSL.md`'s focus section). This change adds no new `report.Status` value and does not
    itself bump the report schema version (#274 does, separately): `Filtered` already existed for
    `go test -run` exclusion and now also covers focus exclusion.
  - Migration: a suite that intentionally keeps a committed `FIt` (e.g. a WIP branch, or CI
    debugging) must set `GO_SPECS_ALLOW_FOCUS=1` in that environment, or `t.Setenv("GO_SPECS_ALLOW_FOCUS",
    "1")` in the test itself, or remove the `FIt`. ([#273](https://github.com/getsyntegrity/go-specs/issues/273))

- Two sibling `Describe`/`When` groups sharing a literal name now get distinct reported `Path`s:
  the first keeps its declared name, and each later sibling gets `name#k` for the smallest `k>=2`
  that never collides with a literal sibling name or an already-assigned label (see docs/DSL.md's
  "Duplicate sibling group names"). This changes report `Path`s — JSON, JUnit XML, TXT, HTML, and a
  hooked group's synthetic `[BeforeAll]`/`[AfterAll]` case — **only** for a suite that declares such
  a duplicate; every other suite's `Path`s are byte-identical to before. Go subtest identity and
  `-run` selection are untouched: #102's accepted `#01` ambiguity for a normalized collision still
  applies exactly as documented there.
  ([#275](https://github.com/getsyntegrity/go-specs/issues/275))

- Built-in assertion failures now reach every reporter with their message: `report.SpecResultEvent.Message`
  (and therefore `Case.Message` in JSON, the JUnit `<failure message=...>` attribute, HTML and TXT) carries
  the assertion text, e.g. `expected 2 to equal 3`, instead of being empty. This applies to
  `ctx.Expect`/`ExpectT`/`EqualTo`/`Context.Snapshot` failures on every execution mode: `Describe`,
  `Builder`/`Runner`, `Spec.ItParallel` and `Builder.ItParallel`. `Builder.ItParallel` already carried
  the text through its parallel failure record, so it is unchanged; this closes the gap on the other
  three. A recovered panic's message still takes
  priority. A failure raised directly through `ctx.T` (`Error`, `Fatal`, `FailNow`) still has no message,
  because go-specs never sees that text. Consumers that treated an empty `Message` on a failed case as
  "assertion failure" should read `Status` instead. ([#272](https://github.com/getsyntegrity/go-specs/issues/272))

- A recovered panic in a `Builder.ItParallel` spec is now reported with status `error` and its stack
  trace in `Output`, the same as a panic in a sequential spec or in `Spec.ItParallel`. It used to be
  reported as `failed` with no stack. Assertion failures in the same specs are still `failed`. JUnit
  output moves those cases from `<failure>` to `<error>`. ([#314](https://github.com/getsyntegrity/go-specs/issues/314))

- Lower per-spec cost on the sequential real-`*testing.T` path: the subtest closure and its bookkeeping
  are now built once per pooled `Context` instead of once per spec. In `make bench-e2e`, `BuildSuite`
  (run only) drops from 30 to 25 allocations per spec, matching the bare `t.Run` baseline, and
  `Describe` (declare and run) from 31 to 26. A `Context` idle in
  the pool also no longer keeps the last spec's program, suite plan and finished `*testing.T` alive.
  No API change. ([#244](https://github.com/getsyntegrity/go-specs/issues/244), [#304](https://github.com/getsyntegrity/go-specs/issues/304))

### Removed

- **Breaking.** Remove the unused `gen/generators` package. It had no consumers in this repository; external imports of `github.com/getsyntegrity/go-specs/gen/generators` must supply their own test inputs.

### Fixed

- Under `go test -run`, `Builder.ItParallel` specs are now selected individually, like `Spec.ItParallel`: the matching spec runs and is reported with its real status, and every excluded spec is reported `filtered` in declaration order and counted in the suite totals. Before, the whole batch ran under one generated subtest that no selector could match, so nothing ran and nothing was reported. Each spec now has its own subtest named by its `Describe` breadcrumb (for example `suite/two` instead of `suite/#00` in `go test -v`), and a failing spec marks its own subtest FAIL; runs without `-run` report the same results (#330).
- Finalized report files are now published with mode `0644` (via an explicit chmod, independent of umask) instead of `os.CreateTemp`'s `0600`, and are durable: the temp file is `fsync`ed before the rename and the containing directory is synced after it (skipped on Windows and where unsupported). Multiple targets are published individually, each atomically; a failure part-way leaves earlier targets published and the error names the failing target. The mode is exported as `coordination.ReportFileMode` (#319).
- A malformed `config-error.json` now makes `coordination.Finalize` return a `*ConfigError` (reason `malformed-config-error`, message naming the file and the parse failure), so `go-specs-report finalize` exits 78 instead of 1, renders nothing and keeps the shards (#311).
- `coordination.Finalize` (and `go-specs-report finalize`) now rejects, before rendering or deleting anything, `Cleanup` with no output target and any output target whose resolved path (relative paths made absolute, symlinks resolved) lies inside the run directory. Both are configuration failures (`*ConfigError`, exit 78) and leave every shard in place; before, `finalize -cleanup` with no output flags silently deleted all shards, and a target inside the run directory was rendered and then deleted (#309).

- Documentation now shows a `go test` shard invocation that works: `go test ./... -args -- -shard 1/2` (or `SHARD=1/2`). `-shard` is not registered with Go's `flag` package, so the previously implied `-args -shard 1/2` exits with `flag provided but not defined`. Verified through a real `go test` subprocess; invalid values still fail closed (#312).
- Registering on a `*Spec` after its `Describe`/`When` scope closed (a captured handle used after `Describe` returned, or from inside an executing `It`) now panics with an actionable `specs: Spec.<Method> called after its Describe/When scope closed` message instead of a nil dereference or a write into a reused compiler (#317). The zero-value `Spec` diagnostic is unchanged.
- **Breaking.** `BeforeEach`/`AfterEach` registered after an `It` (or other spec) or a nested `Describe`/`When` in the same scope now panic at build time on `Spec` (compiler and `Analyze` paths) and `Builder`, instead of silently applying only to later specs (#307). Declare per-spec hooks before the first spec or nested scope of their scope; suites that relied on the old behavior must reorder them. See `docs/DSL.md`.

- JUnit XML no longer emits an empty `<properties></properties>` element when no coverage is attached; the container appears only when it holds coverage `<property>` entries, as some JUnit validators require (#316).

- Coverage profile parsing (`report.ParseCoverageProfile`, `report.ParseCoverageProfileMerged`, and so `go-specs report` finalization) now rejects a `mode:` other than `set`/`count`/`atomic`, negative execution or statement counts, and a repeated file/span with conflicting statement counts, instead of returning inflated or corrupt totals (#310).

- TXT and HTML reports now print each ordinary case as its full scope path (`Checkout/when the cart is empty/fails`) instead of the bare leaf name, so two failing specs with the same name under different `When` blocks are distinguishable and the disambiguated paths from #275 are actually visible in those formats. Group hook cases keep their `<group path> [BeforeAll]` label (#313).

- Module-wide merged reports now record which package each suite came from. `report.Suite` gains a `Package` field, filled from each shard's `PackagePath` and rendered as `package` in JSON, a `package` attribute plus a package-prefixed `classname` in JUnit, `Suite: <name> (package <path>)` in TXT, and a label beside the suite heading in HTML. Two packages with identical suite and spec names are no longer ambiguous. Single-package reports are unchanged (empty package omitted everywhere); the JSON `schemaVersion` stays `"3"` because a new field alone never bumps it (#308).


- `Contain`'s `FailureMessage` reported a genuinely missing element, an actual type it has no
  strategy for at all (an `int`, a `map`, `nil`, ...), and an `expected` value whose type could
  never match the actual's elements (a non-string needle against a string, or a mismatched slice
  element type) with the exact same wording — `expected 42 to contain 1` gave no hint that `42`
  was the real problem, not a missing `1`. `FailureMessage` now appends an explicit reason for the
  latter two cases (`Match`'s `bool` result, already `false` for both, is unchanged); the plain
  `expected X to contain Y` wording is kept as-is for a genuine miss. Map-key containment remains
  unsupported and is diagnosed the same as any other unsupported actual type; whether to add it is
  a separate decision. ([#277](https://github.com/getsyntegrity/go-specs/issues/277))

- `Builder.ItParallel` results are reported in declaration order. The parallel goroutines used to emit
  `SpecStarted`/`SpecFinished` as each spec finished, so the order of cases in every report depended
  on scheduling. They now match `Spec.ItParallel`, which already reported in declaration order. ([#315](https://github.com/getsyntegrity/go-specs/issues/315))

- `Builder.ItParallel` now runs `AfterEach` for every spec, whatever its outcome. A failing assertion,
  a panic in the body or a failing `BeforeEach` used to stop the spec before its `AfterEach` hooks, so
  cleanup was silently skipped; `Builder.It` and `Spec.ItParallel` already ran them. The hooks run after
  every `ctx.Go` task has finished, in the documented order: innermost scope first, last registered
  first within a scope (see the **Breaking.** `AfterEach` order entry under Changed). The first failure
  stays the reported one, and a panic inside an `AfterEach` is reported as an error.
  ([#334](https://github.com/getsyntegrity/go-specs/issues/334))

- A failing `Builder.ItParallel` spec now prints its failure message on its own Go subtest instead of
  on the parent test as `spec[N]: ...`, so `go test -v` shows the text under the spec's `--- FAIL` line.
  This applies with a real `*testing.T`; with any other `testing.TB` (for example a `*testing.B`) the
  message is still reported on the parent as before.
  ([#330](https://github.com/getsyntegrity/go-specs/issues/330))

[v0.3.0]: https://github.com/getsyntegrity/go-specs/compare/v0.2.0...v0.3.0
