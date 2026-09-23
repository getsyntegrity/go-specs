# Suite hook contract: `BeforeAll` / `AfterAll`

Status: normative. Tracking issue: [#207](https://github.com/getsyntegrity/go-specs/issues/207).

This document is the contract every go-specs execution engine must follow for **group hooks** —
setup and teardown that run once per `Describe`/`When` group, instead of once per spec the way
`BeforeEach`/`AfterEach` already do. It is engine-agnostic: today only the canonical `Describe`
engine (`specs.Describe`, `Spec.Describe`, `Spec.When`, `Spec.It`) implements it, but any engine
that later adds `BeforeAll`/`AfterAll` — including the Builder/Runner engine in a follow-up change
— must implement exactly these rules, not reinterpret them.

## The problem this solves

Every hook in go-specs today is per-spec: `BeforeEach`/`AfterEach` run before and after *each*
`It`, even when three specs in the same `When` block all need the same expensive fixture — a
Postgres container, a shared test server, a large seeded dataset. Without a once-per-group hook,
a user either pays that setup cost once per spec, or steps outside the DSL entirely with `TestMain`
or `sync.Once` — losing the DSL's structure and its reporting.

`BeforeAll`/`AfterAll` close that gap: register once on a `Describe`/`When` group, and the hook
runs exactly once for the group, no matter how many specs (including nested groups' specs) it
contains.

## The mechanism, in one paragraph

Each group is *entered* lazily, right before the first spec that will actually run inside it —
not when the group is declared. Entering a group runs its `BeforeAll` hooks outer-to-inner (the
same order `BeforeEach` already uses); leaving a group — right after its last spec or subgroup
finishes — runs its `AfterAll` hooks inner-to-outer (the same order `AfterEach` already uses). A
group with no spec to run at all is never entered, so an unused fixture never pays its setup cost.
A hook failure is reported as one synthetic case attached to the group, distinct from a real spec,
and never stops teardown: once a group's `BeforeAll` has started, its `AfterAll` is guaranteed to
run.

## Example

```go
specs.Describe(t, "checkout", func(s *specs.Spec) {
    var db *sql.DB

    s.BeforeAll(func(ctx *specs.Context) {
        db = mustStartTestDatabase() // one container for every spec below
    })
    s.AfterAll(func(ctx *specs.Context) {
        db.Close()
    })

    s.When("cart has items", func(w *specs.Spec) {
        w.It("charges the card", func(ctx *specs.Context) { /* uses db */ })
        w.It("emails a receipt", func(ctx *specs.Context) { /* uses db */ })
    })
})
```

`mustStartTestDatabase` runs exactly once, right before `"charges the card"` (the first runnable
spec), and `db.Close()` runs exactly once, right after `"emails a receipt"` (the last one) —
however many `It`s the suite grows to.

## Rules

### H1 — Scope: every group has its own hooks

Every group has its own `BeforeAll`/`AfterAll`: the root `specs.Describe` body, every nested
`Describe`, every `When`. A group's hooks are never inherited by nested groups — they belong to
exactly the group they were registered on, unlike `BeforeEach`/`AfterEach`, which flatten onto
every descendant spec. Multiple registrations in the same group run in registration order, the
same convention `BeforeEach`/`AfterEach` already use:

```go
s.BeforeAll(func(ctx *specs.Context) { order = append(order, "first") })
s.BeforeAll(func(ctx *specs.Context) { order = append(order, "second") })
// runs "first" then "second"
```

### H2 — Entry and order: outer before inner, inner after outer

A group is entered lazily, right before its first runnable spec (including a spec that belongs to
a nested group). Setup is outer→inner; teardown is inner→outer:

```
outer BeforeAll → inner BeforeAll → [BeforeEach → It → AfterEach]* → inner AfterAll → outer AfterAll
```

A group's `AfterAll` runs right after its last spec or subgroup finishes, before the suite moves
on to the group's next sibling.

```go
s.Describe("outer", func(o *specs.Spec) {
    o.BeforeAll(func(*specs.Context) { order = append(order, "outer:before") })
    o.AfterAll(func(*specs.Context) { order = append(order, "outer:after") })
    o.When("inner", func(w *specs.Spec) {
        w.BeforeAll(func(*specs.Context) { order = append(order, "inner:before") })
        w.AfterAll(func(*specs.Context) { order = append(order, "inner:after") })
        w.It("spec", func(*specs.Context) { order = append(order, "spec") })
    })
})
// order: outer:before, inner:before, spec, inner:after, outer:after
```

### H3 — The runnable set is decided before entry

Whether a group has any runnable spec is decided from information known *before* the group is
entered — today that is simply "does this group's subtree declare at least one `It`"; an engine
that adds compile-time `Skip`/`Pending` or upfront name filtering feeds that decision here too. A
group with **zero runnable specs is never entered**: none of its `BeforeAll`/`AfterAll` run, and no
synthetic hook case is emitted. Its specs (if any exist but were all filtered out before this
point) report their own status exactly as they do today.

```go
s.When("no specs here", func(w *specs.Spec) {
    w.BeforeAll(func(*specs.Context) { panic("never runs") })
    // no It() at all — BeforeAll above is never called, no [BeforeAll] case is emitted
})
```

A spec that only turns out not to execute at *run time* — for example, one that skips itself from
inside its own body — does not change this: the group was already entered, its `BeforeAll` already
ran, and its `AfterAll` still runs.

### H4 — `BeforeAll` failure

A failure — an assertion via `ctx`, `Fatal`/`FailNow` (`runtime.Goexit`), or a panic — in any
`BeforeAll` of a group:

- is reported **once**, as a synthetic case `[BeforeAll]` whose path is the group's own path (the
  same declared `Describe`/`When` chain a real spec in that group would carry);
- stops the remaining `BeforeAll`s of that same group, and every nested group's hooks — a nested
  group is never entered at all when an ancestor already failed;
- reports every spec of the group, including specs of nested groups, as **skipped**, with a message
  naming the failing group; no spec body and no `BeforeEach` of the group runs;
- does not affect sibling groups or the rest of the suite: once the failing group's own `AfterAll`
  has run and the suite leaves it, execution continues normally.

```go
s.Describe("A", func(a *specs.Spec) {
    a.BeforeAll(func(ctx *specs.Context) { ctx.Expect(false).To(specs.BeTrue()) }) // fails
    a.It("never runs", func(*specs.Context) { t.Fatal("must not execute") })
})
s.It("B, a sibling of A, runs normally", func(*specs.Context) { /* ... */ })
```

### H5 — The `AfterAll` guarantee

Once a group was entered — its first `BeforeAll` started, or it has no `BeforeAll` at all and its
first spec started — every one of its `AfterAll`s runs, including after a `BeforeAll`
failure/panic, a spec failure, or a spec panic. A failing `AfterAll` does not stop the remaining
`AfterAll`s of that same group, or of an outer group.

### H6 — `AfterAll` failure

Reported once, as a synthetic case `[AfterAll]` under the group's own path. Specs that already
passed keep their passed status — an `AfterAll` failure never retroactively fails a spec that
already finished. Per H5, every remaining `AfterAll` (the rest of this group's own list, and every
outer group's) still runs.

```go
s.BeforeAll(func(*specs.Context) { /* ok */ })
s.AfterAll(func(ctx *specs.Context) { ctx.Expect(false).To(specs.BeTrue()) }) // fails
s.AfterAll(func(*specs.Context) { order = append(order, "still runs") })
s.It("passes and keeps its status", func(*specs.Context) {})
```

### H7 — Panic and `Goexit` attribution

Hook panics and `FailNow`/`Fatal` (`runtime.Goexit`) go through the same single recovery authority
every other execution path in this engine already uses (`reportRecoveredPanic` /
`recoverSpecFailure`, `specs/panic_report.go`): they are never double-reported, and they are
attributed to the synthetic hook case, not to whichever spec happens to run next. A `Goexit` in a
group hook must not abort sibling groups — each group's hooks run inside their own isolated
subtest, the same isolation a real spec's body already gets (`runSpecProgramIsolated`).

### H8 — Reporting: hook cases are structurally distinct

Synthetic hook cases appear in every renderer (TXT, JSON, HTML, JUnit XML) and are counted in
totals, exactly like a real spec. They are distinguishable from a real spec by a **structural**
field — `report.SpecResultEvent.Hook`, a compact `report.HookKind` enum (`HookBeforeAll` /
`HookAfterAll`, zero value `HookNone` for a real spec), copied to `report.Case.Hook` as a plain
string (`"BeforeAll"`/`"AfterAll"`) by the collector — not merely by the bracketed name. The marker
lives only on the *result* event: `report.SpecStartEvent` carries no `Hook` field, so a failed
hook's `SpecStarted` event is never marked and a consumer must attribute at `SpecFinished`. It is a
`uint8` enum rather than a string on `SpecStartEvent`/`SpecResultEvent` deliberately: `SpecResultEvent`
is copied by value once per spec on every engine, so a string field there would be a per-spec tax
paid even by a suite with no group hooks at all, which is exactly what H10 forbids; `HookKind`
instead fits into padding the existing `Failed`/`Skipped`/`Filtered`/`Pending` bools already leave
before `Duration`, so `SpecResultEvent`'s size is unchanged. Only a *failed* hook produces a case: a
passing `BeforeAll` or `AfterAll` emits nothing, so the report shape of an existing suite that
registers no group hooks is completely unchanged.

A hook case's identity in every renderer is (group path, `"[BeforeAll]"`/`"[AfterAll]"`), because
every group shares the same marker name. Its JUnit classname is the full group path, not the group
path with its last element dropped as for a real spec, since a hook case's `Path` already *is* the
group and carries no separate leaf name. TXT and HTML print it as `<group path> [BeforeAll]`, e.g.
`Checkout/when cart has items [BeforeAll]`; real specs keep printing their own name only.

### H9 — Parallel and `FailFast` (normative now, implemented later)

This rule is normative for any engine that supports parallel specs or `FailFast` — today that is
the Builder/Runner engine, which implements it in a follow-up change, not in the `Describe` engine change:

- `BeforeAll` completes before any spec of the group starts, including a parallel one.
- `AfterAll` starts only after every spec of the group — including every parallel one — has
  finished (i.e., after the group's parallel batch has been waited on).
- `FailFast` stops only at the next group boundary; it never skips the `AfterAll` of a group that
  was already entered. A hook failure counts as a failure for `FailFast`, exactly like a spec
  failure.

### H10 — Cost: opt-in only

A suite that registers no `BeforeAll`/`AfterAll` anywhere keeps its current allocation counts
(pinned by `specs/allocation_contract_test.go`) and its current report output, byte for byte. Group
hook bookkeeping is allocated only for a suite that actually uses `BeforeAll`/
`AfterAll`; a suite that does not pays nothing for a feature it never asked for.

Allocation *counts* are not enough to prove that, because a struct can grow without allocating
more often. The first draft of #207 added five slice headers to `ExecutionPlan` and `NodeArena`,
kept `BenchmarkDescribeVariant_Describe` at exactly 426 allocs/op, and still cost every suite about
96 B/op: `ExecutionPlan` grew from 192 to 264 bytes and moved up an allocation size class. The rule
is therefore pinned in bytes, by `specs/group_hook_cost_test.go`:

- `ExecutionPlan` (192 bytes) and `NodeArena` (96 bytes) keep their exact pre-#207 sizes. Both sit
  exactly on a size-class boundary, so they carry no group-hook field at all.
- Group-hook storage hangs off one pointer on `CompiledSuite` and one on the Analyze registry, both
  of which had 8 bytes of slack in their size class. The pointer stays nil, and nothing behind it is
  allocated, until a `BeforeAll`/`AfterAll` is registered.

With that layout, `BenchmarkDescribeVariant_Describe/_DescribeFlat/_DescribeFast` report the same
B/op and allocs/op as before the feature (57,180–57,188 B/op and 426 allocs/op on both trees).

The per-spec event is part of this rule. `report.SpecResultEvent` is built and copied by value for
every spec on every engine, so any field this feature adds to it is a cost every suite pays, hook
or no hook. Its size is therefore pinned at 112 bytes on 64-bit platforms by
`TestSpecResultEventSizeUnchanged` (`report/hook_case_test.go`), and the hook marker is the one-byte
`report.HookKind`, placed in the padding the event's bool flags already leave. This is not
theoretical: a first draft of #207 carried the marker as a `string` on `SpecStartEvent`, which grew
the event from 112 to 128 bytes and slowed `BenchmarkSuite_1000` — a Builder/Runner suite that
never emits a hook case — from ~17.5 µs/op to ~20.2 µs/op. The compact field restored 112 bytes and
~17.4–17.6 µs/op.

## Where a hook's `*Context` comes from

`BeforeAll`/`AfterAll` receive a `*specs.Context`, the same type `BeforeEach`/`AfterEach`/`It`
receive, built the same way: acquired from the runner's context pool and `Reset` against the
group's own isolated subtest backend (see H7) before the hook runs. State a `BeforeAll` needs to
share with the specs it sets up for (e.g. `db` in the example above) is carried the same way it
already is without this feature: a variable captured by the hook and spec closures declared in the
same Go scope, not through the `*Context` itself.

## Engine support

| Engine | `BeforeAll`/`AfterAll` |
| --- | --- |
| `Describe`/`Spec` (canonical, `ExecutionPlan` + `CompiledSuite`) | ✅ supported |
| `Builder`/`Program`/`Runner` | ❌ not yet — a follow-up change after the `Describe` engine; H9 above is what it must implement |

See `docs/EXECUTION_ENGINES.md` for the full engine inventory this table is part of.
