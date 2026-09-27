// Package parallel_attribution_test is a fixture executed by TestParallelAssertionSourceAttribution
// in the specs package via a real `go test` subprocess. It is under testdata/ so `go test ./...`
// never runs it directly — every spec in here fails on purpose.
//
// Each intentionally failing assertion is tagged with a `// want:<Key>` comment. The parent test
// reads this file to learn the line number each key sits on, then asserts that the real go test
// output embeds that file:line inside the failure message of Test<Key>. Moving a line around is
// therefore safe; the expectation follows it.
//
// Each fixture below is a single-spec ItParallel group so its failure is always spec[0]. That keeps
// one location per failing test for the parent to match; reportFailures (scheduler.go) reports every
// failing spec in index order, which testdata/parallel_multifailure covers instead.
package parallel_attribution_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestItParallelEqualTo(t *testing.T) {
	b := specs.NewBuilder()
	b.Describe("parallel equalto", func() {
		b.ItParallel("fails", func(ctx *specs.Context) {
			specs.EqualTo(ctx, 1, 2) // want:ItParallelEqualTo
		})
	})
	specs.NewRunner(b.Build()).Run(t)
}

func TestItParallelExpectToEqual(t *testing.T) {
	b := specs.NewBuilder()
	b.Describe("parallel expect toequal", func() {
		b.ItParallel("fails", func(ctx *specs.Context) {
			ctx.Expect(1).ToEqual(2) // want:ItParallelExpectToEqual
		})
	})
	specs.NewRunner(b.Build()).Run(t)
}
