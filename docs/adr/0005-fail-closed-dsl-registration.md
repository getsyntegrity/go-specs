# ADR-0005: A DSL Surface With Nowhere To Register Fails Closed

## Status

Accepted. Breaking change.

## Date

2026-09-18

## Deciders

go-specs maintainers, via #151 / #163.

## Context

The worst failure a test framework can have is not a crash. It is reporting green for a suite that
ran nothing.

Two constructions made that reachable:

**1. A `Spec` with no build target.** `Spec` is exported with unexported fields, so `&specs.Spec{}`
compiles. It carries neither a compiler nor a registry — there is nowhere for an `It`, a hook or a
nested block to go. Every registration on it was silently discarded. The suite declared specs,
registered none of them, ran nothing, and reported success.

**2. A registry helper called outside `Analyze`.** `AppendBeforeHook`, `AppendAfterHook` and
`SetPathGen` write into the registry that `Analyze` pushed for the calling goroutine. Called outside
that context — or from a goroutine spawned inside it, since the registry stack is keyed per
goroutine — there was no registry to write to, and the call returned quietly. The caller believed a
hook was installed that would never run.

Both are the same defect: an API accepting an instruction it cannot carry out, and reporting success.

The registry also enforced its "stack is never empty" invariant in four separate call sites, some
guarded and some not, so the behaviour on a violated invariant depended on which entry point you came
through.

## Decision

**Every exported construction path that accepts a registration and cannot fulfil it panics.**

| Surface | Behaviour |
| --- | --- |
| `Spec.It`, `Describe`, `When`, `BeforeEach`, `AfterEach`, `Paths` on a build-target-less `Spec` | panic via `requireBuildTarget` |
| `AppendBeforeHook`, `AppendAfterHook`, `SetPathGen` with no active registry | panic via `requireRegistry` |
| `CurrentSuite`, `CurrentArena` with no active registry | return `nil` — **not** a panic |
| `PrintTreeArena` with nil input | tolerated no-op |
| A `nil` `*Spec` | tolerated no-op |

The messages name both the problem and the fix:

```
panic: specs: Spec.It called on a Spec with no build target;
       obtain a *Spec from Describe/BuildSuite instead of constructing one

panic: specs: AppendBeforeHook called with no active registry;
       call it inside Analyze(fn) on the same goroutine
```

The stack invariant is now enforced in exactly one place, `currentNodeIDLocked`, which every mutating
entry point routes through.

### The read/write asymmetry is the point

A **reader** asking "is a suite being built?" gets `nil`, because *"no"* is a legitimate, complete
answer to that question.

A **writer** handed a hook has nowhere to put it. Returning quietly would discard the caller's work
and report success. There is no honest quiet answer, so it panics.

A `nil` `*Spec` stays a no-op because there is no Spec there to have registered anything into — the
caller is not asserting that a registration happened.

## Decision classification

### Accepted now

- The table above, and the single-site stack invariant.
- Goroutine scoping: a helper called from a goroutine started inside `Analyze` panics rather than
  writing into, or corrupting, a registry nobody will read.

### Deferred

- Whether `Spec` should stop being externally constructible at all — an interface, or an unexported
  type behind a constructor. That would make the first failure mode a compile error instead of a
  panic, and is a larger API change than this record makes.

### Open hypotheses

- That panicking is proportionate. It is a programming error, not a test failure, and it is caught on
  the first run by anyone who makes it. If real usage surfaces a case where the panic fires on
  correct code, that is the trigger to revisit.

## Consequences

**Benefits:** a suite that registers nothing can no longer report green; the failure arrives at the
point of the mistake with the fix in the message; the invariant lives in one place instead of four
inconsistent ones.

**Costs:** this is a breaking change. Code that constructed `&specs.Spec{}` — or called a registry
helper outside `Analyze` — compiled and ran before, and now panics. That code was already broken;
it just did not say so. A panic in a testing library is also a strong instrument, and it puts the
burden on the messages to be actionable, which is why they name the fix rather than the symptom.

## Rejected / deferred alternatives

- **Return an error instead of panicking** — rejected. It would change the signature of every DSL
  method, and a DSL whose every call site handles an error is not a DSL. The error would also be
  ignored in exactly the code that made the mistake.
- **Log a warning and continue** — rejected. A warning in test output is noise that a green result
  overrides. The whole failure mode is "green but wrong"; a warning does not change the result.
- **Make the zero-value `Spec` work by lazily creating a build target** — rejected. The registration
  would go into a suite nobody runs, which is the same silent discard wearing a different hat.
- **Detect it at compile time with a linter** — deferred. Worth having, but a linter is opt-in and
  this must hold for everyone.

## Related work

- #151 — the issue this closes.
- #163 — the implementing change.
- [ADR-0006](0006-arena-registry-as-the-only-tree-surface.md) — the extension surface this hardens.
- [02 · The DSL](../02-DSL.md#12-where-a-spec-comes-from),
  [09 · Extending](../09-EXTENDING.md#3-suite-inspection--analyze).
