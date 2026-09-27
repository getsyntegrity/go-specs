//go:build !js && !wasm && !lint
// +build !js,!wasm,!lint

package benchmarks

import (
	"os"
	"strconv"
	"testing"

	specs "github.com/getsyntegrity/go-specs/specs"
)

// TestBenchmarkRatioGuard is a wall-clock regression guard for #235-class jumps: a commit that
// silently doubles the Runner's or Describe's per-spec cost.
//
// BENCHMARKS.md refuses absolute ns/op thresholds on shared CI runners -- they are a property of
// the machine, not the code, and flake on a noisy box. #235 slipped through 134 commits undetected
// because nothing pinned a bound at all. The fix here is a *ratio* against an in-process baseline,
// measured in the same process, in the same `go test` invocation, on the same machine, back to
// back: a hand-written loop that calls the same 1000 funcs the framework calls (one passing
// comparison each) but pays none of the Runner/Describe dispatch machinery. Dividing the
// framework's ns/op by the baseline's ns/op cancels out CPU speed, so the ratio is comparable
// across a fast laptop and a throttled shared runner even though neither absolute number is.
//
// Gated behind GOSPECS_BENCH_GUARD=1 (see `make bench-ratio-guard`) so `go test ./...` and
// `make bench-smoke` stay fast and this never sits on the PR critical path -- it runs from its own
// `ratio-guard` job in benchmarks.yml, on push to develop and main and on workflow_dispatch (see
// BENCHMARKS.md).
//
// Bounds are per check, not shared, because Describe's ratio is naturally larger and noisier than
// Runner's: Describe pays declaration (Spec/Program construction) *and* execution every b.N
// iteration, where CreateGoSpecsSuite's Program is built once before the timed loop, so Describe's
// ratio includes a cost Runner's does not. See the constants below for the measurements behind
// each bound.
//
// Bounds are calibrated against GitHub Actions (ubuntu-latest), not a developer's laptop, because
// that is where this guard actually runs. That calibration matters more than it looks: a first
// pass at these bounds was measured on a linux/amd64 laptop (10 local `go test` runs) and looked
// generous there, but a same-process *ratio* is not fully machine-independent after all --
// Runner's ratio traveled reasonably well (local 15.73x-17.10x vs. 10 `workflow_dispatch` runs on
// this PR's own branch measuring 14.02x-18.41x, a similar band), but Describe's did not: the same
// 10 CI runs measured 79.40x-115.29x, roughly half the local 210.60x-257.00x. Bounds set from the
// laptop numbers would have left Describe with almost 2x more headroom than intended once deployed
// to the runner that actually executes it -- exactly wide enough to *not* catch a #235-class 2x
// regression. See the constants below for the CI-measured ranges and the resulting bound for each
// check, and BENCHMARKS.md for the full derivation and its false-positive/false-negative tradeoff.
const guardSpecCount = 1000

const (
	// guardRunnerBound is the ratio bound for the Runner check, applied when
	// GOSPECS_BENCH_GUARD_BOUND_RUNNER is unset.
	//
	// Measured on GitHub Actions (ubuntu-latest, go1.26.6): 10 `workflow_dispatch` runs of this
	// workflow's `ratio-guard` job against this PR's branch measured ratio 14.02x-18.41x (mean
	// ~15.7x, median ~15.2x; two of the ten runs were mild outliers at 18.14x/18.41x, the rest
	// clustered 14.02x-15.47x).
	//
	// #235 showed individual commits roughly doubling the Runner's own ns/op in one commit
	// (11.4us -> 23.6us, a 2.07x jump). A same-shaped regression today would double the measured
	// ratio the same way. 25x sits above the observed ceiling (18.41x, ~36% headroom -- more than
	// the ~2.6x-31% spread observed run to run) while staying below 2x the observed floor (14.02x
	// -> 28.04x, ~12% margin), so a #235-class regression is caught starting from anywhere in the
	// 10 runs actually measured, and a smaller regression (as little as ~1.6x) is still caught when
	// it starts from a typical (non-outlier) run.
	guardRunnerBound = 25.0

	// guardDescribeBound is the ratio bound for the Describe check, applied when
	// GOSPECS_BENCH_GUARD_BOUND_DESCRIBE is unset.
	//
	// Measured the same way (10 `workflow_dispatch` runs of the `ratio-guard` job, same branch,
	// same CI runner class): ratio 79.40x-115.29x (mean ~96.7x, median ~93.8x) -- a ~45% spread,
	// noisier than Runner's because Describe pays suite declaration every b.N iteration and that
	// cost is more sensitive to scheduling/allocator variance on a shared runner.
	//
	// That spread leaves less room to satisfy both "safely above observed noise" and "safely below
	// a 2x regression from the observed floor" at once than Runner's does: the gap between the
	// observed ceiling (115.29x) and 2x the observed floor (79.40x -> 158.80x) is only ~1.38x, where
	// Runner's equivalent gap is ~1.52x. 150x is a deliberate compromise, not a fully "generous"
	// bound: ~30% headroom over the observed ceiling (150/115.29), and it still sits below 2x the
	// observed floor (150 < 158.80, ~6% margin) -- so a #235-class regression is caught even
	// starting from the single noisiest-fast run in the sample, but only just. Starting from a
	// typical (median ~93.8x) run, sensitivity is much better, catching a regression as small as
	// ~1.6x. With only 10 CI samples behind it, an unobserved noise spike above 115.29x (or a
	// regression smaller than ~1.9x starting from the observed floor) could go either uncaught or
	// falsely flagged; see BENCHMARKS.md's "Wall-clock regression guard (ratio-based, opt-in)" for
	// that tradeoff stated plainly.
	guardDescribeBound = 150.0
)

// guardBaselineFuncs returns n closures, each performing one passing int comparison -- the same
// per-spec work the Runner/Describe cases run. Built once (like CreateGoSpecsSuite's compiled
// Program) so the timed loop below pays the same "call a function value" cost the framework pays
// per spec, and only the framework's dispatch machinery (hooks, defer/recover, reporter accounting)
// is left as the difference the ratio measures.
func guardBaselineFuncs(n int) []func() {
	fns := make([]func(), n)
	for i := range fns {
		fns[i] = func() {
			if one := 1; one != 1 {
				panic("unreachable")
			}
		}
	}
	return fns
}

// guardBaselineLoop returns a func that calls every func in fns once, in order, with no framework
// involved. It is the in-process baseline the ratio is measured against.
func guardBaselineLoop(fns []func()) func(tb testing.TB) {
	return func(tb testing.TB) {
		for _, fn := range fns {
			fn()
		}
	}
}

// guardDescribeBody is the identical per-spec body used by the Runner path (via
// CreateGoSpecsSuite/BuildSpecsProgram) and the Describe path, so the only variable between them is
// the entry point, exactly like BenchmarkDescribeVariant_* above.
func guardDescribeBody(n int) func(s *specs.Spec) {
	return func(s *specs.Spec) {
		for i := 0; i < n; i++ {
			s.It("spec", func(ctx *specs.Context) {
				specs.EqualTo(ctx, 1, 1)
			})
		}
	}
}

func guardEnvFloat(key string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil && v > 0 {
		return v
	}
	return def
}

// guardRatio runs subject and baseline with testing.Benchmark (which self-calibrates b.N), computes
// subject-ns-per-op / baseline-ns-per-op, logs it, and fails if it exceeds bound.
func guardRatio(t *testing.T, name string, subject, baseline func(b *testing.B), bound float64) {
	t.Helper()

	baseRes := testing.Benchmark(baseline)
	subjRes := testing.Benchmark(subject)

	baseNs := float64(baseRes.T.Nanoseconds()) / float64(baseRes.N)
	subjNs := float64(subjRes.T.Nanoseconds()) / float64(subjRes.N)
	ratio := subjNs / baseNs

	t.Logf("%s: baseline=%.1f ns/op subject=%.1f ns/op ratio=%.2fx (bound %.1fx)", name, baseNs, subjNs, ratio, bound)
	if ratio > bound {
		t.Errorf("%s: ratio %.2fx exceeds bound %.1fx (baseline %.1f ns/op, subject %.1f ns/op) -- wall-clock regression?",
			name, ratio, bound, baseNs, subjNs)
	}
}

func TestBenchmarkRatioGuard(t *testing.T) {
	if os.Getenv("GOSPECS_BENCH_GUARD") != "1" {
		t.Skip("set GOSPECS_BENCH_GUARD=1 (or run `make bench-ratio-guard`) to run the in-process baseline ratio guard")
	}

	runnerBound := guardEnvFloat("GOSPECS_BENCH_GUARD_BOUND_RUNNER", guardRunnerBound)
	describeBound := guardEnvFloat("GOSPECS_BENCH_GUARD_BOUND_DESCRIBE", guardDescribeBound)

	t.Run("Runner", func(t *testing.T) {
		suite := CreateGoSpecsSuite(guardSpecCount)
		loop := guardBaselineLoop(guardBaselineFuncs(guardSpecCount))

		guardRatio(t, "Runner", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				suite(b)
			}
		}, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				loop(b)
			}
		}, runnerBound)
	})

	t.Run("Describe", func(t *testing.T) {
		body := guardDescribeBody(guardSpecCount)
		loop := guardBaselineLoop(guardBaselineFuncs(guardSpecCount))

		guardRatio(t, "Describe", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				specs.Describe(b, "suite", body)
			}
		}, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				loop(b)
			}
		}, describeBound)
	})
}
