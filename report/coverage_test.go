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

// --- ParseCoverageProfileMerged (contract v1.2.8 §9, finding F7) ---
//
// Under `-coverpkg` overlap, `go test -coverprofile` legitimately writes more than one raw entry
// for the same instrumented block — one per contributing test binary. ParseCoverageProfile does
// not deduplicate that and double-counts Total. These tests pin the new, additive parser that
// does: it keys a block by (file, startLine.startCol, endLine.endCol, numStmt), combines execution
// with OR for `mode: set` and summation for `mode: count`/`atomic`, and computes coverage from the
// deduplicated statement counts.

func TestParseCoverageProfileMergedDeduplicatesOverlappingBlocksModeSet(t *testing.T) {
	// The same block (identical file, span and numStmt) appears twice, as it would when two
	// -coverpkg test binaries both instrument the same shared package. A naive per-line sum would
	// report Total=6 (3+3); the correct, deduplicated Total is 3.
	profile := `mode: set
github.com/getsyntegrity/go-specs/shared/util.go:10.1,12.2 3 1
github.com/getsyntegrity/go-specs/shared/util.go:10.1,12.2 3 0
`
	cov, err := ParseCoverageProfileMerged(strings.NewReader(profile))
	if err != nil {
		t.Fatalf("ParseCoverageProfileMerged: %v", err)
	}
	if len(cov.Packages) != 1 {
		t.Fatalf("got %d packages, want 1 (the block must be deduplicated, not treated as two): %+v", len(cov.Packages), cov.Packages)
	}
	pc := cov.Packages[0]
	if pc.Total != 3 {
		t.Fatalf("Total = %d, want 3 (deduplicated statement count, not 6)", pc.Total)
	}
	// mode: set combines with OR: one contributor saw it executed (count=1), the other did not
	// (count=0); the block is covered.
	if pc.Covered != 3 {
		t.Fatalf("Covered = %d, want 3 (OR of the two contributors: one executed it)", pc.Covered)
	}
	if cov.Total.Covered != 3 || cov.Total.Total != 3 {
		t.Fatalf("aggregate = %+v, want covered=3 total=3", cov.Total)
	}
}

func TestParseCoverageProfileMergedSumsModeCount(t *testing.T) {
	// mode: count/atomic combine with summation, matching `go tool cover -func`'s own semantics.
	profile := `mode: count
github.com/getsyntegrity/go-specs/shared/util.go:10.1,12.2 3 2
github.com/getsyntegrity/go-specs/shared/util.go:10.1,12.2 3 5
`
	cov, err := ParseCoverageProfileMerged(strings.NewReader(profile))
	if err != nil {
		t.Fatalf("ParseCoverageProfileMerged: %v", err)
	}
	if len(cov.Packages) != 1 || cov.Packages[0].Total != 3 || cov.Packages[0].Covered != 3 {
		t.Fatalf("packages = %+v, want one package with Total=3 Covered=3 (deduplicated, executed)", cov.Packages)
	}
}

func TestParseCoverageProfileMergedCoverpkgOverlapFixtureDoesNotDoubleCount(t *testing.T) {
	// A representative -coverpkg overlap fixture: two packages, one of which ("shared/util.go") is
	// imported and instrumented by both of two contributing test binaries, alongside a block that
	// is genuinely unexecuted anywhere and one that appears only once. Compares against
	// ParseCoverageProfile to demonstrate the double-count it does NOT fix.
	profile := `mode: count
github.com/getsyntegrity/go-specs/shared/util.go:10.1,12.2 3 1
github.com/getsyntegrity/go-specs/shared/util.go:10.1,12.2 3 4
github.com/getsyntegrity/go-specs/shared/util.go:14.1,16.2 2 0
github.com/getsyntegrity/go-specs/shared/util.go:14.1,16.2 2 0
github.com/getsyntegrity/go-specs/pkg/a/a.go:5.1,7.2 4 1
`
	naive, err := ParseCoverageProfile(strings.NewReader(profile))
	if err != nil {
		t.Fatalf("ParseCoverageProfile: %v", err)
	}
	var naiveShared PackageCoverage
	for _, p := range naive.Packages {
		if p.ImportPath == "github.com/getsyntegrity/go-specs/shared" {
			naiveShared = p
		}
	}
	if naiveShared.Total != 10 {
		t.Fatalf("test setup assumption failed: ParseCoverageProfile was expected to double-count Total to 10, got %d", naiveShared.Total)
	}

	merged, err := ParseCoverageProfileMerged(strings.NewReader(profile))
	if err != nil {
		t.Fatalf("ParseCoverageProfileMerged: %v", err)
	}
	byPath := map[string]PackageCoverage{}
	for _, p := range merged.Packages {
		byPath[p.ImportPath] = p
	}
	shared := byPath["github.com/getsyntegrity/go-specs/shared"]
	if shared.Total != 5 {
		t.Fatalf("shared package Total = %d, want 5 (3+2 deduplicated blocks, not 10)", shared.Total)
	}
	if shared.Covered != 3 {
		t.Fatalf("shared package Covered = %d, want 3 (only the first block executed, summed count 5 > 0)", shared.Covered)
	}
	a := byPath["github.com/getsyntegrity/go-specs/pkg/a"]
	if a.Total != 4 || a.Covered != 4 {
		t.Fatalf("pkg/a = %+v, want covered=4 total=4 (unaffected by overlap elsewhere)", a)
	}
	// Aggregate must use the deduplicated, summed statement counts: (5+4)=9 total, (3+4)=7 covered.
	if merged.Total.Total != 9 || merged.Total.Covered != 7 {
		t.Fatalf("aggregate = %+v, want covered=7 total=9", merged.Total)
	}
}

func TestParseCoverageProfileMergedPackagesSortedByImportPath(t *testing.T) {
	profile := `mode: set
github.com/z/pkg/a.go:1.1,1.2 1 1
github.com/a/pkg/b.go:1.1,1.2 1 1
`
	cov, err := ParseCoverageProfileMerged(strings.NewReader(profile))
	if err != nil {
		t.Fatalf("ParseCoverageProfileMerged: %v", err)
	}
	if len(cov.Packages) != 2 || cov.Packages[0].ImportPath != "github.com/a/pkg" || cov.Packages[1].ImportPath != "github.com/z/pkg" {
		t.Fatalf("packages not sorted by import path: %+v", cov.Packages)
	}
}

func TestParseCoverageProfileMergedRejectsMissingModeLine(t *testing.T) {
	_, err := ParseCoverageProfileMerged(strings.NewReader("github.com/x/y.go:1.1,1.2 1 1\n"))
	if err == nil {
		t.Fatal("expected an error for a profile missing its mode line, got nil")
	}
}

func TestParseCoverageProfileMergedRejectsMalformedBlock(t *testing.T) {
	profile := "mode: set\ngithub.com/x/y.go:not-a-block\n"
	_, err := ParseCoverageProfileMerged(strings.NewReader(profile))
	if err == nil {
		t.Fatal("expected an error for a malformed block line, got nil")
	}
}

func TestParseCoverageProfileMergedEmptyInput(t *testing.T) {
	_, err := ParseCoverageProfileMerged(strings.NewReader(""))
	if err == nil {
		t.Fatal("expected an error for empty input, got nil")
	}
}

func TestParseCoverageProfileMergedDistinguishesDifferentSpansAtTheSameNumStmt(t *testing.T) {
	// Two DIFFERENT blocks in the same file that happen to share numStmt must not collapse into
	// one: the key is (file, start, end, numStmt) together, not numStmt alone.
	profile := `mode: set
github.com/getsyntegrity/go-specs/shared/util.go:10.1,12.2 3 1
github.com/getsyntegrity/go-specs/shared/util.go:20.1,22.2 3 0
`
	cov, err := ParseCoverageProfileMerged(strings.NewReader(profile))
	if err != nil {
		t.Fatalf("ParseCoverageProfileMerged: %v", err)
	}
	if len(cov.Packages) != 1 {
		t.Fatalf("got %d packages, want 1: %+v", len(cov.Packages), cov.Packages)
	}
	pc := cov.Packages[0]
	if pc.Total != 6 {
		t.Fatalf("Total = %d, want 6 (two distinct blocks, 3 statements each)", pc.Total)
	}
	if pc.Covered != 3 {
		t.Fatalf("Covered = %d, want 3 (only the first block executed)", pc.Covered)
	}
}
