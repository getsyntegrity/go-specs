package report

import (
	"strings"
	"testing"
)

func TestParseCoverageProfile(t *testing.T) {
	profile := `mode: set
github.com/getsyntegrity/go-specs/specs/spec.go:10.1,12.2 3 1
github.com/getsyntegrity/go-specs/specs/spec.go:14.1,16.2 2 0
github.com/getsyntegrity/go-specs/report/model.go:5.1,7.2 4 1
`
	cov, err := ParseCoverageProfile(strings.NewReader(profile))
	if err != nil {
		t.Fatalf("ParseCoverageProfile: %v", err)
	}
	if len(cov.Packages) != 2 {
		t.Fatalf("got %d packages, want 2: %+v", len(cov.Packages), cov.Packages)
	}

	byPath := map[string]PackageCoverage{}
	for _, p := range cov.Packages {
		byPath[p.ImportPath] = p
	}

	specs := byPath["github.com/getsyntegrity/go-specs/specs"]
	if specs.Covered != 3 || specs.Total != 5 {
		t.Fatalf("specs package = %+v, want covered=3 total=5", specs)
	}
	report := byPath["github.com/getsyntegrity/go-specs/report"]
	if report.Covered != 4 || report.Total != 4 {
		t.Fatalf("report package = %+v, want covered=4 total=4", report)
	}

	// Aggregate must be the sum of statement counts, never an average of package percentages
	// (specs is 60%, report is 100% — an average would be 80%; the sum is 7/9 ≈ 77.8%).
	if cov.Total.Covered != 7 || cov.Total.Total != 9 {
		t.Fatalf("aggregate = %+v, want covered=7 total=9", cov.Total)
	}
}

func TestParseCoverageProfilePackagesSortedByImportPath(t *testing.T) {
	profile := `mode: set
github.com/z/pkg/a.go:1.1,1.2 1 1
github.com/a/pkg/b.go:1.1,1.2 1 1
`
	cov, err := ParseCoverageProfile(strings.NewReader(profile))
	if err != nil {
		t.Fatalf("ParseCoverageProfile: %v", err)
	}
	if len(cov.Packages) != 2 || cov.Packages[0].ImportPath != "github.com/a/pkg" || cov.Packages[1].ImportPath != "github.com/z/pkg" {
		t.Fatalf("packages not sorted by import path: %+v", cov.Packages)
	}
}

func TestParseCoverageProfileRejectsMissingModeLine(t *testing.T) {
	_, err := ParseCoverageProfile(strings.NewReader("github.com/x/y.go:1.1,1.2 1 1\n"))
	if err == nil {
		t.Fatal("expected an error for a profile missing its mode line, got nil")
	}
}

func TestParseCoverageProfileRejectsMalformedBlock(t *testing.T) {
	profile := "mode: set\ngithub.com/x/y.go:not-a-block\n"
	_, err := ParseCoverageProfile(strings.NewReader(profile))
	if err == nil {
		t.Fatal("expected an error for a malformed block line, got nil")
	}
}

func TestParseCoverageProfileEmptyInput(t *testing.T) {
	_, err := ParseCoverageProfile(strings.NewReader(""))
	if err == nil {
		t.Fatal("expected an error for empty input, got nil")
	}
}
