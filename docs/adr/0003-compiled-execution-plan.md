# ADR-0003: Declare, Compile, Run — an Immutable Flat Execution Plan

## Status

Accepted.

## Date

2026-09-18 (recorded). The model is the outcome of the incremental-execution rewrite that defines
the current `develop` branch.

## Deciders

go-specs maintainers, via the incremental-execution rewrite and #109.

## Context

The obvious implementation of a nested BDD DSL is a tree of nodes walked at run time: for each spec,
climb to the root collecting `BeforeEach` hooks, run them outward-in, run the body, then run
`AfterEach` hooks inward-out.

That design pays, on **every spec**, for work whose answer cannot change: which hooks apply, in what
order, what the scope names are, whether the spec is focused. On a suite of 50,000 specs the
framework spends more time rediscovering its own structure than running the code under test.

It also makes the hot loop pointer-chasing over a tree — the least cache-friendly shape available —
and forces a per-spec allocation for the collected ancestor slice.

## Decision

Execution is split into stages, and each stage's output is immutable input to the next:

```
Declare → Compile (once, sync.Once) → Plan (flat instructions) → Run (linear scan) → Observe
```

1. **Everything decidable at compile time is decided at compile time**: hook flattening, scope
   naming, focus filtering, group coalescing.
2. **The plan is a flat slice of instructions** with index-based relationships. No pointer tree
   survives into execution. No maps, no per-step metadata, no hook resolution on the hot path.
3. **Compilation is one-shot**, guarded by `sync.Once`, producing a `*CompiledSuite` that can be
   built once and run many times.
4. **Focus and skip are compile-time filters**, not run-time branches. A focused suite compiles only
   focused specs; a skipped spec's body is never compiled into a step at all.
5. **Group coalescing is a memory-layout optimization with no semantic effect.** Specs sharing a hook
   layout share one `before`/`after` slice, but each spec still runs its own hooks. One hook-set
   execution per spec, never per group.

## Decision classification

### Accepted now

- All five rules above, for both build paths (bytecode compiler and arena) and all runner variants.
- The per-spec hook-execution invariant (#109), asserted in the runner and the plan executor in
  lockstep.

### Deferred

- Whether the `Builder`/`Runner` model's group-scoped hook placement should be aligned with the
  `Describe`/`Spec` model's per-spec placement. Today they genuinely differ: in
  `Builder`/`Runner`, a group's hooks sit *outside* the `t.Run` call, so a `BeforeEach` runs once per
  group. Tracked in #109.

### Open hypotheses

- That no future feature requires a run-time structural decision. Conditional or data-driven spec
  generation would test this. `Paths` already generates candidates during execution, and it does so
  by driving its own controller loop rather than by mutating the plan — that pattern is the
  precedent to follow.

## Consequences

**Benefits:** a ~1.7 µs run for 1000 specs against ~85 µs and ~244 µs for the comparators; linear
scaling to 50,000 specs; zero allocations on the success path; a run loop small enough to reason
about in full. Compiling also makes structural inspection possible without execution
([ADR-0006](0006-arena-registry-as-the-only-tree-surface.md)).

**Costs:** two representations of the same suite exist — the declared callbacks and the compiled
plan — and a contributor must hold both. A bug in the compiler is harder to locate than a bug in a
tree walk, because the failing artifact is an instruction stream rather than the structure the author
wrote. Group coalescing in particular is an optimization that *looks* semantic, and keeping it from
becoming semantic takes deliberate test coverage.

## Rejected / deferred alternatives

- **Walk the declared tree at run time** — rejected. It is the cost this design exists to remove, and
  it puts a per-spec allocation and pointer chase on the hot path.
- **Cache resolved hooks per scope, keep the tree** — rejected. It solves the repeated resolution but
  keeps the pointer chasing and adds cache-invalidation questions to a structure that is immutable
  after declaration anyway. If the structure cannot change, compile it once and be done.
- **Compile lazily, per spec, on first execution** — rejected. It spreads the cost instead of
  removing it, and it makes the first run of a suite systematically different from the second, which
  conflicts with [ADR-0002](0002-deterministic-execution.md).

## Related work

- #109 — the per-spec hook-execution invariant and the `Builder`/`Runner` divergence.
- [ADR-0002](0002-deterministic-execution.md) — declaration-order execution over the flat plan.
- [03 · Execution model](../03-EXECUTION-MODEL.md), [08 · Performance](../08-PERFORMANCE.md).
