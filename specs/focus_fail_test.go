// focus_fail_test.go pins issue #273's core policy: a committed FIt fails the enclosing test,
// always, unless GO_SPECS_ALLOW_FOCUS=1 opts out — and every spec that focus excludes (an ordinary
// It, and now also a SkipIt/PendingIt/ItParallel that focus used to drop without a trace) is
// reported Filtered, exactly once, in every engine and under RunShard. These tests use spyTB
// (below) to drive the real tb.Errorf call path in-process, without failing the test process
// itself; focus_fail_real_process_test.go covers the same policy end-to-end, in a real subprocess,
// where the enclosing test's actual pass/fail is observable.
package specs

import (
	"fmt"
	"strings"
	"testing"
)

// spyTB wraps a real testing.TB (the outer test's own t, embedded so every other promoted method —
// Name, Cleanup, Log, ... — still resolves against a live backend instead of panicking on a nil
// interface), capturing Errorf calls instead of forwarding them: it lets a test drive code that
// calls tb.Errorf on a policy failure (reportFocusPolicy, in both engines) without that Errorf call
// failing the outer test process itself.
type spyTB struct {
	testing.TB
	errors []string
}

func (s *spyTB) Helper() {}
func (s *spyTB) Errorf(format string, args ...any) {
	s.errors = append(s.errors, fmt.Sprintf(format, args...))
}
func (s *spyTB) failed() bool { return len(s.errors) > 0 }
func (s *spyTB) soleMessage(t *testing.T) string {
	t.Helper()
	if len(s.errors) != 1 {
		t.Fatalf("expected exactly one Errorf call, got %d: %v", len(s.errors), s.errors)
	}
	return s.errors[0]
}

// TestFocusFailPolicy_CompilerPathFailsWithoutOptOut pins the default: a suite built through the
// bytecode compiler (top-level Describe/BuildSuite, no Analyze) with an active FIt fails the
// enclosing test via exactly one Errorf, naming the focused/excluded counts, while the focused spec
// still runs and reports its own result.
func TestFocusFailPolicy_CompilerPathFailsWithoutOptOut(t *testing.T) {
	var focusedRan, plainRan bool
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.It("plain", func(*Context) { plainRan = true })
		s.FIt("focused", func(*Context) { focusedRan = true })
		s.SkipIt("skip", nil)
		s.PendingIt("pending", nil)
	})
	spy := &spyTB{TB: t}
	suite.Run(spy)

	if !focusedRan {
		t.Error("the focused spec must still run")
	}
	if plainRan {
		t.Error("the unfocused spec must not run")
	}
	msg := spy.soleMessage(t)
	for _, want := range []string{"go-specs:", "1 focused spec(s)", "3 spec(s) excluded", "GO_SPECS_ALLOW_FOCUS=1", "focused"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
}

// TestFocusFailPolicy_CompilerPathOptOut pins the only opt-out: GO_SPECS_ALLOW_FOCUS=1 disables the
// Errorf call above, without disabling the focus filter itself.
func TestFocusFailPolicy_CompilerPathOptOut(t *testing.T) {
	t.Setenv(allowFocusEnvVar, "1")
	var focusedRan, plainRan bool
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.It("plain", func(*Context) { plainRan = true })
		s.FIt("focused", func(*Context) { focusedRan = true })
	})
	spy := &spyTB{TB: t}
	suite.Run(spy)

	if spy.failed() {
		t.Errorf("expected no Errorf call under GO_SPECS_ALLOW_FOCUS=1, got %v", spy.errors)
	}
	if !focusedRan || plainRan {
		t.Errorf("focus filtering itself must be unaffected by the opt-out: focusedRan=%v plainRan=%v", focusedRan, plainRan)
	}
}

// TestFocusFailPolicy_RegistryPathIncludesFileLine pins that the Analyze/registry arena path names
// the focused spec's own file:line in the message when caller-location capture is enabled
// (SetCaptureCallerLocation — off by default, for allocation reasons unrelated to this feature; see
// caller_location_test.go): the arena node already carries it in that case, at zero extra cost,
// where the bytecode-compiler path (tested above) names only the breadcrumb either way.
func TestFocusFailPolicy_RegistryPathIncludesFileLine(t *testing.T) {
	withCaptureCallerLocation(t, true)
	var suite *CompiledSuite
	Analyze(func() {
		suite = BuildSuite(nil, "suite", func(s *Spec) {
			s.It("plain", func(*Context) {})
			s.FIt("focused", func(*Context) {})
		})
	})
	spy := &spyTB{TB: t}
	suite.Run(spy)

	msg := spy.soleMessage(t)
	if !strings.Contains(msg, "focus_fail_test.go:") {
		t.Errorf("message %q does not name this file's own location", msg)
	}
	if !strings.Contains(msg, "suite/focused") {
		t.Errorf("message %q does not name the focused spec's breadcrumb", msg)
	}
}

// TestFocusFailPolicy_BuilderPath pins the same policy on the Builder/Runner engine.
func TestFocusFailPolicy_BuilderPath(t *testing.T) {
	var focusedRan, plainRan bool
	b := NewBuilder()
	b.It("plain", func(*Context) { plainRan = true })
	b.FIt("focused", func(*Context) { focusedRan = true })
	prog := b.Build()

	spy := &spyTB{TB: t}
	NewRunner(prog).Run(spy)

	if !focusedRan || plainRan {
		t.Errorf("focusedRan=%v plainRan=%v, want true/false", focusedRan, plainRan)
	}
	msg := spy.soleMessage(t)
	if !strings.Contains(msg, "1 focused spec(s)") || !strings.Contains(msg, "1 spec(s) excluded") {
		t.Errorf("message %q does not name the right counts", msg)
	}
}

// TestFocusFailPolicy_BuilderPathOptOut mirrors TestFocusFailPolicy_CompilerPathOptOut for Builder.
func TestFocusFailPolicy_BuilderPathOptOut(t *testing.T) {
	t.Setenv(allowFocusEnvVar, "1")
	b := NewBuilder()
	b.It("plain", func(*Context) {})
	b.FIt("focused", func(*Context) {})
	prog := b.Build()

	spy := &spyTB{TB: t}
	NewRunner(prog).Run(spy)
	if spy.failed() {
		t.Errorf("expected no Errorf call under GO_SPECS_ALLOW_FOCUS=1, got %v", spy.errors)
	}
}

// TestFocusFailPolicy_NestedFocusDropsSkipAndPendingAsFiltered exercises a nested focus alongside
// SkipIt/PendingIt marks a focus previously dropped without a trace (issue #273's explicit scope):
// both build paths behind *Spec must report every excluded spec — the plain It, the SkipIt mark and
// the PendingIt mark — as Filtered, none of them as Skipped/Pending, while the focused spec (nested
// two levels down) still runs and reports passed.
func TestFocusFailPolicy_NestedFocusDropsSkipAndPendingAsFiltered(t *testing.T) {
	t.Setenv(allowFocusEnvVar, "1") // this test is about reporting, not the fail policy itself

	build := func(t *testing.T, fn func(fn func(*Spec)) *CompiledSuite) {
		t.Helper()
		rep := &recordingReporter{}
		suite := fn(func(s *Spec) {
			s.It("top-level plain", func(*Context) {})
			s.SkipIt("top-level skip", nil)
			s.When("nested", func(w *Spec) {
				w.PendingIt("nested pending", nil)
				w.FIt("nested focused", func(*Context) {})
			})
		})
		suite.Reporter = rep
		suite.Run(t)

		byName := map[string]bool{} // name -> Filtered
		for _, e := range rep.specFinished {
			byName[e.Name] = e.Filtered
		}
		for _, name := range []string{"top-level plain", "top-level skip", "nested pending"} {
			filtered, ok := byName[name]
			if !ok {
				t.Errorf("%q was not reported at all, want it reported Filtered", name)
				continue
			}
			if !filtered {
				t.Errorf("%q reported Filtered=false, want true", name)
			}
		}
		if filtered, ok := byName["nested focused"]; !ok || filtered {
			t.Errorf("nested focused = (ok=%v, filtered=%v), want (true, false): it must run", ok, filtered)
		}
	}

	t.Run("compiler path", func(t *testing.T) {
		build(t, func(fn func(*Spec)) *CompiledSuite { return BuildSuite(nil, "suite", fn) })
	})
	t.Run("registry path", func(t *testing.T) {
		var suite *CompiledSuite
		build(t, func(fn func(*Spec)) *CompiledSuite {
			Analyze(func() { suite = BuildSuite(nil, "suite", fn) })
			return suite
		})
	})
}

// TestFocusFailPolicy_RunShardFailsEveryShardAndReportsMarksOnce pins RunShard's contract (issue
// #273): every shard's own RunShard call fails its own enclosing test when focus is active (each
// shard is ordinarily its own CI job), but the excluded specs are reported Filtered exactly once
// across the union of every shard — shard 0's job, the same deterministic rule already used for
// compile-time SkipIt/PendingIt marks (shardSelection.reportsMarks).
func TestFocusFailPolicy_RunShardFailsEveryShardAndReportsMarksOnce(t *testing.T) {
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.It("a", func(*Context) {})
		s.It("b", func(*Context) {})
		s.FIt("focused", func(*Context) {})
	})

	const shardCount = 3
	var filteredTotal, allEventsTotal int
	for shard := 0; shard < shardCount; shard++ {
		rep := &recordingReporter{}
		suite.Reporter = rep
		spy := &spyTB{TB: t}
		suite.RunShard(spy, shard, shardCount)

		if !spy.failed() {
			t.Errorf("shard %d: expected RunShard to fail its own enclosing test, it did not", shard)
		}
		for _, e := range rep.specFinished {
			allEventsTotal++
			if e.Filtered {
				filteredTotal++
			}
		}
	}
	if filteredTotal != 2 {
		t.Errorf("filteredTotal = %d, want 2 (a and b, each exactly once across every shard)", filteredTotal)
	}
	if allEventsTotal != 3 {
		t.Errorf("allEventsTotal = %d, want 3 (a, b filtered once + focused running once)", allEventsTotal)
	}
}

// TestFocusFailPolicy_PackageRunShardFailsEveryShardAndReportsMarksOnce is the RunShard test above
// for the Builder engine's package-level specs.RunShard/RunShardWithReporter.
func TestFocusFailPolicy_PackageRunShardFailsEveryShardAndReportsMarksOnce(t *testing.T) {
	b := NewBuilder()
	b.It("a", func(*Context) {})
	b.It("b", func(*Context) {})
	b.FIt("focused", func(*Context) {})
	prog := b.Build()

	const shardCount = 3
	var filteredTotal, allEventsTotal int
	for shard := 0; shard < shardCount; shard++ {
		rep := &recordingReporter{}
		spy := &spyTB{TB: t}
		RunShardWithReporter(prog, spy, shard, shardCount, "suite", rep)

		if !spy.failed() {
			t.Errorf("shard %d: expected RunShard to fail its own enclosing test, it did not", shard)
		}
		for _, e := range rep.specFinished {
			allEventsTotal++
			if e.Filtered {
				filteredTotal++
			}
		}
	}
	if filteredTotal != 2 {
		t.Errorf("filteredTotal = %d, want 2 (a and b, each exactly once across every shard)", filteredTotal)
	}
	if allEventsTotal != 3 {
		t.Errorf("allEventsTotal = %d, want 3 (a, b filtered once + focused running once)", allEventsTotal)
	}
}

// TestFocusFailPolicy_RunShardOptOut pins that GO_SPECS_ALLOW_FOCUS=1 suppresses the failure on
// every shard, for both RunShard entry points.
func TestFocusFailPolicy_RunShardOptOut(t *testing.T) {
	t.Setenv(allowFocusEnvVar, "1")

	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.It("a", func(*Context) {})
		s.FIt("focused", func(*Context) {})
	})
	for shard := 0; shard < 2; shard++ {
		spy := &spyTB{TB: t}
		suite.RunShard(spy, shard, 2)
		if spy.failed() {
			t.Errorf("CompiledSuite.RunShard shard %d: expected no Errorf under the opt-out, got %v", shard, spy.errors)
		}
	}

	b := NewBuilder()
	b.It("a", func(*Context) {})
	b.FIt("focused", func(*Context) {})
	prog := b.Build()
	for shard := 0; shard < 2; shard++ {
		spy := &spyTB{TB: t}
		RunShard(prog, spy, shard, 2)
		if spy.failed() {
			t.Errorf("specs.RunShard shard %d: expected no Errorf under the opt-out, got %v", shard, spy.errors)
		}
	}
}

// TestFocusFailPolicy_NoFocusNeverCallsErrorf pins the base case: a suite with no FIt at all never
// calls Errorf through reportFocusPolicy, in either engine, whatever GO_SPECS_ALLOW_FOCUS is set to.
func TestFocusFailPolicy_NoFocusNeverCallsErrorf(t *testing.T) {
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.It("a", func(*Context) {})
	})
	spy := &spyTB{TB: t}
	suite.Run(spy)
	if spy.failed() {
		t.Errorf("expected no Errorf call for a suite with no FIt, got %v", spy.errors)
	}

	b := NewBuilder()
	b.It("a", func(*Context) {})
	prog := b.Build()
	spy2 := &spyTB{TB: t}
	NewRunner(prog).Run(spy2)
	if spy2.failed() {
		t.Errorf("expected no Errorf call for a suite with no FIt (Builder), got %v", spy2.errors)
	}
}
