package coordination

import "strings"

// ConfigErrorReason is the machine-readable cause of a configuration failure. It is what
// config-error.json records and what tooling branches on; the human diagnostic is separate and
// may be reworded freely (contract v1.2.7 §5).
type ConfigErrorReason string

// The closed set from contract v1.2.7 §5. Anything not in this list is not a configuration error.
const (
	ReasonMissingRunID     ConfigErrorReason = "missing-run-id"
	ReasonMissingRunToken  ConfigErrorReason = "missing-run-token"
	ReasonInvalidRunID     ConfigErrorReason = "invalid-run-id"
	ReasonInvalidRunToken  ConfigErrorReason = "invalid-run-token"
	ReasonInvalidGate      ConfigErrorReason = "invalid-activation-gate"
	ReasonMarkerMissing    ConfigErrorReason = "marker-missing"
	ReasonMarkerMismatch   ConfigErrorReason = "marker-mismatch"
	ReasonMarkerUnreadable ConfigErrorReason = "marker-unreadable"
	// ReasonInvalidReportDir extends contract v1.2.7 §5's set. §5 defines the reason vocabulary as
	// closed, and this value is not in it, because the contract does not anticipate that a
	// relative GO_SPECS_REPORT_DIR resolves per-package under `go test`. Flagged for the contract
	// rather than folded into a neighbouring reason, which would misreport the cause.
	ReasonInvalidReportDir ConfigErrorReason = "invalid-report-dir"
)

// ConfigError reports reporting coordination that was requested but cannot be used.
//
// It is modelled on specs.ShardConfigError, for the same reason: a CI log line that explains the
// red build rather than one that just says "invalid". Contract v1.2.7 §5 makes that normative
// here — every one of these messages MUST name the exact environment variable at fault and, when
// the likely cause is a leftover export rather than a genuine CI misconfiguration, MUST state the
// exact remedy. "Invalid reporting configuration" without a variable name is not an acceptable
// diagnostic: the person hitting this is usually a developer who does not know the variable is
// set at all.
//
// A *ConfigError never means "reporting is disabled". Callers must fail; degrading to disabled
// reporting is the silent failure this type exists to prevent (contract v1.2.7 §5, §8).
type ConfigError struct {
	// Source names the environment variable at fault: one of the Env* constants.
	Source string
	// Value is the offending value, already quoted for display. It is deliberately empty for
	// EnvRunToken faults — contract v1.2.7 §5 forbids echoing the token, even a malformed one,
	// because these messages land in CI logs.
	Value string
	// Reason is the machine-readable cause recorded in config-error.json.
	Reason ConfigErrorReason
	// Detail says why the value cannot be used, in terms of the pattern or check it failed.
	Detail string
	// Remedy states what to do instead, including how to turn reporting off entirely.
	Remedy string
}

func (e *ConfigError) Error() string {
	var b strings.Builder
	b.WriteString("go-specs report: invalid reporting configuration")
	if e.Source != "" {
		b.WriteString(" from ")
		b.WriteString(e.Source)
	}
	if e.Value != "" {
		b.WriteString(" (")
		b.WriteString(e.Value)
		b.WriteString(")")
	}
	if e.Detail != "" {
		b.WriteString(": ")
		b.WriteString(e.Detail)
	}
	if e.Remedy != "" {
		b.WriteString("; ")
		b.WriteString(e.Remedy)
	}
	return b.String()
}

// remedyFor states the exact command that makes the problem go away. The two shapes differ
// because the likely cause differs: a bad gate value is a deliberate-but-wrong setting, while a
// bad identity value is usually a leftover export in a shell nobody remembers configuring.
func remedyFor(source string) string {
	switch source {
	case EnvGate:
		return "set " + EnvGate + ` to "1" or "0", or run "unset ` + EnvGate + `" to turn shard reporting off for this shell`
	default:
		return "fix " + source + ` in the invoking script, or run "unset ` + EnvRunID + " " + EnvRunToken + " " + EnvGate + `" to turn shard reporting off for this shell`
	}
}
