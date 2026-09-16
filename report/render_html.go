package report

import (
	"html/template"
	"io"
)

// htmlTemplate is the entire self-contained report: inline CSS, no external stylesheet, script,
// font, or image reference, so the rendered file works as a CI artifact opened offline. Every
// dynamic value is inserted through html/template, which context-aware-escapes it — a spec Name
// or failure Message containing "<script>" or similar renders as inert text, not markup.
const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>go-specs report</title>
<style>
  body { font-family: -apple-system, Segoe UI, Helvetica, Arial, sans-serif; margin: 2rem; color: #1a1a1a; }
  h1 { font-size: 1.4rem; }
  h2 { font-size: 1.1rem; margin-top: 2rem; }
  table { border-collapse: collapse; width: 100%; margin-top: 0.5rem; }
  th, td { text-align: left; padding: 0.35rem 0.6rem; border-bottom: 1px solid #ddd; font-size: 0.9rem; }
  th { background: #f5f5f5; }
  .summary span { display: inline-block; margin-right: 1.5rem; font-size: 0.95rem; }
  .status { font-weight: 600; padding: 0.1rem 0.5rem; border-radius: 3px; font-size: 0.8rem; }
  .status-passed { background: #e3f7e8; color: #1a7f37; }
  .status-failed { background: #fde8e8; color: #b3261e; }
  .status-error { background: #fde8e8; color: #b3261e; }
  .status-skipped, .status-filtered { background: #f0f0f0; color: #555; }
  .diagnostics { white-space: pre-wrap; font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 0.8rem; color: #444; margin: 0.2rem 0 0.6rem 0; }
  .coverage-bar { display: inline-block; width: 80px; height: 8px; background: #eee; border-radius: 4px; overflow: hidden; vertical-align: middle; margin-right: 0.4rem; }
  .coverage-fill { display: block; height: 8px; background: #1a7f37; }
</style>
</head>
<body>
<h1>go-specs report</h1>
<div class="summary">
  <span>Total: <strong>{{.Execution.Total}}</strong></span>
  <span>Passed: <strong>{{.Execution.Passed}}</strong></span>
  <span>Failed: <strong>{{.Execution.Failed}}</strong></span>
  <span>Error: <strong>{{.Execution.Error}}</strong></span>
  <span>Skipped: <strong>{{.Execution.Skipped}}</strong></span>
  <span>Filtered: <strong>{{.Execution.Filtered}}</strong></span>
  <span>Duration: <strong>{{.Duration}}s</strong></span>
</div>

{{range .Suites}}
<h2>{{.Name}}</h2>
<div class="summary">
  <span>total={{.Totals.Total}}</span>
  <span>passed={{.Totals.Passed}}</span>
  <span>failed={{.Totals.Failed}}</span>
  <span>error={{.Totals.Error}}</span>
  <span>skipped={{.Totals.Skipped}}</span>
  <span>filtered={{.Totals.Filtered}}</span>
  <span>duration={{.Duration}}s</span>
</div>
<table>
<thead><tr><th>Status</th><th>Case</th><th>Duration</th></tr></thead>
<tbody>
{{range .Cases}}
<tr>
  <td><span class="status status-{{.StatusClass}}">{{.Status}}</span></td>
  <td>{{.Name}}{{if .Message}}<div class="diagnostics">{{.Message}}</div>{{end}}{{if .Output}}<div class="diagnostics">{{.Output}}</div>{{end}}</td>
  <td>{{.Duration}}s</td>
</tr>
{{end}}
</tbody>
</table>
{{end}}

{{if .HasCoverage}}
<h2>Coverage</h2>
<table>
<thead><tr><th>Package</th><th>Covered</th><th>Total</th><th>Percent</th></tr></thead>
<tbody>
{{range .Coverage.Packages}}
<tr>
  <td>{{.ImportPath}}</td>
  <td>{{.Covered}}</td>
  <td>{{.Total}}</td>
  <td><span class="coverage-bar"><span class="coverage-fill" style="width:{{.Percentage}}%"></span></span>{{.Percentage}}%</td>
</tr>
{{end}}
<tr>
  <td><strong>Aggregate</strong></td>
  <td>{{.Coverage.Total.Covered}}</td>
  <td>{{.Coverage.Total.Total}}</td>
  <td><span class="coverage-bar"><span class="coverage-fill" style="width:{{.Coverage.Total.Percentage}}%"></span></span>{{.Coverage.Total.Percentage}}%</td>
</tr>
</tbody>
</table>
{{end}}
</body>
</html>
`

var htmlTmpl = template.Must(template.New("report").Parse(htmlTemplate))

type htmlTotals struct {
	Total, Passed, Failed, Error, Skipped, Filtered int
}

type htmlCase struct {
	Name        string
	Status      string
	StatusClass string
	Duration    string
	Message     string
	Output      string
}

type htmlSuite struct {
	Name     string
	Duration string
	Totals   htmlTotals
	Cases    []htmlCase
}

type htmlPackageCoverage struct {
	ImportPath string
	Covered    int
	Total      int
	Percentage string
}

type htmlCoverage struct {
	Packages []htmlPackageCoverage
	Total    htmlPackageCoverage
}

type htmlView struct {
	Execution   htmlTotals
	Duration    string
	Suites      []htmlSuite
	Coverage    htmlCoverage
	HasCoverage bool
}

// RenderHTML writes r as a single self-contained HTML file (see htmlTemplate) showing the
// execution summary, every suite's cases with failure/error diagnostics, and package plus
// aggregate coverage. It does not highlight source lines — out of scope per issue #141.
func RenderHTML(w io.Writer, r NormalizedReport) error {
	view := htmlView{
		Execution: toHTMLTotals(r.Execution),
		Duration:  formatSeconds(r.Duration),
	}
	for _, s := range r.Suites {
		hs := htmlSuite{
			Name:     s.Name,
			Duration: formatSeconds(s.Duration),
			Totals:   toHTMLTotals(s.Totals),
		}
		for _, c := range s.Cases {
			hs.Cases = append(hs.Cases, htmlCase{
				Name:        c.Name,
				Status:      string(c.Status),
				StatusClass: string(c.Status),
				Duration:    formatSeconds(c.Duration),
				Message:     c.Message,
				Output:      c.Output,
			})
		}
		view.Suites = append(view.Suites, hs)
	}
	if len(r.Coverage.Packages) > 0 {
		view.HasCoverage = true
		for _, p := range r.Coverage.Packages {
			view.Coverage.Packages = append(view.Coverage.Packages, htmlPackageCoverage{
				ImportPath: p.ImportPath,
				Covered:    p.Covered,
				Total:      p.Total,
				Percentage: formatPercentage(p.Percentage()),
			})
		}
		view.Coverage.Total = htmlPackageCoverage{
			Covered:    r.Coverage.Total.Covered,
			Total:      r.Coverage.Total.Total,
			Percentage: formatPercentage(r.Coverage.Total.Percentage()),
		}
	}
	return htmlTmpl.Execute(w, view)
}

func toHTMLTotals(t Totals) htmlTotals {
	return htmlTotals{t.Total, t.Passed, t.Failed, t.Error, t.Skipped, t.Filtered}
}
