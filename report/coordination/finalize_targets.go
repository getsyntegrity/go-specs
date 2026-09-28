package coordination

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/getsyntegrity/go-specs/report"
)

// validateFinalizeTargets rejects, before Finalize renders or deletes anything, the two option
// combinations that can destroy the only copy of a report (issue #309):
//
//   - Cleanup with no output target. Cleanup removes the run directory, so with nothing rendered
//     the merged report would exist only in memory and every shard would be lost.
//   - A target whose resolved location is inside the run directory. It would be rendered and then
//     removed by Cleanup, and even without Cleanup it would litter the shard directory that gc and
//     later finalize runs own.
//
// Both are configuration failures (*ConfigError, exit 78). A target is compared by resolved
// location: relative paths are made absolute, and symlinks are resolved on the deepest ancestor
// that already exists, so a link into the run directory is caught even though the target's own
// directory has not been created yet.
func validateFinalizeTargets(opts FinalizeOptions, base string) error {
	if opts.Cleanup && len(opts.Targets) == 0 {
		return &ConfigError{
			Source: "finalize options",
			Reason: ReasonInvalidFinalizeOptions,
			Detail: "Cleanup would delete the run's shards but no output target was given, so the merged report would be lost",
			Remedy: "pass at least one output target (-json, -xml, -txt or -html) outside the run directory, or drop -cleanup",
		}
	}

	run, err := resolveLocation(runDir(base, opts.RunID))
	if err != nil {
		return fmt.Errorf("go-specs report: resolve run directory %s: %w", runDir(base, opts.RunID), err)
	}
	for _, t := range opts.Targets {
		loc, err := resolveLocation(t.Path)
		if err != nil {
			return &ConfigError{
				Source: "finalize options",
				Value:  fmt.Sprintf("%q", t.Path),
				Reason: ReasonInvalidFinalizeOptions,
				Detail: fmt.Sprintf("cannot resolve %s target location: %v", targetFormat(t), err),
				Remedy: "pass a resolvable output path outside the run directory",
			}
		}
		if within(run, loc) {
			return &ConfigError{
				Source: "finalize options",
				Value:  fmt.Sprintf("%q", t.Path),
				Reason: ReasonInvalidFinalizeOptions,
				Detail: fmt.Sprintf("the %s target resolves inside the run directory %s, where it would be deleted by cleanup and mixed with shards", targetFormat(t), run),
				Remedy: "write the report outside the run directory",
			}
		}
	}
	return nil
}

func targetFormat(t report.Target) string {
	return string(t.Format)
}

// resolveLocation returns an absolute path for p with symlinks resolved on the deepest existing
// ancestor. The not-yet-existing tail is appended lexically, which is exactly what MkdirAll will
// create.
func resolveLocation(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return resolveExisting(abs)
}

func resolveExisting(abs string) (string, error) {
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return abs, nil
	}
	head, err := resolveExisting(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(head, filepath.Base(abs)), nil
}

// within reports whether path is dir itself or lies below it. Comparison is by path element, so
// "run-1-reports" is not inside "run-1".
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
