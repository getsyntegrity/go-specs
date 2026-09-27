package coordination

import (
	"bytes"
	"math/rand"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/report"
)

// merge_test.go pins T3 of issue #146: merging accepted shard envelopes into one deterministic
// module-wide report (contract v1.2.8 §7).

func envelopeFor(pkg string, suite report.Suite, exec report.Totals, dur time.Duration) ShardEnvelope {
	return ShardEnvelope{
		ShardSchemaVersion: ShardSchemaVersion,
		RunID:              "run-1",
		PackagePath:        pkg,
		Report: report.NormalizedReport{
			SchemaVersion: report.SchemaVersion,
			Execution:     exec,
			Duration:      dur,
			Suites:        []report.Suite{suite},
		},
	}
}

func threeEnvelopes() []ShardEnvelope {
	return []ShardEnvelope{
		envelopeFor("example.com/m/alpha",
			report.Suite{Name: "Alpha", Cases: []report.Case{{Name: "one", Status: report.StatusPassed}}},
			report.Totals{Total: 1, Passed: 1}, 100*time.Millisecond),
		envelopeFor("example.com/m/beta",
			report.Suite{Name: "Beta", Cases: []report.Case{
				{Name: "two", Status: report.StatusFailed},
				{Name: "three", Status: report.StatusPassed},
			}},
			report.Totals{Total: 2, Passed: 1, Failed: 1}, 200*time.Millisecond),
		envelopeFor("example.com/m/gamma",
			report.Suite{Name: "Gamma", Cases: []report.Case{{Name: "four", Status: report.StatusSkipped}}},
			report.Totals{Total: 1, Skipped: 1}, 50*time.Millisecond),
	}
}

func TestMergeReportsSumsExecutionTotalsAndDuration(t *testing.T) {
	merged := mergeReports(threeEnvelopes())
	want := report.Totals{Total: 4, Passed: 2, Failed: 1, Skipped: 1}
	if merged.Execution != want {
		t.Fatalf("Execution = %+v, want %+v", merged.Execution, want)
	}
	if merged.Duration != 350*time.Millisecond {
		t.Fatalf("Duration = %v, want 350ms", merged.Duration)
	}
	if merged.SchemaVersion != report.SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", merged.SchemaVersion, report.SchemaVersion)
	}
}

func TestMergeReportsOrdersSuitesByPackagePathRegardlessOfInputOrder(t *testing.T) {
	envs := threeEnvelopes()
	inOrder := mergeReports(envs)

	var wantJSON bytes.Buffer
	if err := report.RenderJSON(&wantJSON, inOrder); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}

	// Every permutation of completion order must render byte-identical JSON: the merge order is a
	// pure function of PackagePath, never of which producer happened to finish, or be globbed,
	// first (contract v1.2.8 §7, "Output ordering is stable ... regardless of package completion
	// order").
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20; i++ {
		shuffled := append([]ShardEnvelope(nil), envs...)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })

		got := mergeReports(shuffled)
		var gotJSON bytes.Buffer
		if err := report.RenderJSON(&gotJSON, got); err != nil {
			t.Fatalf("RenderJSON: %v", err)
		}
		if gotJSON.String() != wantJSON.String() {
			t.Fatalf("shuffle %d produced different output:\n--- want ---\n%s\n--- got ---\n%s", i, wantJSON.String(), gotJSON.String())
		}
	}
}

func TestMergeReportsPreservesSuiteAndCaseOrderWithinAShard(t *testing.T) {
	env := envelopeFor("example.com/m/ordered",
		report.Suite{Name: "Ordered", Cases: []report.Case{
			{Name: "z", Status: report.StatusPassed},
			{Name: "a", Status: report.StatusPassed},
			{Name: "m", Status: report.StatusPassed},
		}},
		report.Totals{Total: 3, Passed: 3}, 0)

	merged := mergeReports([]ShardEnvelope{env})
	if len(merged.Suites) != 1 {
		t.Fatalf("got %d suites, want 1", len(merged.Suites))
	}
	names := make([]string, len(merged.Suites[0].Cases))
	for i, c := range merged.Suites[0].Cases {
		names[i] = c.Name
	}
	want := []string{"z", "a", "m"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("case order = %v, want %v (a shard's own case order must never be reordered)", names, want)
		}
	}
}

func TestMergeReportsPreservesHookCasesAndCountsThemLikeTheCollector(t *testing.T) {
	// #207/#228: a synthetic BeforeAll/AfterAll case carries Hook, and is counted in Execution
	// exactly as report.Collector already counts it — i.e. this merge does nothing special with
	// it, it simply sums the totals the producer's own collector already computed.
	env := envelopeFor("example.com/m/hooked",
		report.Suite{Name: "Hooked", Cases: []report.Case{
			{Name: "BeforeAll", Status: report.StatusPassed, Hook: "BeforeAll"},
			{Name: "it works", Status: report.StatusPassed},
			{Name: "AfterAll", Status: report.StatusPassed, Hook: "AfterAll"},
		}},
		report.Totals{Total: 3, Passed: 3}, 0)

	merged := mergeReports([]ShardEnvelope{env})
	cases := merged.Suites[0].Cases
	if len(cases) != 3 {
		t.Fatalf("got %d cases, want 3", len(cases))
	}
	if cases[0].Hook != "BeforeAll" || cases[2].Hook != "AfterAll" {
		t.Fatalf("hook markers did not survive the merge: %+v", cases)
	}
	if cases[1].Hook != "" {
		t.Fatalf("an ordinary case gained a Hook marker: %+v", cases[1])
	}
	if merged.Execution.Total != 3 || merged.Execution.Passed != 3 {
		t.Fatalf("Execution = %+v, want the hook cases counted like any other case", merged.Execution)
	}
}

func TestMergeReportsOfNoShardsIsAnEmptyReport(t *testing.T) {
	merged := mergeReports(nil)
	if merged.SchemaVersion != report.SchemaVersion {
		t.Fatalf("SchemaVersion = %q", merged.SchemaVersion)
	}
	if len(merged.Suites) != 0 || merged.Execution.Total != 0 {
		t.Fatalf("merged = %+v, want an empty report", merged)
	}
}
