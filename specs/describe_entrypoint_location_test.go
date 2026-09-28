package specs

import (
	"runtime"
	"strings"
	"testing"
)

// captureCallSite returns the file and line of its own caller (runtime.Caller(1), one frame up from
// this function), so a test can record "the next statement is the user's declaration site" without
// hardcoding a line number. It fails the test if the frame is unavailable.
func captureCallSite(t *testing.T) (file string, line int) {
	t.Helper()
	_, file, line, ok := runtime.Caller(1)
	if !ok {
		t.Fatalf("runtime.Caller(1) failed")
	}
	return file, line
}

// TestDescribeEntryPointsPinCallerLocationUnderRegistry is the safety net for issue #276: extracting
// a shared internal implementation for Describe, DescribeWithReporter, DescribeFlat and
// DescribeFlatWithReporter must not change what callerLocation reports for the root node built via
// the Analyze/registry path. Each entry point calls callerLocation(2) directly today, expecting to
// land on the user's call site immediately below captureCallSite, not on any wrapper frame; a shared
// implementation that forwards a fixed skip without adjusting it for the extra call frame would
// instead point at the wrapper itself (spec.go), which this test catches.
//
// For every entry point, captureCallSite(t) is the statement immediately preceding the entry-point
// call, so the entry point's declaration line is always exactly one line below the captured one —
// this holds regardless of which entry point is under test or what is declared earlier in the
// subtest, so the expectation never depends on a hand-counted line offset.
func TestDescribeEntryPointsPinCallerLocationUnderRegistry(t *testing.T) {
	withCaptureCallerLocation(t, true)

	t.Run("Describe", func(t *testing.T) {
		var wantFile string
		var wantLine int
		suite := Analyze(func() {
			wantFile, wantLine = captureCallSite(t)
			wantLine += 2
			Describe(nil, "Root", func(s *Spec) {
				s.It("leaf", func(ctx *Context) {})
			})
		})
		assertRootLocation(t, suite, wantFile, wantLine)
	})

	t.Run("DescribeWithReporter", func(t *testing.T) {
		rep := &recordingReporter{}
		var wantFile string
		var wantLine int
		suite := Analyze(func() {
			wantFile, wantLine = captureCallSite(t)
			wantLine += 2
			DescribeWithReporter(nil, "Root", rep, func(s *Spec) {
				s.It("leaf", func(ctx *Context) {})
			})
		})
		assertRootLocation(t, suite, wantFile, wantLine)
	})

	t.Run("DescribeFlat", func(t *testing.T) {
		var wantFile string
		var wantLine int
		suite := Analyze(func() {
			wantFile, wantLine = captureCallSite(t)
			wantLine += 2
			DescribeFlat(nil, "Root", func(s *Spec) {
				s.It("leaf", func(ctx *Context) {})
			})
		})
		assertRootLocation(t, suite, wantFile, wantLine)
	})

	t.Run("DescribeFlatWithReporter", func(t *testing.T) {
		rep := &recordingReporter{}
		var wantFile string
		var wantLine int
		suite := Analyze(func() {
			wantFile, wantLine = captureCallSite(t)
			wantLine += 2
			DescribeFlatWithReporter(nil, "Root", rep, func(s *Spec) {
				s.It("leaf", func(ctx *Context) {})
			})
		})
		assertRootLocation(t, suite, wantFile, wantLine)
	})
}

// assertRootLocation asserts the root node's declaration site (built via the Analyze/registry path)
// matches wantFile/wantLine exactly, and independently that it lands in this test file rather than in
// spec.go or registry.go, so a wrapper-attribution bug is caught even if wantLine were ever computed
// wrong.
func assertRootLocation(t *testing.T, suite *SuiteTree, wantFile string, wantLine int) {
	t.Helper()
	if suite == nil || suite.Arena == nil {
		t.Fatalf("expected a built suite tree, got %#v", suite)
	}
	rootID := suite.Arena.Children[suite.RootID][0]
	root := &suite.Arena.Nodes[rootID]
	if !strings.HasSuffix(root.File, "describe_entrypoint_location_test.go") {
		t.Fatalf("expected root declared in this test file, got %q", root.File)
	}
	if root.File != wantFile {
		t.Fatalf("expected root file %q, got %q", wantFile, root.File)
	}
	if root.Line != wantLine {
		t.Fatalf("expected root line %d, got %d", wantLine, root.Line)
	}
}
