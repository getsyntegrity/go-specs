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

## Tasks

- [x] T1 — Contract §8/§13 amendment and acceptance-criteria matrix posted on #146. Route: inline
  (1 doc file). Check: structural readback, `make fmt-check`.
- [x] T2 — `coordination.GC` retention primitive with behaviour tests (strict TDD). Route:
  delegated writer. Check: `go test ./report/coordination/`.
- [x] T3 — `cmd/go-specs-report` (`init`, `finalize`, `gc`) with behaviour tests: exits 0, 78 and
  1; strict missing producers; ownership; gc; the finalization barrier. Route: delegated writer
  (2+ non-trivial files). Check: `go test ./cmd/...`.
- [ ] T4 — Docs: CLI reference, three `RunID` recipes, the manifest recipe, and generic CI, GitHub
  Actions and Shipwright examples. Route: delegated writer. Check: readback, and the example
  commands run against the fixture.
- [ ] T5 — PR against `develop` referencing #146, with CI results. Route: inline. Check: CI green.
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
