// Package attribution_test is a fixture executed by TestAssertionSourceAttribution in the specs
// package via a real `go test` subprocess. It is under testdata/ so `go test ./...` never runs it
// directly — every spec in here fails on purpose.
//
// Each intentionally failing assertion is tagged with a `// want:<Key>` comment. The parent test
// reads this file to learn the line number each key sits on, then asserts that the real go test
// output attributes the failure of Test<Key> to exactly that file and line. Moving a line around is
// therefore safe; the expectation follows it.
package attribution_test

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

// --- Describe: per-spec subtests (the default sequential model) ---

// TestItParallel pins that Spec.ItParallel (issue #245) gets the same real testing.T.Helper
// attribution as an ordinary sequential It: unlike Builder.ItParallel (out of scope here — see the
// package doc comment above, and parallel_attribution_test.go), each ItParallel spec runs as its
// own real Go subtest with a live, non-nil ctx.T, so its failure is attributed to the user's own
// line the same way any other spec's is.
func TestItParallel(t *testing.T) {
	specs.Describe(t, "itparallel", func(s *specs.Spec) {
		s.ItParallel("fails", func(ctx *specs.Context) {
			specs.EqualTo(ctx, 1, 2) // want:ItParallel
		})
	})
}

func TestEqualTo(t *testing.T) {
	specs.Describe(t, "equalto", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			specs.EqualTo(ctx, 1, 2) // want:EqualTo
		})
	})
}

func TestEqualToString(t *testing.T) {
	specs.Describe(t, "equalto string", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			specs.EqualTo(ctx, "got", "want") // want:EqualToString
		})
	})
}

func TestExpectTToEqual(t *testing.T) {
	specs.Describe(t, "expectt toequal", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			specs.ExpectT(ctx, 1).ToEqual(2) // want:ExpectTToEqual
		})
	})
}

func TestExpectTTo(t *testing.T) {
	specs.Describe(t, "expectt to", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			specs.ExpectT(ctx, false).To(specs.BeTrue()) // want:ExpectTTo
		})
	})
}

// TestExpectTToNilMatcher and TestCtxExpectToNilMatcher cover the nil-matcher branch of To (issue
// #236), which reports through its own helper rather than the ordinary matcher-failure path.
func TestExpectTToNilMatcher(t *testing.T) {
	specs.Describe(t, "expectt to nil matcher", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			specs.ExpectT(ctx, 42).To(nil) // want:ExpectTToNilMatcher
		})
	})
}

func TestCtxExpectToNilMatcher(t *testing.T) {
	specs.Describe(t, "ctx expect to nil matcher", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			ctx.Expect(42).To(nil) // want:CtxExpectToNilMatcher
		})
	})
}

func TestCtxExpectToEqualPrimitive(t *testing.T) {
	specs.Describe(t, "ctx expect toequal primitive", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			ctx.Expect(1).ToEqual(2) // want:CtxExpectToEqualPrimitive
		})
	})
}

// TestCtxExpectToEqualDeep exercises the reflect.DeepEqual fallback rather than the primitive
// fast path, since the two leave ToEqual through different branches.
func TestCtxExpectToEqualDeep(t *testing.T) {
	specs.Describe(t, "ctx expect toequal deep", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			ctx.Expect([]int{1, 2}).ToEqual([]int{1, 3}) // want:CtxExpectToEqualDeep
		})
	})
}

// TestCtxExpectToEqualTypeMismatch hits the fast-path type switch with a mismatched expected type,
// which falls through to reflect.DeepEqual.
func TestCtxExpectToEqualTypeMismatch(t *testing.T) {
	specs.Describe(t, "ctx expect toequal type mismatch", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			ctx.Expect(1).ToEqual(int64(1)) // want:CtxExpectToEqualTypeMismatch
		})
	})
}

func TestCtxExpectTo(t *testing.T) {
	specs.Describe(t, "ctx expect to", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			ctx.Expect(false).To(specs.BeTrue()) // want:CtxExpectTo
		})
	})
}

func TestCtxExpectToEqualMatcher(t *testing.T) {
	specs.Describe(t, "ctx expect equal matcher", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			ctx.Expect("got").To(specs.Equal("want")) // want:CtxExpectToEqualMatcher
		})
	})
}

func TestSnapshot(t *testing.T) {
	specs.Describe(t, "snapshot", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			ctx.Snapshot("stored", map[string]any{"value": "actual"}) // want:Snapshot
		})
	})
}

// --- Assertions inside hooks and nested blocks ---

func TestInsideBeforeEach(t *testing.T) {
	specs.Describe(t, "before each", func(s *specs.Spec) {
		s.BeforeEach(func(ctx *specs.Context) {
			specs.EqualTo(ctx, 1, 2) // want:InsideBeforeEach
		})
		s.It("runs", func(ctx *specs.Context) {})
	})
}

func TestNested(t *testing.T) {
	specs.Describe(t, "outer", func(s *specs.Spec) {
		s.When("inner", func(s *specs.Spec) {
			s.It("fails", func(ctx *specs.Context) {
				specs.EqualTo(ctx, 1, 2) // want:Nested
			})
		})
	})
}

// --- Flat execution models (no per-spec subtest) ---

func TestFlat(t *testing.T) {
	specs.DescribeFlat(t, "flat", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			specs.EqualTo(ctx, 1, 2) // want:Flat
		})
	})
}

func TestFast(t *testing.T) {
	specs.DescribeFast(t, "fast", func(s *specs.Spec) {
		s.It("fails", func(ctx *specs.Context) {
			ctx.Expect(1).ToEqual(2) // want:Fast
		})
	})
}

// --- Builder / Runner ---

func TestBuilderRunner(t *testing.T) {
	b := specs.NewBuilder()
	b.Describe("builder", func() {
		b.It("fails", func(ctx *specs.Context) {
			specs.EqualTo(ctx, 1, 2) // want:BuilderRunner
		})
	})
	specs.NewRunner(b.Build()).Run(t)
}

// --- Bare Context on a real *testing.T (no spec tree at all) ---

func TestBareContext(t *testing.T) {
	ctx := specs.NewContext(t)
	specs.EqualTo(ctx, 1, 2) // want:BareContext
}
