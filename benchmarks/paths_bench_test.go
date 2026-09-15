//go:build !js && !wasm && !lint
// +build !js,!wasm,!lint

package benchmarks

import (
	"testing"

	specs "github.com/pablogore/go-specs/specs"
)

// Path exploration benchmarks: full execution of one generated spec through the bounded
// proposal controller — the third cost center BENCHMARKS.md names, alongside assertions and the
// runner. Both suites are compiled once, before ResetTimer, so the loop measures candidate
// generation plus per-candidate execution only, never suite construction or DSL parsing.
//
// These also guard the per-candidate naming added for #103: naming is skipped entirely when the
// backend is a *testing.B, because runnableBackend.Run opens no named subtest there, so these
// numbers must not move when candidate identity changes.

const (
	// cartesianLargeDimension × 3 dimensions = 1000 candidates per op — large enough that any
	// per-candidate cost shows up well above noise.
	cartesianLargeDimension = 10
	// pathSampleCount is a sampled run's requested budget; sampling also exercises the seeded RNG
	// and the duplicate-signature filter that Cartesian never touches.
	pathSampleCount = 200
)

func dimensionValues(n int) []any {
	values := make([]any, 0, n)
	for i := 0; i < n; i++ {
		values = append(values, i)
	}
	return values
}

// buildCartesianPathsSuite compiles one spec whose Paths() expands to dimension^3 candidates.
func buildCartesianPathsSuite(tb testing.TB, dimension int) *specs.CompiledSuite {
	return specs.BuildSuite(tb, "paths-cartesian", func(s *specs.Spec) {
		s.Paths(func(pb *specs.PathBuilder) {
			pb.Values("alpha", dimensionValues(dimension))
			pb.Values("beta", dimensionValues(dimension))
			pb.Values("gamma", dimensionValues(dimension))
		}).It("combines dimensions", func(ctx *specs.Context) {
			specs.EqualTo(ctx, ctx.Path().Int("alpha"), ctx.Path().Int("alpha"))
		})
	})
}

// buildSampledPathsSuite compiles one spec whose Paths() draws samples from a seeded RNG.
func buildSampledPathsSuite(tb testing.TB, samples int) *specs.CompiledSuite {
	return specs.BuildSuite(tb, "paths-sample", func(s *specs.Spec) {
		s.Paths(func(pb *specs.PathBuilder) {
			pb.Bool("vip")
			pb.IntRange("price", 0, 10000)
		}).Sample(samples).Seed(7).It("samples the space", func(ctx *specs.Context) {
			specs.EqualTo(ctx, ctx.Path().Int("price"), ctx.Path().Int("price"))
		})
	})
}

func BenchmarkPaths_Cartesian_Large(b *testing.B) {
	suite := buildCartesianPathsSuite(b, cartesianLargeDimension)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		suite.Run(b)
	}
}

func BenchmarkPaths_Sample(b *testing.B) {
	suite := buildSampledPathsSuite(b, pathSampleCount)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		suite.Run(b)
	}
}
