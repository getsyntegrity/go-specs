# report-cli — #146 spec 3/3: `cmd/go-specs-report` (`init`, `finalize`, `gc`)

## Problem

`report/coordination` already has every primitive a multi-package reporting run needs:
`InitializeRun` creates the run marker, `Finalize` merges the shards, and `ExitCode` maps the
outcome. Spec 1 (#230) shipped them and spec 2 (#270) proved them end to end. A CI job still has
to write Go code to call them, though. There is also no way to remove the run directories that a
killed job leaves behind, and the contract's CI recipes are an explicit stub
(`docs/REPORTING.md`: "lands in the follow-up documentation issue").

## What changes

- A thin CLI, `cmd/go-specs-report`, with three verbs. `init` wraps `InitializeRun`, `finalize`
  wraps `Finalize` + `ExitCode`, and `gc` wraps a new retention primitive. The CLI parses flags and
  environment variables and prints results. Ownership, merge and render stay in
  `report/coordination`.
- A library `GC` primitive in `report/coordination`. It removes run directories whose `run.json`
  marker is older than a retention window (contract §5, "Abandoned markers"). It never runs
  automatically.
- A contract amendment (§8 and §13), plus CI documentation: generic CI, GitHub Actions and
  Shipwright, three `RunID` recipes and a producer-manifest recipe.

## Decisions (recorded before implementation)

1. **Thin CLI, stdlib `flag`.** It follows the only binary precedent in the repo
   (`tools/perfcheck`), so it adds no new dependency. Rejected: a subcommand framework such as
   cobra, which would be a new module dependency for three verbs.
2. **Missing shards are strict, with no permissive option.** Any missing or rejected producer
   exits `1`. Rejected: an `--allow-missing` flag for local use. It adds a mode that CI could
   inherit by accident. A local developer who wants a partial report can pass a shorter manifest,
   which is explicit and shows up in the command line. Contract §13 records strict-only as the
   final answer.
3. **A Finalize-side ownership failure exits `78`.** This covers a missing `run.json`, a wrong
   `RunID`, a wrong token, and a relative `BaseDir` that fails ownership. It is invalid
   configuration, like the preflight row. `ExitCode` already does this (spec 1). What is missing is
   the contract text: §8 gets its own row before the CLI exposes the code. An empty or unreadable
   producer manifest also exits `78`.
4. **`ExpectedProducers` comes from an explicit manifest file** (`--producers <file>`, one import
   path per line, `#` comments allowed). The CLI never runs `go list`. The docs show a
   reproducible `go list` pipeline that the *invoker* runs and checks in. That keeps the list
   reviewable, and a CI step can diff it to catch drift.
5. **`init` generates the token when none is given.** It prints `KEY=value` lines
   (`GO_SPECS_RUN_ID`, `GO_SPECS_RUN_TOKEN`, `GO_SPECS_REPORT_DIR`, `GO_SPECS_REPORT_SHARDS`) to
   stdout so a job can append them to `$GITHUB_ENV` or `source` them. `init` never invents the
   `RunID`, because the invoker owns its uniqueness (§5).
6. **Exit codes.** `0` means reporting succeeded, whatever the test result. `78` (`EX_CONFIG`)
   means invalid configuration. `1` means a reporting failure (missing or rejected producers, or a
   render or IO error). `2` means a CLI usage error such as an unknown verb or flag (stdlib `flag`
   convention).
7. **Delivery is a single PR against `develop`**, because the user asked for one. That overrides
   the ask-on-risk slicing default, even though the forecast exceeds about 400 lines.
8. **Contract version bump to v1.2.9.** Adding a §8 row is a normative change, so the contract
   follows its own changelog convention instead of editing v1.2.8 silently. §8's reporting-failure
   rows now name `1` instead of "distinct from `go test`'s codes". `1` comes from a separate
   process, so it cannot collide with `go test`'s own status.
9. **Feature document location.** This file lives in `docs/investigations/odd-tasks/`, following
   the repo relocation in #268, instead of `odd/tasks/`.
10. **Shipwright dropped by maintainer decision (2026-09-27).** "Shipwright" in T4's scope meant
    this repository's own `pablogore/shipwright` tool (`.shipwright/workflow.yaml`), not
    shipwright.io's Kubernetes `Build`/`ClusterBuildStrategy` CRDs — the original doc example used
    the wrong tool's API shape entirely. The maintainer decided to drop the Shipwright example
    because v0.12.0 cannot express this flow: no arbitrary shell-command step, no always-run step
    (both required to guarantee `finalize` still runs after a failing `go test`), and its
    `go-test` provider hardcodes `go test`'s flags without `-count=1` (mandatory per "`-count=1`
    is mandatory" above). `docs/REPORTING.md` now states these reasons briefly instead of carrying
    a non-working example; `.shipwright/` and the disabled `shipwright` job in `ci.yml` are left
    untouched, per the maintainer's instruction that a separate CI PR owns those.

## Tasks

- [x] T1 — Contract §8/§13 amendment and acceptance-criteria matrix posted on #146. Route: inline
  (1 doc file). Check: structural readback, `make fmt-check`.
- [x] T2 — `coordination.GC` retention primitive with behaviour tests (strict TDD). Route:
  delegated writer. Check: `go test ./report/coordination/`.
- [x] T3 — `cmd/go-specs-report` (`init`, `finalize`, `gc`) with behaviour tests: exits 0, 78 and
  1; strict missing producers; ownership; gc; the finalization barrier. Route: delegated writer
  (2+ non-trivial files). Check: `go test ./cmd/...`.
- [x] T4 — Docs: CLI reference, three `RunID` recipes, the manifest recipe, and generic CI, GitHub
  Actions and Shipwright examples. Route: delegated writer. Check: readback, and the example
  commands run against the fixture.
- [x] T5 — PR against `develop` referencing #146, with CI results. Route: inline. Check: CI green.
  Do not merge.

Out of this cut, to become a follow-up issue: AC-21's panicking and filtered specs through the real
`Finalize` e2e fixture. Those fixtures exist only in `integration_test.go`'s shard-emission tests.

## TDD

Strict TDD is on (user global config), with runner `go test`. Tests are written first, their RED
state is observed, then GREEN.

## Verification

`make fmt-check`, `go vet ./...`, `go build ./...`, `go test ./...`, and `go test -race ./...`
(explicitly requested for this change).

## Progress

- Branch `feat/146-report-cli` from `develop` @ `c4d15f7`.
- T1: contract bumped to v1.2.9 (§8 finalize-side ownership row, reporting-failure code `1`, §13
  items resolved). Matrix and decisions posted:
  https://github.com/getsyntegrity/go-specs/issues/146#issuecomment-5854139409
- T2: `coordination.GC` (`report/coordination/gc.go`, `gc_test.go`), plus a new
  `ReasonInvalidRetention` in `errors.go`. TDD: RED observed as
  `report/coordination/gc_test.go:40:14: undefined: GC` (build failure, `go test ./report/coordination/... -run TestGC`)
  before `gc.go` existed. GREEN: `go test ./report/coordination/... -run TestGC -v` — all 7 cases
  pass. Commit `b19c669` (`feat(coordination): add GC for abandoned run directories (#146)`).
  Verification: `go vet ./report/...` clean; `gofmt -l` clean; `go test ./report/...` all pass;
  `go test -race ./report/coordination/...` pass.
- T3: `cmd/go-specs-report` (`main.go`, `run.go`, `cmd_init.go`, `cmd_finalize.go`, `cmd_gc.go`,
  plus behaviour tests). TDD: RED observed as
  `cmd/go-specs-report/helpers_test.go:42:9: undefined: run` (build failure, `go test ./cmd/...`)
  before any CLI source file existed. GREEN: `go test ./cmd/... -v` — all 24 cases (including the
  6-case ownership/configuration table and the 2-case reporting-failure table) pass on first
  implementation. Commit `016ebc4`
  (`feat(cmd): add go-specs-report CLI with init, finalize and gc (#146)`).
  Verification: `make fmt-check` clean; `go vet ./...` clean; `go build ./...` clean;
  `go test ./...` all pass; `go test -race ./report/coordination/... ./cmd/...` pass.
  No deviation from the spec's flag/verb/exit-code design. Open question: `docs/REPORTING.md`
  (T4) has not been written yet, so `init`/`finalize`/`gc`'s exact flags are documented only in
  their own `-h` output and this file — T4 owns publishing the CI recipes.
- T4: `docs/REPORTING.md`'s stub replaced with "Running it in CI with `go-specs-report`"
  (install, the three verbs' flags/env fallbacks, exit codes 0/78/1/2, strict-missing-producers
  rationale, three `RunID` recipes, the producer-manifest `go list` pipeline with a CI drift
  check, worked generic/GitHub Actions/Shipwright examples, and `gc` usage). The existing
  library "Wiring a run" section is kept and now points at the CLI section as the
  lower-friction option. Commit `9a1e08d`
  (`docs(report): document go-specs-report CLI, RunID and manifest recipes, and CI examples (#146)`).
  README.md has no existing reporting section, so it was left unchanged per the task's own
  condition.

  Executable proof (built `go-specs-report` from this worktree, run against the real
  `report/coordination/internal/e2efixture/{producera,producerb}` fixtures, run id
  `proof-manual-*`, manifest listing both packages):
  - Hook-failure gate on (`GO_SPECS_FIXTURE_HOOK_FAIL=1`), correct manifest: `go test` exited
    `1` (producera's deliberately failing `BeforeAll`), `go-specs-report finalize` exited `0`
    ("found 2, missing 0, rejected 0"; `report.json`/`report.xml` written), and the generic
    script's own combined exit was `1` — driven entirely by the test failure, not by finalize.
  - Hook-failure gate off, manifest with one extra non-existent producer appended: `go test`
    exited `0`, `go-specs-report finalize` exited `1` ("found 2, missing 1, rejected 0",
    `missing: .../nonexistent`), and the script's combined exit was `1` — this time driven by
    finalize, proving the two failure paths are independently reachable.
  - `gc -retention=1ns -dry-run` then `gc -retention=1ns` against a freshly-`init`'d run:
    printed `would remove <id>` then `removed <id>`; `gc -retention=0s` correctly refused with
    exit `78` (retention must be positive).
  - YAML validation: the GitHub Actions and Shipwright examples were each extracted to a temp
    file and parsed with `python3 -c "import yaml; yaml.safe_load(open(...))"` — both parsed
    without error.

  Verification: `make fmt-check`: clean (no output). `go vet ./...`: clean. `go build ./...`:
  clean. `go test ./...`: all packages `ok` (no `-race`, per standing test-execution rules).
- T4 review fixes: the GitHub Actions mask step read the token through a `${{ env.* }}`
  expression, which Actions prints expanded before the mask applies, so it now uses the shell
  variable. The Shipwright script used bash-only `<(...)` under `/bin/sh`, plus an in-repo
  `go run ./cmd/...`. The producer-manifest filter matched any package whose tests *link*
  `report/coordination`, including `cmd/go-specs-report`, which is not a producer; it now matches
  test files that call `ShardWriterFromEnv`. The drift check is now executable. Re-verified with
  `dash`: the generator lists 5 packages (only `report/coordination` needs removing by hand, as
  documented); drift check exits 0 when clean and 1 when stale; the generic script exits
  test=1/finalize=0/script=1 and test=0/finalize=1/script=1; the Shipwright script exits 1 with
  hook failure on and 0 with it off, and writes `report.json` both times; both YAML blocks parse.
- Shipwright drop and `report-cli` CI job (2026-09-27): removed the Shipwright worked example from
  `docs/REPORTING.md` per decision #10 above, replacing it with a short "Other CI runners"
  paragraph; fixed the remaining "generic shell script, GitHub Actions, and Shipwright" mention to
  name only the two examples that remain. Added a new `report-cli` job to `.github/workflows/ci.yml`
  that dogfoods the documented GitHub Actions example for real: builds `cmd/go-specs-report`, runs
  it against `report/coordination/internal/e2efixture/{producera,producerb}` with
  `GO_SPECS_FIXTURE_HOOK_FAIL=1` (deliberately red `go test`), runs `finalize`, uploads the report
  directory, and asserts `steps.test.outcome == 'failure'` and `steps.finalize.outcome ==
  'success'` plus `report.json`'s execution totals (Total 4, Failed 1, Skipped 1, Passed 2) via
  `jq`. `.github/workflows/ci.yml` parses under `python3 -c "import yaml; yaml.safe_load(...)"`;
  `actionlint` is not installed on this machine, so the job was not additionally linted with it.

  Local emulation (temp dir under `/tmp/claude-1000/`, cleaned up after): built the CLI, wrote the
  two-line producer manifest, ran `init` against a `chmod 700` report base dir (this workstation's
  `umask 002` would otherwise make a fresh temp dir group-writable, which `init` itself correctly
  refuses per the "`GO_SPECS_REPORT_DIR`" section above — not a bug in the job, a property of a
  GitHub-hosted runner's already-private `$RUNNER_TEMP` that a local `/tmp` tree does not share),
  ran the gated-red `go test` (exit `1`, `TestProducerA` failing as expected), ran `finalize` (exit
  `0`, "found 2, missing 0, rejected 0"), and ran the same assertion logic as the new CI step
  (`report.json` totals `total=4 failed=1 skipped=1 passed=2`, matching
  `report/coordination/finalize_e2e_test.go`). Combined script exit: `0` — the assertion confirmed
  both outcomes matched what the job expects.
- T5: PR #279 opened; CI green on `2b5ca60` (run 36325013626): `test` incl. `-race`, `report-cli`,
  `goreleaser` pass; `shipwright` skipped (disabled on `develop`). Left for review, not merged.
