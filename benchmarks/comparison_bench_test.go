//go:build !js && !wasm && !lint
// +build !js,!wasm,!lint

package benchmarks

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	specs "github.com/getsyntegrity/go-specs/specs"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
)

// Head-to-head comparison: the same check expressed as hand-written Go, go-specs,
// Testify and Gomega.
//
// Reading guide — what this measures and what it does NOT:
//
//   - Every scenario has a _Baseline row: the check written by hand, with no assertion
//     library at all. That row is the zero point. Without it, a library-to-library ratio
//     conflates "cost of Testify" with "cost of having an assertion library", and the
//     resulting multiplier invites a conclusion the numbers do not support.
//
//   - go-specs exposes THREE distinct call shapes, and they do not cost the same. They are
//     three separate categories, never two; every benchmark carries the suffix of the one
//     it exercises, so no row is ambiguous:
//
//     _GoSpecsTyped    -> specs.EqualTo(ctx, actual, expected)
//     The typed generic direct path: generic over comparable, compared with ==,
//     no handle is built and nothing is claimed. This is the cheapest shape.
//
//     _GoSpecsExpectT  -> specs.ExpectT(ctx, v).ToEqual(...) / .To(...)
//     A typed-facing API, and still NOT the same code path as EqualTo: it
//     allocates a typedExpectation, claims a single-use handle with an atomic
//     CompareAndSwap, and releases it, so it costs several times EqualTo even
//     when both report zero allocations. Until #177 it also boxed v into an
//     `any` field and allocated for anything not served by the runtime's small
//     -integer cache; #191 changed the field to hold the value at its own type
//     T, which removed that. These rows exist to keep that difference measured
//     rather than assumed. See COMPARISON.md.
//
//     _GoSpecsMatcher  -> ctx.Expect(v).To(Matcher)
//     The matcher/interface path: v enters as any and the comparison goes
//     through an interface call into the matcher.
//
//     EqualTo and ExpectT(...).ToEqual are NOT the same code path, and grouping them
//     under one "typed" label would hide a real and measurable difference.
//     Comparing the matcher path against Testify's hand-optimised assert.True likewise
//     measures two different things, so each go-specs shape is reported on its own row.
//
//   - Assertions are all on passing values, so this is the happy path. Failure formatting
//     is not measured.
//
// Run: go test ./benchmarks -run=NONE -bench=BenchmarkCompare -benchmem -benchtime=2s -count=10
// Conclusions: see benchmarks/COMPARISON.md

type point struct {
	X int
	Y string
}

var (
	cmpInt    = 42
	cmpString = "syntegrity"
	cmpStruct = point{X: 7, Y: "seven"}
	cmpSlice  = []int{1, 2, 3, 4, 5}
	cmpErr    = errors.New("boom")
	// The idiomatic wrap, and the exact shape the correctness section of COMPARISON.md
	// analyses. An earlier revision used errors.Join(cmpErr), which unwraps through
	// Unwrap() []error and is not what a caller writes when adding context to an error.
	cmpWrapped  = fmt.Errorf("layer: %w", cmpErr)
	cmpNilError error
)

// --- Scenario: comparable equality (int) ---

func BenchmarkCompare_IntEqual_Baseline(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if cmpInt != 42 {
			b.Fatal("not equal")
		}
	}
}

func BenchmarkCompare_IntEqual_GoSpecsTyped(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.EqualTo(ctx, cmpInt, 42)
	}
}

func BenchmarkCompare_IntEqual_GoSpecsExpectT(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.ExpectT(ctx, cmpInt).ToEqual(42)
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

func BenchmarkCompare_StringEqual_Baseline(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if cmpString != "syntegrity" {
			b.Fatal("not equal")
		}
	}
}

func BenchmarkCompare_StringEqual_GoSpecsTyped(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.EqualTo(ctx, cmpString, "syntegrity")
	}
}

func BenchmarkCompare_StringEqual_GoSpecsExpectT(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.ExpectT(ctx, cmpString).ToEqual("syntegrity")
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

// --- Scenario: comparable struct equality ---

func BenchmarkCompare_StructEqual_Baseline(b *testing.B) {
	expected := point{X: 7, Y: "seven"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if cmpStruct != expected {
			b.Fatal("not equal")
		}
	}
}

func BenchmarkCompare_StructEqual_GoSpecsTyped(b *testing.B) {
	ctx := specs.NewContext(b)
	expected := point{X: 7, Y: "seven"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.EqualTo(ctx, cmpStruct, expected)
	}
}

func BenchmarkCompare_StructEqual_GoSpecsExpectT(b *testing.B) {
	ctx := specs.NewContext(b)
	expected := point{X: 7, Y: "seven"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.ExpectT(ctx, cmpStruct).ToEqual(expected)
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

// --- Scenario: slice equality ---
//
// A slice is not comparable, so go-specs has no typed path here: it goes through the
// matcher and ends in reflect.DeepEqual, like the other two. The baseline uses
// slices.Equal, which is what a developer writes by hand.

func BenchmarkCompare_SliceEqual_Baseline(b *testing.B) {
	expected := []int{1, 2, 3, 4, 5}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !slices.Equal(cmpSlice, expected) {
			b.Fatal("not equal")
		}
	}
}

func BenchmarkCompare_SliceEqual_GoSpecsMatcher(b *testing.B) {
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
//
// All three go-specs spellings are measured. assert.True is a hand-written branch, so the
// honest comparison against it is the typed path, not the ExpectT or matcher path.

func BenchmarkCompare_BeTrue_Baseline(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !true {
			b.Fatal("not true")
		}
	}
}

func BenchmarkCompare_BeTrue_GoSpecsTyped(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.EqualTo(ctx, true, true)
	}
}

func BenchmarkCompare_BeTrue_GoSpecsExpectT(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.ExpectT(ctx, true).To(specs.BeTrue())
	}
}

func BenchmarkCompare_BeTrue_GoSpecsMatcher(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.Expect(true).To(specs.BeTrue())
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

func BenchmarkCompare_BeNil_Baseline(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if cmpNilError != nil {
			b.Fatal("not nil")
		}
	}
}

func BenchmarkCompare_BeNil_GoSpecsTyped(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.EqualTo(ctx, cmpNilError, nil)
	}
}

func BenchmarkCompare_BeNil_GoSpecsMatcher(b *testing.B) {
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

func BenchmarkCompare_Contains_Baseline(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !strings.Contains(cmpString, "tegri") {
			b.Fatal("does not contain")
		}
	}
}

func BenchmarkCompare_Contains_GoSpecsMatcher(b *testing.B) {
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
// go-specs now unwraps inside the assertion: since #183, assert.ValuesEqual asks
// errors.Is(actual, expected) when both sides are errors, and specs.MatchError /
// specs.MatchErrorAs are the explicit spellings. Testify (ErrorIs) and Gomega (MatchError)
// unwrap inside the assertion too.
//
// Two go-specs rows are therefore measured, and they answer different questions:
//
//   - _GoSpecsTyped  -> errors.Is at the call site, fed to specs.EqualTo. This is the
//     spelling the earlier revisions of this table were generated from, kept so the row
//     stays comparable across runs.
//   - _GoSpecsMatcher -> ctx.Expect(err).To(specs.MatchError(target)). This is the row
//     that is semantically equivalent to assert.ErrorIs and gomega.MatchError: the
//     unwrapping happens inside the assertion, not at the call site. Comparing the
//     call-site spelling against an in-assertion one would compare two different shapes.

func BenchmarkCompare_ErrorIs_Baseline(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !errors.Is(cmpWrapped, cmpErr) {
			b.Fatal("not the same error")
		}
	}
}

func BenchmarkCompare_ErrorIs_GoSpecsTyped(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.EqualTo(ctx, errors.Is(cmpWrapped, cmpErr), true)
	}
}

func BenchmarkCompare_ErrorIs_GoSpecsMatcher(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.Expect(cmpWrapped).To(specs.MatchError(cmpErr))
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
