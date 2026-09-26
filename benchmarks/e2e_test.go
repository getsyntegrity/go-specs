//go:build !js && !wasm && !lint
// +build !js,!wasm,!lint

package benchmarks

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	specs "github.com/getsyntegrity/go-specs/specs"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
)

// End-to-end cost of a suite as `go test` actually runs it.
//
// The Benchmark* functions in this package run on a *testing.B. That backend is the one place the
// runner deliberately skips its per-spec t.Run subtest (see runSpecRecovered in specs/runner.go),
// and the Testify/Gomega "runner" rows are plain assertion loops with no subtest at all. Both are
// useful for isolating framework overhead, but neither is what a user's suite costs: under a real
// *testing.T every go-specs spec is a subtest, and so is every table-driven Testify/Gomega case.
//
// This test measures that path. Every framework runs the same N specs, one passing equality
// assertion each, and every spec is its own subtest — the shape `go test -v` reports. A hand-written
// t.Run loop is the baseline, so the table separates what the framework adds from what the testing
// package already costs.
//
// It is a Test, not a Benchmark, because the *testing.T path is the thing being measured and a
// benchmark function never receives one. It is opt-in (GOSPECS_E2E=1) so `go test ./...` stays
// fast; run it with `make bench-e2e`. The numbers are observational, like every ns/op figure in
// BENCHMARKS.md: nothing here asserts a threshold.

const e2eDefaultSpecs = 1000

type e2eCase struct {
	name string
	run  func(t *testing.T)
}

func e2eNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("spec %04d", i)
	}
	return names
}

func e2eCases(n int) []e2eCase {
	names := e2eNames(n)
	prebuilt := specs.BuildSuite(nil, "suite", func(s *specs.Spec) {
		for _, name := range names {
			s.It(name, func(ctx *specs.Context) { specs.EqualTo(ctx, 1, 1) })
		}
	})
	return []e2eCase{
		{"baseline (t.Run + if)", func(t *testing.T) {
			for _, name := range names {
				t.Run(name, func(t *testing.T) {
					if one := 1; one != 1 {
						t.Fatal("unreachable")
					}
				})
			}
		}},
		{"testify (t.Run + assert.Equal)", func(t *testing.T) {
			for _, name := range names {
				t.Run(name, func(t *testing.T) { assert.Equal(t, 1, 1) })
			}
		}},
		{"gomega (t.Run + NewWithT)", func(t *testing.T) {
			for _, name := range names {
				t.Run(name, func(t *testing.T) { gomega.NewWithT(t).Expect(1).To(gomega.Equal(1)) })
			}
		}},
		{"go-specs Describe (declare+run)", func(t *testing.T) {
			specs.Describe(t, "suite", func(s *specs.Spec) {
				for _, name := range names {
					s.It(name, func(ctx *specs.Context) { specs.EqualTo(ctx, 1, 1) })
				}
			})
		}},
		{"go-specs BuildSuite (run only)", func(t *testing.T) {
			prebuilt.Run(t)
		}},
	}
}

func e2eEnvInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}

func TestEndToEnd_SubtestPath(t *testing.T) {
	if os.Getenv("GOSPECS_E2E") != "1" {
		t.Skip("set GOSPECS_E2E=1 (or run `make bench-e2e`) to measure the *testing.T path")
	}
	n := e2eEnvInt("GOSPECS_E2E_SPECS", e2eDefaultSpecs)
	runs := e2eEnvInt("GOSPECS_E2E_RUNS", 15)

	type row struct {
		name   string
		median time.Duration
		allocs float64
	}
	var rows []row
	for _, c := range e2eCases(n) {
		c := c
		// Each measured run gets its own parent subtest so subtest bookkeeping from earlier runs
		// (and earlier frameworks) is never on the path being timed.
		measure := func(label string) time.Duration {
			var d time.Duration
			t.Run(label, func(t *testing.T) {
				start := time.Now()
				c.run(t)
				d = time.Since(start)
			})
			return d
		}
		measure(c.name + " warmup")
		times := make([]time.Duration, runs)
		for i := range times {
			times[i] = measure(fmt.Sprintf("%s run %d", c.name, i))
		}
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		var allocs float64
		t.Run(c.name+" allocs", func(t *testing.T) {
			allocs = testing.AllocsPerRun(3, func() { c.run(t) })
		})
		rows = append(rows, row{c.name, times[len(times)/2], allocs})
	}

	base := rows[0].median
	fmt.Printf("E2E specs=%d runs=%d (median wall time per suite, every spec a t.Run subtest)\n", n, runs)
	fmt.Printf("E2E | %-34s | %12s | %10s | %12s | %8s |\n", "Framework", "per suite", "per spec", "vs baseline", "allocs")
	for _, r := range rows {
		fmt.Printf("E2E | %-34s | %12s | %10s | %11.2fx | %8.0f |\n",
			r.name, r.median.Round(time.Microsecond), (r.median / time.Duration(n)).Round(10*time.Nanosecond),
			float64(r.median)/float64(base), r.allocs)
	}
}
