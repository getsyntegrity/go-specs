# ADR-0006: The Arena Registry Is the Only Structural Surface

## Status

Accepted. Breaking change.

## Date

2026-09-18

## Deciders

go-specs maintainers, via #156 / #161.

## Context

The repository carried three structural representations of the same idea, only one of which was
reachable:

1. **`NodeArena`** — flat `Nodes`, `Children`, `BeforeHooks` and `AfterHooks` slices with parent and
   child relationships by index. The representation the compiler actually uses.
2. **A pointer-based `Node` tree**, with `PrintTree` and `Walk`. Exported, documented, and with
   **zero callers** — superseded by the arena during the incremental-execution rewrite, but never
   removed.
3. **A separate `specs/internal/registry` package** with its own `Node` and `Registry` types and no
   importers at all.

Plus `Context.rng`, which looked like a seeded random source and was `nil` on every pooled path
because the pool's reset cleared it and only `NewContext` ever set it.

Dead exported API is not free. It is API: it appears in `go doc`, it gets found by autocomplete, it
gets used, and then it constrains. And a second structural representation invites the two to drift —
at which point a tool that inspects a suite and a runner that executes one describe different things.

## Decision

Remove the pointer-based tree (`Node`, `PrintTree`, `Walk`), the orphaned
`specs/internal/registry` package, and `Context.rng`.

The **arena-based surface is the only structural surface**, and it is supported, documented and
tested:

| Symbol | Kind |
| --- | --- |
| `Analyze(fn) *SuiteTree` | establishes the build context for the calling goroutine |
| `SuiteTree.Walk`, `SuiteTree.Tree` | traversal and rendering |
| `CurrentSuite()`, `CurrentArena()` | read-only accessors; `nil` outside `Analyze` |
| `AppendBeforeHook`, `AppendAfterHook`, `SetPathGen` | mutating; panic outside `Analyze` ([ADR-0005](0005-fail-closed-dsl-registration.md)) |
| `PrintTreeArena(arena, rootID, depth, w)` | supported tree printer |

Because both build paths converge on the same plan, a suite inspected through `Analyze` and a suite
run through `Describe` cannot describe different things.

`Context.rng` is removed outright rather than fixed: making it work would have created a real
wall-clock-seeded RNG inside a framework whose contract is deterministic execution
([ADR-0002](0002-deterministic-execution.md)).

## Decision classification

### Accepted now

- The removals above, as a breaking change.
- The arena surface as the single supported structural API, with the read/write asymmetry from
  [ADR-0005](0005-fail-closed-dsl-registration.md).

### Deferred

- Whether `SuiteTree` should expose a stable serialization format for external tooling. Today a
  consumer walks the tree in-process. A JSON form would be useful for editor integrations and would
  also be a new compatibility surface to maintain.

### Open hypotheses

- That the arena is sufficient for every inspection use case. The removed pointer tree was more
  convenient to traverse ad hoc; if a real consumer needs parent-pointer navigation that indices make
  awkward, the answer is a helper over the arena, not a second representation.

## Consequences

**Benefits:** one structural representation, so inspection and execution cannot drift; a smaller
public API surface; no exported symbol that has never worked; the determinism contract stops being
contradicted by a field in the type it governs.

**Costs:** breaking. Any code importing `specs.Node`, `PrintTree`, `Walk` or `Context.rng` no longer
compiles. Index-based traversal is genuinely less ergonomic than pointer traversal for one-off
inspection code, and external tooling that relied on the removed symbols must be rewritten against
the arena.

## Rejected / deferred alternatives

- **Deprecate rather than remove** — rejected. A deprecation is a promise to keep maintaining the
  thing. These symbols had zero callers and, in the RNG's case, never worked; deprecating dead code
  keeps its cost while adding a migration notice nobody needs.
- **Keep the pointer tree as a convenience view over the arena** — rejected. Two views of one
  structure is exactly the drift risk this record removes, and there was no caller asking for the
  convenience.
- **Fix `Context.rng` to seed properly** — rejected in
  [ADR-0002](0002-deterministic-execution.md); it would have made a latent contradiction into a
  real one.

## Related work

- #156 — the issue this closes.
- #161 — the implementing change.
- [ADR-0002](0002-deterministic-execution.md), [ADR-0005](0005-fail-closed-dsl-registration.md).
- [09 · Extending](../09-EXTENDING.md#3-suite-inspection--analyze).
