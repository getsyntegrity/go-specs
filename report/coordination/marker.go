package coordination

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// markerSchemaVersion versions run.json independently of the report model and of the shard
// envelope. A reader checks it before trusting any other field.
const markerSchemaVersion = "1"

// maxMarkerBytes bounds the read before decoding. A marker is a few hundred bytes; anything
// larger is corrupt or hostile, and an unbounded read of an attacker-influenced path is an OOM
// waiting to happen (contract v1.2.6 §10).
const maxMarkerBytes = 64 << 10

// markerPublishOps is the publish seam for run.json.
//
// It exists so a test can make the publish fail after the temp file has been written, and then
// assert that no partial marker survives. Without that injection the "never leaves an empty
// marker" property is unpinnable: every test would exercise only the success path, where writing
// in place and publishing atomically look identical.
var markerPublishOps = realPublishOps

// runMarker is the on-disk ownership evidence for one run. It never contains the raw token —
// only the digest of the token's decoded bytes (contract v1.2.6 §5).
type runMarker struct {
	SchemaVersion string    `json:"schemaVersion"`
	RunID         RunID     `json:"runId"`
	RunTokenHash  string    `json:"runTokenHash"`
	CreatedAt     time.Time `json:"createdAt"`
}

// RunOwnership is the result of successfully claiming or verifying a run's marker. It is the
// evidence a producer needs before it is allowed to touch a shard: proof that this process's
// RunToken matches the one recorded when the run was initialized.
//
// This is the only mechanism in the contract that can tell "another producer of the same run"
// apart from "an unrelated invocation that reused the same RunID". Both present identical RunIDs
// to every process involved; only the genuine invocation holds the matching token.
type RunOwnership struct {
	RunID RunID
	// BaseDir is the ABSOLUTE reporting directory this run resolved to. InitializeRun reports it
	// because the invoker must export exactly this value: a relative GO_SPECS_REPORT_DIR resolves
	// per-package under `go test`, so no producer would find the marker.
	BaseDir    string
	MarkerPath string
	// TokenHash is the lowercase hex SHA-256 of the token's decoded bytes, matching the marker's
	// stored value. Every shard envelope carries it, so a shard dropped into the run directory by
	// a foreign process is rejected rather than merged.
	TokenHash string
}

// InitializeRunOptions configures the preflight step that runs once, before `go test`
// (contract v1.2.6 §3 step 2).
type InitializeRunOptions struct {
	RunID   RunID
	Token   RunToken
	BaseDir string
	// Force is explicit operator recovery only: it removes an existing marker and its shard
	// directory before recreating them. It is never set automatically as a fallback after a
	// failed create — the force path is a deliberate operator action, not a race resolver
	// (contract v1.2.6 §5).
	Force bool
}

// InitializeRun exclusively creates the run marker.
//
// It fails when the marker already exists, and that failure is always real and actionable: either
// the RunID was not unique per invocation, or a previous run was abandoned. Exclusive creation is
// only fail-closed if reuse is genuinely abnormal, which is why the contract makes per-invocation
// uniqueness the primary rule rather than relying on a staleness heuristic — a marker left by a
// killed run is byte-for-byte identical to one held by a slow but living run
// (contract v1.2.6 §5).
//
// This is the only code path allowed to create a run marker. Producers only ever read and verify.
func InitializeRun(ctx context.Context, opts InitializeRunOptions) (RunOwnership, error) {
	if err := ctx.Err(); err != nil {
		return RunOwnership{}, err
	}
	// Identity is validated before anything touches the filesystem, so a crafted RunID never
	// reaches a path join.
	runID, err := ValidateRunID(string(opts.RunID))
	if err != nil {
		return RunOwnership{}, err
	}
	token, err := ValidateRunToken(string(opts.Token))
	if err != nil {
		return RunOwnership{}, err
	}
	hash, err := HashRunToken(token)
	if err != nil {
		return RunOwnership{}, err
	}

	// The preflight runs once, from wherever the invoker chose, so a relative base is meaningful
	// here and is resolved rather than rejected. Producers receive the absolute form.
	base, err := filepath.Abs(baseDirOrDefault(opts.BaseDir))
	if err != nil {
		return RunOwnership{}, fmt.Errorf("go-specs report: resolve %s: %w", opts.BaseDir, err)
	}
	if err := ensureSafeBaseDir(base); err != nil {
		return RunOwnership{}, err
	}

	if opts.Force {
		if err := os.RemoveAll(runDir(base, runID)); err != nil {
			return RunOwnership{}, fmt.Errorf("go-specs report: force-remove %s: %w", runDir(base, runID), err)
		}
	}
	if err := mkdirSecure(runDir(base, runID)); err != nil {
		return RunOwnership{}, err
	}
	if err := mkdirSecure(shardsDir(base, runID)); err != nil {
		return RunOwnership{}, err
	}

	path := markerPath(base, runID)
	body, err := json.MarshalIndent(runMarker{
		SchemaVersion: markerSchemaVersion,
		RunID:         runID,
		RunTokenHash:  hash,
		CreatedAt:     time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return RunOwnership{}, fmt.Errorf("go-specs report: encode %s: %w", path, err)
	}

	// The marker is published with the same create-no-replace protocol as a shard: write a temp
	// file inside the run directory, fsync, close, then link.
	//
	// It is NOT written with O_CREATE|O_EXCL straight into place, although that is also exclusive,
	// because a write or close that fails after the exclusive create leaves an EMPTY marker on
	// disk. That file is indistinguishable from a real one to exclusive creation, so every retry
	// of the run id then fails as "already initialized" and every producer reading it fails as
	// marker-unreadable: a transient ENOSPC or EIO would permanently brick the run id and force
	// the operator onto the --force path. Publishing atomically means the marker is either wholly
	// absent or wholly complete.
	if err := writeAndPublish(runDir(base, runID), markerFileName, body, markerPublishOps()); err != nil {
		if errors.Is(err, ErrDuplicateProducer) {
			return RunOwnership{}, fmt.Errorf(
				"go-specs report: run %q is already initialized at %s: either %s was reused across two invocations (it must be unique per invocation, reruns included) or a previous run was abandoned; clear abandoned runs with the gc path, or pass --force / InitializeRunOptions.Force if reusing this run id is deliberate",
				runID, path, EnvRunID)
		}
		return RunOwnership{}, err
	}

	return RunOwnership{RunID: runID, BaseDir: base, MarkerPath: path, TokenHash: hash}, nil
}

func writeAndSync(f *os.File, body []byte) error {
	if _, err := f.Write(body); err != nil {
		return err
	}
	return f.Sync()
}

// VerifyRunOwnership proves that this process belongs to the run whose directory it is about to
// write into, and fails closed on every state it cannot prove.
//
// A matching digest means a legitimate participant: a retry within the same invocation, another
// package's producer, or the finalizer. A marker with a different digest, or no marker at all
// while the gate is on, means a RunID collision or a misconfiguration — never "proceed as if
// reporting were disabled", and never "ownership is unverified but probably fine"
// (contract v1.2.6 §5).
func VerifyRunOwnership(baseDir string, runID RunID, token RunToken) (RunOwnership, error) {
	id, err := ValidateRunID(string(runID))
	if err != nil {
		return RunOwnership{}, err
	}
	tok, err := ValidateRunToken(string(token))
	if err != nil {
		return RunOwnership{}, err
	}
	base := baseDirOrDefault(baseDir)
	if err := requireAbsoluteBaseDir(base); err != nil {
		return RunOwnership{}, err
	}
	if err := ensureSafeBaseDir(base); err != nil {
		return RunOwnership{}, err
	}

	path := markerPath(base, id)
	marker, err := readMarker(path)
	if err != nil {
		return RunOwnership{}, err
	}
	if marker.SchemaVersion != markerSchemaVersion {
		return RunOwnership{}, markerError(ReasonMarkerUnreadable, fmt.Sprintf(
			"%s records schema version %q, and this build understands %q; an unrecognized marker is not partially trusted",
			path, marker.SchemaVersion, markerSchemaVersion))
	}
	if marker.RunID != id {
		return RunOwnership{}, markerError(ReasonMarkerMismatch, fmt.Sprintf(
			"%s belongs to run %q, not %q; the run directory and its marker disagree about which run owns them",
			path, marker.RunID, id))
	}

	hash, err := HashRunToken(tok)
	if err != nil {
		return RunOwnership{}, err
	}
	if !TokenHashesEqual(marker.RunTokenHash, hash) {
		// The token value never appears here: this diagnostic lands in CI logs.
		return RunOwnership{}, markerError(ReasonMarkerMismatch, fmt.Sprintf(
			"this process's %s does not match the token recorded in %s, so run %q is owned by a different invocation; two invocations presenting the same %s but different tokens is exactly the collision this check exists to reject",
			EnvRunToken, path, id, EnvRunID))
	}

	return RunOwnership{RunID: id, BaseDir: base, MarkerPath: path, TokenHash: hash}, nil
}

func readMarker(path string) (runMarker, error) {
	f, err := openFileNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return runMarker{}, markerError(ReasonMarkerMissing, fmt.Sprintf(
				"no run marker at %s; the run was never initialized, so nothing can prove this process belongs to it — run the preflight step that creates the marker and exports %s before %s",
				path, EnvRunToken, EnvGate))
		}
		return runMarker{}, markerError(ReasonMarkerUnreadable, fmt.Sprintf("cannot read %s: %v", path, err))
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(io.LimitReader(f, maxMarkerBytes))
	if err != nil {
		return runMarker{}, markerError(ReasonMarkerUnreadable, fmt.Sprintf("cannot read %s: %v", path, err))
	}

	var marker runMarker
	if err := json.Unmarshal(raw, &marker); err != nil {
		return runMarker{}, markerError(ReasonMarkerUnreadable, fmt.Sprintf(
			"%s is not a readable run marker: %v", path, err))
	}
	if marker.RunTokenHash == "" {
		return runMarker{}, markerError(ReasonMarkerUnreadable, fmt.Sprintf(
			"%s records no token digest, so ownership cannot be proved; an absent digest is a failure to verify, never a successful verification", path))
	}
	return marker, nil
}

func markerError(reason ConfigErrorReason, detail string) error {
	return &ConfigError{
		Source: EnvRunID,
		Reason: reason,
		Detail: detail,
		Remedy: remedyFor(EnvRunID),
	}
}
