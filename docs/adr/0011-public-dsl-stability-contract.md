# ADR-0011: The Public DSL Stability Contract

## Status

Accepted. Corrects an error in the previously published list.

## Date

2026-09-18

## Deciders

go-specs maintainers. Source: `RULES.md`, restated in `AGENTS.md`.

## Context

go-specs is pre-1.0, so most of it may move. But the DSL is what appears in every test file of every
consuming repository. A breaking change there is not a migration — it is a find-and-replace across
someone else's whole test suite, for every consumer at once.

Pre-1.0 therefore cannot mean "everything is fair game". It has to mean "here is the part that is
not, and everything else is".

The previously published list also contained a form that **does not exist**:
`ctx.Expect(...).ToBeNil(...)`. No such method is defined anywhere in the module; calling it is a
compile error. It appeared in `RULES.md` and was then inherited, unverified, into
`docs/LEGACY_PARITY.md`, where it was cited as evidence that the API had parity with an earlier
branch. A stability contract that promises a nonexistent API is worse than no contract, because it is
evidence that nothing checked it.

## Decision

### The stable surface

These forms do not break without an explicit decision to break them:

```go
specs.Describe(tb, name, fn)
specs.When(name, fn)
specs.It(name, fn)
ctx.Expect(actual).ToEqual(expected)
specs.Paths(...)
```

### The correction

**`ctx.Expect(...).ToBeNil(...)` is removed from the contract.** It never existed. The nil check is
the matcher form:

```go
ctx.Expect(value).To(specs.BeNil())
```

`RULES.md` and the [legacy parity appendix](../appendix/legacy-parity.md) are corrected accordingly.

### Where the list lives

The authoritative reference for DSL behaviour is [02 · The DSL](../02-DSL.md), regenerated against
the code. `RULES.md` and `AGENTS.md` carry the stability *promise*; where a terse list and the
regenerated reference disagree, the reference wins — and the disagreement is a bug to fix, not a
matter of interpretation.

### Explicitly outside the contract

Stability is not implied for anything absent from the list, including: `Builder`, `Program`,
`Runner`, `MinimalRunner`, `BlockRunner`, `BytecodeRunner`, `Analyze` and the registry helpers,
sharding, `report` internals, and the `DescribeFlat`/`DescribeFast` aliases.

## Decision classification

### Accepted now

- The five stable forms, and the removal of the sixth that never existed.
- The rule that the regenerated reference outranks the terse lists.

### Deferred

- Promoting more of the surface into the contract at 1.0. `ctx.Expect(x).To(matcher)` and
  `specs.EqualTo` are strong candidates — both are widely used and neither is likely to change — but
  broadening the promise is a 1.0 decision, not a pre-1.0 one.
- An automated check that every form in the contract compiles. That is the mechanism that would have
  prevented the `ToBeNil` error, and it does not exist yet.

### Open hypotheses

- That five forms are enough to be useful. If real consumers depend on `EqualTo` or the matcher form
  as heavily as on `ToEqual`, the list is describing less than it should.

## Consequences

**Benefits:** a consumer can write tests against a named, bounded surface; a contributor knows which
change requires a decision and which does not; the framework keeps freedom to move everything else
while pre-1.0.

**Costs:** the contract constrains design — an improvement to `Describe`'s signature is now a
negotiation rather than a commit. And the list is maintained by hand, which is exactly how the
`ToBeNil` error survived long enough to be cited as evidence in a second document. Until a
compile-checked list exists, this record's own accuracy depends on review.

## Rejected / deferred alternatives

- **Promise nothing until 1.0** — rejected. Consumers exist now, and "pre-1.0" is not a licence to
  break every test file in every consuming repository without warning.
- **Promise the whole public API** — rejected. It would freeze the runner variants, the registry
  surface and the reporting internals, all of which are actively moving and none of which appear in
  a typical user's test file.
- **Keep `ToBeNil` and implement it to match the documentation** — rejected. The documentation was
  wrong, not prescient; adding an API to make a stale document true is how a codebase accumulates
  surface nobody asked for.

## Related work

- `RULES.md`, `AGENTS.md` — where the promise is stated.
- [02 · The DSL](../02-DSL.md) — the regenerated reference.
- [appendix · legacy parity](../appendix/legacy-parity.md) — the second document that inherited the
  error.
- [ADR-0005](0005-fail-closed-dsl-registration.md), [ADR-0006](0006-arena-registry-as-the-only-tree-surface.md) —
  recent breaking changes, all outside this contract.
