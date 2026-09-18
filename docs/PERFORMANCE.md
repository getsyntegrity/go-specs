# Performance

This document explains why go-specs is extremely fast: execution cost, allocation behavior, scaling, and the design choices that enable sub-nanosecond assertions and microsecond-scale suite runs.

---

## 1. Assertion execution cost

Execution paths differ sharply between frameworks. go-specs uses a direct comparison path with no matcher allocation or reflection.

```mermaid
flowchart LR
    A[Test Assertion] --> B[go-specs]
    B --> C[Direct Comparison]
    C --> D[Return]

    A --> E[Testify]
    E --> F[Helper + Assertion]
    F --> G[Comparison]
    G --> H[Return]

    A --> I[Gomega]
    I --> J[Create Matcher]
    J --> K[Reflection]
    K --> L[Compare]
    L --> M[Return]
```

- **go-specs** — One direct comparison (e.g. `actual == expected` for comparable types). No matcher object, no reflection. The fast path is a few instructions.
- **Testify** — Helper and assertion machinery add overhead before comparison; no reflection on the equality path but more call depth and branching.
- **Gomega** — Allocates matcher objects and uses reflection for comparison, which is significantly more expensive.

go-specs avoids matcher allocation and reflection entirely on the assertion hot path, which is why a single equality assertion is ~1 ns (EqualTo) or ~7 ns (Expect/ToEqual) versus ~86 ns (Testify) or ~238 ns (Gomega).

---

## 2. Runner execution cost

The runner executes steps with a minimal loop: no branching on step type, no maps, no per-step allocation.

```mermaid
flowchart TD
    Start --> Step1[Step 1]
    Step1 --> Step2[Step 2]
    Step2 --> Step3[Step 3]
    Step3 --> End
```

Execution is a simple sequential loop:

```go
for i := 0; i < len(steps); i++ {
    steps[i](ctx)
}
```

This gives:

- **Predictable branch patterns** — The loop has one branch (loop condition). No type switches or map lookups in the hot path.
- **CPU cache friendliness** — Steps are stored in a contiguous slice; the runner reads function pointers in order, which prefetchers handle well.
- **Minimal dispatch cost** — Each step is a direct function call. No indirection, no reflection, no interface method lookup in the inner loop.

---

## 3. Allocation comparison

Allocation behavior during test execution differs greatly between frameworks.

```mermaid
flowchart LR
    GoSpecs[go-specs] --> ZeroAlloc[0 allocations]
    Testify[Testify] --> SomeAlloc[helper calls]
    Gomega[Gomega] --> ManyAlloc[matcher allocations]
```

go-specs performs **no allocations during test execution** on the success path of its typed equality assertions — `EqualTo(ctx, x, y)` and `ExpectT(ctx, x).ToEqual(y)` — and that holds for a value of any type or size, not only for small integers. The Context is pooled and the runner reuses one per spec (or per group); expectations are not pooled — they never escape the assertion that consumes them, so escape analysis keeps them on the stack. Those assertions use generics and direct comparison, and hold the value at its own type, so nothing is converted to an interface.

The two forms that do take an `any` pay for the conversion. `ExpectT(x).To(matcher)` costs one allocation and `ctx.Expect(x).ToEqual(y)` costs two, for any value Go cannot convert for free — every string, every struct, and every integer outside the runtime's static small-integer table. The matcher cost belongs to the `Matcher` interface being `Match(any)`: a typed value cannot reach a matcher without one conversion, and only a generic `Matcher[T]` would remove it. Exact per-shape counts are published in [../README.md](../README.md) and pinned by `TestAssertionAllocationsByValueShape`.

Testify’s assertion path avoids reflection but still involves helper and formatting code paths that can allocate in failure cases; the success path is low-allocation. Gomega allocates matcher objects and supporting structures per expectation, leading to multiple allocations per assertion.

---

## 4. Suite scaling

Runtime scales linearly with the number of specs because execution is a flat loop over a precompiled step list.

```mermaid
graph LR
    Suite100[Suite 100] --> Suite1000[Suite 1000]
    Suite1000[Suite 1000] --> Suite10000[Suite 10000]
    Suite10000[Suite 10000] --> Suite50000[Suite 50000]
```

Execution has **O(n) complexity**: double the specs, double the steps, and roughly double the run time. There is no extra per-spec cost from hook resolution, map lookups, or allocation. Large suites (tens of thousands of specs) run in microseconds because the inner loop is just stepping through a slice and calling functions.

---

## 5. Memory access pattern

The execution plan is a flat slice of step functions. The runner reads them sequentially.

```mermaid
flowchart LR
    Program[Program] --> StepArray[Step Array]
    StepArray --> SequentialAccess[Sequential Access]
    SequentialAccess --> RunnerLoop[Runner Loop]
```

The runner reads step functions **sequentially from memory**. No random access, no pointer chasing through trees or maps. Sequential access is cache-friendly: the CPU can prefetch the next steps while executing the current one. This keeps the inner loop small and predictable.

---

## 6. Hook execution path

Nested before/after hooks are compiled into the execution plan. At run time there is no tree traversal—just a linear sequence of steps.

```mermaid
flowchart TD
    BeforeOuter[BeforeOuter] --> BeforeInner[BeforeInner]
    BeforeInner --> Test[Test]
    Test --> AfterInner[AfterInner]
    AfterInner --> AfterOuter[AfterOuter]
```

Hooks are **compiled into the execution plan** and do not require runtime traversal. The builder flattens scope and emits one sequence per spec (e.g. outer before → inner before → spec body → inner after → outer after). The runner executes that sequence as Step 1, Step 2, … with no lookup or recursion. Hook order is fixed at compile time.

---

## 7. Execution pipeline

Heavy work happens during compilation; execution is a thin loop over the compiled plan.

```mermaid
flowchart LR
    TestFile[Test File] --> DSL[DSL]
    DSL --> Builder[Builder]
    Builder --> Program[Program]
    Program --> Runner[Runner]
    Runner --> Results[Results]
```

Parsing, hook collection, and plan construction happen in the **Builder** when the suite is created. By the time the **Runner** runs, the plan is a flat list of function pointers. The runner does not parse DSL, resolve hooks, or allocate per spec—it just iterates and calls. So execution cost is dominated by the user’s test code and assertions, not by framework machinery.

---

## 8. Performance summary

The main design choices that make go-specs extremely fast:

| Choice | Effect |
| ------ | ------ |
| **Compiled execution plan** | Suites are compiled once into a flat step list. No hook resolution or tree traversal at run time. |
| **Zero allocations** | The Context is pooled; expectations are stack-allocated. The assertion and runner success path allocate nothing. |
| **No reflection** | Assertions use generics and direct comparison. No `reflect.DeepEqual` or type switches in the hot path. |
| **Direct function dispatch** | Each step is invoked as `step(ctx)`. No indirection or dynamic dispatch in the inner loop. |
| **Sequential memory access** | The plan is a contiguous slice; the runner walks it in order, which is cache-friendly. |

Together, these allow go-specs to execute a single equality assertion in **~1 ns** and full suites of **tens of thousands of specs in microseconds**, with no allocations on the success path and linear scaling with suite size.

## Further reading

- [BENCHMARKS.md](BENCHMARKS.md) — Benchmark numbers, methodology, and scaling results.
- [ARCHITECTURE.md](ARCHITECTURE.md) — How the DSL compiles to a program and how the runner executes it.
- [EXECUTION_MODEL.md](EXECUTION_MODEL.md) — Execution plan structure and runner loop in more detail.
