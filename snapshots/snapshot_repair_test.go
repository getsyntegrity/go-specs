package snapshots

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCorruptSnapshot plants a snapshot file holding merge-conflict markers, the realistic way a
// snapshot file stops parsing (issue #242), and returns the caller file and snapshot path.
func writeCorruptSnapshot(t *testing.T) (callerFile, snapshotPath string) {
	t.Helper()
	dir := t.TempDir()
	callerFile = filepath.Join(dir, "corrupt_test.go")
	snapshotPath = filepath.Join(dir, "__snapshots__", "corrupt_test.snap.json")
	if err := os.MkdirAll(filepath.Dir(snapshotPath), 0755); err != nil {
		t.Fatal(err)
	}
	corrupt := "{\n<<<<<<< HEAD\n  \"a\": 1\n=======\n  \"a\": 2\n>>>>>>> branch\n}\n"
	if err := os.WriteFile(snapshotPath, []byte(corrupt), 0644); err != nil {
		t.Fatal(err)
	}
	return callerFile, snapshotPath
}

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := warnOutput
	warnOutput = &buf
	t.Cleanup(func() { warnOutput = prev })
	return &buf
}

// TestEvaluate_UpdateModeRepairsCorruptSnapshotFile verifies #242: in update mode an unparseable
// snapshot file is treated as empty and rewritten, and the discarded file is named in a warning.
func TestEvaluate_UpdateModeRepairsCorruptSnapshotFile(t *testing.T) {
	t.Setenv(UpdateSnapshotsEnv, "1")
	warnings := captureWarnings(t)
	callerFile, snapshotPath := writeCorruptSnapshot(t)

	result := Evaluate(nil, callerFile, "a", 3)
	if !result.Passed {
		t.Fatalf("expected update mode to repair the corrupt file, got failure: %s", result.Message)
	}

	data, err := Load(snapshotPath)
	if err != nil {
		t.Fatalf("Load after repair: %v", err)
	}
	if len(data) != 1 || string(data["a"]) != "3" {
		t.Fatalf("expected the rewritten file to hold only a=3, got %v", data)
	}
	if got := warnings.String(); !strings.Contains(got, snapshotPath) {
		t.Fatalf("expected a warning naming %s, got %q", snapshotPath, got)
	}
}

// TestEvaluate_CorruptSnapshotFileStillFailsOutsideUpdateMode pins that the repair is limited to
// update mode: a normal run keeps reporting the parse error instead of silently passing.
func TestEvaluate_CorruptSnapshotFileStillFailsOutsideUpdateMode(t *testing.T) {
	t.Setenv(UpdateSnapshotsEnv, "")
	warnings := captureWarnings(t)
	callerFile, _ := writeCorruptSnapshot(t)

	result := Evaluate(nil, callerFile, "a", 3)
	if result.Passed {
		t.Fatal("expected a corrupt snapshot file to fail outside update mode")
	}
	if !strings.Contains(result.Message, "parse snapshot file") {
		t.Fatalf("expected the parse error in the message, got %q", result.Message)
	}
	if warnings.Len() != 0 {
		t.Fatalf("expected no repair warning outside update mode, got %q", warnings.String())
	}
}

// TestEvaluate_UpdateModeStillFailsOnReadError pins that only a parse failure is repaired: an
// unreadable snapshot path is still an error in update mode, never a reason to overwrite it.
func TestEvaluate_UpdateModeStillFailsOnReadError(t *testing.T) {
	t.Setenv(UpdateSnapshotsEnv, "1")
	captureWarnings(t)
	dir := t.TempDir()
	callerFile := filepath.Join(dir, "unreadable_test.go")
	// A directory where the snapshot file should be makes ReadFile fail without being a parse error.
	if err := os.MkdirAll(filepath.Join(dir, "__snapshots__", "unreadable_test.snap.json"), 0755); err != nil {
		t.Fatal(err)
	}

	result := Evaluate(nil, callerFile, "a", 3)
	if result.Passed {
		t.Fatal("expected a read error to keep failing in update mode")
	}
	if !strings.Contains(result.Message, "snapshot: load") {
		t.Fatalf("expected the load error in the message, got %q", result.Message)
	}
}
