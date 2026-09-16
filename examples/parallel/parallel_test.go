package parallel_test

import (
	"testing"

	"github.com/pablogore/go-specs/specs"
)

func add(a, b int) int {
	return a + b
}

func TestParallel(t *testing.T) {
	b := specs.NewBuilder()
	b.Describe("parallel math", func() {
		b.ItParallel("adds numbers", func(ctx *specs.Context) {
			ctx.Expect(add(1, 1)).ToEqual(2)
		})
	})
	prog := b.Build()
	specs.NewRunner(prog).Run(t)
}
