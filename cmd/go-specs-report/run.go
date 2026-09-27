package main

import (
	"fmt"
	"io"
)

const usage = `usage: go-specs-report <verb> [flags]

verbs:
  init      create the run marker before go test runs (contract v1.2.9 §3 step 2)
  finalize  merge shards into module-wide reports (contract v1.2.9 §7)
  gc        remove run directories abandoned before finalize (contract v1.2.9 §5)

run "go-specs-report <verb> -h" for a verb's own flags.
`

// run is the CLI's whole entry point, factored out of main so a test can call it in-process
// against a fake environment and captured output instead of spawning a subprocess.
//
// env mirrors os.LookupEnv's shape rather than os.Getenv's, because "unset" and "set to empty" are
// different requests everywhere this package reads GO_SPECS_* — the same distinction
// report/coordination itself draws (its own environment interface, config.go).
func run(args []string, env func(string) (string, bool), stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}

	verb, rest := args[0], args[1:]
	switch verb {
	case "init":
		return runInit(rest, env, stdout, stderr)
	case "finalize":
		return runFinalize(rest, env, stdout, stderr)
	case "gc":
		return runGC(rest, env, stdout, stderr)
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "go-specs-report: unknown verb %q\n\n%s", verb, usage)
		return exitUsage
	}
}

// exitUsage is the stdlib `flag` convention this CLI follows for a usage error: no verb, an
// unknown verb, an unknown flag, or unexpected positional arguments (contract v1.2.9 §7 decision
// record #6). It is distinct from coordination.ExitConfig (78): a usage error means the invocation
// itself is malformed, never that the reporting configuration it describes is invalid.
const exitUsage = 2

// resolveFlag returns flagVal when the caller passed it explicitly, otherwise the named
// environment variable's value when it is SET (even to empty), otherwise "". It never conflates
// "flag not passed" with "flag passed as empty string": an explicit -run-id="" is not what this
// CLI's flags ever legitimately mean, so falling through to the environment for it is safe.
func resolveFlag(flagVal string, env func(string) (string, bool), name string) string {
	if flagVal != "" {
		return flagVal
	}
	if v, ok := env(name); ok {
		return v
	}
	return ""
}
