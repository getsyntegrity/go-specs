//go:build !js && !wasm && !lint
// +build !js,!wasm,!lint

package benchmarks

import (
	"testing"

	specs "github.com/pablogore/go-specs/specs"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
)

// BuildSpecsProgram creates a compiled Program with n specs, each running one EqualTo(ctx, 1, 1).
// All specs share one Describe and one (optional) BeforeEach, so they form one group.
// Deterministic: same n always produces the same program shape.
func BuildSpecsProgram(n int) *specs.Program {
	b := specs.NewBuilder()
	b.Describe("suite", func() {
		b.BeforeEach(func(*specs.Context) {})
		for i := 0; i < n; i++ {
			b.It("spec", func(ctx *specs.Context) {
				specs.EqualTo(ctx, 1, 1)
			})
		}
	})
	return b.Build()
}

// BuildSpecsProgramWithHooks creates a Program with n specs and depth levels of BeforeEach.
// Each spec runs depth before hooks then one assertion. Deterministic.
func BuildSpecsProgramWithHooks(n, depth int) *specs.Program {
	b := specs.NewBuilder()
	b.Describe("suite", func() {
		for d := 0; d < depth; d++ {
			d := d
			b.BeforeEach(func(*specs.Context) { _ = d })
		}
		for i := 0; i < n; i++ {
			b.It("spec", func(ctx *specs.Context) {
				specs.EqualTo(ctx, 1, 1)
			})
		}
	})
	return b.Build()
}

// RunSpecsProgram runs the program with specs.NewRunner. Used by benchmarks.
func RunSpecsProgram(tb testing.TB, prog *specs.Program) {
	if prog == nil {
		return
	}
	specs.NewRunner(prog).Run(tb)
}

// Suite sizes for benchmarks (deterministic, realistic).
const (
	SuiteSize100   = 100
	SuiteSize1000  = 1000
	SuiteSize10000 = 10000
	SuiteSize50000 = 50000
)

// CreateGoSpecsSuite builds a suite of n specs (one EqualTo per spec) and returns a runnable function.
// Call it before b.ResetTimer(); the returned function runs the suite with minimal allocations per run.
func CreateGoSpecsSuite(specCount int) func(tb testing.TB) {
	prog := BuildSpecsProgram(specCount)
	r := specs.NewRunner(prog)
	return func(tb testing.TB) {
		r.Run(tb)
	}
}

// CreateTestifySuite returns a function that runs n assert.Equal(tb, 1, 1) calls.
// Call it before b.ResetTimer(); the returned function runs the loop with no per-spec suite allocation.
func CreateTestifySuite(specCount int) func(tb testing.TB) {
	return func(tb testing.TB) {
		for i := 0; i < specCount; i++ {
			assert.Equal(tb, 1, 1)
		}
	}
}

// CreateGomegaSuite returns a function that runs n Gomega expectations (Expect(1).To(Equal(1))).
// Call it before b.ResetTimer(). NewWithT(tb) is called once per suite run inside the returned function.
func CreateGomegaSuite(specCount int) func(tb testing.TB) {
	return func(tb testing.TB) {
		g := gomega.NewWithT(tb)
		for i := 0; i < specCount; i++ {
			g.Expect(1).To(gomega.Equal(1))
		}
	}
}
