package coordination

import (
	"errors"
	"fmt"
	"os"
)

// ShardWriterFromEnv is the one call a participating package makes from TestMain.
//
// It resolves this process's coordination configuration, proves run ownership, and returns a
// writer. When the gate is off it returns a writer that does nothing and a nil error, so a package
// that wires it in is not affected by ordinary `go test` runs at all.
//
// On a configuration or ownership failure it records config-error.json best-effort before
// returning the error, because the test binary's own exit status cannot carry that distinction:
// cmd/go never propagates a test binary's exit code (contract v1.2.6 §8). The caller surfaces the
// error and fails the binary before m.Run().
//
// The complete integration, which is all that contract v1.2.6 §3 step 3 asks of a package:
//
//	var reporter = report.NewMultiFormat()
//
//	func TestMain(m *testing.M) {
//		writer, err := coordination.ShardWriterFromEnv("example.com/mod/pkg")
//		if err != nil {
//			fmt.Fprintln(os.Stderr, err)
//			os.Exit(1)
//		}
//		code := m.Run()
//		if err := writer.Write(reporter.Report()); err != nil {
//			fmt.Fprintln(os.Stderr, err)
//			os.Exit(1)
//		}
//		os.Exit(code)
//	}
//
// Write is called after m.Run() and before os.Exit, mirroring MultiFormatReporter.Flush's
// placement so the two compose in one TestMain body. On an ordinary failure that code still runs,
// so a red package still publishes a complete shard; only abrupt termination bypasses it (F5).
func ShardWriterFromEnv(packagePath string) (*ShardWriter, error) {
	cfg, err := ShardConfigFromEnv(packagePath)
	if err != nil {
		recordConfigError(packagePath, err)
		return nil, err
	}
	return NewShardWriter(cfg), nil
}

// recordConfigError writes the config-error.json record when it can, and stays silent when it
// cannot.
//
// It is best-effort by design. When the run identity itself is what failed, there is no run
// directory to address and no record to write — the binary still fails loudly with the same
// diagnostic on stderr, and finalize reports the affected producers as missing
// (contract v1.2.6 §5).
func recordConfigError(packagePath string, cause error) {
	var cfgErr *ConfigError
	if !errors.As(cause, &cfgErr) {
		return
	}
	// The gate is on whenever this is reached, so these reads belong on the Getenv path.
	rawID, _ := defaultResolver.env.Lookup(EnvRunID)
	runID, err := ValidateRunID(rawID)
	if err != nil {
		return
	}
	baseDir := baseDirOrDefault(mustLookup(EnvReportDir))
	if err := WriteConfigError(baseDir, runID, packagePath, cfgErr.Reason, cause.Error()); err != nil {
		// Reporting the failure to report is a diagnostic, never a second failure: the caller is
		// already about to fail the binary on the original cause.
		fmt.Fprintf(os.Stderr, "go-specs: could not record the configuration failure: %v\n", err)
	}
}

func mustLookup(name string) string {
	v, _ := defaultResolver.env.Lookup(name)
	return v
}
