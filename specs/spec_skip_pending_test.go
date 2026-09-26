// spec_skip_pending_test.go pins issue #245's T1: SkipIt/PendingIt on *Spec must never run fn and
// must report StatusSkipped/StatusPending the same way Builder.SkipIt/PendingIt already do (see
// builder.go's finalize and pending_test.go). Both Spec build paths are covered: the bytecode
// compiler (top-level Describe/BuildSuite) and the Analyze/registry path.
package specs

import (
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// TestSpecSkipItNeverRunsBody_CompilerPath pins SkipIt on the bytecode-compiler build path
// (currentRegistry() == nil): the body must never run, and a real spec elsewhere in the suite must
// still run normally.
func TestSpecSkipItNeverRunsBody_CompilerPath(t *testing.T) {
	skippedRan := false
	otherRan := false
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.SkipIt("not ready", func(*Context) { skippedRan = true })
		s.It("runs", func(*Context) { otherRan = true })
	})
	suite.Run(t)
	if skippedRan {
		t.Fatal("SkipIt's body must never run")
	}
	if !otherRan {
		t.Fatal("a sibling It must still run")
	}
}

// TestSpecSkipItNeverRunsBody_RegistryPath is the Analyze/registry-path equivalent.
func TestSpecSkipItNeverRunsBody_RegistryPath(t *testing.T) {
	skippedRan := false
	otherRan := false
	var suite *CompiledSuite
	Analyze(func() {
		suite = BuildSuite(nil, "suite", func(s *Spec) {
			s.SkipIt("not ready", func(*Context) { skippedRan = true })
			s.It("runs", func(*Context) { otherRan = true })
		})
	})
	suite.Run(t)
	if skippedRan {
		t.Fatal("SkipIt's body must never run (registry path)")
	}
	if !otherRan {
		t.Fatal("a sibling It must still run (registry path)")
	}
}

// TestSpecSkipItReportsStatusSkipped pins the reported event shape: SpecStarted immediately
// followed by SpecFinished{Skipped: true}, no subtest identity assumptions, mirroring
// runner.go's reporterObserver.specSkipped for Builder.
func TestSpecSkipItReportsStatusSkipped(t *testing.T) {
	rep := &recordingReporter{}
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.SkipIt("not ready", nil)
	})
	suite.Reporter = rep
	suite.Run(t)

	if len(rep.specFinished) != 1 {
		t.Fatalf("expected exactly one SpecFinished event, got %d: %+v", len(rep.specFinished), rep.specFinished)
	}
	e := rep.specFinished[0]
	if e.Name != "not ready" {
		t.Errorf("Name = %q, want %q", e.Name, "not ready")
	}
	if !e.Skipped {
		t.Error("expected Skipped: true")
	}
	if e.Pending || e.Failed || e.Filtered {
		t.Errorf("expected only Skipped set, got %+v", e)
	}
}

// TestSpecPendingItNeverRunsBody_CompilerPath mirrors TestSpecSkipItNeverRunsBody_CompilerPath for
// PendingIt: fn may be nil, and when non-nil it must still never run.
func TestSpecPendingItNeverRunsBody_CompilerPath(t *testing.T) {
	pendingRan := false
	otherRan := false
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.PendingIt("not implemented yet", func(*Context) { pendingRan = true })
		s.It("runs", func(*Context) { otherRan = true })
	})
	suite.Run(t)
	if pendingRan {
		t.Fatal("PendingIt's body must never run")
	}
	if !otherRan {
		t.Fatal("a sibling It must still run")
	}
}

// TestSpecPendingItNeverRunsBody_RegistryPath is the Analyze/registry-path equivalent.
func TestSpecPendingItNeverRunsBody_RegistryPath(t *testing.T) {
	pendingRan := false
	var suite *CompiledSuite
	Analyze(func() {
		suite = BuildSuite(nil, "suite", func(s *Spec) {
			s.PendingIt("not implemented yet", func(*Context) { pendingRan = true })
		})
	})
	suite.Run(t)
	if pendingRan {
		t.Fatal("PendingIt's body must never run (registry path)")
	}
}

// TestSpecPendingItNilFnNeverPanics pins that fn may be nil (a pending spec often has no
// implementation sketch yet), on both build paths.
func TestSpecPendingItNilFnNeverPanics(t *testing.T) {
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.PendingIt("no body yet", nil)
	})
	suite.Run(t) // must not panic

	var analyzed *CompiledSuite
	Analyze(func() {
		analyzed = BuildSuite(nil, "suite", func(s *Spec) {
			s.PendingIt("no body yet", nil)
		})
	})
	analyzed.Run(t) // must not panic
}

// TestSpecPendingItReportsStatusPending mirrors TestSpecSkipItReportsStatusSkipped for Pending,
// and pins that CompiledSuite now tallies PendingSpecs on SuiteEndEvent (issue #245), matching
// runner.go's reporterObserver.pending for Builder (report/events.go's PendingSpecs field already
// existed for #208 but the ExecutionPlan/CompiledSuite engine never populated it before).
func TestSpecPendingItReportsStatusPending(t *testing.T) {
	rep := &recordingReporter{}
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.PendingIt("not implemented yet", nil)
		s.It("runs", func(*Context) {})
	})
	suite.Reporter = rep
	suite.Run(t)

	var found *report.SpecResultEvent
	for i := range rep.specFinished {
		if rep.specFinished[i].Name == "not implemented yet" {
			found = &rep.specFinished[i]
		}
	}
	if found == nil {
		t.Fatalf("no SpecFinished event for the pending spec; got %+v", rep.specFinished)
	}
	if !found.Pending {
		t.Error("expected Pending: true")
	}
	if found.Skipped || found.Failed || found.Filtered {
		t.Errorf("expected only Pending set, got %+v", found)
	}
	if len(rep.suiteFinished) != 1 {
		t.Fatalf("expected exactly one SuiteFinished event, got %d", len(rep.suiteFinished))
	}
	if got := rep.suiteFinished[0].PendingSpecs; got != 1 {
		t.Errorf("SuiteEndEvent.PendingSpecs = %d, want 1", got)
	}
}

// TestSpecSkipItOnSpecWithNoBuildTargetPanics mirrors Spec.It's requireBuildTarget contract
// (issue #151): a Spec constructed directly, with neither a compiler nor a registry, must panic
// rather than silently discard the registration.
func TestSpecSkipItOnSpecWithNoBuildTargetPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected SkipIt on a build-target-less Spec to panic")
		}
	}()
	(&Spec{}).SkipIt("x", nil)
}

func TestSpecPendingItOnSpecWithNoBuildTargetPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected PendingIt on a build-target-less Spec to panic")
		}
	}()
	(&Spec{}).PendingIt("x", nil)
}
