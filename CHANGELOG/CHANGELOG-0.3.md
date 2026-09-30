<!-- BEGIN MUNGE: GENERATED_TOC -->

- [v0.3.2](#v032)
  - [Changelog since v0.3.1](#changelog-since-v031)
  - [Changes by Kind](#changes-by-kind)
    - [Other (Cleanup or Flake)](#other-cleanup-or-flake)
    - [Dependency](#dependency)
  - [Hand-written entries](#hand-written-entries)
  - [Dependencies](#dependencies)
    - [Added](#added)
    - [Changed](#changed)
    - [Removed](#removed)
  - [[v0.3.1] - 2026-09-29](#v031---2026-09-29)
    - [Fixed](#fixed)
  - [[v0.3.0] - 2026-09-29](#v030---2026-09-29)
    - [Added](#added-1)
    - [Changed](#changed-1)
    - [Removed](#removed-1)
    - [Fixed](#fixed-1)

<!-- END MUNGE: GENERATED_TOC -->

<!-- NEW RELEASE NOTES ENTRY -->

# v0.3.2

## Changelog since v0.3.1

## Changes by Kind

### Other (Cleanup or Flake)

- Update benchmark charts ([#343](https://github.com/getsyntegrity/go-specs/pull/343), [@go-specs-release[bot]](https://github.com/apps/go-specs-release))
- Add 20 scala-style matchers ([#345](https://github.com/getsyntegrity/go-specs/pull/345), [@pablogore](https://github.com/pablogore))
- Update benchmark charts ([#352](https://github.com/getsyntegrity/go-specs/pull/352), [@go-specs-release[bot]](https://github.com/apps/go-specs-release))
- Add structural diffs to equality failure diagnostics (#358) ([#364](https://github.com/getsyntegrity/go-specs/pull/364), [@pablogore](https://github.com/pablogore))
- Interface mocking with stubs, expectations and automatic verification ([#365](https://github.com/getsyntegrity/go-specs/pull/365), [@pablogore](https://github.com/pablogore))
- Add Eventually and Consistently assertions (#356) ([#366](https://github.com/getsyntegrity/go-specs/pull/366), [@pablogore](https://github.com/pablogore))
- Add Context.Cleanup, Errorf, Helper and Testing for helper packages ([#367](https://github.com/getsyntegrity/go-specs/pull/367), [@pablogore](https://github.com/pablogore))
- Examples, contract tests and docs for interface mocking ([#369](https://github.com/getsyntegrity/go-specs/pull/369), [@pablogore](https://github.com/pablogore))
- Shared cycle-safe rendering for assertion diagnostics ([#370](https://github.com/getsyntegrity/go-specs/pull/370), [@pablogore](https://github.com/pablogore))
- Deterministic, bounded ordering when the tie-break budget is exhausted ([#371](https://github.com/getsyntegrity/go-specs/pull/371), [@pablogore](https://github.com/pablogore))
- Fuzz assertion diagnostics over generated value graphs ([#372](https://github.com/getsyntegrity/go-specs/pull/372), [@pablogore](https://github.com/pablogore))
- Add typed table-driven spec registration (#360) ([#376](https://github.com/getsyntegrity/go-specs/pull/376), [@pablogore](https://github.com/pablogore))
- Add typed projection matcher with field diagnostics (#362) ([#377](https://github.com/getsyntegrity/go-specs/pull/377), [@pablogore](https://github.com/pablogore))
- Add semantic JSON equality with path diagnostics (#363) ([#378](https://github.com/getsyntegrity/go-specs/pull/378), [@pablogore](https://github.com/pablogore))
- Add quantified and ordered collection matchers (#359) ([#379](https://github.com/getsyntegrity/go-specs/pull/379), [@pablogore](https://github.com/pablogore))
- Property testing with shrinking and replay (#361) ([#380](https://github.com/getsyntegrity/go-specs/pull/380), [@pablogore](https://github.com/pablogore))
- Reorganize examples as a complete reference, one file per feature (#381) ([#382](https://github.com/getsyntegrity/go-specs/pull/382), [@pablogore](https://github.com/pablogore))
- Remove stale v0.3.2 heading from develop ([#385](https://github.com/getsyntegrity/go-specs/pull/385), [@pablogore](https://github.com/pablogore))

### Dependency

- Bump pgregory.net/rapid from 1.2.0 to 1.3.0 in /property in the gomod-minor-patch group across 1 directory ([#386](https://github.com/getsyntegrity/go-specs/pull/386), [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump actions/cache/restore from 4.3.0 to 6.1.0 ([#387](https://github.com/getsyntegrity/go-specs/pull/387), [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump actions/cache/save from 4.3.0 to 6.1.0 ([#388](https://github.com/getsyntegrity/go-specs/pull/388), [@dependabot[bot]](https://github.com/apps/dependabot))
- Bump actions/cache from 4.3.0 to 6.1.0 in /.github/actions/go-setup ([#389](https://github.com/getsyntegrity/go-specs/pull/389), [@dependabot[bot]](https://github.com/apps/dependabot))

## Hand-written entries

#### Added

- `specs.Table` and `specs.TableParallel`: typed table-driven spec registration (#360). `Table(s, rows, name, body)` registers each row as its own ordinary `It` (`TableParallel` uses `ItParallel`), so every row is its own Go subtest selectable with `-run`, runs inside the enclosing `BeforeEach`/`AfterEach` hooks, and is reported and failed independently, with no extra hierarchy segment. Empty names, duplicate names (including names that differ only by space versus underscore) and nil functions panic at registration before any row is registered; rows are copied at registration. Plain `for` plus `It` remains supported. See `docs/DSL.md` and `examples/table`.
- Quantified and ordered collection matchers (#359), in `assert` and re-exported from `specs`: `EveryElement(m)`, `AnyElement(m)`, `NoElement(m)`, `ExactlyNElements(n, m)`, `AtLeastNElements(n, m)`, `AtMostNElements(n, m)`, `HaveElementsInOrder(ms...)` (exact sequence) and `ContainElementsInOrder(ms...)` (subsequence). They judge each element of a slice or array with a child matcher and their failure messages name the failing or matching elements by zero-based index (at most 10 listed). An empty or nil collection follows logic (`EveryElement` and `NoElement` hold vacuously, `AnyElement` does not), an unsupported value, a nil child matcher or a negative count never matches, and each child is asked about each element at most once per evaluation (they implement `Evaluator`), so side-effecting children are never re-run for diagnostics. They compose with `Not`, `All` and `Any`. Existing collection matchers are unchanged. See `docs/DSL.md`.
- `Project(name, project, child)` (`assert`, re-exported from `specs`): a typed matcher that maps the actual to a field or derived value and applies an existing matcher to it, so a failure keeps the child's explanation and starts with the projection path (`Status: expected open to equal paid`, `Items[1].Quantity: ...` for nested projections) instead of the opaque message of `Satisfy` (#362). It is an ordinary `Matcher` and composes with `Not`, `All` and `Any`; the projection and the child run once per assertion under `Evaluate`. An actual of the wrong type or an untyped nil, a nil projection and a nil child never match and call nothing; a panicking projection is recovered and reported as a failure, while a panic in the child propagates as with `Satisfy`. The `Matcher` interface and the equality fast path are unchanged. See `docs/DSL.md`.
- `MatchJSON` (`assert.MatchJSON`, `specs.MatchJSON`) compares two JSON documents semantically (#363): object key order and whitespace are ignored, array order is kept, `null` differs from a missing key, and numbers compare by exact decimal value without `float64` (`1` equals `1.0`; `9007199254740993` differs from `9007199254740992`). Invalid JSON, empty input, trailing content and duplicate object keys never match. A failure lists up to ten differences by JSON path (`$.owner.tags[1]`) with expected and actual values, bounded and deterministic like the structural diff of `Equal`. It is strict document equality with no subset matching, and it does not touch snapshot semantics. See `docs/JSON_MATCHERS.md`.
- Fuzzing of the assertion diagnostics. Three fuzz targets (`FuzzEqualFailureMessage`, `FuzzMatcherDiagnostics`, `FuzzPollMessage` in `assert/fuzz_diagnostics_test.go`) decode a byte recipe of at most 512 bytes into a value graph of at most 64 nodes (maps, slices, pointers, structs with exported and unexported fields, a `Stringer`, an error that wraps a value, cycles, shared references, slice views with one address and different lengths, several NaN keys with different payloads, and values that reach the render limits) and check that building `EqualFailureMessage`, the `FailureMessage` and `Description` of representative matchers and of `Not`, `All` and `Any`, and `PollResult.Message` of `Eventually` and `Consistently` on a `ManualClock` terminates, keeps its output bounded and marked where a limit applies, gives the same text when repeated and when the graph is rebuilt (a map keyed by a pointer is excluded from the rebuild check, since it is ordered by address by design), and leaves verdicts and matcher evaluation counts unchanged. `go test ./...` replays the seeds and the committed `assert/testdata/fuzz` corpus, and also in child processes so a stack overflow fails one subtest instead of killing the suite. A separate `fuzz.yml` workflow runs each target weekly and on demand (`gh workflow run fuzz.yml -f duration=10m`); it is not part of `ci.yml`. See `docs/CONTRIBUTING.md`.
- Property testing with shrinking and replay (#361), in a new nested Go module, `github.com/getsyntegrity/go-specs/property`, so the core module gains no dependency. It adapts `pgregory.net/rapid` (chosen over native `testing.F` alone; the comparison and the rejected alternative are in `docs/PROPERTY_TESTING.md`). `property.Run(prop, opts...)` returns a `Result` and `property.Check(t, prop, opts...)` reports it on the test: the outcome is `Passed`, `Failed` (an assertion failed), `Panicked` (value and stack kept) or `Exhausted` (too many inputs rejected), and the result keeps the seed, the first failing input, the shrunk counterexample and the assertion messages of the counterexample run only. A property asserts with the go-specs matchers (`p.Expect(x).To(assert.Equal(y))`, `p.Reject`, `p.Assume`), takes inputs with `property.Draw` from rapid generators, and has its failure state per generated input, so a failing input never fails the surrounding test or a later run. A failure replays exactly with `WithSeed(result.Seed)` or from the committed corpus file in `testdata/rapid`; `WithChecks`, `WithSteps`, `WithShrinkTime`, `WithName`, `WithFailFile` and `WithoutFailFile` configure it, and `property.Fuzz(prop)` turns a property into a native fuzz target for coverage-guided campaigns. Root `go test ./...` does not build the nested module: use `make test-property`; CI runs it in a new `property` job (vet, test, race). See `docs/PROPERTY_TESTING.md`.
- `ctx.Cleanup(fn)`, `ctx.Errorf(format, args...)` and `ctx.Helper()`: a per-case seam for packages built on go-specs, such as `mock` (#357). `Cleanup` runs `fn` when the case ends, on every engine (`Spec.It`, `Spec.ItParallel`, `Builder.It`, `Builder.ItParallel`, `RunParallel` and the flat runners) and on fake backends: after `AfterEach` and after every `ctx.Go` task settled, last registered first, also after a fatal assertion or a panic. A panic inside `fn` is recovered and reported as an error of that case, and the other cleanups still run. `Errorf` is a non-fatal failure through the assertion path: the case keeps running, the first message is the one `SpecResultEvent.Message` reports, it is safe to call from `ctx.Go` tasks and from cleanups (including through a finished task's `Context`), where it is folded into the spec at the settle points, and the failure is attributed to the caller (`ctx.T` helper marking, or the stack walk on `Builder.ItParallel`, which now also skips `mock` frames). `*specs.Context` therefore satisfies `interface{ Helper(); Cleanup(func()); Errorf(string, ...any) }`. Cases that never call them pay one nil check and no allocation. See `docs/DSL.md`.
- `mock.Controller`: interface mocking with expectations, stubbing and automatic verification (#357). `mock.NewController(ctx)` (any `*specs.Context`, `*testing.T` or `*testing.B`) owns one shared recorder; hand-written typed adapters forward each interface method to `Method(name).Call(args...)`. `Expect(args...)` declares a call shape with `Times`, `AtLeast`, `AtMost`, `Never` and `AnyTimes` (default exactly once), `Return` (sequential responses, the last repeats) or `Do`, and plain values, `mock.Any`, `mock.Match`/`mock.MatchT` predicates and `mock.Captor` as argument matchers; `InOrder` checks the global call order across methods and `Controller.Spy` spies. Unmet expectations and order violations are verified at case cleanup on every engine, also after a failed or panicking body; unexpected calls are reported immediately with the method, arguments and declaration site, attributed to the adapter line, and prohibitions (`Never()`, `Times(0)`, `AtMost(0)`) take precedence over permissive expectations regardless of declaration order (reported as forbidden calls). `Mock`, `Spy`, `Any` and `Equal` are unchanged. The controller is safe for concurrent use; recorded arguments are copied shallowly (see the ownership note). See `docs/DSL.md`.
- Structural diffs in equality failures (#358). `ctx.Expect(x).ToEqual(y)`, `Equal` and `assert.EqualFailureMessage` now follow the existing `expected ... to equal ...` line with a `differences:` section that names each mismatch by path (`Order.Items[2].Price: expected 9.99, actual 12.5`) for structs, slices, arrays, maps, pointers and interfaces. It reports nil versus empty, nil pointers, type mismatches, missing and unexpected slice elements and map keys, and terminates on cycles and unexported fields; map output is sorted and deterministic. Output is bounded (10 differences, 8 path segments, 80 characters per value) and says when it truncates. The diff is built only after a failed comparison and equality verdicts (`==`, `reflect.DeepEqual`, `errors.Is`) are unchanged; scalar and error failures keep their single-line messages. A self-containing map or slice no longer overflows the stack while the failure message renders. The diff walk also terminates on self-containing slices, and map keys that share a prefix longer than the 80-character display limit are ordered by their full value, so their order never depends on map iteration (struct keys compare on every field), and distinct keys that would show the same truncated path get an ordinal (`#1`, `#2`) so their lines stay distinguishable. Map entries with NaN keys, which can never be looked up again, are reported as missing and unexpected with their real values in a deterministic order. See `docs/DSL.md`.
- `ctx.Eventually(fn, matcher, opts...)` and `ctx.Consistently(fn, matcher, opts...)` poll a callback until a matcher passes or, for `Consistently`, keeps passing for a bounded interval (`assert.Eventually` and `assert.Consistently` return the verdict as an `assert.PollResult` instead of failing a spec) (#356). The callback runs again on every attempt, the matcher is evaluated without recording intermediate failures, and only the final verdict fails the spec, with the termination reason, elapsed time, attempts, last observed value and matcher failure. Options: `WithTimeout` (default 1s), `WithInterval` (default 10ms), `WithContext` and `WithClock`. The callback runs on the calling goroutine and is never interrupted, so the helpers start no goroutines; a panic in the callback or matcher is recovered as the final verdict, and invalid options fail the spec without running an attempt. A failing result renders the last observed value and a recovered panic value cycle-safely and bounded (a map or slice that contains itself prints `<cycle>` instead of overflowing the stack; `Satisfy` renders its actual the same way, keeping `%v` for ordinary values), and the attempt timer is released even when the callback calls `runtime.Goexit` (`t.FailNow`). `NewManualClock()` (`assert.ManualClock`) makes time deterministic for tests. Existing assertions and their allocation contracts are unchanged. See `docs/DSL.md`.
- `HaveLen(n)` and `BeEmpty()` matchers (`assert` and re-exported from `specs`) for the length of a string, slice, array, map or chan. A nil slice, map or chan is empty. An actual with no length fails with a `HaveLen: int has no length` style message instead of panicking or reporting a bogus length, and both compose with `Not`, `All` and `Any`. See `docs/DSL.md`.
- `StartWith(prefix)`, `EndWith(suffix)` and `MatchRegex(pattern)` string matchers (`assert` and re-exported from `specs`). The actual may be a `string`, a `[]byte` or a named string type; anything else fails with a `StartWith: int is not a string or []byte` style message. `MatchRegex` compiles its RE2 pattern once, and an invalid pattern never panics: it never matches and the failure message carries the compile error. See `docs/DSL.md`.
- `HaveKey(key)`, `HaveValue(value)` and `HavePair(key, value)` map matchers (`assert` and re-exported from `specs`). `map[string]any` and `map[string]string` take allocation-free fast paths and any other map falls back to reflection. A key whose type cannot be assigned to the map's key type is a non-match with an explanatory message, never a panic, and values compare with `ValuesEqual`. A non-map actual fails with a `HaveKey: int is not a map` style message. See `docs/DSL.md`.
- `ContainAllOf(elems...)`, `ContainAnyOf(elems...)`, `ContainTheSameElementsAs(elems)` and `BeOneOf(values...)` collection matchers (`assert` and re-exported from `specs`). Elements compare with `ValuesEqual`, like `Contain`. `ContainTheSameElementsAs` is order-insensitive multiset equality, so duplicates count, and its failure message lists the missing and unexpected elements. `ContainAllOf` and `ContainAnyOf` also accept a string (substrings). `[]int`, `[]string`, `[]float64` and `[]any` take allocation-free fast paths. See `docs/DSL.md`.
- `BeGreaterThan(x)`, `BeGreaterThanOrEqual(x)`, `BeLessThan(x)`, `BeLessThanOrEqual(x)`, `BeBetween(lo, hi)` and `BeCloseTo(target, delta)` ordering matchers (`assert` and re-exported from `specs`). They accept every int, uint and float kind, named types such as `time.Duration` included, and compare different kinds by exact value (a negative int is below every uint, and an int64 beyond 2^53 is not rounded to a float64), without allocating for builtin types. Strings compare with strings. NaN never matches. `BeBetween` is inclusive on both ends. `time.Time` is not supported. A non-orderable actual or a number-against-string comparison fails with an explanatory message instead of panicking. See `docs/DSL.md`.
- `BeZero()` and `Satisfy(description, pred)` general matchers (`assert` and re-exported from `specs`). `BeZero` follows `reflect.Value.IsZero` (so an empty non-nil slice is not zero, and a nil interface is). `Satisfy` runs a custom `func(any) bool` and names it in failure messages and in `Not`/`All`/`Any` output; a nil predicate never matches and says so instead of panicking. See `docs/DSL.md`.

#### Fixed

- The structural diff of `Equal` kept building paths and rendering values after it had ten differences, so `EqualFailureMessage(make([]int, 1_000_000), []int{})` rendered a million elements to show ten lines (a large map against an empty one rendered every key and value as well). The walk now stops as soon as its entry limit is full, in every loop (slice elements, missing and unexpected entries of slices and maps, struct fields), and a map keeps its keys symbolic while it walks: only the keys on the lines actually printed are rendered, once each, after the lines are chosen. The message text is unchanged, including `... more differences not shown (limit 10)`, with one refinement: the `#1`, `#2` ordinal that tells apart keys of one map that render alike (long strings with a common prefix) now counts the colliding keys shown in the message, at that map level, in key order. A colliding key that is not shown (equal on both sides, or past the tenth difference) no longer takes an ordinal away from, or adds one to, a shown key, and every line below one key carries that key's ordinal. Keys of any position get their ordinals, so two long keys past the 40th are told apart too. Verdicts and matcher evaluation counts are unchanged. Found in review of the fuzzing PR; `TestStructuralDiffStopsRenderingOncePastItsLimit` counts the renders.
- Building the diagnostic of a map with many NaN keys of one bit pattern could take seconds, which the fuzzing engine reports as a hung worker: the tie-break fingerprints were bounded per entry but not per message, and a message rebuilt them on every render. Found by `FuzzMatcherDiagnostics` and `FuzzPollMessage` and fixed by the per-message work budget of the tie-break (see the entry above); the fuzz recipes no longer cap how many entries of a map tie, and `TestFuzzFindingTieBreakFingerprintCost` keeps the shape.
- Two more costs the fuzz targets found once ties were uncapped, each reported as a hung worker (the fuzzing engine allows about a second for one input): the structural diff of `Equal` probed every subtree below its depth limit with a full walk that sorted and labelled each map and walked a subtree shared under many paths again for each of them (1.9 s for one message; the probe now walks maps without sorting and answers a reference pair asked twice once), and a map shared by several parents or containing itself was scanned for its smallest entries once for every path that reaches it (3 s for one input; a map of 64 or more entries is now scanned once per message and prints the same everywhere in it). Output, verdicts and matcher evaluation counts are unchanged.
- The bounded renderer printed the same map differently from one call to the next when the 10,000-node budget ran out while it was rendering the keys of a map of up to 1,024 entries: it rendered the keys in iteration order, so which entries were cut to `<truncated>` depended on the order the runtime iterates the map. It now renders them in the total key order, so the text is the same on every call. Found by `FuzzPollMessage`; verdicts and matcher evaluation counts are unchanged.
- Assertion failure messages no longer overflow the stack, panic or call user methods on self-containing values, and are bounded for huge ones (follow-up to #358 and #356, which fixed `Equal`, `Satisfy` and poll results only). Every matcher message that prints a value (`NotEqual`, `BeNil`, `BeTrue`, `BeFalse`, `Contain`, `HaveLen`, `BeEmpty`, `HaveKey`, `HaveValue`, `HavePair`, `ContainAllOf`, `ContainAnyOf`, `ContainTheSameElementsAs`, `BeOneOf`, `BeGreaterThan` and the other ordering matchers, `BeZero`, `MatchError`, `MatchErrorAs`, the `Not` message, and the `Description` used by `Not`, `All` and `Any`) now goes through one shared renderer (`assert/render_safe.go`). An ordinary value keeps its exact text, including `String` and `Error` output. A value that contains itself, has a NaN map key or exceeds 10,000 nodes is printed by a bounded renderer that marks `<cycle>`, stops at 8 levels, 16 elements or fields and 10,000 nodes, prints `<truncated>` when the budget runs out, and never calls a `String`, `Error`, `Format` or `GoString` method on your value. The same holds for the structural diff renderer of `Equal`, which now also has a node budget, selects the map entries it prints in one `MapRange` pass instead of sorting the whole map, and cuts a long string before quoting it. The poll renderer now caps struct fields at 16, orders map keys that render alike (NaN) deterministically, and prints the 16 smallest keys of a map over 1,024 entries. Reference identity (type, address and, for slices, length) is defined once and shared by the diff walk, the tie-break comparison and the poll renderer. Format change: a value over the 10,000-node budget (a `[]int` counts one node per element) that was printed in full by these messages is now printed truncated. Verdicts, the number of matcher evaluations and matcher semantics are unchanged. See `docs/DSL.md`.
- Assertion diagnostics for map entries whose keys tie (NaN keys with the same bit pattern) are now deterministic when their values differ only past the old 10,000-node tie-break budget. Each tied entry gets one bounded fingerprint of its value (4,096 bytes, 64 levels, nested maps of up to 256 entries, pointers followed by content and cycles by back-reference, no addresses, no user methods); entries are ordered by key, fingerprint, then complete before cut off, a pure comparison that no longer depends on a shared budget or visited set and never reports values as equal because a budget ran out. Entries that cannot be told apart within the budget are reported as one explicit group (`[NaN #1]: 3 entries indistinguishable within the diagnostic budget: all 3 missing in actual`) with no per-entry ordinals, and the value renderers print `<ambiguous>` for them. The work of all the fingerprints of one message is bounded too, by 32,768 visited values (nodes, counting the sort of nested maps) per diagnostic message for `EqualFailureMessage` (first line and diff together) and per printed value for the poll, `Satisfy` and matcher renderers: the budget is split by tie class size, never by iteration order (every tied entry of a map gets the same allowance, `min(4,096, half of the budget left ÷ tied entries)` rounded down to a power of two, and a tie class is never split), so a map of 1,000 tied NaN keys or the same map printed many times costs a few milliseconds instead of seconds, and an entry that runs out of allowance joins an ambiguity group instead of being called equal. Format change: such entries no longer print an arbitrary value; ordinary maps, distinguishable NaN-keyed entries, verdicts and the number of matcher evaluations are unchanged, and pointer, channel and unsafe-pointer keys still order by address. See `docs/DSL.md`.

## Dependencies

### Added
_No changes._

### Changed
_No changes._

### Removed
_No changes._

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
