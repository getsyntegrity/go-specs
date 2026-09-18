package specs

import (
	"context"
	"fmt"
	"testing"
)

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

// The guards are what keep a misuse from panicking mid-suite; each one spends the Expectation and
// returns without reporting.
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

// A Context with no backend is a real state, not a hypothetical: NewContext(nil) produces one
// (asTestBackend returns nil for a nil TB), and so does a Context released back to the pool, which
// Reset(nil) leaves with a nil backend. EqualTo, ExpectT.ToEqual and Snapshot all guard it and
// return quietly; the matcher path and Expectation.ToEqual did not, so a failing assertion on such
// a context dereferenced a nil backend inside the reporting tail and panicked — surfacing as a
// crash at reportMatcherFailure rather than as the assertion the user wrote. Every assertion entry
// point must degrade the same way.
func TestAssertionsWithoutABackendReturnQuietlyInsteadOfPanicking(t *testing.T) {
	cases := map[string]func(*Context){
		"Expect(...).To":      func(ctx *Context) { ctx.Expect(42).To(Equal(7)) },
		"ExpectT(...).To":     func(ctx *Context) { ExpectT(ctx, 42).To(Equal(7)) },
		"Expect(...).ToEqual": func(ctx *Context) { ctx.Expect(42).ToEqual(7) },
		// The three that already guarded it, held to the same contract so a future refactor
		// cannot quietly drop the guard from one of them either.
		"EqualTo":              func(ctx *Context) { EqualTo(ctx, 42, 7) },
		"ExpectT(...).ToEqual": func(ctx *Context) { ExpectT(ctx, 42).ToEqual(7) },
	}
	for name, invoke := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("expected a quiet no-op without a backend, got panic: %v", r)
				}
			}()

			ctx := &Context{}
			invoke(ctx)

			// Nothing was reported, so nothing may be recorded either: a failure the runner can
			// see but no backend ever heard would stop a FailFast run with no message to show.
			if ctx.failed {
				t.Error("expected no recorded failure when the assertion had nowhere to report")
			}
		})
	}
}

// The pooled Expectation must be returned even on the guarded early-return paths; otherwise every
// assertion made against a backend-less context leaks one, and the pool stops amortising anything.
// Acquiring after the guarded call must hand back a clean Expectation, never one still carrying the
// previous actual value.
func TestGuardedAssertionsStillReleaseThePooledExpectation(t *testing.T) {
	noBackend := &Context{}
	noBackend.Expect("stale").To(Equal(1))
	noBackend.Expect("stale").ToEqual(1)
	ExpectT(noBackend, 99).To(Equal(1))

	ctx, b := newCapturedContext()
	ctx.Expect(42).To(Equal(42))

	if b.failed {
		t.Fatalf("expected a clean pooled Expectation to assert normally, got %q", b.message)
	}
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

// planBackend is a testBackend that swallows failure reports instead of forwarding them to a real
// *testing.T. A spec that fails through the real assertion path reports to its backend, and a real
// backend would fail the enclosing test — which is why TestDescribeWithReporterMarksFailedFlatSpec
// calls ctx.recordFailure() directly rather than asserting. Swallowing the report is what lets the
// end-to-end test below drive a genuine ExpectT(...).To(matcher) failure and still observe the
// reporter events it produced.
type planBackend struct {
	fatalfMsg string
}

func (p *planBackend) Helper()                              {}
func (p *planBackend) FailNow()                             {}
func (p *planBackend) Fatal(args ...any)                    { p.fatalfMsg = fmt.Sprint(args...) }
func (p *planBackend) Fatalf(format string, args ...any)    { p.fatalfMsg = fmt.Sprintf(format, args...) }
func (p *planBackend) Error(args ...any)                    {}
func (p *planBackend) Errorf(format string, args ...any)    {}
func (p *planBackend) Log(args ...any)                      {}
func (p *planBackend) Logf(format string, args ...any)      {}
func (p *planBackend) Name() string                         { return "planBackend" }
func (p *planBackend) Cleanup(func())                       {}
func (p *planBackend) Run(name string, fn func(testing.TB)) { fn(nil) }

// runFailingTypedSpec compiles a one-spec suite whose only assertion fails through the typed matcher
// path, runs it over the real execution plan, and returns what the reporter and the suite counter
// saw. specCounter is the exact type CompiledSuite.run builds SuiteEndEvent.FailedSpecs from, so
// counter.failed here is that field's value, not a proxy for it.
func runFailingTypedSpec(t *testing.T, body func(ctx *Context)) (*recordingReporter, *specCounter) {
	t.Helper()
	c := newBytecodeCompiler()
	c.PushScope("TypedMatcherSuite")
	s := &Spec{name: "TypedMatcherSuite", compiler: c}
	s.It("fails a typed matcher assertion", body)
	plan := c.TakePlan()

	rep := &recordingReporter{}
	counter := &specCounter{EventReporter: rep}
	runPlanSpecsInOrder(context.Background(), &planBackend{}, counter, plan)
	return rep, counter
}

// TestExpectTToFailureReachesTheReporterAndTheSuiteCount closes the gap the unit tests above leave.
// They assert ctx.failed, which is the mechanism; this asserts the properties the CHANGELOG actually
// promises a consumer — SpecResultEvent.Failed and SuiteEndEvent.FailedSpecs — driven by a real
// ExpectT(ctx, x).To(matcher) failure running through the compiled plan. Without it, a refactor that
// decouples ctx.failed from the reporter leaves ctx.failed true, the unit tests green, and the
// original defect back: a red run reported to every reporter-driven consumer as a passing spec.
func TestExpectTToFailureReachesTheReporterAndTheSuiteCount(t *testing.T) {
	rep, counter := runFailingTypedSpec(t, func(ctx *Context) {
		ExpectT(ctx, true).To(BeFalse())
	})

	if len(rep.specFinished) != 1 {
		t.Fatalf("expected exactly one SpecFinished event, got %d", len(rep.specFinished))
	}
	if !rep.specFinished[0].Failed {
		t.Error("expected SpecResultEvent.Failed=true; a reporter-driven consumer would be told the spec passed")
	}
	if counter.failed != 1 {
		t.Errorf("expected SuiteEndEvent.FailedSpecs=1, got %d", counter.failed)
	}
	if counter.total != 1 {
		t.Errorf("expected TotalSpecs=1, got %d", counter.total)
	}
}

// TestExpectToFailureReachesTheReporterAndTheSuiteCount is the untyped counterpart, so the two paths
// are held to the same observable contract rather than only the typed one being pinned.
func TestExpectToFailureReachesTheReporterAndTheSuiteCount(t *testing.T) {
	rep, counter := runFailingTypedSpec(t, func(ctx *Context) {
		ctx.Expect(42).To(Equal(43))
	})

	if len(rep.specFinished) != 1 {
		t.Fatalf("expected exactly one SpecFinished event, got %d", len(rep.specFinished))
	}
	if !rep.specFinished[0].Failed {
		t.Error("expected SpecResultEvent.Failed=true for the untyped path")
	}
	if counter.failed != 1 {
		t.Errorf("expected SuiteEndEvent.FailedSpecs=1, got %d", counter.failed)
	}
}
