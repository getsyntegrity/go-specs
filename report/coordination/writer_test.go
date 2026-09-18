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

func enabledConfig(t *testing.T, base string, pkg string) ShardConfig {
	t.Helper()
	own := mustInitRun(t, base, "run-1", validToken)
	return ShardConfig{
		Activated:   true,
		RunID:       "run-1",
		Token:       validToken,
		BaseDir:     base,
		PackagePath: pkg,
		Ownership:   own,
	}
}

func sampleReport() report.NormalizedReport {
	return report.NormalizedReport{
		SchemaVersion: report.SchemaVersion,
		Execution:     report.Totals{Total: 2, Passed: 1, Failed: 1},
		Duration:      1500 * time.Millisecond,
		Suites: []report.Suite{{
			Name:  "Calculator",
			Cases: []report.Case{{Name: "adds", Status: report.StatusPassed}},
		}},
	}
}

func readEnvelope(t *testing.T, path string) ShardEnvelope {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var env ShardEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return env
}

func TestShardWriterPublishesACompleteIdentifiableShard(t *testing.T) {
	base := secureTempDir(t)
	cfg := enabledConfig(t, base, "example.com/m/calc")

	if err := NewShardWriter(cfg).Write(sampleReport()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	path := filepath.Join(shardsDir(base, "run-1"), ShardFileName("example.com/m/calc"))
	env := readEnvelope(t, path)

	if env.ShardSchemaVersion != ShardSchemaVersion {
		t.Fatalf("ShardSchemaVersion = %q", env.ShardSchemaVersion)
	}
	if env.RunID != "run-1" {
		t.Fatalf("RunID = %q", env.RunID)
	}
	if env.PackagePath != "example.com/m/calc" {
		t.Fatalf("PackagePath = %q, want the original unsanitized import path", env.PackagePath)
	}
	wantHash, _ := HashRunToken(validToken)
	if env.RunTokenHash != wantHash {
		t.Fatalf("RunTokenHash = %q, want the run's digest so a foreign shard is rejected", env.RunTokenHash)
	}
	if env.ProducedAt.IsZero() {
		t.Fatal("ProducedAt is zero")
	}
	if env.Report.Execution.Total != 2 || len(env.Report.Suites) != 1 {
		t.Fatalf("execution data did not survive: %+v", env.Report)
	}
	if mode := modeOf(t, path); mode != 0o600 {
		t.Fatalf("shard mode = %04o, want 0600", mode)
	}
}

func TestShardNeverCarriesCoverage(t *testing.T) {
	// Producers do not calculate, parse, copy or point at coverage data. The invoker hands the one
	// combined profile straight to the finalizer, which alone owns block deduplication and
	// coverage arithmetic (contract v1.2.6 §9).
	base := secureTempDir(t)
	cfg := enabledConfig(t, base, "example.com/m/calc")

	rep := sampleReport()
	rep.Coverage = report.Coverage{
		Packages: []report.PackageCoverage{{ImportPath: "example.com/m/calc", Covered: 7, Total: 10}},
		Total:    report.AggregateCoverage{Covered: 7, Total: 10},
	}
	if err := NewShardWriter(cfg).Write(rep); err != nil {
		t.Fatalf("Write: %v", err)
	}

	path := filepath.Join(shardsDir(base, "run-1"), ShardFileName("example.com/m/calc"))
	env := readEnvelope(t, path)
	if len(env.Report.Coverage.Packages) != 0 || env.Report.Coverage.Total != (report.AggregateCoverage{}) {
		t.Fatalf("the shard carries coverage: %+v", env.Report.Coverage)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"importPath"`) {
		t.Fatalf("the shard serializes coverage blocks:\n%s", raw)
	}
}

func TestDisabledWriterWritesNothingAndSucceeds(t *testing.T) {
	base := secureTempDir(t)
	err := NewShardWriter(ShardConfig{BaseDir: base, PackagePath: "example.com/m/calc"}).Write(sampleReport())
	if err != nil {
		t.Fatalf("a disabled writer returned an error: %v", err)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a disabled writer touched the filesystem: %v", entries)
	}
}

func TestSecondWriteForTheSamePackageIsADuplicateProducerError(t *testing.T) {
	base := secureTempDir(t)
	cfg := enabledConfig(t, base, "example.com/m/calc")
	w := NewShardWriter(cfg)

	if err := w.Write(sampleReport()); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	err := w.Write(sampleReport())
	if !errors.Is(err, ErrDuplicateProducer) {
		t.Fatalf("second Write returned %v, want ErrDuplicateProducer", err)
	}
}

func TestTwoPackagesWithCollidingPrefixesProduceTwoShards(t *testing.T) {
	// Hardening item 1, end to end: two distinct package paths whose sanitizations collide must
	// produce two distinct shard files, not one overwriting the other.
	base := secureTempDir(t)
	own := mustInitRun(t, base, "run-1", validToken)

	for _, pkg := range []string{"foo/bar", "foo_bar"} {
		cfg := ShardConfig{
			Activated: true, RunID: "run-1", Token: validToken,
			BaseDir: base, PackagePath: pkg, Ownership: own,
		}
		if err := NewShardWriter(cfg).Write(sampleReport()); err != nil {
			t.Fatalf("Write(%q): %v", pkg, err)
		}
	}

	entries, err := os.ReadDir(shardsDir(base, "run-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d shards, want 2: %v", len(entries), entries)
	}

	seen := map[string]bool{}
	for _, e := range entries {
		env := readEnvelope(t, filepath.Join(shardsDir(base, "run-1"), e.Name()))
		seen[env.PackagePath] = true
	}
	for _, pkg := range []string{"foo/bar", "foo_bar"} {
		if !seen[pkg] {
			t.Fatalf("no shard reports package %q; it was overwritten", pkg)
		}
	}
}

func TestAShardWithZeroExecutedTestsIsStillPublished(t *testing.T) {
	// -run, -skip and -short filter TESTS, not packages: the binary still runs and must still
	// publish. Treating "no tests ran" as "skip the shard" would make a filtered run
	// indistinguishable from a crashed one (contract v1.2.6 §5).
	base := secureTempDir(t)
	cfg := enabledConfig(t, base, "example.com/m/calc")

	if err := NewShardWriter(cfg).Write(report.NormalizedReport{SchemaVersion: report.SchemaVersion}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	path := filepath.Join(shardsDir(base, "run-1"), ShardFileName("example.com/m/calc"))
	env := readEnvelope(t, path)
	if env.Report.Execution.Total != 0 {
		t.Fatalf("Execution.Total = %d", env.Report.Execution.Total)
	}
	if env.PackagePath != "example.com/m/calc" {
		t.Fatal("an empty shard must still identify its producer")
	}
}

func TestWriterRefusesToPublishWithoutVerifiedOwnership(t *testing.T) {
	base := secureTempDir(t)
	mustInitRun(t, base, "run-1", validToken)

	cfg := ShardConfig{
		Activated: true, RunID: "run-1", Token: validToken,
		BaseDir: base, PackagePath: "example.com/m/calc",
		// Ownership deliberately left zero: a hand-built config must not be able to publish a
		// shard carrying no proof of which run it belongs to.
	}
	err := NewShardWriter(cfg).Write(sampleReport())
	if err == nil {
		t.Fatal("a shard was published without verified ownership")
	}
	entries, _ := os.ReadDir(shardsDir(base, "run-1"))
	if len(entries) != 0 {
		t.Fatalf("something was written: %v", entries)
	}
}

func TestWriterRefusesAnAssembledPathBeyondTheConsumerBudget(t *testing.T) {
	base := secureTempDir(t)
	deep := filepath.Join(base, strings.Repeat("d", 120), strings.Repeat("e", 120))
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	own := mustInitRun(t, deep, "run-1", validToken)

	cfg := ShardConfig{
		Activated: true, RunID: "run-1", Token: validToken,
		BaseDir: deep, PackagePath: "example.com/m/calc", Ownership: own,
	}
	err := NewShardWriter(cfg).Write(sampleReport())
	if err == nil {
		t.Fatal("a shard was written at a path downstream tooling cannot read back")
	}
	if !strings.Contains(err.Error(), "MAX_PATH") {
		t.Fatalf("diagnostic %q does not explain that this is a consumer-tooling limit", err)
	}
}
