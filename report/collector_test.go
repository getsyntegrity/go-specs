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
