//go:build !js && !wasm && !lint
// +build !js,!wasm,!lint

package benchmarks

import (
	"testing"

	specs "github.com/pablogore/go-specs/specs"
)

// Benchmarks migrated from the former bench/ package: BuildSuite-based runner,
// MinimalRunner (slice of specs, single context), parallel MinimalRunner variants,
// and nested Describe + BeforeEach hooks.

const iterationsBuildSuite = 2000
const nestedHooksDepth = 5

// BenchmarkRunner_GoSpecs_BuildSuite runs a suite built with specs.BuildSuite (2000 specs).
func BenchmarkRunner_GoSpecs_BuildSuite(b *testing.B) {
	suite := specs.BuildSuite(b, "runner", func(s *specs.Spec) {
		for i := 0; i < iterationsBuildSuite; i++ {
			s.It("spec", func(ctx *specs.Context) {
				ctx.Expect(1).ToEqual(1)
			})
		}
	})
	if suite == nil {
		b.Fatal("BuildSuite returned nil")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		suite.Run(b)
	}
}

// BenchmarkRunner_Minimal uses the minimal runner: slice of specs, single context, no allocs in loop.
func BenchmarkRunner_Minimal(b *testing.B) {
	r := specs.NewMinimalRunner(iterationsBuildSuite)
	for i := 0; i < iterationsBuildSuite; i++ {
		r.Add("spec", func(ctx *specs.Context) {
			specs.EqualTo(ctx, 1, 1)
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Run(b)
	}
}

// BenchmarkRunner_MinimalParallel_Chunk1 runs 2000 specs in parallel, one spec per atomic claim (baseline).
func BenchmarkRunner_MinimalParallel_Chunk1(b *testing.B) {
	r := specs.NewMinimalRunner(iterationsBuildSuite)
	for i := 0; i < iterationsBuildSuite; i++ {
		r.Add("spec", func(ctx *specs.Context) { specs.EqualTo(ctx, 1, 1) })
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.RunParallel(b, 0)
	}
}

// BenchmarkRunner_MinimalParallel_Chunk16 runs 2000 specs in parallel with chunk size 16.
func BenchmarkRunner_MinimalParallel_Chunk16(b *testing.B) {
	r := specs.NewMinimalRunner(iterationsBuildSuite)
	for i := 0; i < iterationsBuildSuite; i++ {
		r.Add("spec", func(ctx *specs.Context) { specs.EqualTo(ctx, 1, 1) })
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.RunParallelBatched(b, 0, 16)
	}
}

// BenchmarkRunner_MinimalParallel_Chunk64 runs 2000 specs in parallel with chunk size 64.
func BenchmarkRunner_MinimalParallel_Chunk64(b *testing.B) {
	r := specs.NewMinimalRunner(iterationsBuildSuite)
	for i := 0; i < iterationsBuildSuite; i++ {
		r.Add("spec", func(ctx *specs.Context) { specs.EqualTo(ctx, 1, 1) })
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.RunParallelBatched(b, 0, 64)
	}
}

// BenchmarkRunner_MinimalParallel_LargeSuite_Chunk1 runs 10000 specs, chunk=1 (high contention).
func BenchmarkRunner_MinimalParallel_LargeSuite_Chunk1(b *testing.B) {
	const n = 10000
	r := specs.NewMinimalRunner(n)
	for i := 0; i < n; i++ {
		r.Add("spec", func(ctx *specs.Context) { specs.EqualTo(ctx, 1, 1) })
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.RunParallel(b, 0)
	}
}

// BenchmarkRunner_MinimalParallel_LargeSuite_Chunk16 runs 10000 specs with chunk 16.
func BenchmarkRunner_MinimalParallel_LargeSuite_Chunk16(b *testing.B) {
	const n = 10000
	r := specs.NewMinimalRunner(n)
	for i := 0; i < n; i++ {
		r.Add("spec", func(ctx *specs.Context) { specs.EqualTo(ctx, 1, 1) })
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.RunParallelBatched(b, 0, 16)
	}
}

// buildNestedHooksSuite builds a suite with nested Describe and BeforeEach (depth levels).
func buildNestedHooksSuite(tb testing.TB) *specs.CompiledSuite {
	return specs.BuildSuite(tb, "nested", func(s *specs.Spec) {
		addNestedLevel(s, 1, nestedHooksDepth)
	})
}

func addNestedLevel(s *specs.Spec, level, max int) {
	s.BeforeEach(func(_ *specs.Context) {})
	if level >= max {
		s.It("test", func(ctx *specs.Context) {
			ctx.Expect(1).ToEqual(1)
		})
		return
	}
	s.Describe("level", func(s2 *specs.Spec) {
		addNestedLevel(s2, level+1, max)
	})
}

// BenchmarkHooks_GoSpecs_Nested runs 5 nested BeforeEach + 1 assertion via BuildSuite (DSL).
func BenchmarkHooks_GoSpecs_Nested(b *testing.B) {
	suite := buildNestedHooksSuite(b)
	if suite == nil {
		b.Fatal("buildNestedHooksSuite returned nil")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		suite.Run(b)
	}
}
