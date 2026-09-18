# 06 · Snapshots

> **Audience:** users · **Reading time:** ~8 minutes

Snapshot testing records a value once and asserts it has not changed since. It is the right tool
when the expected value is large, structural and boring to write by hand — a rendered report, a
serialized DTO, a computed ledger — and the wrong tool when the expectation is a single meaningful
number you should be asserting explicitly.

---

## 1. Usage

```go
s.It("creates a user", func(ctx *specs.Context) {
    ctx.Snapshot("create-user", map[string]any{"id": 123, "name": "alice"})
})
```

First run with updates enabled writes the snapshot. Every later run compares against it.

```bash
GO_SPECS_UPDATE_SNAPSHOTS=1 go test ./...   # create or refresh
go test ./...                                # compare
```

## 2. Where snapshots live

```
examples/snapshots/
├── snapshots_test.go
└── __snapshots__/
    └── snapshots_test.snap.json
```

One file per test file, named after the test file's basename, in a `__snapshots__/` directory beside
it. The path is derived from the calling file, not from the test name, so moving a test between
files moves its snapshot.

File contents are JSON, keys sorted deterministically and the document indented — so a diff in code
review is a diff of the value, not a diff of key ordering.

```json
{
  "create-user": {
    "id": 123,
    "name": "alice"
  }
}
```

## 3. Comparison is semantic, not byte-for-byte

Both the stored snapshot and the new value are unmarshalled to `any` and compared with
`reflect.DeepEqual`. Byte comparison would be faster and would be wrong: JSON key order and numeric
representation are not stable enough to carry meaning, and a re-marshalled identical value can
produce different bytes.

Correctness over speed, deliberately — [ADR-0010](adr/0010-snapshot-durability-and-semantics.md).

## 4. Durability

Two failure modes were designed out, both the hard way.

### Writes are atomic

`Save` never truncates the destination in place. It writes to a temporary file **in the same
directory**, `Sync`s it, closes it, sets its mode, and only then renames over the destination.

The temporary file is a sibling rather than something in `os.TempDir()` for a specific reason:
`rename` is only atomic **within one filesystem**. A temp directory on another mount would turn the
atomic rename into a copy, reopening exactly the window the design closes. A crash mid-write
therefore leaves the previous snapshot intact — never a truncated one (#152, #158).

### Concurrent specs do not clobber each other

The load-mutate-save cycle is serialized per file through a lock map, so several specs snapshotting
into the same file in the same process cannot lose each other's writes (#27).

**Scope:** this lock is per process. Two `go test` binaries writing the same snapshot file
concurrently are not protected — and no in-process lock could protect them.

## 5. Failure is observable before it is fatal

A snapshot mismatch reports through `Fatalf`, which calls `runtime.Goexit` and never returns. So the
verdict and the reporting are deliberately separated:

| Function | Role |
| --- | --- |
| `snapshots.Evaluate(helper, callerFile, name, value) Result` | pure verdict, reports nothing |
| `snapshots.RunFromFile(backend, callerFile, name, value) bool` | evaluates, then reports via `Fatalf` on failure |

`specs` calls `Evaluate`, records the failure into the context, and only then triggers the reporting
path. Without that ordering the mismatch would be invisible to the reporter, because `Goexit` would
unwind before the state was written (#115, #136).

This is the same class of problem as the `AfterEach`-survives-`Goexit` rule in
[03 · Execution model](03-EXECUTION-MODEL.md#the-goexit-problem-and-the-fix): in Go, anything you
need to happen after a fatal has to be arranged before it.

## 6. Reviewing snapshot changes

A snapshot diff is a behaviour diff. Three rules that keep the mechanism honest:

1. **Commit snapshots.** A snapshot that is not in version control asserts nothing.
2. **Read the diff before regenerating.** `GO_SPECS_UPDATE_SNAPSHOTS=1` is how a real regression gets
   blessed into the baseline. Regenerating to make a red build green is how a bug ships.
3. **Name snapshots after the behaviour, not the test.** `"create-user"` survives a test rename;
   `"test-case-3"` does not.

## 7. Not to be confused with golden files

The repository has a second, unrelated mechanism for its **own** tests: `report/testdata/golden/`
holds the four rendered report formats, compared against a fixed fixture and regenerated with
`UPDATE_GOLDEN=1 go test ./report/...`.

| | User snapshots | Report golden files |
| --- | --- | --- |
| Package | `snapshots` | `report` test suite |
| Location | `__snapshots__/` beside the test | `report/testdata/golden/` |
| Refresh | `GO_SPECS_UPDATE_SNAPSHOTS=1` | `UPDATE_GOLDEN=1` |
| Audience | users of go-specs | contributors to go-specs |
