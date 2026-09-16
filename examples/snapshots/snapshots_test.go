package snapshots_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

func TestSnapshots(t *testing.T) {
	specs.Describe(t, "user service", func(s *specs.Spec) {
		s.It("creates user snapshot", func(ctx *specs.Context) {
			user := map[string]any{
				"id":   123,
				"name": "alice",
			}
			ctx.Snapshot("create-user", user)
		})
	})
}
