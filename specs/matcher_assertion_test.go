package specs

import "testing"

// The matcher assertion path — Expect(x).To(m), ExpectT(ctx, x).To(m), and the specs.* matcher
// re-exports the README documents — ran only under `go test -bench`, so `go test ./...` proved
// nothing about it. These tests drive it through capturingBackend, which records Fatalf instead of
// aborting the goroutine, so the failure branch can be asserted on rather than just avoided.

// newCapturedContext returns a Context wired to a backend that records failures, plus that backend.
func newCapturedContext() (*Context, *capturingBackend) {
	b := &capturingBackend{}
	ctx := &Context{}
	ctx.Reset(b)
	return ctx, b
}

func TestExpectToReportsNothingWhenTheMatcherPasses(t *testing.T) {
	ctx, b := newCapturedContext()

	ctx.Expect(42).To(Equal(42))

	if b.failed {
		t.Fatalf("expected no failure, got %q", b.message)
	}
	if ctx.failed {
		t.Fatal("expected the context to stay unfailed after a passing matcher")
	}
}

func TestExpectToReportsTheMatcherMessageWhenItFails(t *testing.T) {
	ctx, b := newCapturedContext()

	ctx.Expect(42).To(Equal(43))

	if !b.failed {
		t.Fatal("expected the backend to record a failure")
	}
	if b.message != "expected 42 to equal 43" {
		t.Fatalf("got %q, want %q", b.message, "expected 42 to equal 43")
	}
}

// A matcher failure must mark the Context, not just the backend: FailFast and the Failed flag on
// SpecResultEvent both read ctx.failed, and a failure invisible to them reports a green spec on a
// red run — the same class of defect as issue #115 on the snapshot path.
func TestExpectToMarksTheContextFailedSoFailFastAndReportersSeeIt(t *testing.T) {
	ctx, _ := newCapturedContext()

	ctx.Expect(42).To(Equal(43))

	if !ctx.failed {
		t.Fatal("expected the context to be marked failed after a matcher failure")
	}
}

func TestExpectToRecordsACoverageEdgeWhenTheMatcherPasses(t *testing.T) {
	ctx, _ := newCapturedContext()
	before := &Coverage{}
	ctx.coverage = &Coverage{}

	ctx.Expect(42).To(Equal(42))

	if !ctx.coverage.HasNewCoverage(before) {
		t.Fatal("expected the passing matcher path to record a coverage edge")
	}
}

// The guards are what keep a misuse from panicking mid-suite; each one releases the pooled
// Expectation and returns without reporting.
func TestExpectToIgnoresANilMatcher(t *testing.T) {
	ctx, b := newCapturedContext()

	ctx.Expect(42).To(nil)

	if b.failed {
		t.Fatalf("expected a nil matcher to be ignored, got %q", b.message)
	}
}

func TestExpectToIgnoresAnExpectationWithoutAContext(t *testing.T) {
	var e *Expectation
	e.To(Equal(1)) // nil receiver

	detached := &Expectation{actual: 42}
	detached.To(Equal(43)) // non-nil, but ctx is nil
}

func TestExpectTToReportsNothingWhenTheMatcherPasses(t *testing.T) {
	ctx, b := newCapturedContext()

	ExpectT(ctx, true).To(BeTrue())

	if b.failed {
		t.Fatalf("expected no failure, got %q", b.message)
	}
}

func TestExpectTToReportsTheMatcherMessageWhenItFails(t *testing.T) {
	ctx, b := newCapturedContext()

	ExpectT(ctx, true).To(BeFalse())

	if !b.failed {
		t.Fatal("expected the backend to record a failure")
	}
	if b.message != "expected false, got true (bool)" {
		t.Fatalf("got %q, want %q", b.message, "expected false, got true (bool)")
	}
}

// The typed path must mark the Context exactly as the untyped one does. Without it, a spec whose
// only assertion is ExpectT(ctx, x).To(m) fails the run but reports Failed=false to every reporter,
// and FailFast keeps going past it.
func TestExpectTToMarksTheContextFailedLikeTheUntypedPath(t *testing.T) {
	ctx, _ := newCapturedContext()

	ExpectT(ctx, true).To(BeFalse())

	if !ctx.failed {
		t.Fatal("expected the context to be marked failed after a typed matcher failure")
	}
}

func TestExpectTToRecordsACoverageEdgeWhenTheMatcherPasses(t *testing.T) {
	ctx, _ := newCapturedContext()
	before := &Coverage{}
	ctx.coverage = &Coverage{}

	ExpectT(ctx, true).To(BeTrue())

	if !ctx.coverage.HasNewCoverage(before) {
		t.Fatal("expected the passing typed matcher path to record a coverage edge")
	}
}

func TestExpectTToIgnoresANilMatcher(t *testing.T) {
	ctx, b := newCapturedContext()

	ExpectT(ctx, 42).To(nil)

	if b.failed {
		t.Fatalf("expected a nil matcher to be ignored, got %q", b.message)
	}
}

func TestExpectTToIgnoresAnExpectationWithoutAContext(t *testing.T) {
	expectT[int]{e: nil}.To(Equal(1))
	expectT[int]{e: &Expectation{actual: 42}}.To(Equal(43))
}

// The specs.* re-exports are the surface the README documents; nothing called them. Each must
// return a live matcher that both decides and explains.
func TestReExportedMatchersDecideAndExplain(t *testing.T) {
	cases := map[string]struct {
		matcher     Matcher
		passing     any
		failing     any
		wantMessage string
	}{
		"Equal":    {Equal(1), 1, 2, "expected 2 to equal 1"},
		"NotEqual": {NotEqual(1), 2, 1, "expected 1 not to equal 1"},
		"BeNil":    {BeNil(), nil, 1, "expected nil, got 1 (int)"},
		"BeTrue":   {BeTrue(), true, false, "expected true, got false (bool)"},
		"BeFalse":  {BeFalse(), false, true, "expected false, got true (bool)"},
		"Contain":  {Contain("b"), "abc", "axc", "expected axc to contain b"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if !tc.matcher.Match(tc.passing) {
				t.Fatalf("expected %v to match", tc.passing)
			}
			if tc.matcher.Match(tc.failing) {
				t.Fatalf("expected %v not to match", tc.failing)
			}
			if got := tc.matcher.FailureMessage(tc.failing); got != tc.wantMessage {
				t.Fatalf("got %q, want %q", got, tc.wantMessage)
			}
		})
	}
}

// End to end through the DSL: a passing matcher assertion inside a real spec must leave the run
// green, proving the re-exports work where users actually type them.
func TestMatchersRunThroughTheDescribeDSL(t *testing.T) {
	Describe(t, "the matcher DSL", func(s *Spec) {
		s.It("accepts an equal value", func(ctx *Context) {
			ctx.Expect(42).To(Equal(42))
		})
		s.It("accepts a typed boolean", func(ctx *Context) {
			ExpectT(ctx, true).To(BeTrue())
		})
		s.It("accepts a contained element", func(ctx *Context) {
			ctx.Expect([]string{"a", "b"}).To(Contain("b"))
		})
		s.It("accepts a nil pointer", func(ctx *Context) {
			var p *int
			ctx.Expect(p).To(BeNil())
		})
	})
}
