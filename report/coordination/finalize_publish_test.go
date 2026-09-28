package coordination

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// TestFinalizePublishesReportsWithTheDocumentedPermissions pins issue #319: os.CreateTemp creates
// 0600 files, which a runner or artifact collector under another user cannot read. Every published
// report is 0644, whatever the process umask.
func TestFinalizePublishesReportsWithTheDocumentedPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	publishShard(t, base, own, "run-1", validToken, "example.com/m/alpha", sampleReport())

	out := t.TempDir()
	targets := targetPaths(t, out)
	if _, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		ExpectedProducers: []string{"example.com/m/alpha"},
		Targets:           targets,
	}); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	for _, tg := range targets {
		info, err := os.Stat(tg.Path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != ReportFileMode {
			t.Fatalf("%s mode = %04o, want %04o", tg.Path, got, ReportFileMode)
		}
	}
}

// TestFinalizeNamesTheTargetThatFailedAndKeepsEarlierOnesPublished pins the multi-target contract:
// targets are published one at a time, each atomically, so a later failure leaves the earlier
// targets published and the error identifies the failing one.
func TestFinalizeNamesTheTargetThatFailedAndKeepsEarlierOnesPublished(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	publishShard(t, base, own, "run-1", validToken, "example.com/m/alpha", sampleReport())

	out := t.TempDir()
	first := filepath.Join(out, "first.json")
	// A directory at the target path makes the final rename fail.
	blocked := filepath.Join(out, "blocked.txt")
	if err := os.MkdirAll(filepath.Join(blocked, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		ExpectedProducers: []string{"example.com/m/alpha"},
		Targets: []report.Target{
			{Format: report.FormatJSON, Path: first},
			{Format: report.FormatTXT, Path: blocked},
		},
	})
	if err == nil {
		t.Fatal("Finalize succeeded although the second target could not be published")
	}
	if !strings.Contains(err.Error(), blocked) {
		t.Fatalf("error %q does not name the failing target %s", err, blocked)
	}
	if strings.Contains(err.Error(), first) {
		t.Fatalf("error %q names %s, which was published successfully", err, first)
	}
	if info, statErr := os.Stat(first); statErr != nil || info.Size() == 0 {
		t.Fatalf("the earlier target was not left published (err=%v)", statErr)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 2 {
		t.Fatalf("temp files leaked next to the targets: %v", entries)
	}
}

func TestSyncDirSucceedsOnADirectoryAndReportsARealFailure(t *testing.T) {
	if err := syncDir(t.TempDir()); err != nil {
		t.Fatalf("syncDir on an existing directory: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := syncDir(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("syncDir on a missing directory = %v, want os.ErrNotExist", err)
	}
}
