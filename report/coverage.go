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
