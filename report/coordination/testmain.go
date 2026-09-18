package coordination

import (
	"errors"
	"fmt"
	"io/fs"
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
// cmd/go never propagates a test binary's exit code (contract v1.2.7 §8). The caller surfaces the
// error and fails the binary before m.Run().
//
// The complete integration, which is all that contract v1.2.7 §3 step 3 asks of a package:
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
//			fmt.Fprintln(os.Stderr, err) // never os.Exit here — see below
//		}
//		os.Exit(code)
//	}
//
// Write is called after m.Run() and before os.Exit, mirroring MultiFormatReporter.Flush's
// placement so the two compose in one TestMain body. On an ordinary failure that code still runs,
// so a red package still publishes a complete shard; only abrupt termination bypasses it (F5).
//
// Note what the example does NOT do: it never exits non-zero because Write failed. Contract
// v1.2.7 §8 row 8 is explicit that a publish failure leaves the original test result "preserved,
// unchanged", and that reporting failure "never overwrites or falsifies the test result that
// already happened". Exiting 1 there turns a green package red for a reporting problem — and a
// duplicate publish into the same run id, which is what a re-run of one package produces, is
// enough to trigger it. The reporting failure is not lost: the finalizer sees the producer as
// missing or rejected and fails through its own independent exit status, which is the whole point
// of keeping the two signals separate.
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
// (contract v1.2.7 §5).
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
	token := RunToken(mustLookup(EnvRunToken))
	if !mayRecordConfigError(baseDir, runID, token, cfgErr.Reason) {
		return
	}
	if err := WriteConfigError(baseDir, runID, packagePath, cfgErr.Reason, cause.Error()); err != nil {
		// Reporting the failure to report is a diagnostic, never a second failure: the caller is
		// already about to fail the binary on the original cause.
		fmt.Fprintf(os.Stderr, "go-specs: could not record the configuration failure: %v\n", err)
	}
}

// mayRecordConfigError decides whether this process is entitled to write into the run directory.
//
// The rule is ownership, not the reason code. config-error.json is first-writer-wins and the
// finalizer maps its presence to exit 78, so a record dropped into someone else's run directory
// fails a run that was perfectly healthy — and can crowd out that run's own genuine record. A
// process whose configuration just failed is, by construction, a process that has not proved it
// owns anything.
//
// Two cases are refused outright:
//
//   - ReasonMarkerMismatch: this process PROVED the directory belongs to a different invocation
//     that reused the run id with another token. It is the clearest intruder.
//   - ReasonInvalidGate: the gate value was unparseable, so no run was activated at all. Creating
//     directories for a run nobody asked for is exactly what the gate exists to prevent — a stale
//     GO_SPECS_RUN_ID in a developer's shell must produce no filesystem side effects.
//
// Beyond those, the deciding question is whether a legitimate owner exists. If no marker is
// present, nothing can be harmed and the record is written — that is the useful case, where the
// preflight was skipped and finalize needs to say so rather than merely report missing producers.
// If a marker IS present, the record is written only when this process's token verifies against
// it. That closes the wider version of the same hole: an unrelated invocation that reuses a run id
// while supplying no token at all fails as missing-run-token, never reaches the marker check, and
// would otherwise poison the owner's directory just as effectively as a mismatched one.
func mayRecordConfigError(baseDir string, runID RunID, token RunToken, reason ConfigErrorReason) bool {
	switch reason {
	case ReasonMarkerMismatch, ReasonInvalidGate:
		return false
	}
	if _, err := os.Lstat(markerPath(baseDirOrDefault(baseDir), runID)); errors.Is(err, fs.ErrNotExist) {
		return true
	}
	_, err := VerifyRunOwnership(baseDir, runID, token)
	return err == nil
}

func mustLookup(name string) string {
	v, _ := defaultResolver.env.Lookup(name)
	return v
}
