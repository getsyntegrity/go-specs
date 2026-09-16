package report

import (
	"testing"
	"time"
)

func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		name string
		e    SpecResultEvent
		want Status
	}{
		{"passed", SpecResultEvent{}, StatusPassed},
		{"failed assertion has no output", SpecResultEvent{Failed: true, Message: "expected 1, got 2"}, StatusFailed},
		{"recovered panic has output, classified as error", SpecResultEvent{Failed: true, Output: "goroutine 1 [running]:\n..."}, StatusError},
		{"skipped", SpecResultEvent{Skipped: true}, StatusSkipped},
		{"filtered", SpecResultEvent{Filtered: true}, StatusFiltered},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyStatus(tc.e); got != tc.want {
				t.Fatalf("classifyStatus(%+v) = %v, want %v", tc.e, got, tc.want)
			}
		})
	}
}

func TestCollectorDeterministicOrderingAndTotals(t *testing.T) {
	c := NewCollector()
	c.SuiteStarted(SuiteStartEvent{Name: "A"})
	finishCase(c, "a1", nil, time.Millisecond, false, false, false, "", "")
	finishCase(c, "a2", nil, time.Millisecond, true, false, false, "boom", "")
	c.SuiteFinished(SuiteEndEvent{Name: "A", Duration: 5 * time.Millisecond})

	c.SuiteStarted(SuiteStartEvent{Name: "B"})
	finishCase(c, "b1", nil, time.Millisecond, false, true, false, "", "")
	c.SuiteFinished(SuiteEndEvent{Name: "B", Duration: 2 * time.Millisecond})

	r := c.Report()

	if len(r.Suites) != 2 || r.Suites[0].Name != "A" || r.Suites[1].Name != "B" {
		t.Fatalf("suites not in start order: %+v", r.Suites)
	}
	if len(r.Suites[0].Cases) != 2 || r.Suites[0].Cases[0].Name != "a1" || r.Suites[0].Cases[1].Name != "a2" {
		t.Fatalf("cases not in finish order: %+v", r.Suites[0].Cases)
	}
	if r.Execution != (Totals{Total: 3, Passed: 1, Failed: 1, Skipped: 1}) {
		t.Fatalf("execution totals = %+v", r.Execution)
	}
	if r.Duration != 7*time.Millisecond {
		t.Fatalf("report duration = %v, want sum of suite durations (7ms)", r.Duration)
	}
}

func TestCollectorReportIsASnapshot(t *testing.T) {
	c := NewCollector()
	c.SuiteStarted(SuiteStartEvent{Name: "A"})
	finishCase(c, "a1", nil, 0, false, false, false, "", "")
	c.SuiteFinished(SuiteEndEvent{Name: "A"})

	first := c.Report()
	c.SuiteStarted(SuiteStartEvent{Name: "B"})
	c.SuiteFinished(SuiteEndEvent{Name: "B"})

	if len(first.Suites) != 1 {
		t.Fatalf("Report() snapshot mutated by later events: %+v", first.Suites)
	}
}

// TestCollectorReportDeepCopy proves Report() hands back a fully independent copy, not just a
// fresh Suites slice sharing nested state with the Collector. It mutates every nested slice a
// caller could reach — a Case's Path, a Suite's Cases, Coverage.Packages — through the returned
// report, then takes a second snapshot and asserts none of those mutations are visible in it or
// in the Collector's own state.
func TestCollectorReportDeepCopy(t *testing.T) {
	c := NewCollector()
	c.SuiteStarted(SuiteStartEvent{Name: "A"})
	finishCase(c, "a1", []string{"A", "a1"}, 0, false, false, false, "", "")
	c.SuiteFinished(SuiteEndEvent{Name: "A"})
	c.SetCoverage(Coverage{Packages: []PackageCoverage{{ImportPath: "pkg", Covered: 1, Total: 2}}})

	first := c.Report()

	// Mutate every nested slice reachable from the returned snapshot.
	first.Suites[0].Cases[0].Path[1] = "tampered"
	first.Suites[0].Cases = append(first.Suites[0].Cases, Case{Name: "injected"})
	first.Coverage.Packages[0].ImportPath = "tampered"
	first.Coverage.Packages = append(first.Coverage.Packages, PackageCoverage{ImportPath: "injected"})

	second := c.Report()

	if second.Suites[0].Cases[0].Path[1] != "a1" {
		t.Fatalf("mutating the returned Path leaked into the Collector: %+v", second.Suites[0].Cases[0].Path)
	}
	if len(second.Suites[0].Cases) != 1 {
		t.Fatalf("appending to the returned Cases leaked into the Collector: %+v", second.Suites[0].Cases)
	}
	if second.Coverage.Packages[0].ImportPath != "pkg" {
		t.Fatalf("mutating the returned Coverage.Packages leaked into the Collector: %+v", second.Coverage.Packages)
	}
	if len(second.Coverage.Packages) != 1 {
		t.Fatalf("appending to the returned Coverage.Packages leaked into the Collector: %+v", second.Coverage.Packages)
	}
}
