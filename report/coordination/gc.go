package coordination

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// GCOptions configures one GC call (contract v1.2.9 §5, "Abandoned markers" and
// "Cleanup/retention").
type GCOptions struct {
	// BaseDir is the reporting directory to sweep, one level above the run directories
	// (<BaseDir>/<RunID>/...). It must be absolute, for the same reason every other entry point in
	// this package requires it: GC is typically invoked from wherever an operator or a CI step
	// happens to run it, not from the module root InitializeRun ran from, so a relative value would
	// not name the same directory the rest of the run used.
	BaseDir string
	// Retention is the staleness threshold: a run directory is removed only when its own run.json
	// marker's modification time is older than Retention, never on any other signal. It must be
	// positive. The contract recommends a value comfortably larger than any plausible test run
	// (24h).
	Retention time.Duration
	// Now returns the current time GC measures Retention against. Nil means time.Now; tests inject
	// a fixed clock so aging a marker with os.Chtimes stays deterministic.
	Now func() time.Time
	// DryRun reports what GC would remove without removing anything.
	DryRun bool
}

// GCSkip is one BaseDir entry GC left untouched because it could not prove the entry was an
// abandoned run directory, and why.
type GCSkip struct {
	// Name is the entry's own base name under BaseDir — not a full path, since these are exactly
	// the entries GC refused to treat as a run directory at all.
	Name string
	// Reason is a short, machine-readable explanation: "not-a-run-id", "not-a-directory",
	// "symlink", "no-marker", "marker-symlink", "marker-unreadable", or "remove-failed". It is
	// diagnostic only, not part of any wire format, so it is not drawn from the closed
	// ConfigErrorReason vocabulary (errors.go) that config-error.json's contract actually closes.
	Reason string
}

// GCResult is what one GC call found (contract v1.2.9 §5). Every slice is sorted by RunID/Name for
// deterministic output.
type GCResult struct {
	// Removed lists every run GC removed (or, under DryRun, every run it would have removed).
	Removed []RunID
	// Kept lists every recognizable run directory whose marker was not old enough to remove.
	Kept []RunID
	// Skipped lists every BaseDir entry GC could not prove was an abandoned run directory: not a
	// valid RunID name, not a directory, a symlink, or a directory with no readable run.json.
	Skipped []GCSkip
}

// GC removes run directories under BaseDir whose own run.json marker is older than Retention.
//
// It is the only mechanism that removes the marker of a run killed before finalize
// (contract v1.2.9 §5, "Run marker lifecycle"), and it is never triggered automatically — nothing
// in this package calls it; only an explicit operator action or the `gc` CLI verb does. A marker
// left by a killed run is byte-for-byte identical to one held by a slow but living run, so the
// modification time of the marker itself, compared against the fixed Retention window, is the sole
// staleness rule: there is no second heuristic to fall back on, and none is added here.
//
// GC fails safe. It never removes anything it cannot first prove is a run directory: an entry
// whose name is not a valid RunID, that is not a directory, that is a symlink (at either the run
// directory or the marker itself — neither is ever followed), or that has no readable run.json is
// left in place and reported under GCResult.Skipped, never under Removed. A missing BaseDir means
// there is nothing to collect, not an error: it returns an empty result.
func GC(ctx context.Context, opts GCOptions) (GCResult, error) {
	if err := ctx.Err(); err != nil {
		return GCResult{}, err
	}
	if err := requireAbsoluteBaseDir(opts.BaseDir); err != nil {
		return GCResult{}, err
	}
	if opts.Retention <= 0 {
		return GCResult{}, &ConfigError{
			Reason: ReasonInvalidRetention,
			Value:  fmt.Sprintf("%s", opts.Retention),
			Detail: "GC retention must be a positive duration; a zero or negative window would treat every run as stale, including ones still in progress",
			Remedy: "pass a positive Retention (the contract recommends 24h, comfortably larger than any plausible test run)",
		}
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	entries, err := os.ReadDir(opts.BaseDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return GCResult{}, nil
		}
		return GCResult{}, fmt.Errorf("go-specs report: list %s: %w", opts.BaseDir, err)
	}

	cutoff := now().Add(-opts.Retention)
	var result GCResult

	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		name := e.Name()
		if e.Type()&fs.ModeSymlink != 0 {
			result.Skipped = append(result.Skipped, GCSkip{Name: name, Reason: "symlink"})
			continue
		}
		if !e.IsDir() {
			result.Skipped = append(result.Skipped, GCSkip{Name: name, Reason: "not-a-directory"})
			continue
		}
		runID, err := ValidateRunID(name)
		if err != nil {
			result.Skipped = append(result.Skipped, GCSkip{Name: name, Reason: "not-a-run-id"})
			continue
		}

		markerInfo, err := os.Lstat(markerPath(opts.BaseDir, runID))
		switch {
		case err == nil:
			// fall through
		case errors.Is(err, fs.ErrNotExist):
			result.Skipped = append(result.Skipped, GCSkip{Name: name, Reason: "no-marker"})
			continue
		default:
			result.Skipped = append(result.Skipped, GCSkip{Name: name, Reason: "marker-unreadable"})
			continue
		}
		if markerInfo.Mode()&fs.ModeSymlink != 0 {
			result.Skipped = append(result.Skipped, GCSkip{Name: name, Reason: "marker-symlink"})
			continue
		}

		if markerInfo.ModTime().After(cutoff) {
			result.Kept = append(result.Kept, runID)
			continue
		}

		if !opts.DryRun {
			if err := os.RemoveAll(filepath.Join(opts.BaseDir, name)); err != nil {
				result.Skipped = append(result.Skipped, GCSkip{Name: name, Reason: "remove-failed"})
				continue
			}
		}
		result.Removed = append(result.Removed, runID)
	}

	sortRunIDs(result.Removed)
	sortRunIDs(result.Kept)
	sort.Slice(result.Skipped, func(i, j int) bool { return result.Skipped[i].Name < result.Skipped[j].Name })

	return result, nil
}

func sortRunIDs(ids []RunID) {
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
}
