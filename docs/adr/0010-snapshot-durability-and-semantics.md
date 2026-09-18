# ADR-0010: Snapshots Compare Semantically, Write Atomically, and Fail Observably

## Status

Accepted.

## Date

2026-09-18 (recorded). The three parts landed across #27, #115/#136 and #152/#158.

## Deciders

go-specs maintainers, via #27, #115, #136, #152, #158.

## Context

A snapshot file is a test's expected value living on disk. That gives it three failure modes a
normal assertion does not have.

**It can be compared wrongly.** JSON round-trips are not byte-stable: key order and numeric
representation both vary. A re-marshalled identical value can produce different bytes, and a byte
comparison would report a difference that does not exist.

**It can be destroyed.** A truncate-then-write is not atomic. A crash, a `SIGKILL` or a full disk
mid-write leaves a truncated file — and the truncated file is the only record of what the expected
value was.

**Its failure can be invisible.** Reporting a mismatch goes through `Fatalf`, which calls
`runtime.Goexit` and never returns. Anything that needed to happen after the mismatch was recorded —
including recording it — does not happen.

And because several specs in one package can snapshot into the same file, the load-mutate-save cycle
is a read-modify-write race.

## Decision

### Comparison is semantic

Both the stored snapshot and the new value are unmarshalled to `any` and compared with
`reflect.DeepEqual`. Correctness over speed, explicitly: a snapshot comparison runs once per
snapshot, not once per assertion, so the reflected path is affordable where it would not be in
[04 · Assertions](../04-ASSERTIONS.md).

Stored files have sorted keys and indented JSON, so a review diff is a diff of the value rather than
of formatting.

### Writes are atomic

`Save` never truncates in place. It writes a temporary file **in the same directory**, syncs it,
closes it, sets its mode, and renames over the destination.

The temporary file is a sibling, not something in `os.TempDir()`, because `rename` is only atomic
**within one filesystem**. A temp directory on another mount would silently turn the atomic rename
into a copy, reopening the exact window this closes.

### Concurrent writers are serialized

The load-mutate-save cycle is serialized per file through a lock map, so several specs snapshotting
into the same file in the same process cannot lose each other's writes.

**Scope, stated plainly:** this is per process. Two `go test` binaries writing the same snapshot file
concurrently are not protected, and no in-process lock could protect them.

### Verdict is separated from reporting

```go
snapshots.Evaluate(helper, callerFile, name, value) Result   // pure verdict, reports nothing
snapshots.RunFromFile(backend, callerFile, name, value) bool // evaluates, then reports via Fatalf
```

`specs` calls `Evaluate`, records the failure into the context, and only then triggers the reporting
path that may not return. This is the same shape as the Goexit-safe hook ordering in
[ADR-0004](0004-subtest-isolation-and-goexit-safe-hooks.md): in Go, anything that must happen after a
fatal has to be arranged before it.

### Location and update are conventions, not configuration

Snapshots live at `<dir>/__snapshots__/<testfile-basename>.snap.json`, derived from the **calling
file**. Update mode is `GO_SPECS_UPDATE_SNAPSHOTS=1`.

## Decision classification

### Accepted now

- All five parts above.
- Deriving the path from the caller's file rather than the test name, so moving a test between files
  moves its snapshot with it.

### Deferred

- Cross-process file locking. It would need an OS-level lock with its own failure and staleness
  modes, and the case for it — two `go test` binaries sharing one snapshot file — is a layout problem
  more cheaply fixed by not sharing the file.
- Custom serialization formats. `RegisterSnapshotMatcher` already covers domain-aware comparison,
  which is the usual reason to want one.

### Open hypotheses

- That JSON is sufficient. A value that does not round-trip through JSON cannot be snapshotted, and
  the failure is at marshal time rather than at comparison time.

## Consequences

**Benefits:** a snapshot survives a crash mid-write; concurrent specs in one package do not clobber
each other; a mismatch is visible to reporters instead of vanishing into `Goexit`; a diff in review
shows the value change and nothing else.

**Costs:** the temp-file-plus-rename write is two syscalls and a directory entry rather than one
write, paid per save — acceptable because saves are rare and reads are the common path. The
per-process lock adds a map lookup per snapshot operation. And the `Evaluate`/`RunFromFile` split is
an API shape that exists purely to work around `Goexit`; it needs its comment, or a future
contributor will helpfully collapse it back into one function and reintroduce the bug.

## Rejected / deferred alternatives

- **Byte comparison of the JSON** — rejected. Faster and wrong: JSON key order and numeric
  representation are not stable enough to carry meaning.
- **Write in place with `os.WriteFile`** — rejected by #152. It is the truncation window.
- **Temporary file in `os.TempDir()`** — rejected. Cross-filesystem rename is not atomic, so this
  looks correct and is not.
- **One snapshot file per test case** — rejected. It trades the write race for directory sprawl and
  makes reviewing a set of related snapshot changes harder.
- **Report the mismatch first, then evaluate** — impossible; the report does not return.

## Related work

- #27 — concurrent snapshot writes.
- #115, #136 — snapshot failure observable before `Fatalf`/`Goexit`.
- #152, #158 — atomic snapshot replacement.
- [ADR-0004](0004-subtest-isolation-and-goexit-safe-hooks.md) — the same Goexit problem in the runner.
- [06 · Snapshots](../06-SNAPSHOTS.md).
