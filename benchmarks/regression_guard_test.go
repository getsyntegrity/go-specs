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
// `make bench-smoke` stay fast and this never sits on the PR critical path -- it runs from
// benchmarks.yml on push to main/workflow_dispatch, same cadence as the rest of the timing-sensitive
// suite in that workflow.
//
// Bounds are per check, not shared, because Describe's ratio is naturally larger and noisier than
// Runner's: Describe pays declaration (Spec/Program construction) *and* execution every b.N
// iteration, where CreateGoSpecsSuite's Program is built once before the timed loop, so Describe's
// ratio includes a cost Runner's does not. See the constants below for the measurements behind
// each bound.
const guardSpecCount = 1000

const (
	// guardRunnerBound is the ratio bound for the Runner check, applied when
	// GOSPECS_BENCH_GUARD_BOUND_RUNNER is unset.
	//
	// Measured locally (linux/amd64, i7-13620H, go1.26.6, plain `go test`, 10 separate process
	// runs) with CreateGoSpecsSuite(1000) as the subject: ratio ranged 15.73x-17.10x (~9% spread),
	// mean ~16.6x.
	//
	// #235 showed individual commits roughly doubling the Runner's own ns/op in one commit
	// (11.4us -> 23.6us, a 2.07x jump -- the case the issue calls out by name). Because CPU speed
	// cancels out of a same-process ratio, a same-shaped regression today would double the measured
	// ratio too, to ~31x-34x depending on where in the observed band it started. 30x sits below
	// that entire doubled range (so a #235-class jump from anywhere in the observed band still
	// trips it) while leaving ~1.75x headroom over the noisiest local run (30/17.10), comfortably
	// above the ~9% run-to-run spread actually observed.
	guardRunnerBound = 30.0

	// guardDescribeBound is the ratio bound for the Describe check, applied when
	// GOSPECS_BENCH_GUARD_BOUND_DESCRIBE is unset.
	//
	// Measured the same way, with specs.Describe(b, "suite", body) declaring and running 1000 specs
	// as the subject: ratio ranged 210.60x-257.00x (~22% spread, wider than Runner's because
	// declaration cost is included every iteration), mean ~235x.
	//
	// 400x sits below the doubled low end of the observed band (210.60x * 2 = 421.2x, so a
	// #235-class 2x regression starting anywhere in the observed range still trips it) while
	// leaving ~1.56x headroom over the noisiest local run (400/257.00), well above the ~22% spread
	// actually observed.
	guardDescribeBound = 400.0
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
