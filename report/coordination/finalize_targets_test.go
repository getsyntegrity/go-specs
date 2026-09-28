package coordination

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// TestFinalizeRejectsUnsafeCleanupAndTargetsBeforeTouchingAnything pins issue #309: a Cleanup
// request that could leave no report anywhere, or a target inside the run directory that cleanup
// would delete after rendering it, is a configuration failure detected before any rendering or
// deletion, and the shards stay available.
func TestFinalizeRejectsUnsafeCleanupAndTargetsBeforeTouchingAnything(t *testing.T) {
	setup := func(t *testing.T) (base string) {
		t.Helper()
		base = secureTempDir(t)
		own := mustInitRun(t, base, "run-1", validToken)
		publishShard(t, base, own, "run-1", validToken, "example.com/m/alpha", sampleReport())
		return base
	}
	opts := func(base string, targets []report.Target) FinalizeOptions {
		return FinalizeOptions{
			RunID: "run-1", Token: validToken, BaseDir: base,
			ExpectedProducers: []string{"example.com/m/alpha"},
			Targets:           targets,
			Cleanup:           true,
		}
	}
	assertRejected := func(t *testing.T, base string, res FinalizeResult, err error) {
		t.Helper()
		var cfgErr *ConfigError
		if !errors.As(err, &cfgErr) {
			t.Fatalf("err = %v, want a *ConfigError", err)
		}
		if ec := ExitCode(res, err); ec != ExitConfig {
			t.Fatalf("ExitCode = %d, want %d", ec, ExitConfig)
		}
		names := shardNames(t, base, "run-1")
		if len(names) != 1 {
			t.Fatalf("shards = %v, want the one published shard to survive", names)
		}
	}
	sep := string(filepath.Separator)

	t.Run("cleanup with no targets", func(t *testing.T) {
		base := setup(t)
		res, err := Finalize(context.Background(), opts(base, nil))
		assertRejected(t, base, res, err)
	})

	t.Run("target directly inside the run directory", func(t *testing.T) {
		base := setup(t)
		target := filepath.Join(runDir(base, "run-1"), "report.json")
		res, err := Finalize(context.Background(), opts(base, []report.Target{{Format: report.FormatJSON, Path: target}}))
		assertRejected(t, base, res, err)
		if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("target inside the run directory was rendered: %v", statErr)
		}
	})

	t.Run("target reaches the run directory through dot-dot segments", func(t *testing.T) {
		base := setup(t)
		inside := runDir(base, "run-1") + sep + "x" + sep + ".." + sep + "report.json"
		res, err := Finalize(context.Background(), opts(base, []report.Target{{Format: report.FormatJSON, Path: inside}}))
		assertRejected(t, base, res, err)
	})

	t.Run("relative target resolving into the run directory", func(t *testing.T) {
		base := setup(t)
		t.Chdir(base)
		res, err := Finalize(context.Background(), opts(base, []report.Target{{Format: report.FormatJSON, Path: filepath.Join("run-1", "report.json")}}))
		assertRejected(t, base, res, err)
	})

	t.Run("target under a symlink that points into the run directory", func(t *testing.T) {
		base := setup(t)
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(runDir(base, "run-1"), link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		res, err := Finalize(context.Background(), opts(base, []report.Target{{Format: report.FormatJSON, Path: filepath.Join(link, "sub", "report.json")}}))
		assertRejected(t, base, res, err)
	})

	t.Run("an unsafe target is rejected even without Cleanup", func(t *testing.T) {
		base := setup(t)
		o := opts(base, []report.Target{{Format: report.FormatJSON, Path: filepath.Join(runDir(base, "run-1"), "report.json")}})
		o.Cleanup = false
		res, err := Finalize(context.Background(), o)
		assertRejected(t, base, res, err)
	})

	t.Run("a sibling directory sharing the run directory prefix is external", func(t *testing.T) {
		base := setup(t)
		target := filepath.Join(base, "run-1-reports", "report.json")
		res, err := Finalize(context.Background(), opts(base, []report.Target{{Format: report.FormatJSON, Path: target}}))
		if err != nil {
			t.Fatalf("Finalize: %v", err)
		}
		if ec := ExitCode(res, err); ec != 0 {
			t.Fatalf("ExitCode = %d, want 0", ec)
		}
		if _, statErr := os.Stat(target); statErr != nil {
			t.Fatalf("external target was not rendered: %v", statErr)
		}
		if _, statErr := os.Stat(runDir(base, "run-1")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatal("cleanup did not run for a valid external target")
		}
	})
}
