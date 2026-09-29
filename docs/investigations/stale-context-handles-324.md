# Stale Context handles after pool reuse (#324)

Status: decided. A `ctx` retained after its spec ends is outside the supported contract, and
`ctx.Go` is the supported way to make concurrent assertions. Nothing here changes pooling or
production behavior; the strict mode and the analyzer below are separate proposals, not part of
this decision.

## The problem

A `*specs.Context` is pooled (`contextPool` in `specs/context.go`). When a spec ends, the runner
resets the Context and the next spec receives the same pointer. A goroutine that kept the pointer
(a raw `go func() { ctx.Expect(...) }()` started by a spec that then returned) still holds a
valid-looking handle. Nothing in the handle says which spec it belonged to, so whatever it does
lands on the spec that owns the pointer now.

`ctx.Go` (#318) does not have this problem: its task gets a private Context that is never pooled,
and the spec waits for it. What this note settles is the pooled, spec-level Context that user
code can retain and use from a raw goroutine.

## Current behavior, with evidence

The reproducer is `specs/context_stale_repro_test.go`. It runs spec `a` (leaks `ctx`, starts a
goroutine) and spec `b` (owns the same pointer, then releases the goroutine). Channels order every
step, so the run is deterministic and race-free under `go test -race`; it passed `-count=5`.
`contextPool` is a `sync.Pool`, so reuse is not guaranteed (the race detector drops pooled items
at random on purpose): a run that observes no reuse skips instead of claiming a result, and the
ordinary `go test` run is the one that reliably exercises the reuse.

| Stale operation | Result observed (both engines: `Describe`/`Spec` and `Builder`) |
| --- | --- |
| `ctx.Expect("x").ToEqual("y")` | Fails `b`, with the assertion's own message. `a` stays passed. |
| `ctx.T.Errorf(...)` (real `*testing.T`) | Lands on `b`'s subtest `T`; `ctx.T` is read at call time, so it is `b`'s. `b` fails. |
| `ctx.Go(func(c *Context){...})` | Does not panic; the task joins `b`'s wait group and its failure fails `b`. |

Two more facts the reproducer records. The first spec is never charged, which is why the bug looks
like "the next spec fails with someone else's message and line". And `ctx.Go`'s post-spec panic
(`goFinishedMessage`) fires only while the Context is idle: `Reset` clears `gs.closed`, so after
reuse the guard cannot tell stale from current.

The concurrent case (stale goroutine runs while `b` is executing) is a plain data race on
`failure`, `backend` and `gs`. It is available behind `GO_SPECS_REPRO_324=1`
(`TestStaleContextConcurrentAccessIsADataRace`) and is meant to be run with `-race` to see the
report; it is skipped by default because the detector would rightly fail it. Without `-race` it
finishes with `a` passed and `b` failed.

## Baseline cost (must not regress)

Measured on this branch (i7-13620H, `go test -run '^$' -bench ... -benchmem`):

| Benchmark | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `BenchmarkRunner_Minimal_Loop` | ~15.6k | 0 | 0 |
| `BenchmarkRunner_Block_8` | ~15.0k | 0 | 0 |
| `BenchmarkRunner_Program` | ~30.6k | 0 | 0 |

The allocation contract tests in `specs/allocation_contract_test.go` (minimal, block, program and
describe loops, valueless matchers, error classification) pass and assert zero allocations per
spec. `ctx.Go` allocates a task Context, a goroutine and a closure; specs that never call it pay a
nil check at each settle point.

## Alternatives

**1. Non-pooled per-spec Context.** Every spec gets a fresh Context that is dropped at the end, and
release leaves `backend == nil` so a stale call hits an existing "finished" path. This is fully
safe: a stale handle can never alias another spec. It costs one heap allocation per spec (the
Context is a large struct) plus GC pressure, which breaks the zero-allocation loop contract
above. API compatible. Rejected as the default; viable as an opt-in mode (see plan).

**2. Lease or generation token.** The handle carries a generation; every assertion compares it with
the Context's current generation. This does not work with the current API, for two reasons. The
user holds `*Context`, the very pointer the pool reuses, so there is nowhere to keep a per-spec
generation without changing the type users receive (`func(*Context)` is the public DSL) or
allocating a distinct handle per spec, which is alternative 1 with extra steps. And, as the issue
notes, a generation checked only at assertion time is insufficient: the check and the write are
not atomic against reuse, so a stale call can pass the check just before the runner recycles the
Context. Only an unpooled handle removes the aliasing. Rejected.

**3. Isolated task handle (`ctx.Go`).** Already shipped. It gives the strong guarantee for work
started through it and costs nothing for specs that do not use it. It cannot protect a raw `go`
statement, because the goroutine uses the outer pointer. Kept as the supported route.

**4. Documented raw-`go` prohibition, plus tooling.** Zero runtime cost and no API change. The
weakness is that it relies on users reading the docs, so the natural follow-up is a static check
(a `go vet`-style analyzer) that flags a `*specs.Context` captured by a `go` statement or passed
to one inside a spec body. It will miss indirect flows (a stored closure, a channel of funcs) and
must say so. This is the current position, strengthened.

**5. Detect leaked goroutines at spec end.** Rejected without a prototype: Go exposes no supported
way to ask which goroutines hold a pointer, and goroutine-count heuristics are flaky.

| | API compatible | Allocations for specs without `ctx.Go` | Concurrency safety vs stale handle |
| --- | --- | --- | --- |
| 1 Per-spec Context | yes | +1 per spec | safe |
| 2 Generation token | no (or same as 1) | 0 or +1 | not safe (check/reuse race) |
| 3 `ctx.Go` | yes | 0 | safe for tasks only |
| 4 Prohibition (+ proposed analyzer) | yes | 0 | not protected; an analyzer could flag direct captures only |

## Decided lifecycle contract

Keep the pooled Context and the documented limitation. State it as a rule rather than a warning:
a spec's `ctx` is valid only while that spec's body, hooks and `ctx.Go` tasks are running, and
must not be used from any goroutine that can outlive them. `ctx.Go` is the only protected way to
assert concurrently. A raw `go` goroutine is unprotected and its misuse may be attributed to a
different spec, or race, and is never reported as a framework bug. We do not present the raw
pattern as protected, and we do not promise a panic after reuse.

Migration for a spec that spawns goroutines:

- Replace `go func() { ctx.Expect(x).ToEqual(y) }()` with `ctx.Go(func(ctx *specs.Context) { ... })`.
- If the goroutine only computes a value, keep it a raw goroutine, do not touch `ctx` inside it,
  join it (`sync.WaitGroup` or a channel) before the body returns, and assert on the result in the
  spec goroutine.
- Copy plain data out of the spec rather than capturing `ctx`.

## Separate proposals (not part of this contract)

The contract above stands on its own and does not wait on any of these. They are candidates for
their own issues, each to be justified with measurements; none is implemented, and none would make
the raw-`go` pattern supported. Even an analyzer can only flag direct captures, so late use is
never promised to be detected:

1. **Opt-in strict Context mode.** A runner option (or `GO_SPECS_STRICT_CONTEXT=1`) that hands each
   spec a fresh, non-pooled Context and never returns it to `contextPool`. Default path untouched.
2. **Loud stale use in strict mode.** Confirm what `Expect`, `ctx.T` and `ctx.Go` do on a retired
   Context (`backend == nil`) and make each panic with an actionable `specs:` message; extend the
   reproducer to assert it.
3. **Allocation and speed proof.** Show the default path still passes every allocation contract
   test at 0 allocs/op, and record the strict-mode cost per spec.
4. **Static check.** A `go vet`-style analyzer that flags a `*specs.Context` used inside a `go`
   statement in a spec body, with documented false negatives.
5. **Docs and decision.** Update `docs/DSL.md`, `docs/PERFORMANCE.md` and the changelog with the
   strict mode and analyzer, and decide with data whether strict mode should ever become default.
