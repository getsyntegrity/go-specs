# BENCHMARKS.md

This document explains how benchmarks are structured in go-specs.

---

# Benchmark Philosophy

Benchmarks measure **subsystems independently**.

The framework has two primary cost centers:

1. Assertions
2. Runner execution

---

# Benchmark Suites

All benchmarks live in a single directory:

```
benchmarks/
```

Files:

| File                             | Benchmark                          |
| -------------------------------- | ---------------------------------- |
| assertion_bench_test.go          | single assertion cost (go-specs, Testify, Gomega) |
| matcher_bench_test.go            | matcher / Expect().To() performance |
| comparison_bench_test.go         | head-to-head go-specs vs Testify vs Gomega across 8 assertion scenarios (conclusions in [benchmarks/COMPARISON.md](benchmarks/COMPARISON.md)) |
| runner_bench_test.go            | N specs, one assertion per spec   |
| hooks_bench_test.go             | before-each + assertion per spec  |
| large_suite_bench_test.go       | scaling (100, 1000, 10000, 50000)  |
| minimal_and_buildsuite_bench_test.go | BuildSuite runner, MinimalRunner, parallel, nested hooks |
| regression_guard_test.go        | in-process baseline ratio guard against wall-clock regressions (opt-in, see below) |

See [benchmarks/README.md](benchmarks/README.md) for categories and scripts.

---

# Running Benchmarks

```
go test ./benchmarks -run='^$' -bench=. -benchmem
```

For real numbers, use `make bench-report` (10 iterations) or read the charts that
`benchmarks.yml` publishes from `main`.

---

# Benchmarks in PR CI

`go test ./...` never runs a `Benchmark` function. Benchmark bodies therefore compile
in CI and are never executed, which once let matcher code paths be exercised by
benchmarks and by nothing else — a panic on such a path would have shipped green.

`make bench-smoke` closes that gap and runs on every PR:

```
go test -run='^$' -bench=. -benchtime=1x -benchmem ./...
```

`-benchtime=1x` runs each benchmark for exactly one iteration. That is enough to
**execute** the path and far too little to **measure** it, which is the whole design:
a shared CI runner is throttled and noisy, so this step asserts no wall-clock
threshold and cannot fail on a slow machine. It costs roughly three seconds.

Correctness tests remain the primary contract. Benchmark execution is supplementary
coverage — it proves the code runs, not that it runs fast.

---

# Wall-clock regression guard (ratio-based, opt-in)

[`benchmarks/regression_guard_test.go`](benchmarks/regression_guard_test.go) closes the gap
[#235](https://github.com/getsyntegrity/go-specs/issues/235) fell through: 134 commits regressed
`BenchmarkRunner_GoSpecs` from ~4.0 µs to ~26.8 µs (individual commits jumping as much as ~2.1x) and
nothing failed, because this project rightly refuses an absolute ns/op threshold on shared CI
runners -- see "Not measured in shared CI at all" below and "Contractual vs observational" above.

The guard is still not an absolute threshold. It is a **ratio against an in-process baseline**,
measured in the same process, same `go test` invocation, back to back: a hand-written loop calling
the same 1000 funcs (one passing comparison each, no framework) that the Runner and Describe paths
call. Dividing the framework's ns/op by the baseline's ns/op cancels out CPU speed, which is most of
what makes an absolute ns/op threshold unsafe on a shared runner. `TestBenchmarkRatioGuard` runs two
checks this way:

| Check | Subject | Bound | Measured on GitHub Actions (10 `workflow_dispatch` runs, ubuntu-latest, go1.26.6) |
| ----- | ------- | ----- | ---------------------------------------------------------------------------------- |
| Runner | `CreateGoSpecsSuite(1000)` (pre-built `Program`, `specs.NewRunner(prog).Run`) | 25x | 14.02x – 18.41x (mean ~15.7x) |
| Describe | `specs.Describe(b, "suite", body)` (declares + runs 1000 specs every iteration) | 150x | 79.40x – 115.29x (mean ~96.7x) |

**The bounds are calibrated against the runner that actually executes this guard (GitHub Actions
ubuntu-latest), not a developer's laptop.** That distinction turned out to matter: a first pass at
these bounds was measured on a linux/amd64 laptop and looked comfortably generous there (Runner
15.73x-17.10x, Describe 210.60x-257.00x), which is a similar band for Runner but **roughly half**
for Describe once measured on the actual CI runner class. A same-process ratio cancels out CPU
*speed*, but not every other difference between a desktop and a shared, virtualized runner --
scheduling, allocator behavior and cache effects still land differently, and Describe (which pays
suite declaration every iteration, not just execution) is far more exposed to that than Runner is.
Bounds picked from laptop numbers would have shipped with roughly 2x more headroom on Describe than
intended, which is exactly wide enough to *not* catch a #235-class 2x regression.

Describe's CI-measured ratio is also noisier than Runner's (~45% spread across the 10 runs vs.
Runner's ~31%, driven mostly by two runs that trended high), which puts real tension between "safe
headroom above observed noise" and "reliably below a 2x regression from the observed floor":

- **Runner (25x):** ~36% headroom over the observed ceiling (18.41x), and still below 2x the
  observed floor (14.02x → 28.04x, ~12% margin) -- so a #235-class jump is caught starting from
  anywhere in the 10 runs measured, and a smaller regression (as little as ~1.6x) is caught when it
  starts from a typical, non-outlier run.
- **Describe (150x):** only ~30% headroom over the observed ceiling (115.29x), and only ~6% margin
  below 2x the observed floor (79.40x → 158.80x). This is a deliberate compromise, not a fully
  generous bound: with just 10 CI samples behind it, an unobserved noise spike past 115.29x could
  false-positive, and a regression smaller than ~1.9x starting from the single noisiest-fast run in
  the sample could go uncaught. Starting from a typical (median ~93.8x) run, sensitivity is much
  better, catching a regression as small as ~1.6x, same as Runner.

In short: **Runner reliably catches a #235-class 2x regression across its whole observed range.
Describe reliably catches one from a typical run, but is only marginally tuned to catch one from
the noisiest run seen so far** -- stated plainly rather than claimed away, per the measurements in
`regression_guard_test.go`'s constants.

A consequence of the CI-vs-laptop gap above: **running `make bench-ratio-guard` on a machine other
than CI can show a meaningfully different ratio, especially for Describe, and a local FAIL is not
on its own evidence of a regression.** Treat `benchmarks.yml`'s `ratio-guard` job as authoritative;
use `GOSPECS_BENCH_GUARD_BOUND_RUNNER` / `GOSPECS_BENCH_GUARD_BOUND_DESCRIBE` locally only for a
same-machine before/after comparison, not to reproduce CI's verdict.

The guard is opt-in (`GOSPECS_BENCH_GUARD=1`, or `make bench-ratio-guard`) and deliberately kept out
of `ci.yml`/`make bench-smoke`, so it never sits on the PR critical path. It runs from its own
`ratio-guard` job in `benchmarks.yml`, on push to **both** `develop` and `main`, and on
`workflow_dispatch`. `develop` is the branch every feature, fix and refactor actually lands on
(see `CONTRIBUTING.md`'s branching model) -- `main` only moves on a release/hotfix PR -- so a
main-only trigger would leave the guard checking a branch that barely moves while regressions like
#235 accrue on `develop` commit by commit. The `ratio-guard` job is separate from the `bench` job
above (chart generation and its commit-back step), which keeps its original main-only cadence
regardless of trigger: it is expensive and mutates the repo, and there is no reason to run it on
every `develop` push, or on a manual dispatch against some other branch, just because the guard
does. Override a bound locally or in CI with `GOSPECS_BENCH_GUARD_BOUND_RUNNER` /
`GOSPECS_BENCH_GUARD_BOUND_DESCRIBE` if a deliberate, reviewed change moves the baseline -- ideally
re-measured on GitHub Actions the same way (`gh workflow run benchmarks.yml --ref <branch>`,
repeated a handful of times, reading the `ratio-guard` job's log), not on a laptop.

---

# Contractual vs observational claims

Not every number this project publishes is a promise. The distinction matters, because
a *contract* is something a PR may fail on and a maintainer must fix, while an
*observation* is a measurement that is allowed to move.

## Contractual — enforced by `go test ./...`

Pinned in [`specs/allocation_contract_test.go`](specs/allocation_contract_test.go) with
`testing.AllocsPerRun`. Allocation counts are a property of the generated code, not of
the machine, so they are stable across runners and safe to gate a PR on.

| Guarantee | Pinned by |
| --------- | --------- |
| The **published per-form, per-value-shape allocation table**: `EqualTo` and `ExpectT(...).ToEqual` cost nothing for any comparable `T` at any width; `ExpectT(...).To(matcher)` and `ctx.Expect(...)` cost exactly the interface conversions their signatures force | `TestAssertionAllocationsByValueShape` (`specs/assertion_allocations_test.go`) |
| Valueless matchers (`BeTrue`, `BeFalse`, `BeNil`) allocate nothing | `TestValuelessMatchersAllocateNothing` |
| The recommended error idiom, `errors.Is(...)` fed into `ExpectT(...).To(BeTrue())`, allocates nothing | `TestErrorClassificationAllocatesNothing` |
| The runner loop has **no per-spec allocation**: 5000 specs cost no more allocations than 100 | `TestMinimalRunnerLoopAllocatesNothingPerSpec`, `TestBlockRunnerLoopAllocatesNothingPerSpec`, `TestProgramRunnerLoopAllocatesNothingPerSpecOnTheFlatPath` |

`TestAssertionAllocationsByValueShape` is the authority on assertions, and it pins **exact
counts in both directions** -- so a cost that silently grows *or* disappears fails the
build. It builds its probe values in `init()` rather than writing them as literals, which
matters more than it looks: a string or struct written inline in a test function is folded
by the compiler before the assertion sees it, so a naive test measures the optimiser and
reports a zero the real code path does not deliver. Note in particular that
`ctx.Expect(str).ToEqual(str)` costs **two** allocations for a real string, one per operand
-- it is *not* allocation-free, and only the small-integer case looks like it is.

## Observational — measured, never asserted

These are true today and worth knowing. None of them fails a build.

- **Every ns/op figure**, here and in the README tables. Wall-clock depends on the CPU,
  the runner and what else is on the box. No shared-CI job asserts a timing threshold.
- **Comparisons against Testify and Gomega.** They pin the shape of the difference, not
  a ratio; see [benchmarks/COMPARISON.md](benchmarks/COMPARISON.md).
- **Constructing a value-capturing matcher costs one allocation.** `Equal(x)`,
  `NotEqual(x)` and `Contain(x)` allocate once for the matcher value itself. Build the
  matcher outside a hot loop if it is constant, as the benchmarks and the allocation table
  do, so the cost is attributed to the matcher rather than to the assertion.
- **`ExpectT(...).To(matcher)` costs one further allocation for most values.** `Matcher` is
  `Match(any)`, so the value becomes an interface before a matcher can see it, and for a
  value Go does not convert for free that conversion allocates once. Nothing in `ExpectT`
  can remove it — only a generic `Matcher[T]` would — so it is measured by
  `TestAssertionAllocationsByValueShape` rather than pinned. `ToEqual` is the
  allocation-free route for equality, at any width.
- **A run of a compiled runner costs a small, fixed number of allocations** for pooled
  setup (0 on the current toolchain for both the Builder `Program` runner and the `Describe`
  engine). The contract is that this number does not grow with spec count, not that it is
  zero; both engines are pinned by `specs/allocation_contract_test.go`.
- **End-to-end wall clock on the `*testing.T` path.** `make bench-e2e` runs the same 1000
  specs through go-specs, Testify, Gomega and a bare `t.Run` loop, every spec a subtest. It
  is the number an ordinary `go test` run pays, and the one the README leads with; the
  `*testing.B` benchmarks exclude the subtest and so measure framework overhead only.
- **The `*testing.T` path allocates per spec.** When the runner is handed a real
  `*testing.T` it opens a `t.Run` subtest per spec, which costs tens of allocations each
  inside the standard library. That is the deliberate price of per-spec test identity in
  `go test -v` output; the allocation-free claim covers the flat path only.

## Not measured in shared CI at all

Cross-run absolute-threshold regression detection. [`tools/perfcheck`](tools/perfcheck) can compare
two separate benchmark runs (e.g. current vs a saved baseline) against an ns/op threshold, but that
comparison belongs on a dedicated, quiet machine — running it on a shared GitHub runner would
produce flakes, not signal. It is not wired into any workflow.

Same-run ratio-based regression detection *is* measured in shared CI — see "Wall-clock regression
guard" above. The distinction is what makes it safe: `perfcheck` compares two different runs (so
absolute machine speed has to be assumed constant), while the ratio guard compares two workloads
inside the *same* run, which cancels machine speed out instead of assuming it away.

---

# Expected Performance

Observational, not contractual — see above. Typical figures:

| Operation        | ns/op     |
| ---------------- | --------- |
| Assertion        | 80–150    |
| Runner           | 10–40 µs  |

---

# Important Rule

Do not benchmark:

```
suite construction
DSL parsing
registry initialization
```

inside `b.N` loops.

These must happen before `b.ResetTimer()`.
