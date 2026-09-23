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

**A hooked group needs a unique, non-empty name.** A group that registers `BeforeAll` or `AfterAll`
runs as its own Go subtest (H7), so its name has to work as one. It must be explicit and non-empty,
and no spec or other hooked group of the same suite may have the same full subtest name once `go
test` has normalized both (spaces become `_`, non-printable runes are escaped). The suite is
rejected while it is built — before any spec runs, on every entry point (`Describe`,
`DescribeWithReporter`, `BuildSuite`, and inside `Analyze`) — with a panic naming the group, for
each of these shapes:

- an empty group name: `When("")`, or `specs.Describe(t, "", ...)` with root hooks;
- an `It("")` directly inside the group;
- a spec with the group's full name, e.g. `It("x")` next to a hooked `When("x")`, or `It("sp_ace")`
  next to a hooked `When("sp ace")`;
- another hooked group with the same full name: two hooked sibling `When("x")`, or a hooked
  `When("a/b")` next to `When("a")` containing a hooked `When("b")`.

Each of these would otherwise make Go rename a subtest (`#00`, `#01`) and change a spec's full
subtest name. Groups without `BeforeAll`/`AfterAll` are not checked and keep every name they could
always have.

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

**External test selection (`go test -run`).** A `-run` filter is applied by Go's `testing` package
while the suite runs, so it cannot be part of the decision above. Instead, a group is entered only
when one of its descendant specs *actually starts* — inside that spec's own subtest, right before
its body. The consequences are exact in both directions:

- `go test -run '^TestCheckout$/^checkout$/^cart_has_items$/^charges_the_card$'` selects one spec
  inside a hooked group; the group's `BeforeAll` runs exactly once, before it, and its `AfterAll`
  after it. A group hook is never filtered out on its own while a spec it guards still runs.
- A `-run` pattern that selects no spec of a group means the group is never entered: none of its
  hooks run, and no synthetic hook case is emitted. Its specs are reported `Filtered`, exactly as
  they are without group hooks.

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

**Failures reported directly through `ctx.T`.** An assertion through `ctx`, `ctx.T.Fatal`,
`ctx.T.FailNow` and a panic are always detected. A *non-fatal* report made directly on `ctx.T`
(`ctx.T.Error`, `ctx.T.Errorf`, `ctx.T.Fail`) leaves only one trace: the `*testing.T` it was made on
turns failed. Go's `testing` propagates a failure to every parent test and offers no way to observe
an individual call, so that trace is readable only while the hook's `*testing.T` had not failed
before the hook ran. Every hooked group has its own subtest (see H1 and H7), and that subtest has
never failed when its `BeforeAll` runs, because the group is entered before any of its specs. A
`BeforeAll` that reports through `ctx.T.Errorf` is therefore always detected. (`AfterAll` is
different; see H6.)

### H5 — The `AfterAll` guarantee

Once a group was entered — its first `BeforeAll` started, or it has no `BeforeAll` at all and its
first spec started — every one of its `AfterAll`s runs, including after a `BeforeAll`
failure/panic, a spec failure, or a spec panic. A failing `AfterAll` does not stop the remaining
`AfterAll`s of that same group, or of an outer group.

This includes a run stopped by an unsupported `ctx.T.Parallel()` call (in a spec body or a hook,
see "Hook lifetime"): no further spec runs, but every group already entered still runs its
`AfterAll`s while the stop unwinds, inner group first.

### H6 — `AfterAll` failure

Reported once, as a synthetic case `[AfterAll]` under the group's own path. Specs that already
passed keep their passed status — an `AfterAll` failure never retroactively fails a spec that
already finished. Per H5, every remaining `AfterAll` (the rest of this group's own list, and every
outer group's) still runs.

Limitation: an `AfterAll` that reports a failure *only* through a non-fatal `ctx.T.Error`,
`ctx.T.Errorf` or `ctx.T.Fail`, in a group whose `*testing.T` has already failed (typically because
one of its specs failed), produces no `[AfterAll]` case — for the reason explained under H4. `go
test` still prints the message and fails the group's subtest; only the synthetic case is missing.
Failing through an assertion (`ctx.Expect(...)`), `ctx.T.Fatal`/`FailNow` or a panic is always
reported. Reporting every such `AfterAll` as failed instead would invent failures.

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
group hook must not abort sibling groups.

Hooks are **not subtests**. When the suite runs against a real `*testing.T`, every group that
registers a hook gets a real Go subtest of its own, and its specs and nested groups run inside it.
The subtest names compose to exactly the name each spec had without group hooks
(`TestCheckout/checkout/cart_has_items/charges_the_card` either way), so `-run` patterns and IDE
links keep working. The group subtests themselves are visible, though: `go test -v` prints a
`=== RUN`/`--- PASS` line for `TestCheckout/checkout/cart_has_items`, and `go test -json` reports it
as a test of its own, so tools that count tests from that stream (gotestsum, IDE test trees) count
one more entry per hooked group than before. Each hook then runs synchronously on a goroutine of its own, with `ctx.T` set to
the group's subtest: a `Goexit` or panic unwinds only that goroutine, never the group, the spec
that triggered the group's entry, or a sibling. A hook failure — an assertion, `ctx.T.Fatal`,
`ctx.T.FailNow`, or a panic — is reported as the synthetic case *and* marks the group's subtest
failed, so `go test` prints `--- FAIL: TestCheckout/checkout/cart_has_items` for it. Specs skipped
by a `BeforeAll` failure are reported skipped without a subtest of their own, except the one whose
start triggered the group's entry, whose subtest is marked skipped with the same reason.

Within one suite, the naming rule in H1 guarantees these names. A collision with a subtest that
only exists at run time cannot be known while the suite is built: a second same-named hooked
`specs.Describe(t, "suite", ...)` in the same test function, your own earlier `t.Run("suite", ...)`,
or a spec of an earlier hook-free suite whose full name equals the group's. Go then does what it
always does for a repeated subtest name: it gives the group subtest a `#NN` suffix
(`TestX/suite#01`), and the group's specs sit under it (`TestX/suite#01/when_a/spec`). The suffix is
deterministic, unique and selectable with `-run`; everything else about the group is unchanged.

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
receive, acquired from the runner's context pool. Its failures are reported to the group's subtest
(see H7). State a `BeforeAll` needs to share with the specs it sets up for (e.g. `db` in the example
above) is carried the same way it already is without this feature: a variable captured by the hook
and spec closures declared in the same Go scope, not through the `*Context` itself.

### Hook lifetime

Against a real `*testing.T`, `ctx.T` in a hook is the **group's own subtest**. Anything a
`BeforeAll` registers through it — `ctx.T.Cleanup`, `ctx.T.TempDir`, `ctx.T.Setenv`, and fixture
libraries built on them — therefore lives for the whole group: it is visible to every spec of the
group and to its `AfterAll`, and it is released when the group's subtest ends, right after its
`AfterAll` and before the next sibling group starts. A `ctx.T.Setenv` in group A is restored before
sibling group B runs.

```go
s.When("with a scratch dir", func(w *specs.Spec) {
    var dir string
    w.BeforeAll(func(ctx *specs.Context) {
        dir = ctx.T.TempDir()        // removed after this group's AfterAll
        ctx.T.Setenv("APP_ENV", "test") // restored before the next sibling group
    })
    w.It("writes a file", func(*specs.Context) { /* dir and APP_ENV are both still here */ })
})
```

`ctx.T.Parallel()` is not supported inside a hook: making the group's subtest parallel would detach
the group's hooks from its specs. It is detected and stops the run with a diagnostic, the same way
`ctx.T.Parallel()` inside a sequential spec body already is. `ctx.T.SkipNow()` inside a hook cannot
skip the group; it is reported as a hook failure.

On a backend that is not a real `*testing.T` (a `*testing.B`, or a fake backend in go-specs' own
tests) there are no subtests: hooks run directly, with the same H1–H7 semantics.

## Engine support

| Engine | `BeforeAll`/`AfterAll` |
| --- | --- |
| `Describe`/`Spec` (canonical, `ExecutionPlan` + `CompiledSuite`) | ✅ supported |
| `Builder`/`Program`/`Runner` | ❌ not yet — a follow-up change after the `Describe` engine; H9 above is what it must implement |

See `docs/EXECUTION_ENGINES.md` for the full engine inventory this table is part of.
