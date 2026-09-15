//go:build !js && !wasm && !lint
// +build !js,!wasm,!lint

package benchmarks

import (
	"testing"

	specs "github.com/pablogore/go-specs/specs"
)

// describeVariantSpecs is the suite size the three entry points are compared at.
const describeVariantSpecs = 200

// declareDescribeVariantSuite is the identical body handed to each entry point, so the only variable
// between the benchmarks below is which function received it.
func declareDescribeVariantSuite(s *specs.Spec) {
	for i := 0; i < describeVariantSpecs; i++ {
		s.It("spec", func(ctx *specs.Context) {
			ctx.Expect(1).ToEqual(1)
		})
	}
}

// BenchmarkDescribeVariant_Describe, _DescribeFlat and _DescribeFast exist to keep DescribeFast's
// godoc claim checkable rather than asserted: the three entry points are documented aliases of one
// another (#110), so their allocs/op must match. Run them together and confirm it.
//
// The backend is the *testing.B these benchmarks already run on, which is the one backend that
// genuinely runs without subtests — so this measures declaration plus execution with the subtest
// path excluded, and isolates the flag as the only difference. Whether a *testing.T gets subtests
// from all three is a separate question, proven in specs/subtest_identity_test.go against a real
// `go test -v` transcript.
func BenchmarkDescribeVariant_Describe(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		specs.Describe(b, "suite", declareDescribeVariantSuite)
	}
}

func BenchmarkDescribeVariant_DescribeFlat(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		specs.DescribeFlat(b, "suite", declareDescribeVariantSuite)
	}
}

func BenchmarkDescribeVariant_DescribeFast(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		specs.DescribeFast(b, "suite", declareDescribeVariantSuite)
	}
}
