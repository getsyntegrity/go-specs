package coordination

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/getsyntegrity/go-specs/report"
)

// Exit codes Finalize's outcome maps to, per contract v1.2.8 §8. 78 (EX_CONFIG) is reserved for a
// configuration failure specifically: an ownership check that failed, invalid FinalizeOptions, or
// a recorded config-error.json. 1 is an ordinary reporting failure — missing or rejected
// producers, or any other error, such as a coverage-profile or render failure — never confusable
// with 78 or with `go test`'s own exit codes, and finalize is a separate command whose 1 cannot be
// confused with `go test`'s either.
const (
	// ExitConfig is EX_CONFIG: a configuration failure, never a reporting result (contract v1.2.8
	// §8, "Open decisions" — the finalize-side ownership-failure exit code).
	ExitConfig = 78
	// ExitReportingFailure is finalize's own non-zero code for an ordinary reporting failure.
	ExitReportingFailure = 1
)

// FinalizeOptions configures one Finalize call (contract v1.2.8 §7).
type FinalizeOptions struct {
	RunID RunID
	// Token is verified against run.json (§5) before any shard is read; fail-closed on mismatch.
	Token   RunToken
	BaseDir string
	// CoverProfile is the path to the one combined `go test -coverprofile` file. Optional: an
	// empty string means no coverage is attached to Merged.
	CoverProfile string
	// ExpectedProducers is the required, authoritative list of packages expected to emit shards
	// (contract v1.2.8 §5, "Expected producer set"). Never inferred, e.g. via `go list`.
	ExpectedProducers []string
	// Targets are the module-wide XML/HTML/TXT/JSON outputs, reusing report.Target.
	Targets []report.Target
	// Cleanup prunes the run's shard directory after a successful merge, when true.
	Cleanup bool
}

// FinalizeResult is what Finalize found and produced.
type FinalizeResult struct {
	Merged          report.NormalizedReport
	PackagesFound   []string
	PackagesMissing []string
	Rejected        []RejectedShard
	// ConfigError is non-nil when a pre-test configuration failure was recorded by a package
	// binary (§5); the caller maps a non-nil value to exit code 78 via ExitCode.
	ConfigError *ConfigErrorRecord
}

// Finalize is the only code path allowed to read shard files, merge them, and render module-wide
// reports (contract v1.2.8 §7).
//
// It runs, in order, honouring ctx cancellation between each step:
//
//  1. VerifyRunOwnership against run.json — fail-closed on any mismatch.
//  2. A config-error.json check. If present, it returns immediately with FinalizeResult.ConfigError
//     set and nothing merged or rendered, so the caller can exit 78 (§8).
//  3. Shard discovery and verification (discoverShards): every final shard is checked against
//     ExpectedProducers, RunID and RunTokenHash, its filename digest, and staleness; every
//     rejection uses the closed reason vocabulary, and duplicates are never silently resolved.
//  4. A deterministic merge of every accepted shard (mergeReports).
//  5. Coverage, read from opts.CoverProfile through the block-deduplicating parser
//     (report.ParseCoverageProfileMerged), when a profile path was given.
//  6. Rendering every Target atomically (temp file in the target's own directory, then rename),
//     creating parent directories as needed.
//  7. Cleanup — removing the run directory — only when opts.Cleanup is set AND the finalize
//     succeeded completely: no missing producers, no rejected shards, no render error.
//
// Missing-shard strictness is unconditional: Finalize always reports PackagesMissing/Rejected
// rather than silently tolerating them; it is ExitCode, not Finalize itself, that maps their
// presence to the reporting-failure exit code.
func Finalize(ctx context.Context, opts FinalizeOptions) (FinalizeResult, error) {
	if err := ctx.Err(); err != nil {
		return FinalizeResult{}, err
	}

	ownership, err := VerifyRunOwnership(opts.BaseDir, opts.RunID, opts.Token)
	if err != nil {
		return FinalizeResult{}, err
	}
	base := ownership.BaseDir

	if err := ctx.Err(); err != nil {
		return FinalizeResult{}, err
	}

	cfgErr, err := readConfigErrorIfPresent(base, opts.RunID)
	if err != nil {
		return FinalizeResult{}, err
	}
	if cfgErr != nil {
		// Contract v1.2.8 §7 step 2: report it and return without merging or rendering.
		return FinalizeResult{ConfigError: cfgErr}, nil
	}

	if err := ctx.Err(); err != nil {
		return FinalizeResult{}, err
	}

	discovered, err := discoverShards(shardDiscoveryOptions{
		BaseDir:           base,
		RunID:             opts.RunID,
		RunTokenHash:      ownership.TokenHash,
		ExpectedProducers: opts.ExpectedProducers,
	})
	if err != nil {
		return FinalizeResult{}, err
	}

	if err := ctx.Err(); err != nil {
		return FinalizeResult{}, err
	}

	merged := mergeReports(discovered.Accepted)

	result := FinalizeResult{
		Merged:          merged,
		PackagesFound:   packagePathsOf(discovered.Accepted),
		PackagesMissing: discovered.Missing,
		Rejected:        discovered.Rejected,
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}

	if opts.CoverProfile != "" {
		cov, err := parseCoverProfile(opts.CoverProfile)
		if err != nil {
			return result, err
		}
		result.Merged.Coverage = cov
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}

	if err := renderTargetsAtomically(opts.Targets, result.Merged); err != nil {
		return result, err
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}

	succeeded := len(result.PackagesMissing) == 0 && len(result.Rejected) == 0
	if opts.Cleanup && succeeded {
		if err := os.RemoveAll(runDir(base, opts.RunID)); err != nil {
			return result, fmt.Errorf("go-specs report: clean up %s: %w", runDir(base, opts.RunID), err)
		}
	}

	return result, nil
}

// ExitCode maps a Finalize outcome to the process exit code the caller should use, per contract
// v1.2.8 §8: 78 for every configuration/ownership/validation failure (including a non-nil
// FinalizeResult.ConfigError), 1 for any other error or for a non-empty PackagesMissing/Rejected,
// 0 otherwise.
func ExitCode(res FinalizeResult, err error) int {
	if err != nil {
		if isConfigFailure(err) {
			return ExitConfig
		}
		return ExitReportingFailure
	}
	if res.ConfigError != nil {
		return ExitConfig
	}
	if len(res.PackagesMissing) > 0 || len(res.Rejected) > 0 {
		return ExitReportingFailure
	}
	return 0
}

// isConfigFailure reports whether err is a configuration/ownership/validation failure rather than
// an ordinary reporting failure: a *ConfigError (VerifyRunOwnership's own failure mode, and every
// identity-validation failure it composes) or ErrInvalidExpectedProducers.
func isConfigFailure(err error) bool {
	var cfgErr *ConfigError
	if errors.As(err, &cfgErr) {
		return true
	}
	return errors.Is(err, ErrInvalidExpectedProducers)
}

func packagePathsOf(envelopes []ShardEnvelope) []string {
	if len(envelopes) == 0 {
		return nil
	}
	paths := make([]string, len(envelopes))
	for i, env := range envelopes {
		paths[i] = env.PackagePath
	}
	return paths
}

// parseCoverProfile reads and parses path through the block-deduplicating coverage parser (T1,
// contract v1.2.8 §9): the finalizer is the sole owner of coverage arithmetic, reading the one
// combined profile the invoker names directly, never a per-shard fragment.
func parseCoverProfile(path string) (report.Coverage, error) {
	f, err := os.Open(path)
	if err != nil {
		return report.Coverage{}, fmt.Errorf("go-specs report: open coverage profile %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	cov, err := report.ParseCoverageProfileMerged(f)
	if err != nil {
		return report.Coverage{}, fmt.Errorf("go-specs report: parse coverage profile %s: %w", path, err)
	}
	return cov, nil
}

// renderTargetsAtomically renders rep to every target, each one atomically: a temp file created in
// the target's own directory, then renamed into place. Unlike a shard's create-no-replace publish
// (publish.go), a rendered report target is expected to be overwritten on a re-run of finalize, so
// this uses an ordinary replacing rename rather than link+unlink.
func renderTargetsAtomically(targets []report.Target, rep report.NormalizedReport) error {
	for _, t := range targets {
		if err := renderTargetAtomically(t, rep); err != nil {
			return fmt.Errorf("go-specs report: render %s as %s: %w", t.Path, t.Format, err)
		}
	}
	return nil
}

func renderTargetAtomically(t report.Target, rep report.NormalizedReport) error {
	dir := filepath.Dir(t.Path)
	if dir == "" {
		dir = "."
	}
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory: %w", err)
		}
	}

	tmp, err := os.CreateTemp(dir, ".finalize-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	renderErr := renderReportFormat(tmp, t.Format, rep)
	closeErr := tmp.Close()
	if renderErr != nil {
		_ = os.Remove(tmpPath)
		return renderErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}
	if err := os.Rename(tmpPath, t.Path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("publish %s: %w", t.Path, err)
	}
	return nil
}

func renderReportFormat(w io.Writer, format report.Format, rep report.NormalizedReport) error {
	switch format {
	case report.FormatXML:
		return report.RenderXML(w, rep)
	case report.FormatHTML:
		return report.RenderHTML(w, rep)
	case report.FormatTXT:
		return report.RenderTXT(w, rep)
	case report.FormatJSON:
		return report.RenderJSON(w, rep)
	default:
		return fmt.Errorf("unknown target format %q", format)
	}
}
