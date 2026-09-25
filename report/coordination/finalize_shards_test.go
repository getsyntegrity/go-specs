package coordination

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/report"
)

// finalize_shards_test.go pins T2 of issue #146: shard discovery and verification, plus the
// config-error.json reader, against the closed rejection-reason vocabulary of contract v1.2.8 §7.

// publishShard writes a real, correctly-published shard using the same ShardWriter path a
// producer uses, so an "accepted" fixture is never hand-crafted.
func publishShard(t *testing.T, base string, own RunOwnership, runID RunID, tok RunToken, pkg string, rep report.NormalizedReport) string {
	t.Helper()
	cfg := ShardConfig{Activated: true, RunID: runID, Token: tok, BaseDir: base, PackagePath: pkg, Ownership: own}
	if err := NewShardWriter(cfg).Write(rep); err != nil {
		t.Fatalf("publish shard for %s: %v", pkg, err)
	}
	return filepath.Join(shardsDir(base, runID), ShardFileName(pkg))
}

// writeRawShard writes an arbitrary envelope under an arbitrary filename, for constructing
// deliberately malformed or tampered shards that ShardWriter itself would never produce.
func writeRawShard(t *testing.T, base string, runID RunID, name string, env ShardEnvelope) string {
	t.Helper()
	dir := shardsDir(base, runID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func discoveryOpts(base string, runID RunID, tokenHash string, expected []string) shardDiscoveryOptions {
	return shardDiscoveryOptions{BaseDir: base, RunID: runID, RunTokenHash: tokenHash, ExpectedProducers: expected}
}

func TestDiscoverShardsAcceptsAValidShardAndReportsItFound(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	publishShard(t, base, own, "run-1", validToken, "example.com/m/calc", sampleReport())

	result, err := discoverShards(discoveryOpts(base, "run-1", own.TokenHash, []string{"example.com/m/calc"}))
	if err != nil {
		t.Fatalf("discoverShards: %v", err)
	}
	if len(result.Accepted) != 1 || result.Accepted[0].PackagePath != "example.com/m/calc" {
		t.Fatalf("Accepted = %+v, want one shard for example.com/m/calc", result.Accepted)
	}
	if len(result.Missing) != 0 {
		t.Fatalf("Missing = %v, want none", result.Missing)
	}
	if len(result.Rejected) != 0 {
		t.Fatalf("Rejected = %v, want none", result.Rejected)
	}
}

func TestDiscoverShardsAcceptsAZeroTestShardNotAsMissing(t *testing.T) {
	// -run/-skip/-short filter tests, not packages: the binary still runs and still publishes a
	// shard reporting zero executed tests. That shard is valid, not a missing producer
	// (contract v1.2.8 §5, Expected producer set).
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	publishShard(t, base, own, "run-1", validToken, "example.com/m/filtered", report.NormalizedReport{SchemaVersion: report.SchemaVersion})

	result, err := discoverShards(discoveryOpts(base, "run-1", own.TokenHash, []string{"example.com/m/filtered"}))
	if err != nil {
		t.Fatalf("discoverShards: %v", err)
	}
	if len(result.Missing) != 0 {
		t.Fatalf("a zero-test shard was treated as missing: %v", result.Missing)
	}
	if len(result.Accepted) != 1 {
		t.Fatalf("Accepted = %+v, want the zero-test shard accepted", result.Accepted)
	}
}

func TestDiscoverShardsReportsAnExpectedProducerWithNoShardAsMissing(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)

	result, err := discoverShards(discoveryOpts(base, "run-1", own.TokenHash, []string{"example.com/m/never-ran"}))
	if err != nil {
		t.Fatalf("discoverShards: %v", err)
	}
	if len(result.Missing) != 1 || result.Missing[0] != "example.com/m/never-ran" {
		t.Fatalf("Missing = %v, want [example.com/m/never-ran]", result.Missing)
	}
}

func TestDiscoverShardsRejectsEachClosedReason(t *testing.T) {
	type fixture struct {
		base string
		own  RunOwnership
	}
	validEnvelope := func(f fixture, pkg string) ShardEnvelope {
		return ShardEnvelope{
			ShardSchemaVersion: ShardSchemaVersion,
			RunID:              "run-1",
			RunTokenHash:       f.own.TokenHash,
			PackagePath:        pkg,
			ProducedAt:         time.Now().UTC(),
			Report:             sampleReport(),
		}
	}

	tests := []struct {
		name   string
		setup  func(t *testing.T, f fixture) (path string, expected []string)
		reason string
	}{
		{
			name: "corrupt",
			setup: func(t *testing.T, f fixture) (string, []string) {
				dir := shardsDir(f.base, "run-1")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, ShardFileName("example.com/m/corrupt"))
				if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
					t.Fatal(err)
				}
				return path, []string{"example.com/m/corrupt"}
			},
			reason: ReasonCorrupt,
		},
		{
			name: "schema-version-mismatch",
			setup: func(t *testing.T, f fixture) (string, []string) {
				env := validEnvelope(f, "example.com/m/schema")
				env.ShardSchemaVersion = "99"
				path := writeRawShard(t, f.base, "run-1", ShardFileName("example.com/m/schema"), env)
				return path, []string{"example.com/m/schema"}
			},
			reason: ReasonSchemaVersionMismatch,
		},
		{
			name: "wrong-run-id",
			setup: func(t *testing.T, f fixture) (string, []string) {
				env := validEnvelope(f, "example.com/m/wrongrun")
				env.RunID = "someone-elses-run"
				path := writeRawShard(t, f.base, "run-1", ShardFileName("example.com/m/wrongrun"), env)
				return path, []string{"example.com/m/wrongrun"}
			},
			reason: ReasonWrongRunID,
		},
		{
			name: "ownership-mismatch",
			setup: func(t *testing.T, f fixture) (string, []string) {
				env := validEnvelope(f, "example.com/m/foreign")
				env.RunTokenHash = strings.Repeat("ab", 32)
				path := writeRawShard(t, f.base, "run-1", ShardFileName("example.com/m/foreign"), env)
				return path, []string{"example.com/m/foreign"}
			},
			reason: ReasonOwnershipMismatch,
		},
		{
			name: "filename-hash-mismatch",
			setup: func(t *testing.T, f fixture) (string, []string) {
				env := validEnvelope(f, "example.com/m/misnamed")
				// A correctly-shaped name, but for the WRONG package: its digest cannot match
				// sha256("example.com/m/misnamed").
				path := writeRawShard(t, f.base, "run-1", ShardFileName("example.com/m/someone-else"), env)
				return path, []string{"example.com/m/misnamed"}
			},
			reason: ReasonFilenameHashMismatch,
		},
		{
			name: "stale",
			setup: func(t *testing.T, f fixture) (string, []string) {
				env := validEnvelope(f, "example.com/m/stale")
				env.ProducedAt = time.Now().UTC().Add(-24 * time.Hour) // before run.json's CreatedAt
				path := writeRawShard(t, f.base, "run-1", ShardFileName("example.com/m/stale"), env)
				return path, []string{"example.com/m/stale"}
			},
			reason: ReasonStale,
		},
		{
			name: "unexpected-package",
			setup: func(t *testing.T, f fixture) (string, []string) {
				path := publishShard(t, f.base, f.own, "run-1", validToken, "example.com/m/unexpected", sampleReport())
				return path, []string{} // never named as expected
			},
			reason: ReasonUnexpectedPackage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := secureTempDir(t)
			own := mustInitRun(t, base, "run-1", validToken)
			f := fixture{base: base, own: own}
			path, expected := tt.setup(t, f)
			if len(expected) == 0 {
				expected = []string{"example.com/m/placeholder-to-keep-the-list-non-empty"}
			}
			result, err := discoverShards(discoveryOpts(base, "run-1", own.TokenHash, expected))
			if err != nil {
				t.Fatalf("discoverShards: %v", err)
			}
			if len(result.Rejected) != 1 {
				t.Fatalf("Rejected = %+v, want exactly one rejection", result.Rejected)
			}
			if result.Rejected[0].Path != path {
				t.Fatalf("Rejected[0].Path = %q, want %q", result.Rejected[0].Path, path)
			}
			if result.Rejected[0].Reason != tt.reason {
				t.Fatalf("Rejected[0].Reason = %q, want %q", result.Rejected[0].Reason, tt.reason)
			}
			for _, acc := range result.Accepted {
				if acc.PackagePath == "" {
					t.Fatalf("an empty package path was accepted: %+v", result.Accepted)
				}
			}
		})
	}
}

func TestDiscoverShardsRejectsAllShardsClaimingTheSamePackage(t *testing.T) {
	// The digest — not the readability prefix — is the whole of a shard filename's identity
	// (shardname.go). Two files with different prefixes but the SAME digest, whose envelopes both
	// claim the same PackagePath, are both individually well-formed; the finalizer must reject
	// BOTH, never silently pick one (contract v1.2.8 §5, "Duplicate producer identity").
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)

	canonical := ShardFileName("example.com/m/dup")
	digest := canonical[strings.LastIndex(canonical, prefixSeparator)+len(prefixSeparator):]

	env := ShardEnvelope{
		ShardSchemaVersion: ShardSchemaVersion,
		RunID:              "run-1",
		RunTokenHash:       own.TokenHash,
		PackagePath:        "example.com/m/dup",
		ProducedAt:         time.Now().UTC(),
		Report:             sampleReport(),
	}
	path1 := writeRawShard(t, base, "run-1", "first--"+digest, env)
	path2 := writeRawShard(t, base, "run-1", "second--"+digest, env)

	result, err := discoverShards(discoveryOpts(base, "run-1", own.TokenHash, []string{"example.com/m/dup"}))
	if err != nil {
		t.Fatalf("discoverShards: %v", err)
	}
	if len(result.Accepted) != 0 {
		t.Fatalf("Accepted = %+v, want none: a duplicate must never be silently picked", result.Accepted)
	}
	if len(result.Rejected) != 2 {
		t.Fatalf("Rejected = %+v, want both shards rejected", result.Rejected)
	}
	seen := map[string]bool{}
	for _, r := range result.Rejected {
		if r.Reason != ReasonDuplicatePackage {
			t.Fatalf("reason = %q, want %q", r.Reason, ReasonDuplicatePackage)
		}
		seen[r.Path] = true
	}
	if !seen[path1] || !seen[path2] {
		t.Fatalf("both %q and %q must be rejected, got %v", path1, path2, result.Rejected)
	}
	if len(result.Missing) != 1 || result.Missing[0] != "example.com/m/dup" {
		t.Fatalf("Missing = %v, want [example.com/m/dup]: a duplicate must not count as found", result.Missing)
	}
}

func TestDiscoverShardsIgnoresTempFiles(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	dir := shardsDir(base, "run-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, ShardFileName("example.com/m/inflight")+".tmp-12345")
	if err := os.WriteFile(tmp, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := discoverShards(discoveryOpts(base, "run-1", own.TokenHash, []string{"example.com/m/inflight"}))
	if err != nil {
		t.Fatalf("discoverShards: %v", err)
	}
	if len(result.Accepted) != 0 || len(result.Rejected) != 0 {
		t.Fatalf("a .tmp- file was surfaced: accepted=%v rejected=%v", result.Accepted, result.Rejected)
	}
	if len(result.Missing) != 1 {
		t.Fatalf("Missing = %v, want the package still missing (its shard is still in flight)", result.Missing)
	}
}

func TestDiscoverShardsRejectsEmptyExpectedProducers(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)

	for _, expected := range [][]string{nil, {}, {"  "}} {
		_, err := discoverShards(discoveryOpts(base, "run-1", own.TokenHash, expected))
		if !errors.Is(err, ErrInvalidExpectedProducers) {
			t.Fatalf("ExpectedProducers=%v: got %v, want ErrInvalidExpectedProducers", expected, err)
		}
	}
}

func TestDiscoverShardsNormalizesAndDedupesExpectedProducers(t *testing.T) {
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)
	publishShard(t, base, own, "run-1", validToken, "example.com/m/calc", sampleReport())

	result, err := discoverShards(discoveryOpts(base, "run-1", own.TokenHash,
		[]string{" example.com/m/calc ", "example.com/m/calc", "example.com/m/calc"}))
	if err != nil {
		t.Fatalf("discoverShards: %v", err)
	}
	if len(result.Accepted) != 1 {
		t.Fatalf("Accepted = %+v, want exactly one (duplicated/whitespace-padded expected entries collapse)", result.Accepted)
	}
	if len(result.Missing) != 0 {
		t.Fatalf("Missing = %v, want none", result.Missing)
	}
}

// --- config-error.json reader ---

func TestReadConfigErrorIfPresentReturnsNilWhenAbsent(t *testing.T) {
	base := secureTempDir(t)
	rec, err := readConfigErrorIfPresent(base, "run-1")
	if err != nil {
		t.Fatalf("readConfigErrorIfPresent: %v", err)
	}
	if rec != nil {
		t.Fatalf("rec = %+v, want nil when no config-error.json exists", rec)
	}
}

func TestReadConfigErrorIfPresentReturnsTheRecord(t *testing.T) {
	base := secureTempDir(t)
	if err := WriteConfigError(base, "run-1", "example.com/m/broken", ReasonMissingRunToken, "diag"); err != nil {
		t.Fatal(err)
	}
	rec, err := readConfigErrorIfPresent(base, "run-1")
	if err != nil {
		t.Fatalf("readConfigErrorIfPresent: %v", err)
	}
	if rec == nil {
		t.Fatal("rec = nil, want the written record")
	}
	if rec.PackagePath != "example.com/m/broken" || rec.Reason != ReasonMissingRunToken || rec.Diagnostic != "diag" {
		t.Fatalf("rec = %+v", rec)
	}
}

func TestReadConfigErrorIfPresentRejectsCorruptJSON(t *testing.T) {
	base := secureTempDir(t)
	dir := runDir(base, "run-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configErrorPath(base, "run-1"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readConfigErrorIfPresent(base, "run-1"); err == nil {
		t.Fatal("readConfigErrorIfPresent accepted corrupt JSON")
	}
}
