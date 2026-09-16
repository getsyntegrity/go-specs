//go:build !js && !wasm && !lint
// +build !js,!wasm,!lint

package benchmarks

import (
	"testing"

	specs "github.com/pablogore/go-specs/specs"
	"github.com/onsi/gomega"
)

// Matcher benchmarks: Expect().To(Matcher) style. GoSpecs uses ExpectT(ctx, v).To(BeTrue()) / To(Equal(x)); Gomega uses Expect().To(BeTrue()/Equal()).

func BenchmarkMatcher_GoSpecs(b *testing.B) {
	ctx := specs.NewContext(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		specs.ExpectT(ctx, true).To(specs.BeTrue())
	}
}

func BenchmarkMatcher_Gomega(b *testing.B) {
	g := gomega.NewWithT(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Expect(true).To(gomega.BeTrue())
	}
}
