// shard_flag_invocation_real_process_test.go proves issue #312 end-to-end through a real `go test`
// subprocess. go-specs never registers -shard with Go's flag package (ParseShardFlag only scans
// os.Args), so `go test ./x -args -shard 1/2` dies in the test binary's own flag parsing with
// "flag provided but not defined: -shard" before any spec runs. The invocations that do work are
// `-args -- -shard 1/2` (the `--` ends flag parsing, so the binary never sees the unknown flag)
// and the SHARD environment variables. These tests run each documented form for real and pin the
// documentation to the forms that were observed to work.
package specs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const shardInvocationHelperEnv = "GO_SPECS_SHARD_INVOCATION_HELPER"

// TestShardInvocationHelper is the child body; it only acts when re-invoked by the tests below.
func TestShardInvocationHelper(t *testing.T) {
	if os.Getenv(shardInvocationHelperEnv) == "" {
		t.Skip("helper for TestShardFlagDocumentedInvocationsRealGoTest")
	}
	shard, total, err := ShardFromArgsOrEnv()
	if err != nil {
		t.Logf("SHARD_RESULT err=%v", err)
		t.Fail()
		return
	}
	t.Logf("SHARD_RESULT shard=%d total=%d", shard, total)
}

func goToolForShardInvocation(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go tool not found: %v", err)
	}
	return p
}

func runGoTestShardHelper(t *testing.T, env []string, args ...string) (string, error) {
	t.Helper()
	cmdArgs := []string{"test", "-count=1", "-v", "-run", "^TestShardInvocationHelper$", "."}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command(goToolForShardInvocation(t), cmdArgs...)
	cmd.Env = append(os.Environ(), shardInvocationHelperEnv+"=1")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestShardFlagDocumentedInvocationsRealGoTest(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns go test subprocesses")
	}
	t.Run("args separator form runs the requested shard", func(t *testing.T) {
		out, err := runGoTestShardHelper(t, nil, "-args", "--", "-shard", "1/2")
		if err != nil || !strings.Contains(out, "SHARD_RESULT shard=1 total=2") {
			t.Fatalf("documented form must select shard 1/2, err=%v:\n%s", err, out)
		}
	})
	t.Run("args separator equals form runs the requested shard", func(t *testing.T) {
		out, err := runGoTestShardHelper(t, nil, "-args", "--", "-shard=1/2")
		if err != nil || !strings.Contains(out, "SHARD_RESULT shard=1 total=2") {
			t.Fatalf("documented form must select shard 1/2, err=%v:\n%s", err, out)
		}
	})
	t.Run("environment form runs the requested shard", func(t *testing.T) {
		out, err := runGoTestShardHelper(t, []string{"SHARD=1/2"})
		if err != nil || !strings.Contains(out, "SHARD_RESULT shard=1 total=2") {
			t.Fatalf("SHARD=1/2 must select shard 1/2, err=%v:\n%s", err, out)
		}
	})
	t.Run("bare args form is rejected by go test before any spec runs", func(t *testing.T) {
		out, err := runGoTestShardHelper(t, nil, "-args", "-shard", "1/2")
		if err == nil {
			t.Fatalf("the undocumented bare form unexpectedly passed:\n%s", out)
		}
		if !strings.Contains(out, "flag provided but not defined: -shard") || strings.Contains(out, "SHARD_RESULT") {
			t.Fatalf("expected Go's flag error and no spec run, got:\n%s", out)
		}
	})
	t.Run("invalid shard settings stay fail-closed", func(t *testing.T) {
		out, err := runGoTestShardHelper(t, nil, "-args", "--", "-shard", "5/2")
		if err == nil || !strings.Contains(out, "SHARD_RESULT err=") {
			t.Fatalf("an out-of-range shard must fail, err=%v:\n%s", err, out)
		}
	})
}

func TestExecutionModelDocumentsARunnableShardInvocation(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "docs", "EXECUTION_MODEL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	if !strings.Contains(text, "go test ./... -args -- -shard 1/2") {
		t.Error("docs/EXECUTION_MODEL.md must show the working `-args -- -shard` invocation")
	}
	// Prose may name the broken form to explain it; a runnable code block must not contain it.
	for i, block := range strings.Split(text, "```") {
		if i%2 == 1 && strings.Contains(block, "-args -shard") {
			t.Errorf("docs/EXECUTION_MODEL.md code block shows the broken `-args -shard` form:\n%s", block)
		}
	}
}
