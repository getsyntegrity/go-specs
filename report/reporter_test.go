package report

import (
	"strings"
	"testing"
	"time"
)

// TestReporterPrintsDuration proves the reference text Reporter surfaces SpecResultEvent.Duration
// and SuiteEndEvent.Duration in its output, not just the fields that existed before this PR.
func TestReporterPrintsDuration(t *testing.T) {
	var buf strings.Builder
	r := New(&buf)

	r.SuiteStarted(SuiteStartEvent{Name: "Suite"})
	r.SpecStarted(SpecStartEvent{Name: "spec"})
	r.SpecFinished(SpecResultEvent{SpecStartEvent: SpecStartEvent{Name: "spec"}, Duration: 5 * time.Millisecond})
	r.SuiteFinished(SuiteEndEvent{Name: "Suite", Duration: 5 * time.Millisecond})

	out := buf.String()
	if !strings.Contains(out, "SpecFinished spec [] failed=false skipped=false pending=false duration=5ms") {
		t.Fatalf("expected SpecFinished line to include duration=5ms, got %q", out)
	}
	if !strings.Contains(out, "SuiteFinished Suite duration=5ms skipped=0 pending=0") {
		t.Fatalf("expected SuiteFinished line to include duration=5ms, got %q", out)
	}
}

// TestReporterPrintsSkipped proves the reference text Reporter surfaces SpecResultEvent.Skipped and
// SuiteEndEvent.SkippedSpecs, not just the fields that existed before this PR.
func TestReporterPrintsSkipped(t *testing.T) {
	var buf strings.Builder
	r := New(&buf)

	r.SuiteStarted(SuiteStartEvent{Name: "Suite"})
	r.SpecStarted(SpecStartEvent{Name: "spec"})
	r.SpecFinished(SpecResultEvent{SpecStartEvent: SpecStartEvent{Name: "spec"}, Skipped: true})
	r.SuiteFinished(SuiteEndEvent{Name: "Suite", SkippedSpecs: 1})

	out := buf.String()
	if !strings.Contains(out, "SpecFinished spec [] failed=false skipped=true pending=false duration=0s") {
		t.Fatalf("expected SpecFinished line to include skipped=true, got %q", out)
	}
	if !strings.Contains(out, "SuiteFinished Suite duration=0s skipped=1 pending=0") {
		t.Fatalf("expected SuiteFinished line to include skipped=1, got %q", out)
	}
}

// TestReporterPrintsPending proves the reference text Reporter surfaces SpecResultEvent.Pending
// and SuiteEndEvent.PendingSpecs, distinct from Skipped/SkippedSpecs.
func TestReporterPrintsPending(t *testing.T) {
	var buf strings.Builder
	r := New(&buf)

	r.SuiteStarted(SuiteStartEvent{Name: "Suite"})
	r.SpecStarted(SpecStartEvent{Name: "spec"})
	r.SpecFinished(SpecResultEvent{SpecStartEvent: SpecStartEvent{Name: "spec"}, Pending: true})
	r.SuiteFinished(SuiteEndEvent{Name: "Suite", PendingSpecs: 1})

	out := buf.String()
	if !strings.Contains(out, "SpecFinished spec [] failed=false skipped=false pending=true duration=0s") {
		t.Fatalf("expected SpecFinished line to include pending=true, got %q", out)
	}
	if !strings.Contains(out, "SuiteFinished Suite duration=0s skipped=0 pending=1") {
		t.Fatalf("expected SuiteFinished line to include pending=1, got %q", out)
	}
}
