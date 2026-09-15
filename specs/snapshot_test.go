package specs

import (
	"fmt"
	"strings"
	"testing"
)

func TestSnapshotMatchPasses(t *testing.T) {
	Describe(t, "SnapshotMatch", func(s *Spec) {
		s.It("matches stored snapshot", func(ctx *Context) {
			ctx.Snapshot("create-user", map[string]any{
				"id":   123,
				"name": "alice",
			})
		})
	})
}

func TestSnapshotMissingFails(t *testing.T) {
	fake := &fakeSnapshotBackend{}
	runSnapshot(fake, "test.go", "nonexistent-key", 42)
	if fake.fatalfMsg == "" {
		t.Fatal("expected Fatalf when snapshot key is missing")
	}
	if fake.fatalfMsg != "" && !strings.Contains(fake.fatalfMsg, "missing") {
		t.Errorf("expected message about missing snapshot, got: %s", fake.fatalfMsg)
	}
}

type fakeSnapshotBackend struct {
	fatalfMsg string
}

func (f *fakeSnapshotBackend) Helper() {}

func (f *fakeSnapshotBackend) FailNow() {}

func (f *fakeSnapshotBackend) Fatal(args ...any) {
	f.fatalfMsg = fmt.Sprint(args...)
}

func (f *fakeSnapshotBackend) Fatalf(format string, args ...any) {
	f.fatalfMsg = fmt.Sprintf(format, args...)
}

func (f *fakeSnapshotBackend) Error(args ...any) {}

func (f *fakeSnapshotBackend) Errorf(format string, args ...any) {}

func (f *fakeSnapshotBackend) Log(args ...any) {}

func (f *fakeSnapshotBackend) Logf(format string, args ...any) {}

func (f *fakeSnapshotBackend) Name() string { return "" }

func (f *fakeSnapshotBackend) Cleanup(func()) {}

func (f *fakeSnapshotBackend) Run(name string, fn func(testing.TB)) { fn(nil) }

// TestSnapshotFailureLeavesContextUnfailed pins a defect, not a guarantee.
//
// Every other assertion calls Context.recordFailure() on each of its failure branches, which is
// what SpecResultEvent.Failed is built from. Context.Snapshot calls it only when runtime.Caller
// fails; the mismatch verdict is decided inside snapshots.RunFromFile, which reports to the backend
// itself and never touches ctx.failed. So a failing snapshot exits `go test` red while the reporter
// is told the spec passed: Failed is false, and SuiteEndEvent.FailedSpecs does not count it.
//
// Any reporter-driven consumer — a JUnit writer, a CI summary, a flake tracker — disagrees with the
// exit code for exactly this one assertion.
//
// This predates the source-attribution fix and is not fixed here; see issue #115. The assertion
// below deliberately requires ctx.failed to stay false, so whoever fixes it has to change this test
// on purpose rather than discovering the behaviour changed underneath them.
func TestSnapshotFailureLeavesContextUnfailed(t *testing.T) {
	fake := &fakeSnapshotBackend{}
	ctx := &Context{backend: fake}

	ctx.Snapshot("nonexistent-key-pinning-the-unrecorded-failure", 42)

	if fake.fatalfMsg == "" {
		t.Fatal("expected the snapshot mismatch to be reported to the backend")
	}
	if ctx.failed {
		t.Fatal("ctx.failed is now set on a snapshot mismatch — the defect this test pins is fixed; " +
			"assert Failed is true instead and close issue #115")
	}
}
