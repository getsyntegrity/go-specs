package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/getsyntegrity/go-specs/report/coordination"
)

// defaultRetention matches the contract's own recommendation (v1.2.9 §5, "Abandoned markers"): a
// value comfortably larger than any plausible test run.
const defaultRetention = 24 * time.Hour

// runGC wraps coordination.GC (contract v1.2.9 §5). Like finalize, -report-dir is passed through
// unresolved: GC itself requires an absolute BaseDir and rejects a relative one, and the CLI must
// not paper over a run that never exported the absolute directory init produced.
func runGC(args []string, env func(string) (string, bool), stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reportDir := fs.String("report-dir", "", "reporting base directory; MUST already be absolute (env "+coordination.EnvReportDir+"; default "+coordination.DefaultBaseDir+")")
	retention := fs.Duration("retention", defaultRetention, "remove a run only when its run.json marker is older than this")
	dryRun := fs.Bool("dry-run", false, "report what would be removed without removing anything")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "go-specs-report gc: unexpected argument(s) %v\n", fs.Args())
		fs.Usage()
		return exitUsage
	}

	resolvedDir := resolveFlag(*reportDir, env, coordination.EnvReportDir)
	if resolvedDir == "" {
		resolvedDir = coordination.DefaultBaseDir
	}

	result, err := coordination.GC(context.Background(), coordination.GCOptions{
		BaseDir:   resolvedDir,
		Retention: *retention,
		DryRun:    *dryRun,
	})
	if err != nil {
		fmt.Fprintf(stderr, "go-specs-report gc: %v\n", err)
		var cfgErr *coordination.ConfigError
		if errors.As(err, &cfgErr) {
			return coordination.ExitConfig
		}
		return coordination.ExitReportingFailure
	}

	verb := "removed"
	if *dryRun {
		verb = "would remove"
	}
	for _, id := range result.Removed {
		fmt.Fprintf(stdout, "%s %s\n", verb, id)
	}
	for _, id := range result.Kept {
		fmt.Fprintf(stdout, "kept %s\n", id)
	}
	for _, s := range result.Skipped {
		fmt.Fprintf(stderr, "go-specs-report gc: skipped %s (%s)\n", s.Name, s.Reason)
	}
	return 0
}
