# Execution Model

How go-specs compiles and runs tests: the execution pipeline, program structure, and performance properties.

## Execution pipeline

The end-to-end flow is:

**DSL → Builder → Program → Runner**

```mermaid
flowchart LR
    DSL --> Builder
    Builder --> Program
    Program --> Runner
```

1. **DSL** — User defines suites with `Describe`, `BeforeEach`, `AfterEach`, `It` (and optionally the Builder API with `ItParallel`).
2. **Builder** — Compiler (or Builder) flattens scope and hooks into a linear plan. No tree is kept at run time.
3. **Program** — The plan is either a flat slice of instructions with per-spec bounds (ExecutionPlan) or a slice of groups, each with before/specs/after steps (Program).
4. **Runner** — Iterates over the plan and invokes each step with a shared Context.

## Execution plan

The compiled artifact is conceptually a list of steps. Each step is a function:

```go
step func(*Context)
```

The runner does not branch on step type for the hot path; it just calls the function. Hooks and spec bodies are all steps; the compiler has already laid them out in the correct order (before hooks, body, after hooks per spec, or grouped for the Program model).

**Execution loop (conceptual):**

```go
ctx := contextPool.Get().(*Context)
ctx.Reset(backend)
for i := 0; i < len(steps); i++ {
    steps[i](ctx)
}
ctx.Reset(nil)
contextPool.Put(ctx)
```

For the ExecutionPlan path, the loop is over instructions within each spec’s slice; for the Program path, it is over groups and then over before/specs/after within each group. In both cases there is a single, flat iteration—no recursion or map lookups.

## Performance properties

- **Zero allocations** — Context (and expectation objects) are pooled. The runner reuses one Context per spec (or per group). On the assertion fast path, no heap allocations occur on success.
- **Sequential memory access** — The plan is a contiguous slice (or a small number of slices). The runner walks them in order, which is cache-friendly.
- **No reflection** — Steps are plain function pointers. Assertions use generics and direct comparison where possible; no `reflect.DeepEqual` or type switches on the hot path.
- **Direct function calls** — Each step is invoked as `step(ctx)`. No indirection or dynamic dispatch in the inner loop.

## Runner loop execution

The runner executes compiled steps in a single loop. Conceptually:

```mermaid
flowchart TD
    Start --> Step1[Step 1]
    Step1 --> Step2[Step 2]
    Step2 --> Step3[Step 3]
    Step3 --> End
```

The runner does:

```go
for i := 0; i < len(steps); i++ {
    steps[i](ctx)
}
```

There is no branching on step type in the hot path; the compiler has already laid out steps in the correct order (e.g. before hooks, body, after hooks). The runner just executes them in sequence.

**Properties:**

- **Zero allocations** — The loop does not allocate; Context and expectations are pooled.
- **Sequential memory access** — Walking a slice of function pointers is cache-friendly.
- **No reflection** — Steps are plain function pointers; no type switches or reflection in the inner loop.
- **Direct function dispatch** — Each step is invoked as `step(ctx)`; no indirection.

---

## Subtest identity

When the backend is a real `*testing.T`, each sequential spec runs in its own subtest — that is what
keeps a `Fatalf` in one spec from unwinding the whole suite. That subtest is named by the spec's
**full `Describe`/`When`/`It` breadcrumb**, joined with `/`, not by the leaf `It` name alone:

```go
Describe(t, "Cart", func(s *specs.Spec) {
    s.When("empty", func(s *specs.Spec) {
        s.It("has no items", func(ctx *specs.Context) { /* ... */ })
    })
})
```

runs as `TestCart/Cart/empty/has_no_items`, so you can select exactly that behaviour:

```bash
go test -run 'TestCart/Cart/empty/has_no_items'
```

Both sequential execution models use this mapping — `Describe`/`Spec`/`ExecutionPlan` and
`Builder`/`Program`/`Runner` — and `specs.SubtestName` exposes it. The mapping is a plain join; the
`testing` package then applies its own presentation rules on top, and those are what a `-run`
pattern must match:

| Declared name | In the `-run` pattern | Why |
|---|---|---|
| `has no items` | `has_no_items` | `testing` rewrites spaces to `_` |
| `a/b` | `a/b` (two elements) | `/` is not escaped; it adds a level |
| `` (empty) | trailing empty element | an empty name does not collapse |
| same breadcrumb twice | `…#01` on the second | `testing`'s usual disambiguation |

Two specs that share only a leaf `It` name under different `When` scopes have different breadcrumbs,
so they no longer collide and are never told apart by an incidental `#01`.

This affects Go subtest identity only. Reporter events keep the framework's own values —
`SpecStartEvent.Name` is the unsanitized leaf name and `Path` is the unsanitized breadcrumb — so
nothing `testing` does here leaks into a report. `DescribeFlat`, `DescribeFast`, and runs against a
`*testing.B` create no subtests at all and are unaffected.

---

## Memory layout concept

The compiled program is a flat slice of function pointers. The builder produces this layout; the runner consumes it in order.

```mermaid
flowchart LR
    Builder --> ProgramMemory[Program Memory Layout]
    ProgramMemory --> StepArray[Step Function Array]
    StepArray --> RunnerLoop[Runner Loop]
```

Steps are compiled into a **flat slice of function pointers**. There are no maps, no trees, and no per-step metadata in the hot path. That keeps the inner loop small and cache-friendly and is a key reason the framework is fast.

---

## Parallel execution model

When using `ItParallel`, consecutive parallel specs are grouped into a single execution step. That step runs the specs concurrently; the runner then continues with the next sequential step.

```mermaid
flowchart TD
    Runner --> ParallelGroup[Parallel group step]
    ParallelGroup --> Spec1[Spec 1]
    ParallelGroup --> Spec2[Spec 2]
    ParallelGroup --> Spec3[Spec 3]
```

The builder groups parallel specs into one step; the runner executes that step (e.g. by launching goroutines and waiting). So from the runner’s perspective, a parallel group is still just one step in the flat plan.

---

## CI sharding

`RunShard` splits a compiled `Program` across CI shards by hook group, not by individual spec: specs sharing a `BeforeEach`/`AfterEach` are compiled into one group, and whole groups are assigned to shards by `gi % shardCount == shardIndex`. If a suite has many specs under one shared hook, that entire group lands on a single shard.

**Known limitation:** this can produce uneven shard runtimes when hook groups are large or unevenly sized, since balancing happens at the group level rather than the individual-spec level. Balancing by spec count is tracked separately and deferred post-v1.0.0.
