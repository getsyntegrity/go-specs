package main

import (
	"os"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/report/coordination"
)

// gc_test.go pins T3 of issue #146: the `gc` verb, which wraps coordination.GC (contract v1.2.9
// §5, "Abandoned markers"/"Cleanup/retention").

func TestGCRemovesAStaleRun(t *testing.T) {
	base := secureTempDir(t)
	own := initRun(t, base, "stale-run", validToken)

	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(own.MarkerPath, old, old); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, []string{"gc", "-report-dir", base, "-retention", "24h"}, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr)
	}
	if stdout == "" {
		t.Fatal("gc printed nothing about the removed run")
	}
	if _, err := os.Stat(own.MarkerPath); !os.IsNotExist(err) {
		t.Fatalf("stale-run was not removed: err=%v", err)
	}
}

func TestGCKeepsAFreshRun(t *testing.T) {
	base := secureTempDir(t)
	own := initRun(t, base, "fresh-run", validToken)

	_, stderr, code := runCLI(t, []string{"gc", "-report-dir", base, "-retention", "24h"}, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr)
	}
	if _, err := os.Stat(own.MarkerPath); err != nil {
		t.Fatalf("fresh-run was removed: %v", err)
	}
}

func TestGCDryRunRemovesNothing(t *testing.T) {
	base := secureTempDir(t)
	own := initRun(t, base, "stale-run", validToken)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(own.MarkerPath, old, old); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runCLI(t, []string{"gc", "-report-dir", base, "-retention", "24h", "-dry-run"}, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr)
	}
	if stdout == "" {
		t.Fatal("dry-run gc printed nothing about what it would remove")
	}
	if _, err := os.Stat(own.MarkerPath); err != nil {
		t.Fatalf("dry-run actually removed stale-run: %v", err)
	}
}

func TestGCRelativeReportDirFailsWithExitConfig(t *testing.T) {
	_, stderr, code := runCLI(t, []string{"gc", "-report-dir", "relative/runs"}, nil)
	if code != coordination.ExitConfig {
		t.Fatalf("exit code = %d, want %d (ExitConfig); stderr=%s", code, coordination.ExitConfig, stderr)
	}
}

func TestGCFallsBackToTheReportDirEnvironmentVariable(t *testing.T) {
	base := secureTempDir(t)
	own := initRun(t, base, "stale-run", validToken)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(own.MarkerPath, old, old); err != nil {
		t.Fatal(err)
	}

	env := map[string]string{coordination.EnvReportDir: base}
	_, stderr, code := runCLI(t, []string{"gc", "-retention", "24h"}, env)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr)
	}
	if _, err := os.Stat(own.MarkerPath); !os.IsNotExist(err) {
		t.Fatalf("stale-run was not removed via env fallback: err=%v", err)
	}
}
