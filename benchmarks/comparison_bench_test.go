//go:build !js && !wasm && !lint
// +build !js,!wasm,!lint

package benchmarks

import (
	"errors"
	"testing"

	specs "github.com/getsyntegrity/go-specs/specs"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
)

// Head-to-head comparison: the same assertion expressed in go-specs, Testify and Gomega.
//
// Every scenario asserts on a passing value, so the loop measures the happy path only:
// value comparison plus whatever boxing, reflection or matcher allocation the library needs.
// Setup stays outside the timed region; b.ResetTimer() marks the boundary.
//
// Run: go test ./benchmarks -run='^$' -bench=BenchmarkCompare -benchmem
// Conclusions: see benchmarks/COMPARISON.md

type point struct {
	X int
	Y string
}

var (
	cmpInt      = 42
	cmpString   = "syntegrity"
	cmpStruct   = point{X: 7, Y: "seven"}
	cmpSlice    = []int{1, 2, 3, 4, 5}
	cmpErr      = errors.New("boom")
	cmpWrapped  = errors.Join(cmpErr)
	cmpNilError error
)

// --- Scenario: comparable equality (int) ---

func BenchmarkCompare_IntEqual_GoSpecs(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.EqualTo(ctx, cmpInt, 42)
	}
}

func BenchmarkCompare_IntEqual_Testify(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		assert.Equal(b, cmpInt, 42)
	}
}

func BenchmarkCompare_IntEqual_Gomega(b *testing.B) {
	g := gomega.NewWithT(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Expect(cmpInt).To(gomega.Equal(42))
	}
}

// --- Scenario: string equality ---

func BenchmarkCompare_StringEqual_GoSpecs(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.EqualTo(ctx, cmpString, "syntegrity")
	}
}

func BenchmarkCompare_StringEqual_Testify(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		assert.Equal(b, cmpString, "syntegrity")
	}
}

func BenchmarkCompare_StringEqual_Gomega(b *testing.B) {
	g := gomega.NewWithT(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Expect(cmpString).To(gomega.Equal("syntegrity"))
	}
}

// --- Scenario: struct equality (comparable struct: go-specs stays on the == fast path) ---

func BenchmarkCompare_StructEqual_GoSpecs(b *testing.B) {
	ctx := specs.NewContext(b)
	expected := point{X: 7, Y: "seven"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.EqualTo(ctx, cmpStruct, expected)
	}
}

func BenchmarkCompare_StructEqual_Testify(b *testing.B) {
	expected := point{X: 7, Y: "seven"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		assert.Equal(b, cmpStruct, expected)
	}
}

func BenchmarkCompare_StructEqual_Gomega(b *testing.B) {
	g := gomega.NewWithT(b)
	expected := point{X: 7, Y: "seven"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Expect(cmpStruct).To(gomega.Equal(expected))
	}
}

// --- Scenario: slice equality (deep comparison in all three) ---

func BenchmarkCompare_SliceEqual_GoSpecs(b *testing.B) {
	ctx := specs.NewContext(b)
	expected := []int{1, 2, 3, 4, 5}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.Expect(cmpSlice).To(specs.Equal(expected))
	}
}

func BenchmarkCompare_SliceEqual_Testify(b *testing.B) {
	expected := []int{1, 2, 3, 4, 5}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		assert.Equal(b, cmpSlice, expected)
	}
}

func BenchmarkCompare_SliceEqual_Gomega(b *testing.B) {
	g := gomega.NewWithT(b)
	expected := []int{1, 2, 3, 4, 5}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Expect(cmpSlice).To(gomega.Equal(expected))
	}
}

// --- Scenario: boolean truth ---

func BenchmarkCompare_BeTrue_GoSpecs(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.ExpectT(ctx, true).To(specs.BeTrue())
	}
}

func BenchmarkCompare_BeTrue_Testify(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		assert.True(b, true)
	}
}

func BenchmarkCompare_BeTrue_Gomega(b *testing.B) {
	g := gomega.NewWithT(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Expect(true).To(gomega.BeTrue())
	}
}

// --- Scenario: nil check (the usual "no error returned" assertion) ---

func BenchmarkCompare_BeNil_GoSpecs(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.Expect(cmpNilError).To(specs.BeNil())
	}
}

func BenchmarkCompare_BeNil_Testify(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		assert.NoError(b, cmpNilError)
	}
}

func BenchmarkCompare_BeNil_Gomega(b *testing.B) {
	g := gomega.NewWithT(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Expect(cmpNilError).To(gomega.BeNil())
	}
}

// --- Scenario: substring containment ---

func BenchmarkCompare_Contains_GoSpecs(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.Expect(cmpString).To(specs.Contain("tegri"))
	}
}

func BenchmarkCompare_Contains_Testify(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		assert.Contains(b, cmpString, "tegri")
	}
}

func BenchmarkCompare_Contains_Gomega(b *testing.B) {
	g := gomega.NewWithT(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Expect(cmpString).To(gomega.ContainSubstring("tegri"))
	}
}

// --- Scenario: wrapped error identity (errors.Is semantics) ---
//
// go-specs has no unwrapping error matcher: specs.Equal resolves through assert.ValuesEqual,
// which ends in reflect.DeepEqual and therefore does NOT see through errors.Join/fmt.Errorf %w.
// The idiomatic go-specs spelling is errors.Is at the call site, which is what is measured here.
// Testify (ErrorIs) and Gomega (MatchError) unwrap inside the assertion.

func BenchmarkCompare_ErrorIs_GoSpecs(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.ExpectT(ctx, errors.Is(cmpWrapped, cmpErr)).To(specs.BeTrue())
	}
}

func BenchmarkCompare_ErrorIs_Testify(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		assert.ErrorIs(b, cmpWrapped, cmpErr)
	}
}

func BenchmarkCompare_ErrorIs_Gomega(b *testing.B) {
	g := gomega.NewWithT(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Expect(cmpWrapped).To(gomega.MatchError(cmpErr))
	}
}
