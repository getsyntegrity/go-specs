package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/getsyntegrity/go-specs/report/coordination"
)

// init_test.go pins T3 of issue #146: the `init` verb, which wraps coordination.InitializeRun
// (contract v1.2.9 §3 step 2, §7's "ships with whichever slice delivers InitializeRun first").

func TestInitPrintsTheFourKeyValueLinesAndCreatesTheMarker(t *testing.T) {
	base := secureTempDir(t)

	stdout, stderr, code := runCLI(t, []string{
		"init", "-run-id", "run-1", "-token", string(validToken), "-report-dir", base,
	}, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr)
	}

	kv := parseKV(t, stdout)
	absBase, err := filepath.Abs(base)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		coordination.EnvGate:      "1",
		coordination.EnvRunID:     "run-1",
		coordination.EnvRunToken:  string(validToken),
		coordination.EnvReportDir: absBase,
	}
	for k, v := range want {
		if kv[k] != v {
			t.Fatalf("stdout[%s] = %q, want %q (full stdout:\n%s)", k, kv[k], v, stdout)
		}
	}
	if len(kv) != len(want) {
		t.Fatalf("stdout has %d KEY=value lines, want exactly %d:\n%s", len(kv), len(want), stdout)
	}

	markerPath := filepath.Join(absBase, "run-1", "run.json")
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("run.json was not created at %s: %v", markerPath, err)
	}
}

func TestInitGeneratesATokenWhenNoneIsGiven(t *testing.T) {
	base := secureTempDir(t)

	stdout, stderr, code := runCLI(t, []string{"init", "-run-id", "run-1", "-report-dir", base}, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr)
	}
	kv := parseKV(t, stdout)
	tok := kv[coordination.EnvRunToken]
	if _, err := coordination.ValidateRunToken(tok); err != nil {
		t.Fatalf("generated token %q does not validate: %v", tok, err)
	}
}

func TestInitTwiceWithTheSameRunIDFailsWithExitConfig(t *testing.T) {
	base := secureTempDir(t)
	args := []string{"init", "-run-id", "run-1", "-token", string(validToken), "-report-dir", base}

	if _, _, code := runCLI(t, args, nil); code != 0 {
		t.Fatalf("first init: exit code = %d, want 0", code)
	}
	_, stderr, code := runCLI(t, args, nil)
	if code != coordination.ExitConfig {
		t.Fatalf("second init: exit code = %d, want %d (ExitConfig); stderr=%s", code, coordination.ExitConfig, stderr)
	}
	if stderr == "" {
		t.Fatal("second init printed no diagnostic on stderr")
	}
}

func TestInitForceSucceedsAfterAnExistingRun(t *testing.T) {
	base := secureTempDir(t)
	args := []string{"init", "-run-id", "run-1", "-token", string(validToken), "-report-dir", base}

	if _, _, code := runCLI(t, args, nil); code != 0 {
		t.Fatalf("first init: exit code = %d, want 0", code)
	}
	_, stderr, code := runCLI(t, append(args, "-force"), nil)
	if code != 0 {
		t.Fatalf("forced init: exit code = %d, want 0; stderr=%s", code, stderr)
	}
}

func TestInitMissingRunIDFailsWithExitConfig(t *testing.T) {
	base := secureTempDir(t)
	_, stderr, code := runCLI(t, []string{"init", "-token", string(validToken), "-report-dir", base}, nil)
	if code != coordination.ExitConfig {
		t.Fatalf("exit code = %d, want %d (ExitConfig); stderr=%s", code, coordination.ExitConfig, stderr)
	}
}

func TestInitInvalidTokenFailsWithExitConfig(t *testing.T) {
	base := secureTempDir(t)
	_, stderr, code := runCLI(t, []string{
		"init", "-run-id", "run-1", "-token", "not-hex", "-report-dir", base,
	}, nil)
	if code != coordination.ExitConfig {
		t.Fatalf("exit code = %d, want %d (ExitConfig); stderr=%s", code, coordination.ExitConfig, stderr)
	}
}

func TestInitResolvesARelativeReportDirToAbsoluteInItsOutput(t *testing.T) {
	base := secureTempDir(t)
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	stdout, stderr, code := runCLI(t, []string{
		"init", "-run-id", "run-1", "-token", string(validToken), "-report-dir", "runs",
	}, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr)
	}
	kv := parseKV(t, stdout)
	// os.Getwd reports the symlink-resolved path (e.g. /private/var on macOS),
	// so the expectation must resolve base the same way.
	resolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(resolved, "runs")
	if kv[coordination.EnvReportDir] != want {
		t.Fatalf("%s = %q, want %q (must be absolute)", coordination.EnvReportDir, kv[coordination.EnvReportDir], want)
	}
	if !filepath.IsAbs(kv[coordination.EnvReportDir]) {
		t.Fatalf("%s = %q is not absolute", coordination.EnvReportDir, kv[coordination.EnvReportDir])
	}
}

func TestInitFallsBackToEnvironmentVariables(t *testing.T) {
	base := secureTempDir(t)
	env := map[string]string{
		coordination.EnvRunID:     "run-env",
		coordination.EnvRunToken:  string(validToken),
		coordination.EnvReportDir: base,
	}
	stdout, stderr, code := runCLI(t, []string{"init"}, env)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr)
	}
	kv := parseKV(t, stdout)
	if kv[coordination.EnvRunID] != "run-env" {
		t.Fatalf("%s = %q, want %q", coordination.EnvRunID, kv[coordination.EnvRunID], "run-env")
	}
}

// TestInitNeverInventsSchemaDrift is a light contract pin: run.json's own encoding is
// coordination's responsibility, but init must not, say, double-encode or wrap it.
func TestInitNeverInventsSchemaDrift(t *testing.T) {
	base := secureTempDir(t)
	_, _, code := runCLI(t, []string{"init", "-run-id", "run-1", "-token", string(validToken), "-report-dir", base}, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	raw, err := os.ReadFile(filepath.Join(base, "run-1", "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("run.json is not a flat JSON object: %v\n%s", err, raw)
	}
	if _, ok := doc["runId"]; !ok {
		t.Fatalf("run.json has no runId field:\n%s", raw)
	}
}
