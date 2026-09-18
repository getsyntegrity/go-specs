package coordination

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getsyntegrity/go-specs/report"
)

// maxAssembledPathBytes is the downstream-tooling budget for the assembled absolute shard path.
//
// This is NOT there to stop Go from failing: os.fixLongPath transparently rewrites a path to the
// extended-length form near Windows' threshold, so shard creation by go-specs does not fail at 260
// characters. What breaks is the CONSUMER side — archivers, artifact uploaders, CI runners and
// editors that still assume MAX_PATH will choke on a deep run directory that Go itself wrote
// without complaint. A silently-created path that downstream tooling cannot read is a worse
// failure than a loud one, discovered later and further away (contract v1.2.6 §5).
const maxAssembledPathBytes = 260

// ShardWriter is the producer entry point: one call from TestMain, after m.Run() returns and
// before os.Exit, mirroring report.MultiFormatReporter.Flush's existing placement so the two
// compose in the same TestMain body.
type ShardWriter struct {
	cfg ShardConfig
	ops publishOps
	now func() time.Time
}

// NewShardWriter returns a writer for cfg. A writer built from a disabled config is valid and
// does nothing, so callers never have to branch on the gate themselves.
func NewShardWriter(cfg ShardConfig) *ShardWriter {
	return &ShardWriter{cfg: cfg, ops: realPublishOps(), now: func() time.Time { return time.Now().UTC() }}
}

// Write serializes rep into a versioned envelope and publishes it as this package's shard.
//
// It is a no-op returning nil when the config is disabled. On an ordinary test failure it still
// publishes: TestMain's post-m.Run() code runs on failure, including a panic recovered by the
// testing package, so the failure is fully represented in the merged report. Only abrupt process
// termination bypasses it, and making that fact loud is the finalizer's job, not this one's
// (contract v1.2.6 §8, F5).
//
// A shard reporting zero executed tests is a valid shard, not a missing producer: -run, -skip and
// -short filter TESTS, not packages, and the binary still runs. Treating "no tests ran" as "skip
// the shard" would make a filtered run indistinguishable from a crashed one
// (contract v1.2.6 §5, Expected producer set).
func (w *ShardWriter) Write(rep report.NormalizedReport) error {
	if !w.cfg.Enabled() {
		return nil
	}
	if w.cfg.Ownership.TokenHash == "" {
		// Unreachable through ShardConfigFromEnv, which verifies ownership before returning an
		// enabled config. Checked anyway because a hand-built ShardConfig must not be able to
		// publish a shard that carries no proof of which run it belongs to.
		return fmt.Errorf("go-specs report: refusing to publish a shard for run %q: ownership was never verified", w.cfg.RunID)
	}

	// The producer repeats the ancestor check before writing: ownership was verified earlier, and
	// a directory whose parent someone else can swap is not the directory that was verified
	// (contract v1.2.6 §10).
	if err := ensureSafeBaseDir(w.cfg.BaseDir); err != nil {
		return err
	}

	// Coverage is stripped rather than trusted to be absent. A producer that passes through a
	// populated Coverage would have the finalizer merging per-process numbers it is supposed to
	// own exclusively, and the resulting totals would be wrong in a way nothing downstream can
	// detect (contract v1.2.6 §9).
	rep.Coverage = report.Coverage{}

	body, err := json.MarshalIndent(ShardEnvelope{
		ShardSchemaVersion: ShardSchemaVersion,
		RunID:              w.cfg.RunID,
		RunTokenHash:       w.cfg.Ownership.TokenHash,
		PackagePath:        w.cfg.PackagePath,
		ProducedAt:         w.now(),
		Report:             rep,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("go-specs report: encode shard for %s: %w", w.cfg.PackagePath, err)
	}

	dir := shardsDir(w.cfg.BaseDir, w.cfg.RunID)
	name := ShardFileName(w.cfg.PackagePath)
	if err := checkContainedAndBounded(dir, name); err != nil {
		return err
	}
	return writeAndPublish(dir, name, body, w.ops)
}

// checkContainedAndBounded revalidates the assembled destination. A shard's own package path is
// never trusted as a write destination without this check (contract v1.2.6 §10).
func checkContainedAndBounded(dir, name string) error {
	full := filepath.Join(dir, name)
	cleanDir := filepath.Clean(dir) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(full), cleanDir) {
		return fmt.Errorf("go-specs report: refusing to write %s: it escapes the shard directory %s", full, dir)
	}
	abs, err := filepath.Abs(full)
	if err != nil {
		return fmt.Errorf("go-specs report: resolve %s: %w", full, err)
	}
	if len(abs) > maxAssembledPathBytes {
		return fmt.Errorf(
			"go-specs report: refusing to write a shard at a %d-byte path (budget %d): %s. Go itself would write it, but archivers, artifact uploaders and CI runners that still assume MAX_PATH would not be able to read it back. Shorten %s (currently %d bytes) or %s (currently %d bytes)",
			len(abs), maxAssembledPathBytes, abs,
			EnvReportDir, len(filepath.Dir(filepath.Dir(dir))),
			EnvRunID, len(filepath.Base(filepath.Dir(dir))))
	}
	return nil
}
