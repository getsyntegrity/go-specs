package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/getsyntegrity/go-specs/report"
	"github.com/getsyntegrity/go-specs/report/coordination"
)

// runFinalize wraps coordination.Finalize + coordination.ExitCode (contract v1.2.9 §7, §8's
// finalize-side ownership row). It never re-implements ownership verification, shard discovery,
// merging or rendering; every one of those stays in report/coordination.
func runFinalize(args []string, env func(string) (string, bool), stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("finalize", flag.ContinueOnError)
	fs.SetOutput(stderr)
	runID := fs.String("run-id", "", "run identifier (env "+coordination.EnvRunID+")")
	token := fs.String("token", "", "run ownership token (env "+coordination.EnvRunToken+")")
	reportDir := fs.String("report-dir", "", "reporting base directory; MUST already be absolute (env "+coordination.EnvReportDir+"; default "+coordination.DefaultBaseDir+")")
	producers := fs.String("producers", "", "required: path to a file listing expected producer import paths, one per line (blank lines and # comments ignored)")
	coverProfile := fs.String("coverprofile", "", "path to the one combined `go test -coverprofile` file")
	jsonPath := fs.String("json", "", "write the merged report as JSON to this path")
	xmlPath := fs.String("xml", "", "write the merged report as JUnit XML to this path")
	txtPath := fs.String("txt", "", "write the merged report as plain text to this path")
	htmlPath := fs.String("html", "", "write the merged report as HTML to this path")
	cleanup := fs.Bool("cleanup", false, "remove the run's shard directory after a fully successful merge")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "go-specs-report finalize: unexpected argument(s) %v\n", fs.Args())
		fs.Usage()
		return exitUsage
	}

	resolvedRunID := resolveFlag(*runID, env, coordination.EnvRunID)
	resolvedToken := resolveFlag(*token, env, coordination.EnvRunToken)
	resolvedDir := resolveFlag(*reportDir, env, coordination.EnvReportDir)
	if resolvedDir == "" {
		// Deliberately NOT resolved to absolute here (unlike init): contract v1.2.9 §5 requires
		// finalize to receive the already-absolute directory init resolved and the invoker exported.
		// Falling back to the relative DefaultBaseDir, unresolved, means a run that skipped exporting
		// GO_SPECS_REPORT_DIR is rejected by VerifyRunOwnership below with the same diagnostic a
		// genuinely mistyped relative path would get, rather than silently working by accident.
		resolvedDir = coordination.DefaultBaseDir
	}

	if *producers == "" {
		_, _ = fmt.Fprintln(stderr, "go-specs-report finalize: -producers is required: the authoritative, invoker-supplied list of expected package paths (contract v1.2.9 §5, never inferred with `go list`)")
		return coordination.ExitConfig
	}
	expected, err := readProducerManifest(*producers)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "go-specs-report finalize: read -producers %s: %v\n", *producers, err)
		return coordination.ExitConfig
	}
	if len(expected) == 0 {
		_, _ = fmt.Fprintf(stderr, "go-specs-report finalize: -producers %s has no usable package paths after ignoring blank lines and # comments\n", *producers)
		return coordination.ExitConfig
	}

	var targets []report.Target
	for _, t := range []struct {
		path   string
		format report.Format
	}{
		{*jsonPath, report.FormatJSON},
		{*xmlPath, report.FormatXML},
		{*txtPath, report.FormatTXT},
		{*htmlPath, report.FormatHTML},
	} {
		if t.path != "" {
			targets = append(targets, report.Target{Format: t.format, Path: t.path})
		}
	}

	result, err := coordination.Finalize(context.Background(), coordination.FinalizeOptions{
		RunID:             coordination.RunID(resolvedRunID),
		Token:             coordination.RunToken(resolvedToken),
		BaseDir:           resolvedDir,
		CoverProfile:      *coverProfile,
		ExpectedProducers: expected,
		Targets:           targets,
		Cleanup:           *cleanup,
	})
	printFinalizeSummary(stderr, result, err)
	return coordination.ExitCode(result, err)
}

// readProducerManifest reads one import path per line. Blank lines and lines whose first
// non-whitespace character is `#` are comments and are ignored, per the feature document's
// decision #4: the CLI never runs `go list` itself.
func readProducerManifest(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// printFinalizeSummary prints a short human-readable account of what finalize found, to stderr —
// stdout is reserved for machine-readable output the way init's is.
func printFinalizeSummary(stderr io.Writer, result coordination.FinalizeResult, err error) {
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "go-specs-report finalize: %v\n", err)
		return
	}
	if result.ConfigError != nil {
		_, _ = fmt.Fprintf(stderr, "go-specs-report finalize: configuration error recorded by %s: %s (%s)\n",
			result.ConfigError.PackagePath, result.ConfigError.Diagnostic, result.ConfigError.Reason)
		return
	}
	_, _ = fmt.Fprintf(stderr, "go-specs-report finalize: found %d, missing %d, rejected %d\n",
		len(result.PackagesFound), len(result.PackagesMissing), len(result.Rejected))
	for _, p := range result.PackagesMissing {
		_, _ = fmt.Fprintf(stderr, "  missing: %s\n", p)
	}
	for _, r := range result.Rejected {
		_, _ = fmt.Fprintf(stderr, "  rejected: %s (%s)\n", r.Path, r.Reason)
	}
}
