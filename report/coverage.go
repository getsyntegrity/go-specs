package report

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// ParseCoverageProfile parses a Go coverage profile, exactly as `go test -coverprofile=path`
// writes it, into per-package and aggregate statement coverage.
//
// The profile's mode line ("mode: set|count|atomic") does not affect the result: a statement is
// covered when its block's recorded count is greater than 0, regardless of which mode produced
// that count. Aggregate coverage is always the sum of package statement counts (see
// AggregateCoverage), never an average of package percentages.
//
// A package with no executable statements never appears in the profile at all — that is Go's own
// coverage instrumentation's behavior, not something this parser adds — so it is absent from
// Coverage.Packages and contributes nothing to the aggregate, rather than distorting it as a 0%
// or 100% package.
func ParseCoverageProfile(r io.Reader) (Coverage, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	totals := map[string]*PackageCoverage{}
	order := make([]string, 0, 16)
	lineNo := 0
	sawMode := false
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !sawMode {
			if !strings.HasPrefix(line, "mode:") {
				return Coverage{}, fmt.Errorf("coverage profile: line %d: expected mode line, got %q", lineNo, line)
			}
			sawMode = true
			continue
		}
		pkg, numStmt, count, err := parseCoverageLine(line)
		if err != nil {
			return Coverage{}, fmt.Errorf("coverage profile: line %d: %w", lineNo, err)
		}
		pc, ok := totals[pkg]
		if !ok {
			pc = &PackageCoverage{ImportPath: pkg}
			totals[pkg] = pc
			order = append(order, pkg)
		}
		pc.Total += numStmt
		if count > 0 {
			pc.Covered += numStmt
		}
	}
	if err := scanner.Err(); err != nil {
		return Coverage{}, fmt.Errorf("coverage profile: %w", err)
	}
	if !sawMode {
		return Coverage{}, fmt.Errorf("coverage profile: empty input, expected a mode line")
	}

	packages := make([]PackageCoverage, 0, len(order))
	var agg AggregateCoverage
	for _, pkg := range order {
		pc := totals[pkg]
		packages = append(packages, *pc)
		agg.Covered += pc.Covered
		agg.Total += pc.Total
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].ImportPath < packages[j].ImportPath })

	return Coverage{Packages: packages, Total: agg}, nil
}

// parseCoverageLine parses one profile block line:
//
//	file:startLine.startCol,endLine.endCol numStmt count
func parseCoverageLine(line string) (pkg string, numStmt, count int, err error) {
	colon := strings.LastIndex(line, ":")
	if colon < 0 {
		return "", 0, 0, fmt.Errorf("missing ':' in %q", line)
	}
	file := line[:colon]
	fields := strings.Fields(line[colon+1:])
	if len(fields) != 3 {
		return "", 0, 0, fmt.Errorf("malformed block spec in %q", line)
	}
	numStmt, err = strconv.Atoi(fields[1])
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid statement count in %q: %w", line, err)
	}
	count, err = strconv.Atoi(fields[2])
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid execution count in %q: %w", line, err)
	}
	return importPathOf(file), numStmt, count, nil
}

// importPathOf returns the package import path for a coverage profile file entry: the file's
// directory portion, since coverage profile file names are always the file's full import path.
func importPathOf(file string) string {
	if idx := strings.LastIndex(file, "/"); idx >= 0 {
		return file[:idx]
	}
	return file
}

// coverageBlock accumulates every raw profile line observed for one deduplicated block, keyed by
// (file, startLine.startCol, endLine.endCol, numStmt) (contract v1.2.8 §9, finding F7).
type coverageBlock struct {
	pkg      string
	numStmt  int
	executed bool // mode: set — OR of every contributor's count > 0
	count    int  // mode: count/atomic — sum of every contributor's count
}

// ParseCoverageProfileMerged parses a Go coverage profile exactly like ParseCoverageProfile, but
// additionally deduplicates blocks that appear more than once in the same profile — which happens
// routinely under `go test -coverpkg` overlap (contract v1.2.8 §9, finding F7): the go command
// writes one raw entry per contributing test binary for the same instrumented block, and that is
// normal, not corruption. ParseCoverageProfile does not perform this merge and double-counts Total
// on such a profile; this is the new, additive, block-deduplicating parser the finalizer (#146)
// uses instead. It does not change ParseCoverageProfile's documented behaviour for its existing
// (non-overlapping) callers.
//
// A block is identified by (file, startLine.startCol, endLine.endCol, numStmt) — its exact
// source-code span. Two lines sharing that key describe the same instrumented block, and their
// execution is combined using OR for `mode: set` (either contributor executed it) and by
// summation for `mode: count`/`mode: atomic` (Go's own tooling semantics, cross-checked against
// `go tool cover -func`'s correct output). Covered/Total and every percentage are then computed
// from the deduplicated, summed statement counts — never averaged, and never double-counted.
func ParseCoverageProfileMerged(r io.Reader) (Coverage, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var mode string
	blocks := map[string]*coverageBlock{}
	order := make([]string, 0, 16)
	lineNo := 0
	sawMode := false
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !sawMode {
			if !strings.HasPrefix(line, "mode:") {
				return Coverage{}, fmt.Errorf("coverage profile: line %d: expected mode line, got %q", lineNo, line)
			}
			mode = strings.TrimSpace(strings.TrimPrefix(line, "mode:"))
			sawMode = true
			continue
		}
		file, blockSpec, numStmt, count, err := parseCoverageBlockLine(line)
		if err != nil {
			return Coverage{}, fmt.Errorf("coverage profile: line %d: %w", lineNo, err)
		}
		key := file + "|" + blockSpec + "|" + strconv.Itoa(numStmt)
		b, ok := blocks[key]
		if !ok {
			b = &coverageBlock{pkg: importPathOf(file), numStmt: numStmt}
			blocks[key] = b
			order = append(order, key)
		}
		if mode == "set" {
			if count > 0 {
				b.executed = true
			}
		} else {
			// mode: count or mode: atomic
			b.count += count
		}
	}
	if err := scanner.Err(); err != nil {
		return Coverage{}, fmt.Errorf("coverage profile: %w", err)
	}
	if !sawMode {
		return Coverage{}, fmt.Errorf("coverage profile: empty input, expected a mode line")
	}

	totals := map[string]*PackageCoverage{}
	pkgOrder := make([]string, 0, 16)
	for _, key := range order {
		b := blocks[key]
		pc, ok := totals[b.pkg]
		if !ok {
			pc = &PackageCoverage{ImportPath: b.pkg}
			totals[b.pkg] = pc
			pkgOrder = append(pkgOrder, b.pkg)
		}
		pc.Total += b.numStmt
		if b.executed || b.count > 0 {
			pc.Covered += b.numStmt
		}
	}

	packages := make([]PackageCoverage, 0, len(pkgOrder))
	var agg AggregateCoverage
	for _, pkg := range pkgOrder {
		pc := totals[pkg]
		packages = append(packages, *pc)
		agg.Covered += pc.Covered
		agg.Total += pc.Total
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].ImportPath < packages[j].ImportPath })

	return Coverage{Packages: packages, Total: agg}, nil
}

// parseCoverageBlockLine parses one profile block line into its file, its raw block-position
// spelling ("startLine.startCol,endLine.endCol", never itself parsed into integers — it is only
// ever compared as a key), its statement count and its execution count.
func parseCoverageBlockLine(line string) (file, blockSpec string, numStmt, count int, err error) {
	colon := strings.LastIndex(line, ":")
	if colon < 0 {
		return "", "", 0, 0, fmt.Errorf("missing ':' in %q", line)
	}
	file = line[:colon]
	fields := strings.Fields(line[colon+1:])
	if len(fields) != 3 {
		return "", "", 0, 0, fmt.Errorf("malformed block spec in %q", line)
	}
	blockSpec = fields[0]
	numStmt, err = strconv.Atoi(fields[1])
	if err != nil {
		return "", "", 0, 0, fmt.Errorf("invalid statement count in %q: %w", line, err)
	}
	count, err = strconv.Atoi(fields[2])
	if err != nil {
		return "", "", 0, 0, fmt.Errorf("invalid execution count in %q: %w", line, err)
	}
	return file, blockSpec, numStmt, count, nil
}
