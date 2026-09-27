package report

import "testing"

func TestPackageCoveragePercentage(t *testing.T) {
	cases := []struct {
		name       string
		covered    int
		total      int
		wantPctPct float64
	}{
		{"typical", 75, 100, 75},
		{"zero total never divides by zero", 0, 0, 0},
		{"fully covered", 10, 10, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := PackageCoverage{Covered: tc.covered, Total: tc.total}
			if got := p.Percentage(); got != tc.wantPctPct {
				t.Fatalf("Percentage() = %v, want %v", got, tc.wantPctPct)
			}
		})
	}
}

func TestAggregateCoverageNeverAverages(t *testing.T) {
	// Two packages: one tiny and 100% covered, one huge and barely covered. Averaging their
	// percentages would say ~55%; summing statements says the module is barely covered — the
	// arithmetic the issue requires.
	cov := Coverage{
		Packages: []PackageCoverage{
			{ImportPath: "tiny", Covered: 2, Total: 2},     // 100%
			{ImportPath: "huge", Covered: 10, Total: 1000}, // 1%
		},
	}
	var agg AggregateCoverage
	for _, p := range cov.Packages {
		agg.Covered += p.Covered
		agg.Total += p.Total
	}
	got := agg.Percentage()
	if got > 2 {
		t.Fatalf("aggregate percentage = %v, want close to 1%% (sum of statements), not an average of package percentages", got)
	}
}

func TestTotalsAdd(t *testing.T) {
	var tot Totals
	tot.add(StatusPassed)
	tot.add(StatusFailed)
	tot.add(StatusError)
	tot.add(StatusSkipped)
	tot.add(StatusFiltered)
	tot.add(StatusPending)

	want := Totals{Total: 6, Passed: 1, Failed: 1, Error: 1, Skipped: 1, Filtered: 1, Pending: 1}
	if tot != want {
		t.Fatalf("Totals = %+v, want %+v", tot, want)
	}
}
