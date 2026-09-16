//go:build !js && !wasm && !lint
// +build !js,!wasm,!lint

package benchmarks

import (
	"testing"

	specs "github.com/getsyntegrity/go-specs/specs"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
)

// Assertion benchmarks: cost of a single assertion. Setup isolated from timing; loop measures only the assertion path.
// go-specs targets zero allocations on the fast path (EqualTo, ExpectT().ToEqual).

func BenchmarkAssertion_GoSpecs_EqualTo(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.EqualTo(ctx, 42, 42)
	}
}

func BenchmarkAssertion_GoSpecs_ExpectToEqual(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.ExpectT(ctx, 42).ToEqual(42)
	}
}

func BenchmarkAssertion_Testify_Equal(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		assert.Equal(b, 42, 42)
	}
}

func BenchmarkAssertion_Gomega_ExpectToEqual(b *testing.B) {
	g := gomega.NewWithT(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Expect(42).To(gomega.Equal(42))
	}
}
