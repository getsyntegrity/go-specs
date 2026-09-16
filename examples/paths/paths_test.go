package paths_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestPaths(t *testing.T) {
	t.Skip("paths combinatorial execution with top-level Describe deferred to post-v1.0.0")
	specs.Describe(t, "config", func(s *specs.Spec) {
		s.Paths(func(p *specs.PathBuilder) {
			p.IntRange("tier", 1, 3)
			p.IntRange("region", 1, 2)
		}).It("valid configuration", func(ctx *specs.Context) {
			tier := ctx.Path().Int("tier")
			region := ctx.Path().Int("region")

			ctx.Expect(tier > 0).To(specs.BeTrue())
			ctx.Expect(region > 0).To(specs.BeTrue())
		})
	})
}
