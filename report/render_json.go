package report

import (
	"encoding/json"
	"io"
	"math"
)

// JSON DTOs. These mirror NormalizedReport but own their wire shape independently — durations
// become milliseconds and percentages are rounded — so a presentation choice here never forces a
// matching change to the internal model, and vice versa.
type jsonReport struct {
	SchemaVersion string        `json:"schemaVersion"`
	Execution     jsonExecution `json:"execution"`
	Suites        []jsonSuite   `json:"suites"`
	Coverage      jsonCoverage  `json:"coverage"`
}

type jsonTotals struct {
	Total    int `json:"total"`
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
	Error    int `json:"error"`
	Skipped  int `json:"skipped"`
	Filtered int `json:"filtered"`
	Pending  int `json:"pending"`
}

type jsonExecution struct {
	jsonTotals
	DurationMs int64 `json:"durationMs"`
}

type jsonSuite struct {
	Name       string     `json:"name"`
	DurationMs int64      `json:"durationMs"`
	Totals     jsonTotals `json:"totals"`
	Cases      []jsonCase `json:"cases"`
}

type jsonCase struct {
	Name       string   `json:"name"`
	Path       []string `json:"path"`
	Status     string   `json:"status"`
	DurationMs int64    `json:"durationMs"`
	Message    string   `json:"message,omitempty"`
	Output     string   `json:"output,omitempty"`
}

type jsonPackageCoverage struct {
	ImportPath string  `json:"importPath"`
	Covered    int     `json:"covered"`
	Total      int     `json:"total"`
	Percentage float64 `json:"percentage"`
}

type jsonAggregateCoverage struct {
	Covered    int     `json:"covered"`
	Total      int     `json:"total"`
	Percentage float64 `json:"percentage"`
}

type jsonCoverage struct {
	Packages []jsonPackageCoverage `json:"packages"`
	Total    jsonAggregateCoverage `json:"total"`
}

// RenderJSON writes r as documented, schema-versioned JSON (jsonReport). Arrays are always
// arrays, never null, so a consumer never has to special-case an empty suite/case/package list.
func RenderJSON(w io.Writer, r NormalizedReport) error {
	doc := jsonReport{
		SchemaVersion: r.SchemaVersion,
		Execution: jsonExecution{
			jsonTotals: toJSONTotals(r.Execution),
			DurationMs: r.Duration.Milliseconds(),
		},
		Suites:   make([]jsonSuite, 0, len(r.Suites)),
		Coverage: toJSONCoverage(r.Coverage),
	}
	for _, s := range r.Suites {
		doc.Suites = append(doc.Suites, toJSONSuite(s))
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

func toJSONTotals(t Totals) jsonTotals {
	return jsonTotals(t)
}

func toJSONSuite(s Suite) jsonSuite {
	cases := make([]jsonCase, 0, len(s.Cases))
	for _, c := range s.Cases {
		path := c.Path
		if path == nil {
			path = []string{}
		}
		cases = append(cases, jsonCase{
			Name:       c.Name,
			Path:       path,
			Status:     string(c.Status),
			DurationMs: c.Duration.Milliseconds(),
			Message:    c.Message,
			Output:     c.Output,
		})
	}
	return jsonSuite{
		Name:       s.Name,
		DurationMs: s.Duration.Milliseconds(),
		Totals:     toJSONTotals(s.Totals),
		Cases:      cases,
	}
}

func toJSONCoverage(cov Coverage) jsonCoverage {
	packages := make([]jsonPackageCoverage, 0, len(cov.Packages))
	for _, p := range cov.Packages {
		packages = append(packages, jsonPackageCoverage{
			ImportPath: p.ImportPath,
			Covered:    p.Covered,
			Total:      p.Total,
			Percentage: roundPercentage(p.Percentage()),
		})
	}
	return jsonCoverage{
		Packages: packages,
		Total: jsonAggregateCoverage{
			Covered:    cov.Total.Covered,
			Total:      cov.Total.Total,
			Percentage: roundPercentage(cov.Total.Percentage()),
		},
	}
}

// roundPercentage rounds to two decimal places so the JSON text is stable instead of carrying
// float64 division noise (e.g. 33.330000000000005).
func roundPercentage(p float64) float64 {
	return math.Round(p*100) / 100
}
