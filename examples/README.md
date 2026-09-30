# go-specs examples

This directory is the usage reference for go-specs. Every public feature has one file here, and
each file holds several small, runnable cases: the minimal use first, then the variants and
failure messages that matter. Open the file for the feature you care about, find the case whose
name matches your situation, and copy it.

The cases are real tests. `go test ./examples/...` runs all of them, so the examples cannot drift
from the framework. None of them fails on purpose, needs a network, or touches an external
service.

## Layout

- One flat package (`examples_test`, with a `doc.go` so `go build ./...` sees a package) and one
  `*_test.go` file per feature. There is no folder per case.
- Two kinds of case, told apart by name:
  - `Example_<name>` functions are runnable examples with a checked `// Output:` block. They are
    used where the API needs no `*testing.T`: matchers and their failure messages, polling on a
    `ManualClock`, spies, renderers. The `Example_` prefix without a type name is the form `go vet`
    accepts for a package that declares no matching identifier.
  - `Test<Feature>_<case>` functions are demonstrative specs that pass. They are used for DSL
    features that need a live `*testing.T`, such as hooks, `Table`, parallel specs and snapshots.
- Failure diagnostics are shown by printing what a matcher would report (`assert.Evaluate` and the
  `...FailureMessage` helpers), never by leaving a test red.
- Snapshot files live in [`__snapshots__/`](__snapshots__), named after the test file that owns
  them (`snapshots_test.snap.json`, `end_to_end_test.snap.json`).
- Property testing examples are in the nested module [`../property/examples`](../property/examples),
  not here. Property testing needs `pgregory.net/rapid`, and the core module must never import it.
  This package imports only `specs`, `assert`, `mock`, `report` and `snapshots`.

## Running the examples

```bash
# Everything in the core module's examples
go test ./examples/...

# One feature: match the case names shown in the tables below
go test ./examples -run 'Example_matchJSON|TestJSON' -v
go test ./examples -run 'TestHooks' -v

# Snapshot files: compare, then refresh after an intended change
go test ./examples -run TestSnapshots
GO_SPECS_UPDATE_SNAPSHOTS=1 go test ./examples -run TestSnapshots

# Property testing (nested module)
cd property && go test ./examples/...
make test-property
```

Run these from the repository root. `-run` takes a regular expression, and `Example_` functions
are matched by their full name, so `-run 'Example_matchJSON'` also runs `Example_matchJSONNumbers`
and the other cases that share the prefix.

## Index

Feature, then the file that shows it, what it covers, and the cases in that file. Cases are listed
in file order, which is also the reading order: simple first.

### DSL and execution

Structure a suite and control how it runs.

| Feature | File | What it shows | Cases |
| --- | --- | --- | --- |
| Describe, When, It and building a suite | [dsl_test.go](dsl_test.go) | The minimal spec, nesting, `Describe` aliases, `BuildSuite` then run, and a pending `It`. | `TestDSL_minimal`, `TestDSL_nested`, `TestDSL_describeAliases`, `TestDSL_buildSuiteThenRun`, `TestDSL_pendingIt` |
| Hooks | [hooks_test.go](hooks_test.go) | `BeforeEach`/`AfterEach` order, per-spec reset, `BeforeAll`/`AfterAll`, and the build-time panic for late registration. | `TestHooks_perSpecReset`, `TestHooks_order`, `TestHooks_lateRegistrationPanicsAtBuildTime`, `TestHooks_beforeAllAfterAll` |
| Shared behaviors | [shared_behaviors_test.go](shared_behaviors_test.go) | One contract run against two implementations, with the hook order. | `TestSharedBehaviors_sameContractTwoImplementations`, `TestSharedBehaviors_hookOrder` |
| Table-driven specs | [table_test.go](table_test.go) | `Table` and `TableParallel`, migrating a plain `t.Run` table, hooks and groups. | `TestTable_plainGo`, `TestTable_forAndIt`, `TestTable_table`, `TestTable_withHooksAndGroup`, `TestTable_parallel` |
| Focus, skip and pending | [focus_skip_test.go](focus_skip_test.go) | `FIt`, `SkipIt`, `PendingIt` and the spec-function wrappers. | `TestFocusSkip_skipAndPending`, `TestFocusSkip_fitFocusesOneSpec`, `TestFocusSkip_specFnWrappers` |
| Parallel specs | [parallel_test.go](parallel_test.go) | `ItParallel`, hooks around each parallel spec, and `ctx.Go`. | `TestParallel_itParallelRunsIndependentSpecs`, `TestParallel_hooksWrapEachSpec`, `TestParallel_ctxGoRunsAssertionsConcurrently` |
| Fail fast | [fail_fast_test.go](fail_fast_test.go) | Stopping a suite at the first failure. | `TestFailFast_enabledOnPassingSuite` |
| Sharding | [sharding_test.go](sharding_test.go) | Splitting a suite across CI jobs with `RunShard`, shard strings and the environment. | `TestSharding_runShardPartitionsTheSuite`, `Example_parseShardString`, `Example_parseShardStringErrors`, `Example_formatShardFlag`, `TestSharding_fromEnvironment` |
| Builder, Program and Runner | [builder_test.go](builder_test.go) | The compatibility engine: declare, build, run, with a reporter. | `TestBuilder_declareBuildRun`, `TestBuilder_buildProgram`, `TestBuilder_runnerWithReporter` |
| Analyzing a spec tree | [analysis_test.go](analysis_test.go) | `specs.Analyze` builds the spec tree without running it; print it with `Tree` or visit it with `Walk`, for tools built on go-specs. | `Example_analyzeTree`, `Example_analyzeWalk` |

### Context

What a spec body receives.

| Feature | File | What it shows | Cases |
| --- | --- | --- | --- |
| Context | [context_test.go](context_test.go) | `ctx.Expect`, typed assertions, `Cleanup`, the `*testing.T` handle and helpers. | `TestContext_expect`, `TestContext_typedAssertions`, `TestContext_cleanup`, `TestContext_testingHandle`, `TestContext_helper` |

### Matchers

Everything in `assert`, re-exported from `specs`.

| Feature | File | What it shows | Cases |
| --- | --- | --- | --- |
| Equality, nil, booleans, zero | [equality_matchers_test.go](equality_matchers_test.go) | `Equal`, `NotEqual`, `BeNil` (including a typed nil), `BeTrue`/`BeFalse`, `BeZero`. | `TestEquality_inASpec`, `Example_equal`, `Example_equalTypeMismatch`, `Example_equalFailureMessage`, `Example_notEqual`, `Example_beNil`, `Example_beNilTypedNilInInterface`, `Example_beTrueBeFalse`, `Example_beZero` |
| Numbers and ordering | [numeric_matchers_test.go](numeric_matchers_test.go) | Ordering matchers, mixed numeric types, `BeBetween`, `BeCloseTo`. | `Example_orderingMatchers`, `Example_mixedNumericTypes`, `Example_stringOrdering`, `Example_orderingMisuse`, `Example_beBetween`, `Example_beCloseTo` |
| Strings and regular expressions | [string_matchers_test.go](string_matchers_test.go) | `StartWith`, `EndWith`, `MatchRegex` and the text kinds they accept. | `Example_startWithEndWith`, `Example_textKinds`, `Example_matchRegex`, `Example_matchRegexInvalidPattern` |
| Collections and maps | [collection_matchers_test.go](collection_matchers_test.go) | `Contain`, `ContainAllOf`, `ContainAnyOf`, `ContainTheSameElementsAs`, `BeOneOf`, `HaveLen`, `BeEmpty`, map matchers. | `Example_contain`, `Example_containFailureMessages`, `Example_containAllOf`, `Example_containAnyOf`, `Example_containTheSameElementsAs`, `Example_beOneOf`, `Example_haveLenAndBeEmpty`, `Example_haveLenFailureMessages`, `Example_mapMatchers`, `Example_mapMatchersFailureMessages` |
| Quantified and ordered collections | [quantified_collection_matchers_test.go](quantified_collection_matchers_test.go) | `EveryElement`, `AnyElement`, `NoElement`, counting and in-order matchers. | `Example_everyElement`, `Example_anyElement`, `Example_noElement`, `Example_countingMatchers`, `Example_emptyCollections`, `Example_unsupportedInputs`, `Example_haveElementsInOrder`, `Example_containElementsInOrder`, `Example_containElementsInOrderConsumesElements` |
| Errors | [error_matchers_test.go](error_matchers_test.go) | `MatchError`, `MatchErrorAs` and `Equal` on errors, with failure messages. | `TestErrors_inASpec`, `Example_matchError`, `Example_matchErrorFailureMessages`, `Example_matchErrorAs`, `Example_matchErrorAsFailureMessages`, `Example_equalOnErrors` |
| JSON | [json_matchers_test.go](json_matchers_test.go) | `MatchJSON`: input kinds, numbers, path diagnostics, invalid input. See also [docs/JSON_MATCHERS.md](../docs/JSON_MATCHERS.md). | `TestJSON_inASpec`, `Example_matchJSON`, `Example_matchJSONInputKinds`, `Example_matchJSONNumbers`, `Example_matchJSONPathDiagnostics`, `Example_matchJSONInvalidInput` |
| Projection | [projection_test.go](projection_test.go) | `Project`: assert on one field or a derived value, nested and inside `EveryElement`. | `TestProjection_inASpec`, `Example_projectFieldDiagnostics`, `Example_projectSeveralFields`, `Example_projectNested`, `Example_projectInsideEveryElement`, `Example_projectMisuse` |
| Composition | [matcher_composition_test.go](matcher_composition_test.go) | `Not`, `All`, `Any`, nesting, and composites inside other matchers. | `TestComposition_inASpec`, `Example_not`, `Example_all`, `Example_any`, `Example_nestedComposites`, `Example_compositesInsideOtherMatchers` |
| Custom matchers | [custom_matchers_test.go](custom_matchers_test.go) | `Satisfy` and writing your own `Matcher`. | `TestCustom_satisfyInASpec`, `Example_satisfy`, `Example_satisfyMistakes`, `TestCustom_ownMatcherInASpec`, `Example_customMatcher`, `Example_customMatcherInComposites`, `Example_customMatcherWithoutDescriber` |
| Structural diff | [structural_diff_test.go](structural_diff_test.go) | What `Equal` prints when structs, slices and maps differ, and why the output is bounded. | `Example_structDiff`, `Example_sliceAndMapDiff`, `Example_noDiffForScalars`, `Example_nilVersusEmpty`, `Example_diffIsBounded` |

### Polling

Wait for something asynchronous.

| Feature | File | What it shows | Cases |
| --- | --- | --- | --- |
| Eventually and Consistently | [polling_test.go](polling_test.go) | Retrying until a matcher passes or holds, timeouts, cancellation, panics, `ManualClock`. | `Example_eventuallyMatches`, `Example_eventuallyTimeout`, `Example_consistentlyHolds`, `Example_consistentlyBreaks`, `Example_pollingCancellation`, `Example_pollingPanic`, `Example_pollingInvalidOptions`, `TestPolling_inASpecWithManualClock`, `TestPolling_realClockWithBackgroundWork`, `TestPolling_withContext` |

### Test doubles

Package `mock`.

| Feature | File | What it shows | Cases |
| --- | --- | --- | --- |
| Mocks (Controller) | [mocks_test.go](mocks_test.go) | Expectations, returns, counts, argument matchers, `Captor`, `InOrder`, diagnostics, `Reset`, `Verify`. `mock.NewController(ctx)` registers `Verify` with `ctx.Cleanup`, so specs never call it. | `Example_mocksExpectAndReturn`, `Example_mocksResultAccessors`, `Example_mocksCallCounts`, `Example_mocksTooManyCallsDiagnostic`, `Example_mocksUnmetExpectationDiagnostic`, `Example_mocksUnexpectedArgumentsDiagnostic`, `Example_mocksNeverIsForbidden`, `Example_mocksSequentialReturns`, `Example_mocksDoComputesResults`, `Example_mocksArgumentMatchers`, `Example_mocksCaptor`, `Example_mocksInOrder`, `Example_mocksRecordedCalls`, `Example_mocksReset`, `Example_mocksVerifyIsIdempotent`, `TestMocks_interfaceDoubles` |
| Spies | [spies_test.go](spies_test.go) | Recording calls, `CalledWith`, `CalledTimes`, named spies, copied arguments, concurrency, and a spy behind an interface. | `Example_spyRecordsCalls`, `Example_spyCalledWith`, `Example_spyArgumentMatchers`, `Example_spyCalledTimesDiagnostic`, `Example_spyCopiesArguments`, `Example_spyNilIsSafe`, `Example_spyConcurrentCalls`, `Example_spyNamedSpies`, `TestSpies_controllerSpyJoinsGlobalOrder`, `TestSpies_behindAnInterface`, `TestSpies_namedSpyInASpec` |

### Snapshots

Package `snapshots`, used through `ctx.Snapshot`.

| Feature | File | What it shows | Cases |
| --- | --- | --- | --- |
| Snapshots | [snapshots_test.go](snapshots_test.go) | Map and struct snapshots, several names per spec, nil and empty, parallel specs, and the missing, updated and semantic-comparison paths. | `TestSnapshots_basicMap`, `TestSnapshots_structWithNesting`, `TestSnapshots_severalNamesInOneSpec`, `TestSnapshots_nilAndEmpty`, `TestSnapshots_inParallelSpecs`, `Example_snapshotsMissingSnapshot`, `Example_snapshotsUpdateThenCompare`, `Example_snapshotsComparisonIsSemantic`, `Example_snapshotsEmptyNameIsRejected`, `Example_snapshotsUnmarshalableValue` |

### Reporting

Package `report`.

| Feature | File | What it shows | Cases |
| --- | --- | --- | --- |
| Reporting | [reporting_test.go](reporting_test.go) | `Collector`, the four renderers, coverage profiles, targets, and a real run through `DescribeWithReporter`. | `Example_reportingCollectorNormalizesEvents`, `Example_reportingRenderTXT`, `Example_reportingRenderJSON`, `Example_reportingRenderXML`, `Example_reportingRenderHTML`, `Example_reportingParseCoverageProfile`, `Example_reportingParseCoverageProfileError`, `Example_reportingParseTarget`, `Example_reportingEventLog`, `TestReporting_collectorWithDescribeWithReporter`, `TestReporting_renderARealRun`, `TestReporting_multiFormatWritesTargets` |

### Property testing

A separate Go module in `property/`, run with `cd property && go test ./examples/...`.

| Feature | File | What it shows | Cases |
| --- | --- | --- | --- |
| Property testing | [property_test.go](../property/examples/property_test.go) | `property.Run`, generators, assumptions, shrinking, replay by seed and by fail file, and a fuzz target. | `TestProperty_minimalCheck`, `TestProperty_insideASpec`, `TestProperty_generators`, `Example_propertyAssume`, `Example_propertyExhausted`, `Example_propertyShrinking`, `Example_propertyReplayBySeed`, `TestProperty_replayByFailFile`, `TestProperty_withoutFailFileLeavesNothingBehind`, `TestProperty_reportShowsCounterexampleAndReplay`, `Example_propertyPanic`, `FuzzReverseTwice`, `TestProperty_withEveryElement`, `TestProperty_jsonRoundTrip` |

### End to end

Several features together.

| Feature | File | What it shows | Cases |
| --- | --- | --- | --- |
| Payment service | [end_to_end_test.go](end_to_end_test.go) | A payment service with the nested DSL, `Table`, `EveryElement`, a spy, a `Controller` double and a snapshot in one suite. | `TestEndToEnd_paymentSystem`, `Example_endToEndForgottenLedgerCall` |

## Where to go next

- [../README.md](../README.md) for installation and a first spec.
- [../docs/DSL.md](../docs/DSL.md) for the full DSL reference, hooks, tables and mocking.
- [../docs/REPORTING.md](../docs/REPORTING.md) for reports, the `go-specs-report` CLI and CI recipes.
- [../docs/PROPERTY_TESTING.md](../docs/PROPERTY_TESTING.md) for the property module.
- [../docs/JSON_MATCHERS.md](../docs/JSON_MATCHERS.md) for `MatchJSON`.
