// perfcheck compares benchmark results (go test -bench -benchmem -json) against a baseline
// and exits with non-zero status if any benchmark regresses by more than the threshold (default 10%).
//
// Usage:
//
//	go test ./benchmarks -bench . -benchmem -json > bench.json
//	go run tools/perfcheck/main.go bench.json baseline.json
//
// Optional: -threshold=0.10 (fraction, e.g. 0.10 = 10% regression fails CI).
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// go test -json emits one JSON object per line (streaming).
type testEvent struct {
	Action string `json:"Action"`
	Test   string `json:"Test"`
	Output string `json:"Output"`
}

// parseGoTestJSON reads go test -bench -json output and returns benchmark name -> ns/op.
// Only lines containing "ns/op" are parsed; the first matching result per benchmark is kept.
func parseGoTestJSON(path string) (map[string]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	// Benchmark line: "BenchmarkFoo-8\t\t10000000\t\t75.2 ns/op\t\t0 B/op\t\t0 allocs/op\n"
	benchRe := regexp.MustCompile(`^(\S+)\s+(\d+)\s+([\d.]+)\s+ns/op`)

	results := make(map[string]float64)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e testEvent
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue
		}
		if e.Action != "output" || e.Output == "" {
			continue
		}
		// Output may be multi-line; check each line
		for _, line := range strings.Split(strings.TrimSuffix(e.Output, "\n"), "\n") {
			if line == "" || !strings.Contains(line, "ns/op") {
				continue
			}
			sub := benchRe.FindStringSubmatch(line)
			if sub == nil {
				continue
			}
			name := sub[1]
			var nsOp float64
			if _, err := fmt.Sscanf(sub[3], "%f", &nsOp); err != nil {
				continue
			}
			results[name] = nsOp
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func main() {
	threshold := flag.Float64("threshold", 0.10, "regression threshold (fraction, e.g. 0.10 = 10%%)")
	flag.Parse()
	args := flag.Args()
	if len(args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: perfcheck [ -threshold=0.10 ] <current.json> <baseline.json>\n")
		fmt.Fprintf(os.Stderr, "  current.json  = output of: go test -bench . -benchmem -json\n")
		fmt.Fprintf(os.Stderr, "  baseline.json = baseline from a previous run (same format)\n")
		fmt.Fprintf(os.Stderr, "  -threshold     regression fraction (default 0.10 = 10%%)\n")
		os.Exit(2)
	}
	currentPath, baselinePath := args[0], args[1]

	current, err := parseGoTestJSON(currentPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "perfcheck: read current: %v\n", err)
		os.Exit(2)
	}
	baseline, err := parseGoTestJSON(baselinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "perfcheck: read baseline: %v\n", err)
		os.Exit(2)
	}

	var regressions []string
	for name, curNs := range current {
		baseNs, ok := baseline[name]
		if !ok {
			continue
		}
		if baseNs <= 0 {
			continue
		}
		pct := (curNs - baseNs) / baseNs
		if pct > *threshold {
			regressions = append(regressions, fmt.Sprintf("%s: %.2f ns/op -> %.2f ns/op (+%.1f%%)", name, baseNs, curNs, pct*100))
		}
	}

	if len(regressions) > 0 {
		fmt.Fprintf(os.Stderr, "perfcheck: regression(s) above %.0f%% threshold:\n", *threshold*100)
		for _, r := range regressions {
			fmt.Fprintln(os.Stderr, "  ", r)
		}
		os.Exit(1)
	}
}
