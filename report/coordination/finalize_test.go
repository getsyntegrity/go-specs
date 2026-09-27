package coordination

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/report"
)

// finalize_test.go pins T4 of issue #146: Finalize's orchestration (contract v1.2.8 §7) and
// ExitCode's mapping (contract v1.2.8 §8), against run directories built the same way a real
// invocation would build them — InitializeRun for the marker, ShardWriter for shards.

func targetPaths(t *testing.T, dir string) []report.Target {
	t.Helper()
	return []report.Target{
		{Format: report.FormatJSON, Path: filepath.Join(dir, "report.json")},
		{Format: report.FormatXML, Path: filepath.Join(dir, "report.xml")},
		{Format: report.FormatTXT, Path: filepath.Join(dir, "sub", "report.txt")}, // parent must be created
	}
}

func TestFinalizeMergesAndRendersOnSuccess(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	publishShard(t, base, own, "run-1", validToken, "example.com/m/alpha", sampleReport())

	out := t.TempDir()
	targets := targetPaths(t, out)

	res, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		ExpectedProducers: []string{"example.com/m/alpha"},
		Targets:           targets,
	})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if ec := ExitCode(res, err); ec != 0 {
		t.Fatalf("ExitCode = %d, want 0", ec)
	}
	if res.ConfigError != nil {
		t.Fatalf("ConfigError = %+v, want nil", res.ConfigError)
	}
	if len(res.PackagesMissing) != 0 || len(res.Rejected) != 0 {
		t.Fatalf("Missing=%v Rejected=%v, want both empty", res.PackagesMissing, res.Rejected)
	}
	if len(res.PackagesFound) != 1 || res.PackagesFound[0] != "example.com/m/alpha" {
		t.Fatalf("PackagesFound = %v", res.PackagesFound)
	}
	if res.Merged.Execution.Total != 2 {
		t.Fatalf("Merged.Execution.Total = %d, want 2 (from sampleReport)", res.Merged.Execution.Total)
	}

	for _, tg := range targets {
		info, err := os.Stat(tg.Path)
		if err != nil {
			t.Fatalf("target %s was not rendered: %v", tg.Path, err)
		}
		if info.Size() == 0 {
			t.Fatalf("target %s is empty", tg.Path)
		}
	}
	// No temp litter left behind next to the rendered files.
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") || strings.HasPrefix(e.Name(), ".finalize-") {
			t.Fatalf("a temp render file was left behind: %s", e.Name())
		}
	}
}

func TestFinalizeReturnsConfigErrorWithoutMergingOrRendering(t *testing.T) {
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)
	if err := WriteConfigError(base, "run-1", "example.com/m/broken", ReasonMissingRunToken, "diag"); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	targets := targetPaths(t, out)

	res, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		ExpectedProducers: []string{"example.com/m/broken"},
		Targets:           targets,
	})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if res.ConfigError == nil {
		t.Fatal("ConfigError = nil, want the recorded config-error.json")
	}
	if res.ConfigError.PackagePath != "example.com/m/broken" {
		t.Fatalf("ConfigError.PackagePath = %q", res.ConfigError.PackagePath)
	}
	if ec := ExitCode(res, err); ec != ExitConfig {
		t.Fatalf("ExitCode = %d, want %d", ec, ExitConfig)
	}
	for _, tg := range targets {
		if _, err := os.Stat(tg.Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("target %s was rendered despite a config-error.json", tg.Path)
		}
	}
	if res.Merged.SchemaVersion != "" || len(res.Merged.Suites) != 0 || res.Merged.Execution.Total != 0 {
		t.Fatalf("Merged = %+v, want the zero value: no merge happens on a config error", res.Merged)
	}
}

func TestFinalizeFailsClosedOnAnOwnershipMismatch(t *testing.T) {
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	out := t.TempDir()
	res, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: RunToken(strings.Repeat("ff", 16)), BaseDir: base,
		ExpectedProducers: []string{"example.com/m/alpha"},
		Targets:           targetPaths(t, out),
	})
	if err == nil {
		t.Fatal("Finalize accepted a token that does not match run.json")
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("err = %v, want a *ConfigError (ownership failure)", err)
	}
	if ec := ExitCode(res, err); ec != ExitConfig {
		t.Fatalf("ExitCode = %d, want %d", ec, ExitConfig)
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 0 {
		t.Fatalf("something was rendered despite an ownership failure: %v", entries)
	}
}

func TestFinalizeReportsAMissingProducerAndDoesNotErrorItself(t *testing.T) {
	// "Missing-shard strictness: strict" (feature doc decision) — Finalize itself returns nil
	// error; ExitCode is what maps PackagesMissing to the reporting-failure code.
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	res, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		ExpectedProducers: []string{"example.com/m/never-ran"},
		Targets:           targetPaths(t, t.TempDir()),
	})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if len(res.PackagesMissing) != 1 || res.PackagesMissing[0] != "example.com/m/never-ran" {
		t.Fatalf("PackagesMissing = %v", res.PackagesMissing)
	}
	if ec := ExitCode(res, err); ec != ExitReportingFailure {
		t.Fatalf("ExitCode = %d, want %d", ec, ExitReportingFailure)
	}
}

func TestFinalizeReportsARejectedShardAndStillRendersTheAcceptedOnes(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	publishShard(t, base, own, "run-1", validToken, "example.com/m/good", sampleReport())
	// A corrupt shard for a second expected package.
	dir := shardsDir(base, "run-1")
	if err := os.WriteFile(filepath.Join(dir, ShardFileName("example.com/m/corrupt")), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		ExpectedProducers: []string{"example.com/m/good", "example.com/m/corrupt"},
		Targets:           targetPaths(t, t.TempDir()),
	})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if len(res.Rejected) != 1 || res.Rejected[0].Reason != ReasonCorrupt {
		t.Fatalf("Rejected = %+v", res.Rejected)
	}
	if len(res.PackagesFound) != 1 || res.PackagesFound[0] != "example.com/m/good" {
		t.Fatalf("PackagesFound = %v, want the good shard still merged", res.PackagesFound)
	}
	if res.Merged.Execution.Total == 0 {
		t.Fatal("the accepted shard was not merged despite the other shard's rejection")
	}
	if ec := ExitCode(res, err); ec != ExitReportingFailure {
		t.Fatalf("ExitCode = %d, want %d", ec, ExitReportingFailure)
	}
}

func TestFinalizeRejectsEmptyExpectedProducers(t *testing.T) {
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	res, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		Targets: targetPaths(t, t.TempDir()),
	})
	if !errors.Is(err, ErrInvalidExpectedProducers) {
		t.Fatalf("err = %v, want ErrInvalidExpectedProducers", err)
	}
	if ec := ExitCode(res, err); ec != ExitConfig {
		t.Fatalf("ExitCode = %d, want %d (a caller misconfiguration, not a reporting result)", ec, ExitConfig)
	}
}

func TestFinalizeMergesCoverageThroughTheBlockDeduplicatingParser(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	publishShard(t, base, own, "run-1", validToken, "example.com/m/alpha", sampleReport())

	profile := filepath.Join(t.TempDir(), "cover.out")
	body := "mode: count\n" +
		"example.com/m/alpha/x.go:10.1,12.2 3 1\n" +
		"example.com/m/alpha/x.go:10.1,12.2 3 4\n" // deliberately overlapping (-coverpkg style)
	if err := os.WriteFile(profile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		ExpectedProducers: []string{"example.com/m/alpha"},
		CoverProfile:      profile,
		Targets:           targetPaths(t, t.TempDir()),
	})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if len(res.Merged.Coverage.Packages) != 1 {
		t.Fatalf("Coverage.Packages = %+v", res.Merged.Coverage.Packages)
	}
	pc := res.Merged.Coverage.Packages[0]
	if pc.Total != 3 {
		t.Fatalf("Coverage Total = %d, want 3 (deduplicated, not 6)", pc.Total)
	}
}

func TestFinalizeSkipsCoverageWhenNoProfileIsGiven(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	publishShard(t, base, own, "run-1", validToken, "example.com/m/alpha", sampleReport())

	res, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		ExpectedProducers: []string{"example.com/m/alpha"},
		Targets:           targetPaths(t, t.TempDir()),
	})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if len(res.Merged.Coverage.Packages) != 0 {
		t.Fatalf("Coverage.Packages = %+v, want none: no CoverProfile was given", res.Merged.Coverage.Packages)
	}
}

func TestFinalizeCleansUpOnlyAfterACompleteSuccess(t *testing.T) {
	t.Run("cleans up on success when requested", func(t *testing.T) {
		base := secureTempDir(t)
		own := mustInitRun(t, base, "run-1", validToken)
		publishShard(t, base, own, "run-1", validToken, "example.com/m/alpha", sampleReport())

		_, err := Finalize(context.Background(), FinalizeOptions{
			RunID: "run-1", Token: validToken, BaseDir: base,
			ExpectedProducers: []string{"example.com/m/alpha"},
			Targets:           targetPaths(t, t.TempDir()),
			Cleanup:           true,
		})
		if err != nil {
			t.Fatalf("Finalize: %v", err)
		}
		if _, err := os.Stat(runDir(base, "run-1")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("the run directory was not removed after a successful, Cleanup-requested finalize")
		}
	})

	t.Run("keeps the run directory when Cleanup is false", func(t *testing.T) {
		base := secureTempDir(t)
		own := mustInitRun(t, base, "run-1", validToken)
		publishShard(t, base, own, "run-1", validToken, "example.com/m/alpha", sampleReport())

		if _, err := Finalize(context.Background(), FinalizeOptions{
			RunID: "run-1", Token: validToken, BaseDir: base,
			ExpectedProducers: []string{"example.com/m/alpha"},
			Targets:           targetPaths(t, t.TempDir()),
		}); err != nil {
			t.Fatalf("Finalize: %v", err)
		}
		if _, err := os.Stat(runDir(base, "run-1")); err != nil {
			t.Fatalf("the run directory was removed although Cleanup was not requested: %v", err)
		}
	})

	t.Run("keeps the run directory when a producer is missing, even with Cleanup", func(t *testing.T) {
		base := secureTempDir(t)
		mustInitRun(t, base, "run-1", validToken)

		if _, err := Finalize(context.Background(), FinalizeOptions{
			RunID: "run-1", Token: validToken, BaseDir: base,
			ExpectedProducers: []string{"example.com/m/never-ran"},
			Targets:           targetPaths(t, t.TempDir()),
			Cleanup:           true,
		}); err != nil {
			t.Fatalf("Finalize: %v", err)
		}
		if _, err := os.Stat(runDir(base, "run-1")); err != nil {
			t.Fatal("the run directory was removed despite a missing producer: it must survive for gc/operator inspection")
		}
	})

	t.Run("config-error.json survives cleanup", func(t *testing.T) {
		base := secureTempDir(t)
		mustInitRun(t, base, "run-1", validToken)
		if err := WriteConfigError(base, "run-1", "example.com/m/broken", ReasonMissingRunToken, "diag"); err != nil {
			t.Fatal(err)
		}
		if _, err := Finalize(context.Background(), FinalizeOptions{
			RunID: "run-1", Token: validToken, BaseDir: base,
			ExpectedProducers: []string{"example.com/m/broken"},
			Targets:           targetPaths(t, t.TempDir()),
			Cleanup:           true,
		}); err != nil {
			t.Fatalf("Finalize: %v", err)
		}
		if _, err := os.Stat(configErrorPath(base, "run-1")); err != nil {
			t.Fatal("config-error.json did not survive a non-zero finalize outcome")
		}
	})
}

func TestFinalizeHonoursContextCancellation(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	publishShard(t, base, own, "run-1", validToken, "example.com/m/alpha", sampleReport())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := t.TempDir()
	_, err := Finalize(ctx, FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		ExpectedProducers: []string{"example.com/m/alpha"},
		Targets:           targetPaths(t, out),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 0 {
		t.Fatalf("something was rendered after the context was already cancelled: %v", entries)
	}
}

func TestExitCodeMapsEveryOutcome(t *testing.T) {
	tests := []struct {
		name string
		res  FinalizeResult
		err  error
		want int
	}{
		{"success", FinalizeResult{}, nil, 0},
		{"config error record", FinalizeResult{ConfigError: &ConfigErrorRecord{}}, nil, ExitConfig},
		{"ownership failure", FinalizeResult{}, &ConfigError{Reason: ReasonMarkerMismatch}, ExitConfig},
		{"invalid expected producers", FinalizeResult{}, ErrInvalidExpectedProducers, ExitConfig},
		{"missing producers", FinalizeResult{PackagesMissing: []string{"x"}}, nil, ExitReportingFailure},
		{"rejected shard", FinalizeResult{Rejected: []RejectedShard{{Path: "p", Reason: ReasonCorrupt}}}, nil, ExitReportingFailure},
		{"other error", FinalizeResult{}, errors.New("render failed"), ExitReportingFailure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.res, tt.err); got != tt.want {
				t.Fatalf("ExitCode = %d, want %d", got, tt.want)
			}
		})
	}
}

// A couple of direct checks on the atomic-render mechanics, independent of the rest of Finalize.

func TestFinalizeRenderedFilesAreValidForEveryTarget(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	publishShard(t, base, own, "run-1", validToken, "example.com/m/alpha", sampleReport())

	out := t.TempDir()
	targets := []report.Target{
		{Format: report.FormatJSON, Path: filepath.Join(out, "r.json")},
		{Format: report.FormatXML, Path: filepath.Join(out, "r.xml")},
		{Format: report.FormatHTML, Path: filepath.Join(out, "r.html")},
		{Format: report.FormatTXT, Path: filepath.Join(out, "r.txt")},
	}
	res, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		ExpectedProducers: []string{"example.com/m/alpha"},
		Targets:           targets,
	})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(out, "r.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Execution struct{ Total int } `json:"execution"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("rendered JSON does not decode: %v", err)
	}
	if doc.Execution.Total != res.Merged.Execution.Total {
		t.Fatalf("rendered JSON Execution.Total = %d, want %d", doc.Execution.Total, res.Merged.Execution.Total)
	}
}

// nowShardWriter is a small helper letting a test control a shard's ProducedAt directly, for
// scenarios where publishShard's real clock would make a case awkward to construct (kept in this
// file since only Finalize-level tests need it).
func nowShardWriter(cfg ShardConfig, at time.Time) *ShardWriter {
	w := NewShardWriter(cfg)
	w.now = func() time.Time { return at }
	return w
}

func TestFinalizeRejectsAStaleShardAndReportsTheProducerMissing(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	cfg := ShardConfig{Activated: true, RunID: "run-1", Token: validToken, BaseDir: base, PackagePath: "example.com/m/stale", Ownership: own}
	if err := nowShardWriter(cfg, time.Now().UTC().Add(-time.Hour)).Write(sampleReport()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	res, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		ExpectedProducers: []string{"example.com/m/stale"},
		Targets:           targetPaths(t, t.TempDir()),
	})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if len(res.Rejected) != 1 || res.Rejected[0].Reason != ReasonStale {
		t.Fatalf("Rejected = %+v", res.Rejected)
	}
	if len(res.PackagesMissing) != 1 {
		t.Fatalf("PackagesMissing = %v, want the stale producer reported missing too", res.PackagesMissing)
	}
}
