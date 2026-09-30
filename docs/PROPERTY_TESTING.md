# Property testing: engine decision and public API

Status: accepted (issue #361). This record was written before any engine code, as the issue asks.

## The problem

go-specs has no property-testing API. A property is an invariant that must hold for every input a
generator can produce ("reversing a list twice gives the list back"). A useful engine does four things
the plain `It` blocks do not: it generates many inputs, it shrinks a failing input to a small one a human
can read, it replays a failure exactly, and it tells a rejected input (the generator produced
something the property does not apply to) apart from a failed assertion and from a panic.

The fuzz targets in `assert/fuzz_diagnostics_test.go` are not this API. They decode a byte recipe into a
value graph for one purpose, testing the renderers, and their generator is private test code. Nothing
about them is a stable public surface, so this design does not build on them.

## Options compared

### A. Native Go fuzzing (`testing.F`) only

What it gives: no new dependency; coverage-guided search, which finds inputs a random generator never
reaches; a corpus in `testdata/fuzz` that plain `go test` replays.

Why it is not enough on its own, from how `testing.F` works:

- The engine input is a `[]byte` (or a fixed list of primitives). Structured values must be decoded from
  bytes by hand, and minimisation works on the bytes, not on the value, so a failing `[]User` is not
  reduced to a smaller `[]User` in any way the property author controls.
- It cannot be driven from inside a test. A `*testing.F` exists only inside a `FuzzXxx` function, and
  the search only runs under `go test -fuzz`. A normal `go test` runs the seeds once. So a test cannot
  say "run this property, expect it to fail, and check the counterexample", which is exactly the
  acceptance demonstration. This repository already works around it by re-running the test binary as a
  child process (`assert/fuzz_subprocess_test.go`).
- There is no seed to set. A run is not reproducible from a number, only from the corpus file it wrote.
- Rejection is not a first-class outcome, and a fuzz run has no generation limits beyond `-fuzztime`.

### B. A maintained property-testing library: `pgregory.net/rapid`

Evidence gathered (module `pgregory.net/rapid` v1.2.0, read from the module cache, and run locally):

- Typed generators (`rapid.Int()`, `rapid.SliceOf(g)`, `rapid.Custom`, `rapid.Make[T]()`), composable.
- Shrinking of the recorded random bits, so every generator shrinks for free, including user-defined ones
  (`shrink.go`). No per-type shrinker has to be written.
- Deterministic replay: `-rapid.seed=N` reproduces a run, and a failure writes a fail file under
  `testdata/rapid/<Test>/` that a later plain `go test` replays first (`engine.go`, `persist.go`). That
  is a committed, reviewable regression corpus.
- Rejection is explicit: `t.Skip` and `Filter` mark an input invalid, and the engine fails the test when
  too few valid inputs were generated (`invalidChecksMult`).
- Limits are flags: `-rapid.checks`, `-rapid.steps`, `-rapid.shrinktime`.
- It runs against any `rapid.TB`, not only `*testing.T`, so a failing property can be observed from a
  test without failing that test.
- `rapid.MakeFuzz` exposes a property as a native `func(*testing.T, []byte)`, so the same property can
  also run under `go test -fuzz` for coverage-guided campaigns. Option B therefore includes option A's
  strength.
- The module has no dependencies (`go.mod` lists none). License MPL-2.0: a file-level copyleft that
  applies only to modified rapid files, which we do not modify.

Costs, stated plainly:

- A new third-party dependency, which RULES.md and ARCHITECTURE.md keep minimal.
- Configuration is global flags, not an options struct, and results are not returned: the engine
  reports by calling `TB.Errorf` with text that contains the seed and fail file path.
- Its panics for stopping a run (`stopTest`, `invalidData`) are unexported types, so an adapter that
  recovers panics has to tell them from a user's panic by package path.

### Decision

Use rapid, behind a small go-specs adapter in a separate nested Go module, and expose native fuzzing
through rapid's own bridge rather than writing a generator and shrinker ourselves.

Why, against the repository's rules:

- Determinism: a seed, or a fail file, reproduces a failure exactly. Native fuzzing offers neither
  inside a test.
- Low allocations and dependency policy: the core module `github.com/getsyntegrity/go-specs` and its
  `go.mod` do not change. Code that does not use property testing pays nothing, not in build time, not
  in `go.sum`, not in binary size. The hot assertion path is untouched; the adapter calls the existing
  exported `assert.Evaluate`.
- Engine cost: writing our own generators, shrinker and replay format is several thousand lines of
  subtle code for a result worse than an existing, maintained engine. The issue asks for a maintained
  dependency to be compared, and this one wins on every requirement that native fuzzing cannot meet.

Rejected alternative: native `testing.F` only. It stays available, through `rapid.MakeFuzz`, as the
campaign mode, but it cannot be the primary API for the reasons above.

## Where it lives

A nested module at `property/`, import path `github.com/getsyntegrity/go-specs/property`, with its own
`go.mod` that requires rapid. `go build ./...` and `go test ./...` at the repository root do not
descend into a nested module, so CI and the Makefile run it explicitly (see the CI section of this
file once implemented). Its `go` directive follows the same `.go-version` rule as every `go.mod`.

Until a core version that contains these changes is tagged, the nested module uses a `replace`
directive to the parent directory. A release has to drop it, and the nested module gets its own tag
(`property/vX.Y.Z`). That is a release-process decision and is recorded as an open question on the PR.

## Public API proposal

```go
package property

// Check runs prop and fails tb with a full report when the property is falsified.
func Check(tb testing.TB, prop func(*T), opts ...Option)

// Run runs prop and returns the outcome instead of failing a test. Check is Run plus reporting;
// Run exists so a program can inspect, replay and assert on a failure.
func Run(prop func(*T), opts ...Option) Result

// T is the per-run handle passed to the property. Its state is created fresh for every generated
// input, so a failure in one run never marks the next run, or the surrounding test, failed.
type T struct{ /* unexported */ }

func (p *T) Expect(actual any) Expectation // Expectation.To(assert.Matcher), ToEqual(expected)
func (p *T) Reject(format string, args ...any) // input does not apply: counts as rejected
func (p *T) Assume(ok bool)                    // Reject when !ok
func (p *T) Logf(format string, args ...any)

// Draw takes a value from a rapid generator and records it for the counterexample.
func Draw[V any](p *T, g *rapid.Generator[V], label string) V

type Option func(*config)
func WithChecks(n int) Option             // valid inputs to generate (default 100)
func WithSeed(seed uint64) Option         // deterministic replay of one generation run
func WithSteps(n int) Option              // average Repeat steps
func WithShrinkTime(d time.Duration) Option
func WithName(name string) Option         // names the property, and its fail-file directory
func WithoutFailFile() Option             // do not persist a corpus file

type Outcome int // Passed, Failed, Panicked, Exhausted

type Result struct {
    Outcome        Outcome
    Checks         int      // valid inputs run before the first failure
    Rejected       int      // inputs the property rejected before the first failure
    Seed           uint64   // pass to WithSeed to reproduce the failure
    FailFile       string   // corpus file written, when any
    Original       []Draw   // the first failing input found
    Counterexample []Draw   // the input after shrinking
    Failures       []string // assertion failure messages of the counterexample run
    Panic          *PanicInfo
    Report         string   // human-readable summary
}
```

Generators stay rapid's: the adapter does not wrap them, so there is one generator vocabulary and no
second API to keep in step. Importing `pgregory.net/rapid` for generators is the accepted coupling.

Failure semantics: an assertion inside a property records its message on the run's own state and ends
that run, exactly like a fatal assertion in a spec. Three outcomes are kept apart on purpose: a
rejected input is not a failure and is counted; an assertion failure carries the matcher message; a
panic carries the value and stack, and is never reported as an assertion failure.

## Running it in CI

Two modes, documented with the implementation:

- Every pull request: `go test ./...` in the `property` module replays the seeds and any committed
  fail files with the default number of checks. It is deterministic when a seed is set and bounded in
  time.
- Campaign: `go test -fuzz` over the `rapid.MakeFuzz` bridge on a schedule or on demand, like the
  existing `fuzz.yml`, never on the pull request path.
