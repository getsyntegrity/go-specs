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
| JUnit XML | `report.RenderXML` | One `<testsuite>` per `Describe`, coverage as `<properties>`. Failed → `<failure>`, Error (recovered panic) → `<error>`, Skipped/Filtered → `<skipped>`. Pending has no JUnit equivalent, so it renders as `<skipped message="pending"/>` and is counted in the `skipped` attribute, exactly like Filtered. |
| HTML | `report.RenderHTML` | Single self-contained file: inline CSS, no external stylesheet/script/font/image reference — safe to open offline or archive as a CI artifact. |
| Plain text | `report.RenderTXT` | Deterministic; never emits ANSI escape codes. Lists every failed/errored case with its message and output, then a coverage table. |
| JSON | `report.RenderJSON` | Schema-versioned (`schemaVersion: "2"`). The version changes when a field's meaning changes incompatibly, which includes a new value in the closed status vocabulary below: `"2"` added `"status": "pending"` and the `pending` totals field (#208), because a v1 consumer switching exhaustively over `status` would misread a pending case. A new field alone never bumps it. The shard envelope's `shardSchemaVersion` is versioned independently and stays `"1"`. Arrays are always arrays, never `null`. A consumer decoding into a struct with only a subset of fields is unaffected by new fields — see `report/render_json_test.go`'s `TestRenderJSONUnknownFieldsIgnorable`. |

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
- **Pending** — a compile-time `Pending`/`PendingIt` spec ([#208](https://github.com/getsyntegrity/go-specs/issues/208)); its body never ran either, but the spec is declared and not yet implemented, distinct from a spec that is intentionally excluded (Skipped)

## Multi-package reporting: `go test ./...` across many packages

A single `MultiFormatReporter` renders one process's events. For a whole `go test ./...` run,
each participating package publishes one isolated **shard**, and a separate finalize step merges
them into module-wide reports. This section covers the producer side, which is what a package
wires in. The normative rules are in
[`144-report-coordination-contract.md`](144-report-coordination-contract.md); cite it as
"contract v1.2.7 §N", never a bare section number, because sections are amended in place.

### The per-package integration

This is the whole of it — one call before `m.Run()` and one after:

```go
var reporter = report.NewMultiFormat()

func TestMain(m *testing.M) {
	writer, err := coordination.ShardWriterFromEnv("example.com/mod/pkg")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := m.Run()

	if err := writer.Write(reporter.Report()); err != nil {
		fmt.Fprintln(os.Stderr, err) // report it, but never change the exit code
	}
	os.Exit(code)
}
```

`Write` sits after `m.Run()` and before `os.Exit`, the same place `Flush` goes, so the two compose
in one `TestMain`. On an ordinary failure — including a panic the testing package recovers — that
code still runs, so a red package still publishes a complete shard.

**A reporting failure must never change the exit code.** Printing the error and preserving `code`
is not a style choice: a publish failure leaves the test result exactly as it was, and the
finalizer reports the missing or rejected producer through its own independent exit status. Exiting
non-zero there turns a green package red for a reporting problem, and re-running one package inside
the same run id is enough to trigger it.

The accepted cost: `go test` only shows a passing package's output under `-v`, so the printed
error is invisible in a plain run. That is deliberate — the authoritative signal for a reporting
problem is the finalize step, not the test step, and the two are kept separate precisely so one
cannot be mistaken for the other.

A package that wires this in is **unaffected by ordinary `go test` runs**. Shard emission is off
unless `GO_SPECS_REPORT_SHARDS` is explicitly on, and a disabled writer writes nothing and returns
no error.

### The environment

| Variable | Meaning |
|---|---|
| `GO_SPECS_REPORT_SHARDS` | The single activation gate. `1`/`true` enables shard emission; absent, empty, `0`/`false` disables it. Any other value is a configuration error. |
| `GO_SPECS_RUN_ID` | Readable run identifier, `^[A-Za-z0-9_.-]{1,128}$`, **unique per invocation, reruns included**. Required when the gate is on. It never activates reporting by itself. |
| `GO_SPECS_RUN_TOKEN` | Per-invocation ownership nonce, 16–64 random bytes hex-encoded. Required when the gate is on. It is what distinguishes a second producer of *this* run from an unrelated invocation that reused the same run id. |
| `GO_SPECS_REPORT_DIR` | Base directory for run directories. **Must be an absolute path** for producers, and on a local filesystem. `InitializeRun` resolves whatever you give it and returns the absolute form — export that. |

Two things about the directory, both of which bite on a default setup:

- **It must be absolute.** `go test` runs every package's test binary with its working directory
  set to that package's own source directory, so a relative path resolves somewhere different in
  each package of the same invocation and no producer finds the marker. The contract's
  `.go-specs/runs` default is meaningful only for the preflight, which runs once from the module
  root; producers reject a relative value outright rather than fail later as `marker-missing`.
- **Nothing above it may be group- or other-writable without the sticky bit.** If another local
  user can swap a path component, every ownership and no-replace check below it is defending a
  path that is no longer the one it verified. On a machine with `umask 002` a checkout directory is
  typically `0775`, so a run directory inside the repository is refused. Point
  `GO_SPECS_REPORT_DIR` at a private directory — `"$(mktemp -d)"` in CI, or a `0700` directory you
  create yourself.

`GO_SPECS_RUN_ID` alone is inert. That is deliberate: a run identifier left exported in a
developer's shell must never be able to fail a test run that nobody asked reporting to observe.

### `-count=1` is mandatory, and it is not about performance

**A cached package never runs `TestMain`, so it never publishes a shard.** Worse: with the gate on
and a broken configuration, a cached package reports `ok (cached)` and exits `0` — no failure, no
record, nothing to tell a misconfigured reporting run apart from a healthy green one.

Nothing else prevents this. Reading the run id from the environment does **not** invalidate the
cached result, because the read happens in `TestMain` before `m.Run()`, and the test-cache logger
is not installed until `m.Run()` starts. Always pass `-count=1` for a reporting run.

### Wiring a run

1. **Preflight**, once, before `go test`: generate a token, create the run marker.

   ```go
   token, _ := coordination.GenerateRunToken()
   own, err := coordination.InitializeRun(ctx, coordination.InitializeRunOptions{
   	RunID: "ci-42-1-test-0", Token: token, BaseDir: ".go-specs/runs",
   })
   ```

   Marker creation is exclusive: if it fails because the marker exists, the run id was reused or a
   previous run was abandoned. Both are real and actionable — fix the generator, or clear the
   abandoned run explicitly. Never retry with `Force` automatically; it is operator recovery, not
   a race resolver.

2. **`go test -count=1 ./...`** with the four variables exported. Record its status, but do not
   abort the job on it.

3. **Finalize** (issue #146), always — including on cancellation and timeout, which is exactly
   where the missing-producer list is most informative.

Deriving a conforming `GO_SPECS_RUN_ID` is CI-specific and easy to get wrong. On GitHub Actions
`GITHUB_RUN_ID` is stable across re-runs and shared by every matrix leg, so the conforming shape is
`${{ github.run_id }}-${{ github.run_attempt }}-${{ github.job }}-${{ strategy.job-index }}` — all
four parts are load-bearing. On GitLab, `CI_JOB_ID` alone is already unique per retry and per
`parallel:matrix` leg; do not port the GitHub shape across.

### What a shard contains, and what it does not

A shard carries execution data and the identity binding it to its run: the schema version, the run
id, the run's token digest, the package's original import path, a timestamp, and the package's
`NormalizedReport`.

It carries **no coverage** — not totals, not blocks, not a profile path. The invoker passes the one
combined `go test -coverprofile` file directly to the finalizer, which alone owns block
deduplication and coverage arithmetic. A producer that contributed its own numbers would corrupt
the module totals in a way nothing downstream could detect.
