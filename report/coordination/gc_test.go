package coordination

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
)

// gc_test.go pins T2 of issue #146 (spec 3 of 3): the GC retention primitive (contract v1.2.9 §5,
// "Abandoned markers" and "Cleanup/retention"). GC is the only mechanism that removes the marker
// of a run killed before finalize, and it is never triggered automatically — every case here calls
// it explicitly, the same way the `gc` CLI verb will.

// ageMarker back-dates runID's run.json so it looks like it was created `age` ago, the same
// staleness signal a killed run would show days later.
func ageMarker(t *testing.T, base string, runID RunID, age time.Duration) {
	t.Helper()
	path := markerPath(base, runID)
	old := time.Now().Add(-age)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestGCRemovesRunsWhoseMarkerIsOlderThanRetention(t *testing.T) {
	base := secureTempDir(t)
	mustInitRun(t, base, "stale-run", validToken)
	mustInitRun(t, base, "fresh-run", validToken)
	ageMarker(t, base, "stale-run", 48*time.Hour)

	res, err := GC(context.Background(), GCOptions{
		BaseDir:   base,
		Retention: 24 * time.Hour,
		Now:       fixedNow(time.Now()),
	})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "stale-run" {
		t.Fatalf("Removed = %v, want [stale-run]", res.Removed)
	}
	if len(res.Kept) != 1 || res.Kept[0] != "fresh-run" {
		t.Fatalf("Kept = %v, want [fresh-run]", res.Kept)
	}
	if _, err := os.Stat(runDir(base, "stale-run")); !os.IsNotExist(err) {
		t.Fatalf("stale-run directory still exists after GC: err=%v", err)
	}
	if _, err := os.Stat(runDir(base, "fresh-run")); err != nil {
		t.Fatalf("fresh-run directory was removed: %v", err)
	}
}

func TestGCDryRunRemovesNothing(t *testing.T) {
	base := secureTempDir(t)
	mustInitRun(t, base, "stale-run", validToken)
	ageMarker(t, base, "stale-run", 48*time.Hour)

	res, err := GC(context.Background(), GCOptions{
		BaseDir:   base,
		Retention: 24 * time.Hour,
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "stale-run" {
		t.Fatalf("Removed = %v, want [stale-run] to be reported even though DryRun is set", res.Removed)
	}
	if _, err := os.Stat(runDir(base, "stale-run")); err != nil {
		t.Fatalf("DryRun actually removed the run directory: %v", err)
	}
}

func TestGCSkipsEntriesItCannotProveAreAbandonedRunDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink on Windows needs a privilege ordinary CI accounts do not hold")
	}
	base := secureTempDir(t)

	// Not a valid RunID at all.
	if err := os.Mkdir(filepath.Join(base, "not a run id!"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A valid-looking RunID directory with no run.json.
	if err := os.Mkdir(filepath.Join(base, "no-marker-run"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A symlink at the top level, even one named like a valid RunID.
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(base, "symlinked-run")); err != nil {
		t.Fatal(err)
	}

	res, err := GC(context.Background(), GCOptions{
		BaseDir:   base,
		Retention: time.Nanosecond, // everything real would be "stale" if it were considered
	})
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(res.Removed) != 0 {
		t.Fatalf("Removed = %v, want none: nothing here is provably an abandoned run directory", res.Removed)
	}
	if len(res.Skipped) != 3 {
		t.Fatalf("Skipped = %+v, want 3 entries", res.Skipped)
	}
	skippedNames := make([]string, len(res.Skipped))
	for i, s := range res.Skipped {
		skippedNames[i] = s.Name
		if s.Reason == "" {
			t.Fatalf("skipped entry %q has no reason", s.Name)
		}
	}
	sort.Strings(skippedNames)
	want := []string{"no-marker-run", "not a run id!", "symlinked-run"}
	sort.Strings(want)
	if skippedNames[0] != want[0] || skippedNames[1] != want[1] || skippedNames[2] != want[2] {
		t.Fatalf("skipped names = %v, want %v", skippedNames, want)
	}

	for _, name := range []string{"not a run id!", "no-marker-run", "symlinked-run"} {
		if _, err := os.Lstat(filepath.Join(base, name)); err != nil {
			t.Fatalf("%s was removed, but GC could not prove it was an abandoned run directory: %v", name, err)
		}
	}
}

func TestGCRejectsRelativeBaseDir(t *testing.T) {
	_, err := GC(context.Background(), GCOptions{BaseDir: "relative/runs", Retention: time.Hour})
	if err == nil {
		t.Fatal("GC accepted a relative BaseDir")
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("GC error is not a *ConfigError: %v (%T)", err, err)
	}
	if cfgErr.Reason != ReasonInvalidReportDir {
		t.Fatalf("ConfigError.Reason = %q, want %q", cfgErr.Reason, ReasonInvalidReportDir)
	}
	// ExitCode already understands any *ConfigError, including GC's own — this is the mechanism
	// the `gc` CLI verb relies on to report exit 78 without reimplementing the mapping.
	if ec := ExitCode(FinalizeResult{}, err); ec != ExitConfig {
		t.Fatalf("ExitCode(err) = %d, want %d (ExitConfig)", ec, ExitConfig)
	}
}

func TestGCRejectsNonPositiveRetention(t *testing.T) {
	base := secureTempDir(t)
	_, err := GC(context.Background(), GCOptions{BaseDir: base, Retention: 0})
	if err == nil {
		t.Fatal("GC accepted a zero Retention")
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("GC error is not a *ConfigError: %v (%T)", err, err)
	}

	_, err = GC(context.Background(), GCOptions{BaseDir: base, Retention: -time.Hour})
	if err == nil {
		t.Fatal("GC accepted a negative Retention")
	}
	if !errors.As(err, &cfgErr) {
		t.Fatalf("GC error is not a *ConfigError: %v (%T)", err, err)
	}
}

func TestGCOnMissingBaseDirReturnsEmptyResultNoError(t *testing.T) {
	base := filepath.Join(secureTempDir(t), "does-not-exist")
	res, err := GC(context.Background(), GCOptions{BaseDir: base, Retention: time.Hour})
	if err != nil {
		t.Fatalf("GC: %v, want nil: a missing BaseDir means nothing to collect, not an error", err)
	}
	if len(res.Removed) != 0 || len(res.Kept) != 0 || len(res.Skipped) != 0 {
		t.Fatalf("GCResult = %+v, want an entirely empty result", res)
	}
}

func TestGCHonoursContextCancellation(t *testing.T) {
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := GC(ctx, GCOptions{BaseDir: base, Retention: time.Hour})
	if err == nil {
		t.Fatal("GC ignored an already-cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GC error = %v, want context.Canceled", err)
	}
}
