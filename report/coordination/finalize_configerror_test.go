package coordination

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// TestFinalizeTreatsAMalformedConfigErrorRecordAsAConfigurationFailure pins issue #311: a
// config-error.json that exists but cannot be decoded still proves a package failed its pre-test
// checks, so it must map to exit 78, render nothing and keep the shards, with an error naming the
// file and the parse failure.
func TestFinalizeTreatsAMalformedConfigErrorRecordAsAConfigurationFailure(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	publishShard(t, base, own, "run-1", validToken, "example.com/m/alpha", sampleReport())
	path := configErrorPath(base, "run-1")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	res, err := Finalize(context.Background(), FinalizeOptions{
		RunID: "run-1", Token: validToken, BaseDir: base,
		ExpectedProducers: []string{"example.com/m/alpha"},
		Targets:           targetPaths(t, out),
		Cleanup:           true,
	})
	if err == nil {
		t.Fatal("Finalize accepted a malformed config-error.json")
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("err = %v, want a *ConfigError", err)
	}
	if cfgErr.Reason != ReasonMalformedConfigError {
		t.Fatalf("Reason = %q, want %q", cfgErr.Reason, ReasonMalformedConfigError)
	}
	if ec := ExitCode(res, err); ec != ExitConfig {
		t.Fatalf("ExitCode = %d, want %d", ec, ExitConfig)
	}
	msg := err.Error()
	if !strings.Contains(msg, path) || !strings.Contains(msg, "invalid character") {
		t.Fatalf("error %q must name %s and the JSON parse failure", msg, path)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Fatalf("something was rendered despite a malformed config-error.json: %v", entries)
	}
	if names := shardNames(t, base, "run-1"); len(names) != 1 {
		t.Fatalf("shards = %v, want the published shard to survive", names)
	}
}
