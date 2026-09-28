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
| JUnit XML | `report.RenderXML` | One `<testsuite>` per `Describe`, coverage as `<properties>`. Failed → `<failure>`, Error (recovered panic) → `<error>`, Skipped/Filtered → `<skipped>`. Pending has no JUnit equivalent, so it renders as `<skipped message="pending"/>` and is counted in the `skipped` attribute, exactly like Filtered. Unstarted (#274) has no JUnit equivalent either, so it renders as `<skipped message="not run: fail-fast"/>` and also folds into `skipped`; unlike every other status it never counts toward `total`, so the `tests` attribute is `total + unstarted` rather than `total` alone — a JUnit consumer still sees the full declared suite size, not the smaller "reached" count. |
| HTML | `report.RenderHTML` | Single self-contained file: inline CSS, no external stylesheet/script/font/image reference — safe to open offline or archive as a CI artifact. |
| Plain text | `report.RenderTXT` | Deterministic; never emits ANSI escape codes. Lists every failed/errored case, named by its full scope path (`Suite/when x/spec`), with its message and output, then a coverage table. |
| JSON | `report.RenderJSON` | Schema-versioned (`schemaVersion: "3"`). The version changes when a field's meaning changes incompatibly, which includes a new value in the closed status vocabulary below: `"2"` added `"status": "pending"` and the `pending` totals field (#208); `"3"` adds `"status": "unstarted"`, the `unstarted` totals field, and `Case.declared` (#274) — a v1/v2 consumer switching exhaustively over `status` would misread an unstarted case. A new field alone never bumps it. The shard envelope's `shardSchemaVersion` is versioned independently and stays `"1"`. Arrays are always arrays, never `null`. A consumer decoding into a struct with only a subset of fields is unaffected by new fields — see `report/render_json_test.go`'s `TestRenderJSONUnknownFieldsIgnorable`. |

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
- **Skipped** — a compile-time `Skip`/`SkipIt` spec (body never ran), or a spec whose own subtest
  skipped at runtime via `ctx.T.Skip`/`Skipf`/`SkipNow` ([#254](https://github.com/getsyntegrity/go-specs/issues/254)), where the body did start
  running; a spec that fails and then calls `SkipNow` is Failed instead, matching `go test` itself
- **Filtered** — excluded by external test selection (e.g. `go test -run`) before its body ran
- **Pending** — a compile-time `Pending`/`PendingIt` spec ([#208](https://github.com/getsyntegrity/go-specs/issues/208)); its body never ran either, but the spec is declared and not yet implemented, distinct from a spec that is intentionally excluded (Skipped)
- **Unstarted** — a spec `CompiledSuite.SetFailFast(true)` or `Runner.FailFast` prevented from ever being reached, after an earlier spec in the same run already failed ([#274](https://github.com/getsyntegrity/go-specs/issues/274)). Its body never ran either, but unlike every status above, an Unstarted spec never counts toward `Totals.Total` — `Total` keeps its pre-existing meaning of "specs that entered execution, plus declared Skip/Pending that were actually processed", and a fail-fast-prevented spec never did either. When the unreached spec was itself a compile-time `SkipIt`/`Skip` or `PendingIt`/`Pending`, its original declaration survives as `Case.Declared` (`"skip"` or `"pending"`), so a consumer can still tell what it *would* have been reported as.

### `Unstarted` semantics and assumptions (issue #274)

Example: a suite declares 3 specs and enables fail-fast; the first spec fails. The report says
`Total: 1, Failed: 1, Unstarted: 2` — not `Total: 1` alone, and not `Total: 3`. `SkipIt`/`PendingIt`
specs inside a group the fail-fast stop never reached are Unstarted too, keeping their original
declaration in `Case.Declared`.

Two points below are flagged assumptions (not a maintainer decision), made explicit here and in the
PR that introduced them:

- A spec after the stop point that `go test -run` would itself have filtered out is still reported
  Unstarted, not Filtered — go-specs cannot evaluate `-run` without reimplementing `testing`'s own
  matcher, so it cannot tell "fail-fast stopped me" from "`-run` would also have excluded me".
- `RunShard` reports only the unstarted specs of its own shard's units: fail-fast is shard-local, so
  a shard that never itself failed reports no Unstarted specs, even if a sibling shard's fail-fast
  stopped early.

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

This section wires the three library calls directly, which is the lowest-level integration and
the one with no moving parts beyond the Go toolchain. For most CI pipelines, the
[`go-specs-report` CLI](#running-it-in-ci-with-go-specs-report) below is the lower-friction
option: it wraps exactly these three calls, so you write a shell script or a workflow file instead
of a small Go program.

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

   ```go
   res, err := coordination.Finalize(ctx, coordination.FinalizeOptions{
   	RunID: "ci-42-1-test-0", Token: token, BaseDir: own.BaseDir,
   	CoverProfile:      "coverage.out", // "" if the run had no -coverprofile
   	ExpectedProducers: expectedProducers, // required; never derived with `go list`
   	Targets: []report.Target{
   		{Format: report.FormatXML, Path: "artifacts/report.xml"},
   		{Format: report.FormatJSON, Path: "artifacts/report.json"},
   	},
   	Cleanup: true, // prune the run directory, but only once finalize fully succeeds
   })
   os.Exit(coordination.ExitCode(res, err))
   ```

   `Finalize` is the only code path that reads shard files, merges them, and renders module-wide
   reports. It verifies run ownership first and fails closed on any mismatch; a present
   `config-error.json` short-circuits it entirely (`res.ConfigError` set, nothing merged or
   rendered); otherwise every shard is checked against `ExpectedProducers`, and every problem is
   reported rather than silently dropped — `res.PackagesMissing` for an expected producer with no
   valid shard, `res.Rejected` for a shard rejected with its reason (`corrupt`,
   `duplicate-package`, `unexpected-package`, `schema-version-mismatch`, `wrong-run-id`,
   `ownership-mismatch`, `filename-hash-mismatch`, `stale`). Coverage, when `CoverProfile` is set,
   is parsed and merged through `report.ParseCoverageProfileMerged` (below) — never averaged, never
   double-counted under `-coverpkg` overlap.

   `coordination.ExitCode(res, err)` maps the outcome to the process exit code (contract v1.2.9
   §8): `78` (`EX_CONFIG`) for a run-ownership failure, an invalid `FinalizeOptions` (such as a
   missing `ExpectedProducers`), or a recorded `config-error.json`; `1` for any other reporting
   failure, including a missing or rejected producer; `0` otherwise — this is independent of
   whether the tests themselves passed, which is exactly why finalize is always run as its own
   step (issue #146). The full CI wiring — a generic shell script and GitHub Actions — is in
   [Running it in CI with `go-specs-report`](#running-it-in-ci-with-go-specs-report) below.

Deriving a conforming `GO_SPECS_RUN_ID` is CI-specific and easy to get wrong. On GitHub Actions
`GITHUB_RUN_ID` is stable across re-runs and shared by every matrix leg, so the conforming shape is
`${{ github.run_id }}-${{ github.run_attempt }}-${{ github.job }}-${{ strategy.job-index }}` — all
four parts are load-bearing. On GitLab, `CI_JOB_ID` alone is already unique per retry and per
`parallel:matrix` leg; do not port the GitHub shape across.

### Running it in CI with `go-specs-report`

The three library calls above are one small Go program to write once, but most CI pipelines never
write Go for their build step at all — they run shell or YAML. `cmd/go-specs-report` is a thin
command-line wrapper around exactly those three calls (`init`, `finalize`, `gc`); it owns no logic
of its own; ownership checks, shard merging, and rendering all still happen inside
`report/coordination`, unchanged. This section is the CI-facing counterpart to "Wiring a run"
above: same three steps, expressed as commands and environment variables instead of Go structs.

#### Install

From a released version, install the binary once (for example in a CI base image, or as a setup
step) and call it by name for every job:

```sh
go install github.com/getsyntegrity/go-specs/cmd/go-specs-report@<version>
```

Replace `<version>` with the go-specs release you want (a tag such as `v0.12.0`, or `latest`).
Without a released version pinned yet, or to avoid an install step entirely, `go run` fetches and
runs it in one call — slower per invocation (it builds first), but nothing to keep up to date:

```sh
go run github.com/getsyntegrity/go-specs/cmd/go-specs-report@<version> finalize -h
```

From inside a checkout of this module itself (this repository's own CI, or a fork), skip the
module path and version entirely: `go run ./cmd/go-specs-report <verb> ...`.

#### The three verbs

Every verb parses its flags from the command line first and falls back to the matching environment
variable when a flag is omitted; a flag always wins over the environment. `init` is the only verb
that *writes* an identity (it prints one when you do not supply a token), because it is the
preflight step that owns marker creation; `finalize` and `gc` only ever *read* the identity, so
they must receive the same run id, token, and — critically — the same **absolute** report
directory that `init` produced (contract v1.2.9 §5; a relative `-report-dir` is rejected outright
by both).

| Verb | Flag | Env fallback | What the invoker gives it |
|---|---|---|---|
| `init` | `-run-id` | `GO_SPECS_RUN_ID` | Required. `init` never generates a run id — see the three recipes below. |
| | `-token` | `GO_SPECS_RUN_TOKEN` | Optional. Omit it and `init` generates one with `crypto/rand` and prints it; supply it yourself only if something else in your pipeline needs to hold the token before `init` runs. |
| | `-report-dir` | `GO_SPECS_REPORT_DIR` | Optional, default `.go-specs/runs`. `init` resolves whatever it is given to an absolute path before use — export what it prints, do not re-derive it. |
| | `-force` | — | Operator recovery only: removes and recreates an existing marker for a genuinely reused run id. Never wire this into ordinary CI; a real collision means the `RunID` recipe is wrong. |
| `finalize` | `-run-id`, `-token`, `-report-dir` | same three variables | Required, and must be the exact values `init` produced — `finalize` verifies ownership against them and fails closed on any mismatch. Unlike `init`, `-report-dir` is **not** resolved here; pass the absolute path `init` already printed. |
| | `-producers` | — | Required: a path to the manifest file described below. There is no default and no `go list` fallback. |
| | `-coverprofile` | — | Optional: the one combined file `go test -coverprofile=...` wrote. Omit it if the run collected no coverage. |
| | `-json`, `-xml`, `-txt`, `-html` | — | Optional, one per format you want written; omit a flag to skip that format. At least one is normally set, or finalize does the ownership/producer bookkeeping and writes nothing. |
| | `-cleanup` | — | Optional: prune the run's shard directory once the merge fully succeeds. Leave it off while you are still debugging a run; turn it on once the pipeline is trusted, so a run that hits a config error still leaves its evidence on disk. |
| `gc` | `-report-dir` | `GO_SPECS_REPORT_DIR` | Optional, default `.go-specs/runs`, and — like `finalize` — must already be absolute. |
| | `-retention` | — | Optional, default `24h`. Must be a positive duration; `gc` refuses `0s` or negative values with exit `78`, because a zero window would treat a run still in progress as abandoned. |
| | `-dry-run` | — | Report what would be removed without touching the filesystem — run this first when pointing `gc` at a shared directory you do not fully trust yet. |

#### Exit codes

| Code | Meaning | What the invoker should do |
|---|---|---|
| `0` | Reporting succeeded for that verb. For `finalize`, this says nothing about whether `go test` itself passed — see below. | Nothing extra; the verb did its job. |
| `78` (`EX_CONFIG`) | Invalid configuration: a missing or malformed run id/token, an ownership mismatch against `run.json`, a missing/empty/unreadable `-producers` manifest, or (for `gc`) a non-positive `-retention`. | Fix the invocation — this is never a flaky condition to retry. |
| `1` | A reporting failure after configuration checked out: a missing producer, a rejected shard, or a merge/render/IO error. | Read the printed summary (`missing:` / `rejected:` lines on stderr) and fix the run — a package did not publish a valid shard, or the manifest lists a package that no longer exists. |
| `2` | CLI usage error: no verb, an unknown verb, an unknown flag, or unexpected positional arguments (the stdlib `flag` package's own convention). | Fix the command line; this never reaches the library at all. |

**Test failures never change `finalize`'s exit code, and this is the single most common wiring
mistake.** `go test` failing is a *test* result; `finalize` succeeding or failing is a *reporting*
result, and the two are deliberately independent processes with independent exit codes (contract
v1.2.9 §8). Verified end to end for this task (see *Executable proof* in the feature document):
with a deliberately failing test and a correct manifest, `go test` exits `1` and `finalize` exits
`0` — the shards were still complete and valid, so reporting has nothing to complain about. **The
job's final status must therefore combine both exit codes explicitly** — `go test`'s status is the
test result, `finalize`'s status is the reporting result, and a CI step that only checks one of
them will silently ignore the other. The worked examples below all do this combination explicitly;
none of them relies on a single command's exit code standing in for the whole job.

#### Missing producers are strict, on purpose

There is no `-allow-missing` or `--lenient` flag, and none is planned. Contract v1.2.9 §13 records
this as a deliberate, non-negotiable choice: a permissive flag is exactly the kind of setting that
gets copy-pasted into CI once, for one debugging session, and never removed — after which a
genuinely missing producer (a crashed package, a typo'd manifest entry) silently stops failing the
build. If you want a partial report locally — for example, to look at just one package's shard
while iterating — pass a shorter `-producers` file that lists only the packages you actually ran.
That keeps the reduced scope visible on the command line instead of hidden behind a flag that could
be inherited by CI unnoticed.

#### Three `RunID` recipes

Every `GO_SPECS_RUN_ID` must match `^[A-Za-z0-9_.-]{1,128}$` and be unique per invocation, reruns
included (contract v1.2.9 §5) — that pattern excludes spaces and most punctuation, which matters
for the first recipe below. Pick whichever fits your CI system; do not mix parts of two.

1. **CI-native ID plus job/matrix identity plus attempt.** The CI system's own run/job identifiers
   are the natural source, but on their own they are not unique per invocation — see the
   *Run marker lifecycle* discussion in `docs/144-report-coordination-contract.md` §5 for why each
   part below is load-bearing.

   - **GitHub Actions**:
     ```
     ${{ github.run_id }}-${{ github.run_attempt }}-${{ github.job }}-<matrix key>
     ```
     `run_id` identifies the workflow run, `run_attempt` separates re-runs, `job` separates job
     *definitions*, and `<matrix key>` (spell out the actual matrix values, e.g.
     `${{ matrix.go-version }}-${{ matrix.os }}`, not `strategy.job-index` alone — see the
     contract for why) separates legs within one definition. A matrix value can legitimately
     contain characters the `RunID` pattern forbids (a space in an OS name, a `.` is fine but a
     `/` is not); sanitize it first, for example
     `$(echo "${{ matrix.os }}" | tr -c 'A-Za-z0-9_.-' '-')`, rather than interpolating it raw.
   - **GitLab CI**: `$CI_JOB_ID` alone — it is already unique per retry and per `parallel:matrix`
     leg, so do not port the GitHub shape across.

2. **`uuidgen`.** Simplest option when your CI system's own identifiers are inconvenient to
   compose, or when you are testing the pipeline locally:
   ```sh
   GO_SPECS_RUN_ID="$(uuidgen)"
   ```
   A UUID's hyphens and hex digits already satisfy the pattern with no sanitization step.

3. **Timestamp plus randomness**, for a system with no stable run/job identifier at all, or as a
   fallback when in doubt:
   ```sh
   GO_SPECS_RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')"
   ```
   `date -u +%Y%m%dT%H%M%SZ` and hex output from `od` are both already restricted to
   `[A-Za-z0-9]`, so the concatenation with one `-` separator satisfies the pattern without further
   sanitizing.

#### Producer manifest recipe

`finalize -producers` takes a plain text file, one import path per line, blank lines and `#`
comments ignored — **the CLI never runs `go list` itself** (decision #4 in the feature document,
`docs/investigations/odd-tasks/report-cli.md`). You generate it, review it like any other source
file, and commit it; a CI step regenerates and diffs it to catch drift.

Contract v1.2.9 §5 defines the expected-producer set precisely. A package is a producer when its
test binary publishes a shard, which in practice means its `TestMain` calls
`coordination.ShardWriterFromEnv`. The pipeline below selects exactly those packages:

```sh
# 1. `go list` lists every package in your selection (./... below — narrow it to the pattern you
#    actually pass to `go test`) that has test files under the current build configuration. It
#    already evaluates GOOS/GOARCH/-tags, so packages with no test files and packages excluded by
#    build tags never appear — the first two exclusions in §5 need no extra code.
# 2. A package stays only if one of its test files calls ShardWriterFromEnv, i.e. it wires a
#    shard writer into TestMain. Merely importing report/coordination is not enough: a helper or
#    tool whose tests use the package without publishing a shard would otherwise be listed, and
#    finalize would report it missing on every run.
#
# The function writes to stdout, so the same code generates the file and checks it for drift.
# It assumes package directories contain no whitespace.
go_specs_producers() {
  go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}} {{.Dir}}{{range .TestGoFiles}} {{.}}{{end}}{{range .XTestGoFiles}} {{.}}{{end}}{{end}}' ./... |
    while read -r pkg dir files; do
      for f in $files; do
        if grep -q 'ShardWriterFromEnv' "$dir/$f"; then
          echo "$pkg"
          break
        fi
      done
    done | sort -u
}

mkdir -p .go-specs
go_specs_producers > .go-specs/producers.txt
```

Two consequences worth stating explicitly:

- **A package served from the build cache is *not* excluded**, deliberately. `go list` reads
  source, not the cache, so a package `go test` would serve from cache still appears in the
  manifest. That is the point: a cached package with the gate on publishes nothing, and contract
  v1.2.9 §5/§6 needs that to fail loudly as a missing producer (see `-count=1` above).
- **The match is textual.** A test file that mentions `ShardWriterFromEnv` without calling it from
  `TestMain` (a unit test of the library itself, say) is still listed. In go-specs' own repository
  that is `report/coordination`, whose `config_test.go` exercises the function directly; remove
  such entries from the committed file by hand. A wrong entry is never silent: `finalize` names
  it as `missing` and exits `1`.

Commit the generated `.go-specs/producers.txt`, and add a CI step that regenerates it into a
temporary file and fails the build on drift, so a new producer package added without updating the
manifest is caught immediately rather than silently missing from the next report:

```sh
# go_specs_producers is the function defined above.
go_specs_producers | diff -u .go-specs/producers.txt - || {
  echo "producer manifest is stale; regenerate and commit .go-specs/producers.txt" >&2
  exit 1
}
```

#### Worked examples

Every example below runs `go test` and `finalize` as two separate steps, records both exit codes,
and combines them explicitly — never a single combined `&&`/`||` chain that would let one status
hide the other (contract v1.2.9 §8's own recommendation). Each was executed against this
repository's real fixture packages
(`report/coordination/internal/e2efixture/{producera,producerb}`) while writing this section; see
*Executable proof* in `docs/investigations/odd-tasks/report-cli.md` for the exact commands and
observed exit codes. The script below is reproduced verbatim except for the fixture-specific
`$COVERPKG`/`$TEST_PACKAGES` values and the `$PRODUCERS_FILE` path, which a real pipeline replaces
with its own package list and its committed manifest.

**Generic CI (POSIX `sh`):**

```sh
#!/bin/sh
# Requires go-specs-report on PATH (or set GSR to its path).
set -u

: "${GSR:=go-specs-report}"
: "${GO_SPECS_RUN_ID:=proof-$(date -u +%Y%m%dT%H%M%SZ)-$$}"
: "${PRODUCERS_FILE:?set PRODUCERS_FILE to the checked-in producer manifest}"
: "${TEST_PACKAGES:=./...}"   # the package pattern you pass to go test
: "${COVERPKG:=./...}"        # the packages to instrument for coverage

REPORT_DIR="$(mktemp -d)"
export GO_SPECS_REPORT_DIR="$REPORT_DIR"
export GO_SPECS_RUN_ID

# 1. init (preflight): create the run marker, generate a token, export identity.
INIT_OUT="$(mktemp)"
"$GSR" init -run-id="$GO_SPECS_RUN_ID" -report-dir="$GO_SPECS_REPORT_DIR" > "$INIT_OUT"
init_status=$?
if [ "$init_status" -ne 0 ]; then
  echo "init failed with exit $init_status" >&2
  cat "$INIT_OUT" >&2
  exit "$init_status"
fi
# `set -a; . file` (rather than `eval "$(...)"`) exports every KEY=value line from init's stdout
# as an environment variable in this shell; it works identically in POSIX sh, dash, and bash, and
# does not go through a second layer of shell quoting the way `eval` would.
set -a
. "$INIT_OUT"
set +a
rm -f "$INIT_OUT"

# 2. go test: record its status, do not abort the job on it (`set -u`, not `set -e`, around this).
go test -count=1 \
  -coverpkg="$COVERPKG" \
  -coverprofile="$REPORT_DIR/cover.out" \
  $TEST_PACKAGES
test_status=$?

# 3. finalize: always runs, even when go test failed.
"$GSR" finalize \
  -producers="$PRODUCERS_FILE" \
  -coverprofile="$REPORT_DIR/cover.out" \
  -json="$REPORT_DIR/report.json" \
  -xml="$REPORT_DIR/report.xml" \
  -cleanup
finalize_status=$?

echo "test_status=$test_status finalize_status=$finalize_status"

# 4. combine: the job fails if either step failed. test_status is reported as the test outcome;
# finalize_status is reported as the reporting outcome. Neither is derived from the other.
if [ "$test_status" -ne 0 ] || [ "$finalize_status" -ne 0 ]; then
  exit 1
fi
exit 0
```

Run once with `GO_SPECS_FIXTURE_HOOK_FAIL=1` (the e2e fixture's deliberate-failure gate — see
`report/coordination/internal/e2efixture/producera/producera_test.go`), which makes `go test`
fail: `finalize` still exits `0` (both producers still publish a complete shard, per contract
v1.2.9 §8's "tests fail, reporting succeeds" row), and the script's own final exit is `1` because
`test_status=1`. Run again with the hook-failure gate off but an extra, non-existent package
appended to the producers file: `go test` now exits `0`, but `finalize` exits `1` (`missing:
.../nonexistent` on stderr) and the script's final exit is `1` again — this time because
`finalize_status=1`, proving the two failure paths are independently reachable and both are
caught.

**GitHub Actions:**

```yaml
name: report
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"

      # init's KEY=value stdout lines append straight onto $GITHUB_ENV, so every later step in
      # this job inherits GO_SPECS_REPORT_SHARDS/_RUN_ID/_RUN_TOKEN/_REPORT_DIR automatically.
      - name: go-specs-report init
        run: |
          go run github.com/getsyntegrity/go-specs/cmd/go-specs-report@<version> init \
            -run-id="${{ github.run_id }}-${{ github.run_attempt }}-${{ github.job }}" \
            -report-dir="$RUNNER_TEMP/go-specs-runs" >> "$GITHUB_ENV"

      # $GITHUB_ENV is not masked automatically the way a declared `secrets:` value is, and the
      # run token is a credential in everything but name — mask it explicitly, once it is loaded.
      # Read it as a shell variable, not a `${{ env.* }}` expression: Actions prints a step's
      # script with expressions already expanded, which would log the token before the mask.
      - name: mask the run token
        run: echo "::add-mask::$GO_SPECS_RUN_TOKEN"

      # `continue-on-error: true` plus `id:` is what lets a later step read this step's outcome
      # without the job stopping here — the alternative this section's intro promised.
      - name: go test
        id: test
        continue-on-error: true
        run: go test -count=1 -coverprofile=cover.out ./...

      - name: go-specs-report finalize
        if: always()
        run: |
          mkdir -p artifacts
          go run github.com/getsyntegrity/go-specs/cmd/go-specs-report@<version> finalize \
            -producers=.go-specs/producers.txt \
            -coverprofile=cover.out \
            -json=artifacts/report.json \
            -xml=artifacts/report.xml \
            -html=artifacts/report.html \
            -cleanup

      - name: upload report artifacts
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: go-specs-report
          path: artifacts/

      # finalize's own non-zero exit already fails this job through the ordinary step-failure
      # mechanism (no `continue-on-error` on that step); this step re-surfaces the test outcome
      # that the `go test` step above deliberately swallowed, so both signals reach the job status.
      - name: fail the job if go test failed
        if: always() && steps.test.outcome == 'failure'
        run: exit 1
```

**Other CI runners:** the two examples above cover a generic POSIX shell script and GitHub
Actions. Any other runner that can execute an arbitrary shell command, and that offers a step
that always runs (so `finalize` is guaranteed to run even after a failing `go test`), can use the
generic script above unchanged. A runner without both of those — no arbitrary shell-command step,
no always-run step — cannot be relied on to run `finalize` after a failing `go test`, and if its
built-in test provider hardcodes `go test`'s flags without `-count=1` (mandatory above), it
silently reintroduces the cached-package failure mode.

#### `gc`: cleaning up abandoned runs

`finalize -cleanup` already removes a run's own directory once *that* run's merge fully succeeds
(contract v1.2.9 §5). `gc` exists for the runs that never got that far — a job killed by a timeout
or an out-of-memory kill before `finalize` ran, which leaves `run.json` and an unfinished shard
directory behind indefinitely otherwise. Run it as either:

- a **scheduled job** against the shared base directory your CI uses for `GO_SPECS_REPORT_DIR`
  (a nightly cron job is enough, since the retention window is generous by default), or
- a **step at the start of a CI job** that shares a `GO_SPECS_REPORT_DIR` across runs (for example,
  a persistent build agent's local disk), so abandoned runs from previous, killed jobs are swept
  before a new run starts.

`-retention` defaults to `24h` — comfortably larger than any plausible test run — and must be a
positive duration; `gc -retention=0s` is refused with exit `78` rather than silently treating every
run, including one still in progress, as abandoned. A run is stale purely by its `run.json`
marker's modification time, never by guessing whether the process that created it is still alive.
Run `gc -dry-run` first against a directory you do not fully trust yet — it prints exactly what a
real run would remove, without touching the filesystem, so you can confirm the retention window is
what you expect before deleting anything.

### What a shard contains, and what it does not

A shard carries execution data and the identity binding it to its run: the schema version, the run
id, the run's token digest, the package's original import path, a timestamp, and the package's
`NormalizedReport`.

It carries **no coverage** — not totals, not blocks, not a profile path. The invoker passes the one
combined `go test -coverprofile` file directly to the finalizer, which alone owns block
deduplication and coverage arithmetic. A producer that contributed its own numbers would corrupt
the module totals in a way nothing downstream could detect.
