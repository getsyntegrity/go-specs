//go:build !js && !wasm && !lint
// +build !js,!wasm,!lint

package benchmarks

import (
	"testing"

	specs "github.com/getsyntegrity/go-specs/specs"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
)

// Hooks benchmarks: before-each style setup + one assertion per spec. Same hook depth (5) and spec count (100) for fair comparison.
// Setup (build program / NewRunner) outside ResetTimer.

const hooksDepth = 5
const hooksSpecCount = 100

func BenchmarkHooks_GoSpecs(b *testing.B) {
	prog := BuildSpecsProgramWithHooks(hooksSpecCount, hooksDepth)
	r := specs.NewRunner(prog)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Run(b)
	}
}

func BenchmarkHooks_Testify(b *testing.B) {
	setup := func() {}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for j := 0; j < hooksSpecCount; j++ {
			for k := 0; k < hooksDepth; k++ {
				setup()
			}
			assert.Equal(b, 1, 1)
		}
	}
}

func BenchmarkHooks_Gomega(b *testing.B) {
	g := gomega.NewWithT(b)
	setup := func() {}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for j := 0; j < hooksSpecCount; j++ {
			for k := 0; k < hooksDepth; k++ {
				setup()
			}
			g.Expect(1).To(gomega.Equal(1))
		}
	}
}
