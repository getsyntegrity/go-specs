# Reporting

`report` package: `github.com/getsyntegrity/go-specs/report`

go-specs can render a suite's results as JUnit-compatible XML, a self-contained HTML page,
deterministic plain text, or versioned JSON, and can fold in Go coverage-profile data alongside
the execution results. This is opt-in, per test binary, via the existing `report.EventReporter`
mechanism — there is no separate runner and no change to how you invoke `go test`.

## Quick start

Wire a `report.MultiFormatReporter` into a top-level `Describe`/`DescribeWithReporter` call and
flush it once the suite has run — typically in `TestMain`:

```go
func TestMain(m *testing.M) {
	mfr := report.NewMultiFormat(
		report.Target{Format: report.FormatXML, Path: "artifacts/report.xml"},
		report.Target{Format: report.FormatHTML, Path: "artifacts/report.html"},
	)
	code := m.Run() // your specs use specs.DescribeWithReporter(t, name, mfr, fn) below
	if err := mfr.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "go-specs report:", err)
	}
	os.Exit(code)
}

func TestCheckout(t *testing.T) {
	specs.DescribeWithReporter(t, "Checkout", mfr, func(s *specs.Spec) {
		s.It("charges the card", func(ctx *specs.Context) {
			ctx.Expect(charge(100)).ToEqual(100)
		})
	})
}
```

`NewMultiFormat` with no targets is a no-op reporter: it still collects events (so `Report()`
and `SetCoverage` work normally) but `Flush` writes nothing. Reporting only activates for the
targets you explicitly pass in — nothing changes for a package that doesn't opt in.

## Adding coverage

`report.ParseCoverageProfile` reads a profile in exactly the format `go test -coverprofile=path`
writes:

```go
f, err := os.Open("coverage.out")
// handle err
cov, err := report.ParseCoverageProfile(f)
// handle err
mfr.SetCoverage(cov)
```

Coverage is aggregated by summing statement counts across packages — **never** by averaging
package percentages. A module with a 60%-covered package and a 100%-covered package of equal
size reports 80% aggregate coverage (computed from summed statements), not the arithmetic mean of
the two percentages, which would coincidentally also be 80% only when the packages are the same
size. This distinction matters and is enforced by tests (`report/coverage_test.go`,
`report/render_json_test.go`).

## Choosing targets from a flag or env var

`report.ParseTarget` parses one `"format:path"` string — the shape a future `go test`
integration would read from a flag such as `-args -go-specs.report=xml:artifacts/report.xml` or
an environment variable:

```go
target, err := report.ParseTarget(os.Getenv("GO_SPECS_REPORT"))
// handle err
mfr := report.NewMultiFormat(target)
```

Valid formats are `xml`, `html`, `txt`, and `json`.

## Formats

| Format | Renderer | Notes |
|---|---|---|
| JUnit XML | `report.RenderXML` | One `<testsuite>` per `Describe`, coverage as `<properties>`. Failed → `<failure>`, Error (recovered panic) → `<error>`, Skipped/Filtered → `<skipped>`. |
| HTML | `report.RenderHTML` | Single self-contained file: inline CSS, no external stylesheet/script/font/image reference — safe to open offline or archive as a CI artifact. |
| Plain text | `report.RenderTXT` | Deterministic; never emits ANSI escape codes. Lists every failed/errored case with its message and output, then a coverage table. |
| JSON | `report.RenderJSON` | Schema-versioned (`schemaVersion: "1"`). Arrays are always arrays, never `null`. A consumer decoding into a struct with only a subset of fields is unaffected by new fields — see `report/render_json_test.go`'s `TestRenderJSONUnknownFieldsIgnorable`. |

Every renderer takes the same `report.NormalizedReport` and an `io.Writer`; `RenderXML` and
`RenderHTML` escape all case names, messages, and output through `encoding/xml` and
`html/template` respectively, so a failure message containing `<script>` or `&` renders as inert
text, not markup.

## Status vocabulary

Every case is normalized to exactly one of:

- **Passed**
- **Failed** — an assertion or hook failure
- **Error** — a recovered panic (or other infrastructure failure); distinguished from Failed by
  the presence of output (a stack trace)
- **Skipped** — a compile-time `Skip`/`SkipIt` spec; its body never ran
- **Filtered** — excluded by external test selection (e.g. `go test -run`) before its body ran

## Known limitation: `go test ./...` across multiple packages

This package renders one process's events. It does not coordinate multiple concurrent `go test`
package binaries writing to the same output paths, and there is no built-in way yet to activate
reporting for an entire `go test ./...` run without wiring `DescribeWithReporter` into each
package's own `TestMain`.

If you opt multiple packages in, give each one a distinct path (e.g. include the package name in
the target path) so their `Flush` calls don't race on the same file:

```go
target := report.Target{
	Format: report.FormatJSON,
	Path:   fmt.Sprintf("artifacts/%s.report.json", strings.ReplaceAll(pkgPath, "/", "_")),
}
```

A shard-and-merge mechanism that lets `go test ./...` activate reporting module-wide (via a
`-go-specs.report=format:path` flag and safe cross-package aggregation once every package has
finished) is deferred to a follow-up — see
[issue #141](https://github.com/getsyntegrity/go-specs/issues/141)'s "Multi-package constraint".
This package's model, renderers, and coverage parser are already what that follow-up would build
on; only the concurrent activation/aggregation layer is missing.
