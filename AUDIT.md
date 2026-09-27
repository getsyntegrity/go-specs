# Performance and Correctness Audit

> **Historical snapshot.** This audit reflects the codebase as of commit `b1d4292` (2026-03-09).
> Some APIs and benchmarks it describes — including the path-generation/exploration subsystem
> (`Paths`, `PathValues`, `PathGenerator`, and the path benchmark discussed below) — were later
> modified or removed; see [CHANGELOG.md](CHANGELOG.md) for what changed and when. This document
> is not a description of the current HEAD. For the current API surface and benchmark set, see
> the changelog and the up-to-date docs (`README.md`, `BENCHMARKS.md`, `docs/`).

**Scope:** `specs/`, `mock/`, `benchmarks/`  
**Focus:** Hot paths, allocations, reflection, mutex contention, races, determinism, benchmark integrity.

---

## PERFORMANCE FINDINGS

### 1. Reflection in assertion hot path (fallback only)

**Location:** `context.go` (ToEqual), `matcher.go` (Equal.Match, Contain.Match, BeNil.Match), `native_matchers.go` (valuesEqual)

**Issue:**  
- `ToEqual` uses a type switch for int, string, bool, int64 then falls back to `reflect.DeepEqual`. Other common types (float64, uint, time.Time) take the reflection path.  
- `Equal` matcher has the same fast-path set (plus float64); `NotEqual` and `Contain` always go through `valuesEqual` or reflection.  
- `BeNil()` uses `reflect.ValueOf(actual)` and `v.Kind()` on every call.  
- `Contain` for slices uses `reflect.ValueOf(actual)` and `rv.Index(i).Interface()` in a loop (reflection + interface boxing per element).

**Impact:**  
- Hot path (e.g. `Expect(42).ToEqual(42)`) already uses fast path and shows ~0 allocs in benchmarks.  
- Fallback and matcher paths pay reflection cost for non-primitive or slice containment.

**Suggested fix:**  
- Add fast paths for float64, uint, and optionally time.Time in `ToEqual` and in `fastEqualComparable` / Equal matcher where missing.  
- For `BeNil`, add a fast path: `if actual == nil { return true }` then handle common pointer/slice types by type assertion before using reflect.  
- For `Contain` on slices, consider a type switch on common slice types (e.g. []int, []string) and direct iteration to avoid reflection and per-element interface boxing.

---

### 2. Expectation allocation per assertion

**Location:** `context.go` — `Expect(actual any) *Expectation`

**Issue:**  
Every `ctx.Expect(actual)` allocates a new `Expectation` struct. So `Expect(x).ToEqual(y)` or `Expect(x).To(m)` does one allocation per call.

**Impact:**  
- Assertion benchmark uses `ToEqual` only; if the compiler inlines and the expectation does not escape, allocs may still be reported low.  
- Any test that uses many `Expect(...).To(...)` or `Expect(...).ToEqual(...)` calls will allocate proportionally.

**Suggested fix:**  
- Accept as inherent to the current API unless profiling shows this as a bottleneck.  
- A more invasive option would be a pooled or stack-friendly expectation builder (e.g. method on Context that reuses a single Expectation), at the cost of API simplicity.

---

### 3. Mock Spy: mutex held during matcher work

**Location:** `mock/spy.go` — `CalledWith(matchers ...ArgMatcher)`

**Issue:**  
The method holds `s.mu.Lock()` for the full loop over `s.calls` and calls `m.Match(call.Args[i])` under the lock. `mock.Equal` uses `reflect.DeepEqual`, so non-trivial comparison runs while holding the lock.

**Impact:**  
- Contention if multiple goroutines call `CalledWith` or mix `Call` with `CalledWith`.  
- Lock hold time scales with number of calls and cost of each matcher.

**Suggested fix:**  
- Copy the minimal state under the lock (e.g. copy `s.calls` slice and each `Call.Args`), then unlock.  
- Run the matcher loop on the copied data without holding the lock.  
- Same pattern can be applied to `Calls()`: copy under lock (or shallow-copy the slice), unlock, then build the returned slice if a deep copy is required.

---

### 4. Mock Spy: full copy under lock in Calls()

**Location:** `mock/spy.go` — `Calls() []Call`

**Issue:**  
The method holds the lock for the entire duration of building the returned slice, including `append([]any(nil), c.Args...)` for every call.

**Impact:**  
- Lock is held during multiple allocations and copies; hold time grows with number of calls and argument count.

**Suggested fix:**  
- Under lock: allocate `tmp := make([]Call, len(s.calls))` and copy structs (shallow). Optionally copy each `Args` slice into `tmp[i].Args` so we don’t expose internal slices. Then unlock.  
- If the API allows, build the final result from `tmp` after unlock to shorten critical section.

---

### 5. Runner: path slice and PathValues clone per path

**Location:** `runner.go` — path execution block

**Issue:**  
- `var paths []PathValues` and `paths = append(paths, pv.clone())` allocate a new slice and one `PathValues.clone()` per path.  
- `PathValues.clone()` allocates new `values` and `present` slices (index is shared).

**Impact:**  
- Necessary for correctness (each iteration must have its own PathValues).  
- For many paths, this is a meaningful number of allocations.

**Suggested fix:**  
- Keep current behavior for correctness.  
- If profiling shows path execution as a hotspot, consider a pool of PathValues or reusing a single PathValues with `assignTo` per iteration (if no code retains references to path values after the It callback returns).

---

### 6. ancestorsFromRoot allocation per node

**Location:** `runner.go` — `ancestorsFromRoot(node *Node)`

**Issue:**  
Builds a slice from node to root, then reverses it. Allocates a new slice and does a reverse in-place for every `runNode` call (including every Describe/When/It).

**Impact:**  
- One allocation per node execution. For deep trees this adds up; for typical suites impact is modest.

**Suggested fix:**  
- Low priority. If needed, a scratch buffer could be passed through the runner and reused (e.g. `ancestorsFromRoot(node, scratch []*Node) []*Node`), with care to avoid retaining the slice across concurrent or nested runs.

---

### 7. Snapshot: semantic comparison via unmarshal + DeepEqual

**Location:** `snapshot.go` — `runSnapshot`

**Issue:**  
New value is marshaled to JSON. Existing snapshot is unmarshaled to `any`; new value is also unmarshaled to `any`; then `reflect.DeepEqual(existingVal, newVal)` is used. So we do marshal → unmarshal (new) → unmarshal (existing) → DeepEqual.

**Impact:**  
- Snapshot is not in the per-spec hot path; it runs once per `Snapshot()` call.  
- Semantic comparison is correct (handles key order, number types, etc.).  
- For very large snapshots, DeepEqual could be costly; raw byte comparison would be faster but would require normalized JSON (e.g. sorted keys) and would be more brittle.

**Suggested fix:**  
- Keep current approach for correctness and simplicity.  
- If large snapshots become a problem, consider optional byte comparison after normalizing both sides (e.g. unmarshal to generic structure, marshal with sorted keys, compare bytes).

---

## CORRECTNESS RISKS

### 1. Race conditions

**Assessment:**  
- **Registry / Analyze:** `analyzeLock` and per-registry `mu` protect the shared registry stack and tree construction. Execution runs after tree build; runner does not mutate the tree. No concurrent execution of the same suite by design.  
- **Context pool:** `contextPool.Get()` / `Reset` / `Put` are used from a single goroutine per spec; no sharing of a Context across goroutines during a run.  
- **Mock Spy:** All methods that read/write `s.calls` are protected by `s.mu`. Concurrent `Call` and `CalledWith` are safe.  
- **Snapshot:** Writes happen only when `GO_SPECS_UPDATE_SNAPSHOTS=1` and from test code; typically one process, sequential tests. No cross-test sharing of snapshot files in the same package.  
- **Conclusion:** No definite race conditions identified in the audited code. Mock’s lock is held during matcher execution; reducing hold time (see Performance #3–4) improves scalability, not correctness.

---

### 2. Nondeterminism

**Assessment:**  
- **Execution order:** Runner iterates `node.Children` (slices); path execution iterates a slice of PathValues built from `ForEach` in a fixed order. No map iteration in execution order.  
- **Path generation:** PathGenerator uses slices (dims, vars); enumeration order is deterministic. Sampling/exploration use a seeded RNG.  
- **Snapshot save:** `saveSnapshots` builds a sorted key list from the map then iterates keys; file output is deterministic.  
- **Conclusion:** Execution and path order are deterministic; snapshot output is deterministic.

---

### 3. Unsafe or brittle patterns

**Assessment:**  
- **PathValues.index:** `assignTo` and `clone` share `dst.index = pv.index`. The index map is effectively read-only after construction; sharing is intentional and safe.  
- **Context.Reset(nil):** Used before `Put` to clear references; prevents retaining backend/tree references in pooled contexts.  
- No other unsafe or obviously brittle patterns identified in the audited packages.

---

## BENCHMARK INTEGRITY

### 1. Assertion benchmark

**Location:** `benchmarks/` (assertion_bench_test.go, minimal_and_buildsuite_bench_test.go)

**Behavior:**  
- `ctx := specs.NewContext(b)` once; then `ctx.Expect(42).ToEqual(42)` inside the loop.  
- Measures the assertion path (Expect + ToEqual) with a single allocation per iteration for `Expect` (if not optimized away).  
- ToEqual itself uses the int fast path; no reflection in the loop.

**Verdict:** Isolates assertion cost appropriately. Reported ~0 allocs are consistent with the fast path; any alloc from `Expect` is acceptable and documented.

---

### 2. Path benchmark

**Location:** `benchmarks/` (assertion_bench_test.go, minimal_and_buildsuite_bench_test.go)

**Behavior:**  
- For each `b.N`, runs a full suite: `Describe` + `Paths` + `Explore(10).It(...)` (build tree, run 10 path iterations).  
- So the loop includes tree building, path generation, and runner execution.

**Verdict:** Measures “full path spec” cost, not just the path engine in isolation. This is a valid choice for an integration-style benchmark. If the goal is to measure only path generation or only runner overhead, a separate benchmark that isolates that component would be needed.

---

## LOW PRIORITY IMPROVEMENTS

1. **ToEqual fast path:** Add float64 (and optionally uint, time.Time) to the type switch in `context.go` ToEqual to reduce reflection on those types.  
2. **Equal matcher:** Already has float64; consider aligning with `fastEqualComparable` in native_matchers for other numeric types if matcher usage grows.  
3. **pathValuesToStrings:** Currently returns nil/empty; reporter path strings are not populated. Low impact; can be improved when reporter needs real path strings.  
4. **Contain matcher:** Add fast path for `[]int`, `[]string`, etc., with direct iteration and no reflection to reduce cost and allocations when comparing to scalar expected values.

---

## SUMMARY TABLE

| Area                 | Severity   | Finding                                      | Status / recommendation        |
|----------------------|-----------|-----------------------------------------------|--------------------------------|
| Reflection in ToEqual| Low       | Fallback uses DeepEqual; fast path covers int/string/bool/int64 | Add float64/uint if needed     |
| Expectation alloc    | Low       | One struct per Expect() call                  | Accept or add pooled API       |
| Mock CalledWith lock | Medium    | Lock held during DeepEqual in matchers        | Copy calls, then unlock, then match |
| Mock Calls() lock    | Low       | Lock held during full copy                    | Shallow copy then unlock        |
| PathValues clone     | By design | One clone per path iteration                  | Keep for correctness            |
| ancestorsFromRoot    | Low       | One slice alloc per node                      | Optional scratch buffer         |
| Snapshot comparison  | Low       | Unmarshal + DeepEqual                         | Keep for correctness            |
| Races                | None      | Mutexes and single-goroutine execution        | No change                       |
| Nondeterminism       | None      | Slices and sorted keys                        | No change                       |
| Benchmark assertion  | OK        | Isolates Expect+ToEqual                       | No change                       |
| Benchmark path       | OK        | Full path spec in loop                        | Document as integration-style   |

---

*Audit focuses on real performance impact and correctness; avoids stylistic or micro-optimization suggestions that do not affect behavior or measurable performance.*
