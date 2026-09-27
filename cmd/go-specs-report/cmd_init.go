package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/getsyntegrity/go-specs/report/coordination"
)

// runInit wraps coordination.InitializeRun (contract v1.2.9 §3 step 2). Per decision #6 of the
// feature document, any failure here — a missing or invalid identity value, or InitializeRun
// itself refusing — is invalid configuration, so init only ever exits 0 or coordination.ExitConfig
// (besides a usage error in flag parsing).
func runInit(args []string, env func(string) (string, bool), stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	runID := fs.String("run-id", "", "run identifier, required, never generated (env "+coordination.EnvRunID+")")
	token := fs.String("token", "", "run ownership token; generated with crypto/rand when omitted (env "+coordination.EnvRunToken+")")
	reportDir := fs.String("report-dir", "", "reporting base directory (env "+coordination.EnvReportDir+"; default "+coordination.DefaultBaseDir+")")
	force := fs.Bool("force", false, "explicit operator recovery: remove and recreate an existing run marker")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "go-specs-report init: unexpected argument(s) %v\n", fs.Args())
		fs.Usage()
		return exitUsage
	}

	resolvedRunID := resolveFlag(*runID, env, coordination.EnvRunID)
	if resolvedRunID == "" {
		_, _ = fmt.Fprintf(stderr, "go-specs-report init: a run id is required (-run-id or %s); init never generates one — the invoker owns its uniqueness\n", coordination.EnvRunID)
		return coordination.ExitConfig
	}

	resolvedToken := resolveFlag(*token, env, coordination.EnvRunToken)
	if resolvedToken == "" {
		generated, err := coordination.GenerateRunToken()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "go-specs-report init: generate run token: %v\n", err)
			return coordination.ExitConfig
		}
		resolvedToken = string(generated)
	}

	resolvedDir := resolveFlag(*reportDir, env, coordination.EnvReportDir)
	if resolvedDir == "" {
		resolvedDir = coordination.DefaultBaseDir
	}
	// init is the preflight step, run once from wherever the invoker chose (contract v1.2.9 §5,
	// "InitializeRun therefore resolves it and reports the absolute path back, which is what the
	// invoker must export"). Resolving here, once, is exactly that contract obligation; finalize and
	// gc must NOT do this themselves — they require the already-absolute value this step produces.
	absDir, err := filepath.Abs(resolvedDir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "go-specs-report init: resolve %s: %v\n", resolvedDir, err)
		return coordination.ExitConfig
	}

	own, err := coordination.InitializeRun(context.Background(), coordination.InitializeRunOptions{
		RunID:   coordination.RunID(resolvedRunID),
		Token:   coordination.RunToken(resolvedToken),
		BaseDir: absDir,
		Force:   *force,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "go-specs-report init: %v\n", err)
		return coordination.ExitConfig
	}

	// Printed as KEY=value so a CI step can append this straight to $GITHUB_ENV or `source` it.
	// GO_SPECS_REPORT_SHARDS's value must match what ShardConfigFromEnv's gate parser (config.go)
	// treats as "on".
	_, _ = fmt.Fprintf(stdout, "%s=1\n", coordination.EnvGate)
	_, _ = fmt.Fprintf(stdout, "%s=%s\n", coordination.EnvRunID, own.RunID)
	_, _ = fmt.Fprintf(stdout, "%s=%s\n", coordination.EnvRunToken, resolvedToken)
	_, _ = fmt.Fprintf(stdout, "%s=%s\n", coordination.EnvReportDir, own.BaseDir)
	return 0
}
