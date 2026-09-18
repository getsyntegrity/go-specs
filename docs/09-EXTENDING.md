# 09 · Extending go-specs

> **Audience:** tooling authors · **Reading time:** ~10 minutes

There are four extension points, and they are deliberately few. Each one is a plain Go interface or
function — nothing is registered into a global, nothing is discovered by reflection, nothing is
configured by a file.

| Extension point | You provide | Mechanism |
| --- | --- | --- |
| Matchers | `Match` + `FailureMessage` | pass the value to `.To(...)` |
| Reporters | the four `EventReporter` methods | constructor injection |
| Suite inspection | nothing | `Analyze(fn) *SuiteTree` |
| Snapshot matching | a custom matcher | `RegisterSnapshotMatcher` |

---

## 1. Matchers

See [04 · Assertions and matchers](04-ASSERTIONS.md#5-writing-a-custom-matcher). Implement the
interface; there is no registration step.

## 2. Reporters

See [07 · Reporting](07-REPORTING.md#7-writing-your-own-reporter). Implement the four methods and
pass the value to `DescribeWithReporter`, `NewRunnerWithReporter` or `RunShardWithReporter`.

There is **no global reporter registry**, and that is a decision rather than an omission
([ADR-0007](adr/0007-observational-constructor-injected-reporting.md)): a global would make two
concurrently-building suites observable to each other, and would make "which reporter is attached"
depend on import order.

## 3. Suite inspection — `Analyze`

`Analyze` is a **supported extension API**, not legacy residue. It builds a `SuiteTree` without
running anything — for code that generates or inspects a suite: an external DSL, a generator, an
editor integration, a linter.

```go
tree := specs.Analyze(func() {
    specs.Describe(nil, "math", func(s *specs.Spec) {
        s.It("adds", func(ctx *specs.Context) {})
    })
})

fmt.Print(tree.Tree())
```

`Analyze` establishes the build context for the calling goroutine. Package-level helpers then read
or write the registry it pushed, without needing the unexported registry type:

| Helper | Kind | Outside `Analyze` |
| --- | --- | --- |
| `CurrentSuite()`, `CurrentArena()` | read-only | returns `nil` |
| `AppendBeforeHook(fn)`, `AppendAfterHook(fn)`, `SetPathGen(gen)` | mutating | **panics** |
| `PrintTreeArena(arena, rootID, depth, w)` | read-only | nil-tolerant |

### Why reads return nil and writes panic

The asymmetry is the whole point.

"No suite is being built" is a **legitimate answer** to a question, so an accessor returns `nil` and
the caller decides what that means.

A mutating helper asked to register a hook has **nowhere to put it**. Returning quietly would discard
the caller's hook and report success — the caller would then believe a hook is installed that will
never run. So it panics, with a message naming the helper and the fix
([ADR-0005](adr/0005-fail-closed-dsl-registration.md)).

### The goroutine rule

Valid context means *inside the `fn` passed to `Analyze`, or inside a `Describe`/`BuildSuite` nested
in one, **on the same goroutine***.

The registry stack is keyed per goroutine, so concurrent `Analyze` calls never observe each other's
tree. A helper called from a goroutine started inside `Analyze` therefore sees no registry — and
panics, rather than corrupting a registry nobody will read.

```go
specs.Analyze(func() {
    go func() {
        specs.AppendBeforeHook(fn)   // panics: no active registry on this goroutine
    }()
})
```

### What was removed, and why it is not coming back

[ADR-0006](adr/0006-arena-registry-as-the-only-tree-surface.md) records the removal of the
pointer-based `Node` tree, `PrintTree`, `Walk`, an orphaned internal registry package, and
`Context.rng`. All of it was unreachable: zero callers, superseded by the arena, and in the RNG's
case never actually seeded. The arena-based surface above is the retained, documented and tested one.

## 4. Snapshot matching

`RegisterSnapshotMatcher` installs a custom `SnapshotMatcher` used by `Context.Snapshot`. Use it when
your values need domain-aware comparison — tolerating a timestamp field, normalizing an unordered
collection — rather than the default semantic JSON equality
([06 · Snapshots](06-SNAPSHOTS.md#3-comparison-is-semantic-not-byte-for-byte)).

## 5. Building on the compiled plan

If you are writing tooling that needs the program rather than a run:

```go
suite := specs.BuildSuite(t, "payments", func(s *specs.Spec) { /* ... */ })
// inspect or store; suite.Run(t) later

prog := specs.BuildProgram(func(b *specs.Builder) { /* ... */ })
// shard it, run it in parallel, run it repeatedly
```

Both compile without running. `BuildSuite` is what the benchmarks use to keep construction out of the
timed region.

Sharding is a pure function of index (`i % total == shard`), so a distributed runner built on
`ShardBCProgram` + `RunShardWithReporter` is reproducible by construction — see
[03 · Execution model](03-EXECUTION-MODEL.md#9-ci-sharding).

## 6. What is *not* an extension point

| Not extensible | Why |
| --- | --- |
| Execution order | determinism is a contract, not a policy ([ADR-0002](adr/0002-deterministic-execution.md)) |
| Retry behaviour | there is none; a flaky spec is a bug |
| The status vocabulary | five statuses, classified in exactly one place, so renderers cannot disagree |
| Hook ordering | fixed at compile time, outer→inner then inner→outer |

## 7. Staged but unwired: `gen/generators`

`gen/generators` provides adversarial value sets — `Integers()`, `Strings()`, `Empty()`,
`Whitespace()`, `InvalidUTF8()`, `VeryLong()`, `Bytes()` — covering boundary values like `math.MinInt`,
invalid UTF-8 and >10 KB payloads.

**Nothing in the module imports it.** It is not wired into `Paths`, the DSL, the examples or the
benchmarks; its only consumer is its own test file. It is staged infrastructure for a property-testing
feature that has not landed.

It works, and you can call it directly today:

```go
for _, s := range generators.Strings() {
    // feed s into your own table or Paths values
}
```

But treat it as a value library you are using deliberately, not as a supported framework feature with
a stability promise.
