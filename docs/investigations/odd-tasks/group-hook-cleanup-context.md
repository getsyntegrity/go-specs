# group-hook-cleanup-context (go-specs #256)

Branch `fix/256-group-hook-cleanup-context`, base `origin/develop` `162c2f5` (after #255).

## Problem

`runHook` in `specs/group_hooks.go` acquires a pooled `Context` and subtest backend for every
`BeforeAll`/`AfterAll` and releases both as soon as the hook returns. `testing` runs the group
subtest's `Cleanup` functions only when that subtest ends, after every spec and `AfterAll`. A
cleanup registered in the idiomatic form `ctx.T.Cleanup(func() { ctx.T.Error("...") })` therefore
reads a `Context` that is already back in the pool: `ctx.T` is `nil` and the binary crashes, or
the `Context` has been handed to another spec and the failure lands on the wrong subtest.

## What changes

Each hooked group gets one hook `Context`, bound to the group subtest `gt`, shared by the group's
hooks (reset between hooks) and released only after the group's `t.Run` has returned, which is
after its cleanups. This is the same boundary #255 set for spec bodies. A failure raised only from
such a cleanup is reported as the group's `[AfterAll]` case, because cleanups are part of the
group's teardown and testing gives no way to tell which hook registered the cleanup.

Rejected: registering a release closure with `gt.Cleanup` before the first hook. It works, but it
allocates a closure per group, and #232 just removed exactly that kind of per-Context closure.

## Constraints

- No allocation added to suites without group hooks (`TestSuiteWithoutGroupHooksAllocatesNoGroupStorage`).
- Public DSL unchanged. No `-race`, no workbench locally.
- TDD: strict (user CLAUDE.md), runner `go test ./specs/...`.

## Tasks

- [x] T1 RED: subprocess regression test, idiomatic `ctx.T.Cleanup` from `BeforeAll` and `AfterAll`
      (plus `ctx.Expect` in a cleanup); must crash / misreport on the current code.
- [x] T2 GREEN: keep the hook `Context` bound until the group's `t.Run` returns.
- [x] T3 Report a cleanup-only group failure as an `[AfterAll]` case.
- [x] T4 Docs: `docs/SUITE_HOOKS_CONTRACT.md` hook lifetime + CHANGELOG.

## Checks

`make fmt-check`, `go vet ./...`, `go test ./...` (no `-race`).

## Progress

All four tasks landed in one work-unit commit (route: inline; one production file, one test
file, two docs; the behavior, its test and its docs belong together).

- RED observed: `ctx.T` cases crashed with `CLEANUP_T=<nil>` + nil pointer panic; the
  `ctx.Expect` cases were worse — the process PASSED, the assertion silently lost.
- GREEN: `TestGroupHookCleanupRunsAgainstTheGroupRealProcess` passes all four cases.
- `make fmt-check`, `go vet ./...`, `go test ./...`: green, no FAIL lines.
- RDD: off (clone_local), no review run.
