// spec_builder_equivalence_test.go pins issue #245's T3 (spec 1: FIt/SkipIt/PendingIt) and T4
// (spec 2: ItParallel): for the same declared tree shape, *Spec (both build paths) and Builder must
// report the same set of (full name, status) outcomes and run the same set of bodies. Order is
// deliberately not compared: Builder's own finalize buffers a skip/pending mark and attaches it to
// whichever coalesced group closes next (builder.go), so its *reported* order does not always match
// declaration order even on Builder alone — see the feature doc's "What changes" section. The
// contract this issue closes is behavioral equivalence, not event-order equivalence.
//
// BeforeAll/AfterAll are intentionally not part of this table: they exist only on *Spec
// (docs/EXECUTION_ENGINES.md), so there is nothing on Builder to compare them against.
package specs

import (
	"strings"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// classifyOutcome maps one SpecResultEvent to the same closed vocabulary
// report/model.go's classifyStatus uses, restricted to what this table can produce.
func classifyOutcome(e report.SpecResultEvent) string {
	switch {
	case e.Skipped:
		return "skipped"
	case e.Pending:
		return "pending"
	case e.Failed:
		return "failed"
	case e.Filtered:
		return "filtered"
	default:
		return "passed"
	}
}

// outcomeKey is a spec's full breadcrumb — its declared enclosing scopes followed by its own name —
// exactly what both engines already put in SpecStartEvent.Path (specEventPath for *Spec,
// group.specPath/skippedPath/pendingPath for Builder). This is the "full name" the feature doc's T3
// asks for, not the bare leaf name, which is not guaranteed unique across nested scopes.
func outcomeKey(e report.SpecResultEvent) string {
	if len(e.Path) > 0 {
		return strings.Join(e.Path, "/")
	}
	return e.Name
}

// outcomesOf reduces every SpecFinished event a recordingReporter saw to {full name: status}.
func outcomesOf(rep *recordingReporter) map[string]string {
	out := make(map[string]string, len(rep.specFinished))
	for _, e := range rep.specFinished {
		out[outcomeKey(e)] = classifyOutcome(e)
	}
	return out
}

// assertSameOutcomes fails with a readable diff when got and want disagree on any key.
func assertSameOutcomes(t *testing.T, label string, got, want map[string]string) {
	t.Helper()
	for k, wantStatus := range want {
		gotStatus, ok := got[k]
		if !ok {
			t.Errorf("%s: missing outcome for %q, want status %q", label, k, wantStatus)
			continue
		}
		if gotStatus != wantStatus {
			t.Errorf("%s: outcome[%q] = %q, want %q", label, k, gotStatus, wantStatus)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("%s: unexpected outcome for %q (status %q), want it absent", label, k, got[k])
		}
	}
}

// TestFocusSkipPendingEquivalence_FlatMix declares a flat It/SkipIt/PendingIt mix (no focus) on
// *Spec (both build paths) and on Builder, and asserts they report the same outcomes and run the
// same bodies.
func TestFocusSkipPendingEquivalence_FlatMix(t *testing.T) {
	want := map[string]string{"suite/a": "passed", "suite/b": "skipped", "suite/c": "pending", "suite/d": "passed"}
	wantRan := map[string]bool{"a": true, "d": true}

	t.Run("compiler path", func(t *testing.T) {
		ran := map[string]bool{}
		rep := &recordingReporter{}
		suite := BuildSuite(nil, "suite", func(s *Spec) {
			s.It("a", func(*Context) { ran["a"] = true })
			s.SkipIt("b", func(*Context) { ran["b"] = true })
			s.PendingIt("c", func(*Context) { ran["c"] = true })
			s.It("d", func(*Context) { ran["d"] = true })
		})
		suite.Reporter = rep
		suite.Run(t)
		assertSameOutcomes(t, "compiler path", outcomesOf(rep), want)
		if len(ran) != len(wantRan) {
			t.Errorf("compiler path: ran = %v, want %v", ran, wantRan)
		}
		for k := range wantRan {
			if !ran[k] {
				t.Errorf("compiler path: %q did not run, want it to", k)
			}
		}
	})

	t.Run("registry path", func(t *testing.T) {
		ran := map[string]bool{}
		rep := &recordingReporter{}
		var suite *CompiledSuite
		Analyze(func() {
			suite = BuildSuite(nil, "suite", func(s *Spec) {
				s.It("a", func(*Context) { ran["a"] = true })
				s.SkipIt("b", func(*Context) { ran["b"] = true })
				s.PendingIt("c", func(*Context) { ran["c"] = true })
				s.It("d", func(*Context) { ran["d"] = true })
			})
		})
		suite.Reporter = rep
		suite.Run(t)
		assertSameOutcomes(t, "registry path", outcomesOf(rep), want)
		for k := range wantRan {
			if !ran[k] {
				t.Errorf("registry path: %q did not run, want it to", k)
			}
		}
	})

	t.Run("Builder", func(t *testing.T) {
		ran := map[string]bool{}
		rep := &recordingReporter{}
		b := NewBuilder()
		b.Describe("suite", func() {
			b.It("a", func(*Context) { ran["a"] = true })
			b.SkipIt("b", func(*Context) { ran["b"] = true })
			b.PendingIt("c", func(*Context) { ran["c"] = true })
			b.It("d", func(*Context) { ran["d"] = true })
		})
		NewRunnerWithReporter(b.Build(), "suite", rep).Run(t)
		assertSameOutcomes(t, "Builder", outcomesOf(rep), want)
		for k := range wantRan {
			if !ran[k] {
				t.Errorf("Builder: %q did not run, want it to", k)
			}
		}
	})
}

// TestFocusSkipPendingEquivalence_Focused declares a mix with one FIt: only the focused spec
// should appear in outcomes at all (unfocused It/SkipIt/PendingIt are dropped, not merely skipped),
// on both *Spec build paths and Builder.
func TestFocusSkipPendingEquivalence_Focused(t *testing.T) {
	want := map[string]string{"suite/focused": "passed"}
	wantRan := map[string]bool{"focused": true}

	t.Run("compiler path", func(t *testing.T) {
		ran := map[string]bool{}
		rep := &recordingReporter{}
		suite := BuildSuite(nil, "suite", func(s *Spec) {
			s.It("a", func(*Context) { ran["a"] = true })
			s.FIt("focused", func(*Context) { ran["focused"] = true })
			s.SkipIt("b", func(*Context) { ran["b"] = true })
			s.PendingIt("c", func(*Context) { ran["c"] = true })
		})
		suite.Reporter = rep
		suite.Run(t)
		assertSameOutcomes(t, "compiler path", outcomesOf(rep), want)
		if len(ran) != 1 || !ran["focused"] {
			t.Errorf("compiler path: ran = %v, want %v", ran, wantRan)
		}
	})

	t.Run("registry path", func(t *testing.T) {
		ran := map[string]bool{}
		rep := &recordingReporter{}
		var suite *CompiledSuite
		Analyze(func() {
			suite = BuildSuite(nil, "suite", func(s *Spec) {
				s.It("a", func(*Context) { ran["a"] = true })
				s.FIt("focused", func(*Context) { ran["focused"] = true })
				s.SkipIt("b", func(*Context) { ran["b"] = true })
				s.PendingIt("c", func(*Context) { ran["c"] = true })
			})
		})
		suite.Reporter = rep
		suite.Run(t)
		assertSameOutcomes(t, "registry path", outcomesOf(rep), want)
		if len(ran) != 1 || !ran["focused"] {
			t.Errorf("registry path: ran = %v, want %v", ran, wantRan)
		}
	})

	t.Run("Builder", func(t *testing.T) {
		ran := map[string]bool{}
		rep := &recordingReporter{}
		b := NewBuilder()
		b.Describe("suite", func() {
			b.It("a", func(*Context) { ran["a"] = true })
			b.FIt("focused", func(*Context) { ran["focused"] = true })
			b.SkipIt("b", func(*Context) { ran["b"] = true })
			b.PendingIt("c", func(*Context) { ran["c"] = true })
		})
		NewRunnerWithReporter(b.Build(), "suite", rep).Run(t)
		assertSameOutcomes(t, "Builder", outcomesOf(rep), want)
		if len(ran) != 1 || !ran["focused"] {
			t.Errorf("Builder: ran = %v, want %v", ran, wantRan)
		}
	})
}

// TestFocusSkipPendingEquivalence_Nested declares nested Describe/When groups, each mixing a real
// spec with a skip and a pending, plus BeforeEach/AfterEach — exercising both engines' hook
// flattening alongside compile-time marks, and the outcomeKey breadcrumb disambiguation two
// same-named leaves in different scopes need.
func TestFocusSkipPendingEquivalence_Nested(t *testing.T) {
	want := map[string]string{
		"suite/outer/spec":          "passed",
		"suite/outer/skip":          "skipped",
		"suite/outer/inner/spec":    "passed",
		"suite/outer/inner/pending": "pending",
	}
	wantRan := map[string]bool{"outer.spec": true, "inner.spec": true}

	t.Run("compiler path", func(t *testing.T) {
		ran := map[string]bool{}
		var hookOrder []string
		rep := &recordingReporter{}
		suite := BuildSuite(nil, "suite", func(s *Spec) {
			s.Describe("outer", func(o *Spec) {
				o.BeforeEach(func(*Context) { hookOrder = append(hookOrder, "before") })
				o.AfterEach(func(*Context) { hookOrder = append(hookOrder, "after") })
				o.It("spec", func(*Context) { ran["outer.spec"] = true })
				o.SkipIt("skip", func(*Context) { ran["outer.skip"] = true })
				o.When("inner", func(w *Spec) {
					w.It("spec", func(*Context) { ran["inner.spec"] = true })
					w.PendingIt("pending", func(*Context) { ran["inner.pending"] = true })
				})
			})
		})
		suite.Reporter = rep
		suite.Run(t)
		assertSameOutcomes(t, "compiler path", outcomesOf(rep), want)
		for k := range wantRan {
			if !ran[k] {
				t.Errorf("compiler path: %q did not run, want it to", k)
			}
		}
		if len(hookOrder) != 4 {
			t.Errorf("compiler path: BeforeEach/AfterEach ran %d times around 2 specs in scope, want 4: %v", len(hookOrder), hookOrder)
		}
	})

	t.Run("registry path", func(t *testing.T) {
		ran := map[string]bool{}
		rep := &recordingReporter{}
		var suite *CompiledSuite
		Analyze(func() {
			suite = BuildSuite(nil, "suite", func(s *Spec) {
				s.Describe("outer", func(o *Spec) {
					o.It("spec", func(*Context) { ran["outer.spec"] = true })
					o.SkipIt("skip", func(*Context) { ran["outer.skip"] = true })
					o.When("inner", func(w *Spec) {
						w.It("spec", func(*Context) { ran["inner.spec"] = true })
						w.PendingIt("pending", func(*Context) { ran["inner.pending"] = true })
					})
				})
			})
		})
		suite.Reporter = rep
		suite.Run(t)
		assertSameOutcomes(t, "registry path", outcomesOf(rep), want)
		for k := range wantRan {
			if !ran[k] {
				t.Errorf("registry path: %q did not run, want it to", k)
			}
		}
	})

	t.Run("Builder", func(t *testing.T) {
		ran := map[string]bool{}
		rep := &recordingReporter{}
		b := NewBuilder()
		b.Describe("suite", func() {
			b.Describe("outer", func() {
				b.It("spec", func(*Context) { ran["outer.spec"] = true })
				b.SkipIt("skip", func(*Context) { ran["outer.skip"] = true })
				b.Describe("inner", func() {
					b.It("spec", func(*Context) { ran["inner.spec"] = true })
					b.PendingIt("pending", func(*Context) { ran["inner.pending"] = true })
				})
			})
		})
		NewRunnerWithReporter(b.Build(), "suite", rep).Run(t)
		assertSameOutcomes(t, "Builder", outcomesOf(rep), want)
		for k := range wantRan {
			if !ran[k] {
				t.Errorf("Builder: %q did not run, want it to", k)
			}
		}
	})
}

// TestItParallelEquivalence_MixedTree declares a mixed It/ItParallel/SkipIt/PendingIt tree, nested
// under a When, on *Spec (both build paths) and on Builder, and asserts they report the same
// (full name, status) outcomes and run the same bodies (issue #245's T4) — same as the
// focus/skip/pending table above, but for ItParallel. Order is not compared, for the same reason:
// neither engine promises reporting the same order for a coalesced/parallel group as its
// declaration order relative to the rest of the suite, only that the group's own specs come out in
// declaration order relative to each other (pinned separately by
// TestSpecItParallel_ReporterEventOrderIsDeclarationOrder).
func TestItParallelEquivalence_MixedTree(t *testing.T) {
	want := map[string]string{
		"suite/before":        "passed",
		"suite/group/a":       "passed",
		"suite/group/b":       "passed",
		"suite/group/skip":    "skipped",
		"suite/group/pending": "pending",
		"suite/after":         "passed",
	}
	wantRan := map[string]bool{"before": true, "a": true, "b": true, "after": true}

	t.Run("compiler path", func(t *testing.T) {
		ran := map[string]bool{}
		var mu sync.Mutex
		record := func(k string) { mu.Lock(); ran[k] = true; mu.Unlock() }
		rep := &recordingReporter{}
		suite := BuildSuite(nil, "suite", func(s *Spec) {
			s.It("before", func(*Context) { record("before") })
			s.When("group", func(w *Spec) {
				w.ItParallel("a", func(*Context) { record("a") })
				w.ItParallel("b", func(*Context) { record("b") })
				w.SkipIt("skip", func(*Context) { record("skip") })
				w.PendingIt("pending", func(*Context) { record("pending") })
			})
			s.It("after", func(*Context) { record("after") })
		})
		suite.Reporter = rep
		suite.Run(t)
		assertSameOutcomes(t, "compiler path", outcomesOf(rep), want)
		for k := range wantRan {
			if !ran[k] {
				t.Errorf("compiler path: %q did not run, want it to", k)
			}
		}
	})

	t.Run("registry path", func(t *testing.T) {
		ran := map[string]bool{}
		var mu sync.Mutex
		record := func(k string) { mu.Lock(); ran[k] = true; mu.Unlock() }
		rep := &recordingReporter{}
		var suite *CompiledSuite
		Analyze(func() {
			suite = BuildSuite(nil, "suite", func(s *Spec) {
				s.It("before", func(*Context) { record("before") })
				s.When("group", func(w *Spec) {
					w.ItParallel("a", func(*Context) { record("a") })
					w.ItParallel("b", func(*Context) { record("b") })
					w.SkipIt("skip", func(*Context) { record("skip") })
					w.PendingIt("pending", func(*Context) { record("pending") })
				})
				s.It("after", func(*Context) { record("after") })
			})
		})
		suite.Reporter = rep
		suite.Run(t)
		assertSameOutcomes(t, "registry path", outcomesOf(rep), want)
		for k := range wantRan {
			if !ran[k] {
				t.Errorf("registry path: %q did not run, want it to", k)
			}
		}
	})

	t.Run("Builder", func(t *testing.T) {
		ran := map[string]bool{}
		var mu sync.Mutex
		record := func(k string) { mu.Lock(); ran[k] = true; mu.Unlock() }
		rep := &recordingReporter{}
		b := NewBuilder()
		b.Describe("suite", func() {
			b.It("before", func(*Context) { record("before") })
			b.Describe("group", func() {
				b.ItParallel("a", func(*Context) { record("a") })
				b.ItParallel("b", func(*Context) { record("b") })
				b.SkipIt("skip", func(*Context) { record("skip") })
				b.PendingIt("pending", func(*Context) { record("pending") })
			})
			b.It("after", func(*Context) { record("after") })
		})
		NewRunnerWithReporter(b.Build(), "suite", rep).Run(t)
		assertSameOutcomes(t, "Builder", outcomesOf(rep), want)
		for k := range wantRan {
			if !ran[k] {
				t.Errorf("Builder: %q did not run, want it to", k)
			}
		}
	})
}
