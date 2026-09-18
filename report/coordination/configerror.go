package coordination

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// configErrorSchemaVersion versions config-error.json independently of the shard envelope.
const configErrorSchemaVersion = "1"

// ConfigErrorRecord is the pre-test configuration failure record written by a package binary when
// its checks fail before m.Run().
//
// It exists because a test binary's exit status cannot carry this distinction: cmd/go never
// propagates a test binary's exit code — on a failed test action it sets the literal 1 — so a
// TestMain exiting 2 produces "exit status 2" in the output while `go test` exits 1, and CI cannot
// branch on it. The record, not the exit code, is what the preflight/finalize layer maps to exit
// 78 (EX_CONFIG) (contract v1.2.6 §8).
//
// It never contains the raw token.
type ConfigErrorRecord struct {
	SchemaVersion string            `json:"schemaVersion"`
	RunID         RunID             `json:"runId"`
	PackagePath   string            `json:"packagePath"`
	Reason        ConfigErrorReason `json:"reason"`
	Diagnostic    string            `json:"diagnostic"`
	ObservedAt    time.Time         `json:"observedAt"`
}

// WriteConfigError records a pre-test configuration or ownership failure at
// <BaseDir>/<RunID>/config-error.json, using the same create-no-replace publish protocol as a
// shard.
//
// First writer wins: a second failing package that finds the record already present does not
// overwrite it and does not report an error on that account — every package in the invocation
// observes the same misconfiguration, and the first account of it is as good as the tenth
// (contract v1.2.6 §5).
//
// Callers invoke this on the ShardConfigFromEnv error path and then fail the binary. When the run
// directory is itself unusable — which is often what failed — the record cannot be written; the
// binary still fails loudly with the same diagnostic on stderr, and finalize reports the affected
// producers as missing.
func WriteConfigError(baseDir string, runID RunID, packagePath string, reason ConfigErrorReason, diagnostic string) error {
	id, err := ValidateRunID(string(runID))
	if err != nil {
		return err
	}
	base := baseDirOrDefault(baseDir)

	body, err := json.MarshalIndent(ConfigErrorRecord{
		SchemaVersion: configErrorSchemaVersion,
		RunID:         id,
		PackagePath:   packagePath,
		Reason:        reason,
		Diagnostic:    diagnostic,
		ObservedAt:    time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("go-specs report: encode %s: %w", configErrorPath(base, id), err)
	}

	dir := runDir(base, id)
	if err := mkdirSecure(dir); err != nil {
		return err
	}
	err = writeAndPublish(dir, filepath.Base(configErrorPath(base, id)), body, realPublishOps())
	if errors.Is(err, ErrDuplicateProducer) {
		// Another package got there first. That is the expected outcome, not a failure.
		return nil
	}
	return err
}
