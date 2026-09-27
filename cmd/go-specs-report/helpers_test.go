package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
	"github.com/getsyntegrity/go-specs/report/coordination"
)

// validToken is a conforming 16-byte token (32 hex chars), the same fixture value used throughout
// report/coordination's own test suite.
const validToken = coordination.RunToken("0a1b2c3d4e5f60718293a4b5c6d7e8f9")

// secureTempDir returns a temporary directory that satisfies contract v1.2.9 §10 rule 2.
//
// t.TempDir() creates its numbered subdirectory with os.Mkdir(…, 0777), so under the common
// umask 002 the result is 0775 — group-writable, which coordination's ensureSafeBaseDir refuses by
// design (report/coordination/helpers_test.go carries the identical helper, unexported to that
// package).
func secureTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
	return dir
}

// runCLI drives run() in-process, the way every behaviour test in this package exercises the CLI:
// no subprocess, real flag parsing, real coordination calls against a real temp filesystem.
func runCLI(t *testing.T, args []string, env map[string]string) (stdout, stderr string, code int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	lookup := func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
	code = run(args, lookup, &outBuf, &errBuf)
	return outBuf.String(), errBuf.String(), code
}

// parseKV decodes init's KEY=value stdout lines into a map, failing the test on any malformed
// line rather than silently ignoring it.
func parseKV(t *testing.T, s string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("malformed KEY=value line: %q (full output %q)", line, s)
		}
		out[parts[0]] = parts[1]
	}
	return out
}

// initRun sets up a run marker directly through the public coordination API, exactly the way
// finalize and gc tests need a run to already exist without going through the init verb.
func initRun(t *testing.T, base string, id coordination.RunID, tok coordination.RunToken) coordination.RunOwnership {
	t.Helper()
	own, err := coordination.InitializeRun(t.Context(), coordination.InitializeRunOptions{
		RunID: id, Token: tok, BaseDir: base,
	})
	if err != nil {
		t.Fatalf("InitializeRun: %v", err)
	}
	return own
}

// writeShard publishes a real shard through the public producer API (coordination.ShardConfig +
// NewShardWriter), never a hand-crafted file: the same path finalize's own T2/T4 fixtures use, and
// the only one available outside package coordination (its own fixture helpers are unexported).
func writeShard(t *testing.T, base string, own coordination.RunOwnership, id coordination.RunID, tok coordination.RunToken, pkg string, rep report.NormalizedReport) {
	t.Helper()
	cfg := coordination.ShardConfig{
		Activated: true, RunID: id, Token: tok, BaseDir: base, PackagePath: pkg, Ownership: own,
	}
	if err := coordination.NewShardWriter(cfg).Write(rep); err != nil {
		t.Fatalf("write shard for %s: %v", pkg, err)
	}
}

// corruptShard overwrites an already-published shard's bytes with something Finalize cannot parse,
// using own.MarkerPath (the one path the public API actually hands back) to locate the shards
// directory rather than hardcoding the run directory layout.
func corruptShard(t *testing.T, own coordination.RunOwnership, pkg string) {
	t.Helper()
	shardsDir := filepath.Join(filepath.Dir(own.MarkerPath), "shards")
	path := filepath.Join(shardsDir, coordination.ShardFileName(pkg))
	if err := os.WriteFile(path, []byte("not valid json"), 0o600); err != nil {
		t.Fatalf("corrupt shard %s: %v", path, err)
	}
}

func passingReport() report.NormalizedReport {
	return report.NormalizedReport{
		SchemaVersion: report.SchemaVersion,
		Execution:     report.Totals{Total: 1, Passed: 1},
		Suites:        []report.Suite{{Name: "S", Cases: []report.Case{{Name: "ok", Status: report.StatusPassed}}}},
	}
}

func failingReport() report.NormalizedReport {
	return report.NormalizedReport{
		SchemaVersion: report.SchemaVersion,
		Execution:     report.Totals{Total: 1, Failed: 1},
		Suites: []report.Suite{{Name: "S", Cases: []report.Case{
			{Name: "bad", Status: report.StatusFailed, Message: "boom"},
		}}},
	}
}

// writeManifest writes a producers manifest file, one path per line, in dir.
func writeManifest(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, "producers.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}
