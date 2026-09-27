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
	result := runSnapshot(fake, "test.go", "nonexistent-key", 42)
	if result.Passed {
		t.Fatal("expected the comparison to fail when the snapshot key is missing")
	}
	if !strings.Contains(result.Message, "missing") {
		t.Errorf("expected message about missing snapshot, got: %s", result.Message)
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

// TestSnapshotFailureRecordsContextFailure guards issue #115: a failing ctx.Snapshot must flip
// ctx.hasFailed() like every other assertion, since that flag is what SpecResultEvent.Failed and
// SuiteEndEvent.FailedSpecs are built from. Before the fix, the mismatch verdict was decided inside
// snapshots.RunFromFile, which reported straight to the backend (so `go test` still exited red)
// without ever touching ctx.hasFailed() — so a reporter-driven consumer (JUnit writer, CI summary, flake
// tracker) was told the spec passed when it hadn't.
func TestSnapshotFailureRecordsContextFailure(t *testing.T) {
	fake := &fakeSnapshotBackend{}
	ctx := &Context{backend: fake}

	ctx.Snapshot("nonexistent-key-pinning-the-unrecorded-failure", 42)

	if fake.fatalfMsg == "" {
		t.Fatal("expected the snapshot mismatch to be reported to the backend")
	}
	if !ctx.hasFailed() {
		t.Fatal("expected ctx.hasFailed() to be set on a snapshot mismatch, matching every other assertion")
	}
}
