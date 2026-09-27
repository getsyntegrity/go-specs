//go:build !js && !wasm && !lint
// +build !js,!wasm,!lint

package benchmarks

import (
	"strings"
	"testing"

	specs "github.com/getsyntegrity/go-specs/specs"
)

// Representative-value assertion benchmarks (issue #177).
//
// The older BenchmarkAssertion_* cases assert 42 against 42. That pair cannot allocate whatever the
// framework does: the Go runtime serves interface conversions of integers below 256 out of
// runtime.staticuint64s, and both operands are untyped constants the compiler can fold. A suite
// built only on those values reports "0 allocs/op" for a code path that allocates for every string,
// every large integer and every struct a real spec asserts on.
//
// These benchmarks use values produced at run time (see the vars below) across four shapes, so a
// zero here is a property of the assertion path rather than of the operands.
type allocPoint struct {
	X, Y int64
	Name string
}

// Values are built by init rather than written as literals so the compiler cannot fold them into
// constants, hoist their interface conversions out of the benchmark loop, or serve them from the
// runtime's static small-integer table.
var (
	allocSmallInt  int
	allocLargeInt  int
	allocStr       string
	allocStructVal allocPoint
)

func init() {
	allocSmallInt = len("42") + 40            // 42, but not a constant
	allocLargeInt = 1<<40 + len("x")          // far above the static small-integer table
	allocStr = strings.Repeat("assertion", 4) // heap string, not a constant
	allocStructVal = allocPoint{X: int64(allocLargeInt), Y: 7, Name: allocStr}
}

func BenchmarkExpectT_ToEqual_SmallInt(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.ExpectT(ctx, allocSmallInt).ToEqual(allocSmallInt)
	}
}

func BenchmarkExpectT_ToEqual_LargeInt(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.ExpectT(ctx, allocLargeInt).ToEqual(allocLargeInt)
	}
}

func BenchmarkExpectT_ToEqual_String(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.ExpectT(ctx, allocStr).ToEqual(allocStr)
	}
}

func BenchmarkExpectT_ToEqual_Struct(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.ExpectT(ctx, allocStructVal).ToEqual(allocStructVal)
	}
}

func BenchmarkEqualTo_LargeInt(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.EqualTo(ctx, allocLargeInt, allocLargeInt)
	}
}

func BenchmarkEqualTo_String(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.EqualTo(ctx, allocStr, allocStr)
	}
}

func BenchmarkEqualTo_Struct(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.EqualTo(ctx, allocStructVal, allocStructVal)
	}
}

func BenchmarkExpect_ToEqual_String(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.Expect(allocStr).ToEqual(allocStr)
	}
}

func BenchmarkExpectT_To_Matcher_String(b *testing.B) {
	ctx := specs.NewContext(b)
	m := specs.Equal(allocStr)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.ExpectT(ctx, allocStr).To(m)
	}
}
