# ADR-0002: Deterministic Execution Overrides Convenience

## Status

Accepted.

## Date

2026-09-18 (recorded, with the removal of `Context.rng` in #161). The principle is founding and has
been enforced continuously.

## Deciders

go-specs maintainers, via #156 / #161.

## Context

A test suite that produces different results on different runs, from the same inputs, destroys the
thing tests are for. Every flake spends reviewer attention, and the expensive ones spend it
repeatedly without producing information.

Go offers several easy routes to accidental nondeterminism: map iteration order, goroutine
scheduling, wall-clock seeding, and any `sync.Pool`-adjacent reuse that leaks state between units.

`Context` carried an `rng` field that looked like a seeded random source. It was never actually
seeded in practice: `contextPool`'s reset cleared it and only `NewContext` ever set it, so it was
`nil` on every pooled path. Had it worked, it would have seeded from `time.Now().UnixNano()` — a
wall-clock seed inside the execution context of a framework whose stated contract is deterministic
execution.

## Decision

Execution is deterministic. Concretely:

1. **Specs run in declaration order.** The run loop is an index scan over a flat plan. No map
   iteration participates in ordering.
2. **The sequential path schedules no goroutines.** Concurrency is opt-in through `ItParallel` and
   `RunParallel`.
3. **Parallel execution does not make *reporting* nondeterministic.** Workers record failures by spec
   index; after the wait group drains, failures are reported in spec order and the first failing
   index is surfaced — never "whichever goroutine finished first".
4. **Sharding is a pure function of index** (`i % total == shard`), so shard membership is identical
   on every machine and every run.
5. **There is exactly one source of randomness**: the generator that `Spec.RandomSeed(seed)` feeds to
   `Paths`. It is explicit, caller-seeded and reproducible.
6. **No ambient RNG exists on `Context`.** The field was removed rather than fixed.

## Decision classification

### Accepted now

- All six rules above.
- Any new feature that would introduce unseeded randomness, wall-clock-dependent behaviour or
  ordering derived from map iteration is out of scope until it can be made explicit and seeded.

### Deferred

- Whether adaptive path strategies should gain an isolated-re-run mode. Narrowing `-run` to one
  generated candidate perturbs `ExploreCoverage`/`ExploreSmart` corpus state, so the candidate that
  re-runs is not reliably the one that failed. Tracked in #124; documented in
  [appendix · subtest identity](../appendix/subtest-identity.md).

### Open hypotheses

- That declaration order remains the right default. Randomized ordering is a real technique for
  catching inter-spec coupling — but it belongs behind an explicit, seeded, reproducible flag, not as
  ambient behaviour.

## Consequences

**Benefits:** a failure reproduces; a bisect is meaningful; a shard is reproducible; a report is
evidence rather than a sample. Reviewers can trust a red build.

**Costs:** determinism forecloses features that would be easy otherwise — ambient random ordering,
timing-derived behaviour, cheap unique-ID generation inside a spec. Users needing randomness must
seed it themselves, which is more typing than calling a helper that does it for them.

## Rejected / deferred alternatives

- **Fix `Context.rng` to seed properly** — rejected. That would have made a real wall-clock-seeded
  RNG out of dead code, converting a latent contradiction into an actual one.
- **Keep `Context.rng` as a documented no-op** — rejected. An exported-looking field that silently
  does nothing is worse than either alternative: it invites use and rewards it with confusion.
- **Randomize spec order by default, like some frameworks do** — deferred. The technique has value;
  as a default it makes every report a sample of one ordering, which conflicts with rules 1 and 4.

## Related work

- #156, #161 — removal of `Context.rng` and the obsolete tree API.
- #124 — adaptive strategy state under a narrowed `-run`.
- [ADR-0006](0006-arena-registry-as-the-only-tree-surface.md) — the same commit's other removal.
- [03 · Execution model](../03-EXECUTION-MODEL.md#4-ordering).
