# ADR-0004: Per-Spec Subtest Isolation and Goexit-Safe After-Hooks

## Status

Accepted.

## Date

2026-09-18 (recorded). The Goexit-safe hook ordering landed with the #109 work.

## Deciders

go-specs maintainers, via #109 and #111.

## Context

Two Go facts collide in any test framework that runs user code:

1. `t.Fatal`, `t.Fatalf` and `t.FailNow` call `runtime.Goexit`, which unwinds the calling goroutine.
2. `recover()` **cannot observe `Goexit`**. A `defer func() { recover() }()` around a call that
   Goexits will run, but `recover()` returns `nil` and the unwinding continues.

So a framework that runs `before`, `body`, `after` as three sequential calls loses the `after` call
entirely the moment the body fatally fails — precisely when cleanup matters most. A spec that leaks a
file handle, a lock or a container on failure will do it on exactly the runs where diagnosis is
hardest.

Separately, a panic in one spec must not take the suite with it: a 4,000-spec suite that aborts on
spec 12 tells you about one bug and hides the rest.

## Decision

### Isolation

Each sequential spec runs in its own Go subtest (`t.Run`) when the backend is a real `*testing.T`.
`Goexit` then unwinds only that subtest's goroutine, and the runner continues with the next spec.

`*testing.B` backends create no subtests, which is what keeps benchmarks allocation-free. Parallel
backends have no `*testing.TB` at all and fail loudly when a spec calls `t.Run`, rather than
degrading silently.

### Goexit-safe cleanup

After-hooks are registered through a `defer` **before** the before-hooks or the body execute. A
`defer` survives `Goexit`; a straight-line call does not.

```
defer run-after-hooks   ← registered first
run before hooks
run body
```

### Panic containment

Every spec, hook and step runs behind a `recover()`. A recovered panic is recorded as that spec's
failure with its message and `debug.Stack()`, and execution continues.

### Supporting rules

- **After-hooks always run.** Never gated by `FailFast`.
- **Each after-hook is recovered individually**, so one panicking hook does not stop its siblings.
- **Failure messages are first-write-wins.** A body failure beats a later after-hook failure, because
  the first cause is more useful than the last symptom.
- **A filtered spec is detected from inside the closure.** `t.Run` returns `true` even for a subtest
  the `-run` filter discarded, so a `ran` flag is threaded out and the spec is reported as `Filtered`
  rather than `Passed` (#111).

## Decision classification

### Accepted now

- All of the above, consistently across `Runner`, `MinimalRunner`, `BlockRunner`, `BytecodeRunner`
  and the plan executor.

### Deferred

- Per-spec timeouts. Today the execution context derives from `tb.Context()` and `tb.Deadline()`, so
  `go test -timeout` governs the whole run. A spec-level deadline would need its own cancellation and
  reporting story.

### Open hypotheses

- That one subtest per spec is the right granularity. Emitting a subtest per `Describe`/`When` scope
  would remove the breadcrumb-collision cases documented in
  [appendix · subtest identity](../appendix/subtest-identity.md), at the cost of a live `*testing.T`
  per scope, hooks re-resolved per level, and the loss of group coalescing — that is, at the cost of
  [ADR-0003](0003-compiled-execution-plan.md).

## Consequences

**Benefits:** cleanup runs when it is most needed; one bad spec cannot hide the rest of the suite;
`go test -run` addresses individual specs; a panic produces a located failure instead of a dead
process.

**Costs:** a subtest per spec is real `testing` overhead, which is why `*testing.B` opts out. The
`defer`-first ordering is subtle enough that a contributor rearranging those lines for readability
would silently reintroduce the bug — it needs a comment at the site and a test that fails without it.
And because a whole spec is one subtest, the breadcrumb must encode the full scope path into one
name, which creates the collision cases the appendix documents.

## Rejected / deferred alternatives

- **Run `before`/`body`/`after` as three sequential calls** — rejected. This is the bug. It loses
  cleanup on exactly the failing runs.
- **Ban `t.Fatal` inside specs and require `ctx.Expect`** — rejected. Users call standard-library
  helpers that fatal, and a rule the framework cannot enforce is a rule that will be broken silently.
- **Use `t.Cleanup` instead of a manual defer** — deferred. `t.Cleanup` is only available when a real
  `*testing.T` backs the spec; the fake and parallel backends have none, and the hook machinery must
  behave identically across all of them.
- **Let a panic abort the suite, as the standard library does** — rejected. The standard library's
  unit of isolation is the test function; here it is the spec, and aborting the suite would make the
  framework less informative than the thing it wraps.

## Related work

- #109 — hook-execution invariants and after-hook survival.
- #111 — `Filtered` detection from inside the `t.Run` closure.
- [ADR-0003](0003-compiled-execution-plan.md) — the plan whose structure this ordering runs over.
- [ADR-0010](0010-snapshot-durability-and-semantics.md) — the same Goexit problem, solved the same
  way, for snapshot verdicts.
