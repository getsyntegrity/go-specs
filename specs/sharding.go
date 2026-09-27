// sharding.go provides deterministic test sharding for CI: split specs across jobs by index.
//
// Sharding has three states, and the middle one is why this file is careful: sharding can be not
// configured at all (run the whole suite — legitimate), configured and valid (run this partition),
// or configured and unusable (a CI typo such as SHARD_TOTAL=0). The third state used to be reported
// as the first, so every worker ran 100% of the suite and the build still went green. It is now a
// hard failure: invalid configuration never means "run everything" and never means "run nothing
// successfully" (issue #174).
//
// Usage: run only the Nth shard of M total (e.g. shard 2 of 10). Filter before execution so
// the runner's loop is unchanged and allocation-free.
//
//	specs := collectSpecs()
//	shard, total, err := specs.ShardFromArgsOrEnv()
//	switch {
//	case errors.Is(err, specs.ErrShardNotConfigured):
//	    // no sharding requested: run the whole suite
//	case err != nil:
//	    fmt.Fprintln(os.Stderr, err)
//	    os.Exit(2)
//	default:
//	    specs = ShardSpecs(specs, shard, total)
//	}
//	runner := NewMinimalRunnerFromSpecs(specs)
//	runner.Run(t)
package specs

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ErrShardNotConfigured reports that no sharding was requested: no -shard flag and no SHARD,
// SHARD_INDEX or SHARD_TOTAL in the environment. It is not a failure — the caller runs the whole
// suite. It exists so that "no sharding requested" stays distinguishable from "sharding requested
// but unusable", which is a *ShardConfigError and must fail the run.
//
// Test it with errors.Is, never by comparing to nil:
//
//	shard, total, err := specs.ShardFromArgsOrEnv()
//	if errors.Is(err, specs.ErrShardNotConfigured) { /* run everything */ }
var ErrShardNotConfigured = errors.New("specs: sharding not configured")

// ShardConfigError reports sharding that was requested but cannot be used. Its message names the
// exact flag or environment variable at fault, the value it carried, why that value is unusable,
// what did not happen as a result, and the remedy — a CI log line that explains the red build
// rather than one that just says "invalid".
//
// A *ShardConfigError never means "run the whole suite". Callers must fail; degrading to the full
// suite is the silent failure this type exists to prevent.
type ShardConfigError struct {
	// Source names where the configuration came from: "-shard", "SHARD", "SHARD_INDEX" or
	// "SHARD_TOTAL". Empty when the values were passed as arguments rather than parsed.
	Source string
	// Value is the offending value as written, already quoted for display. Empty when the fault
	// is that nothing was written at all.
	Value string
	// Reason says why the value cannot be used, in terms of the field at fault.
	Reason string
	// Remedy states what to do instead, including how to opt out of sharding entirely.
	Remedy string
}

func (e *ShardConfigError) Error() string {
	var b strings.Builder
	b.WriteString("specs: invalid shard configuration")
	if e.Source != "" {
		b.WriteString(" from ")
		b.WriteString(e.Source)
	}
	if e.Value != "" {
		b.WriteString(" (")
		b.WriteString(e.Value)
		b.WriteString(")")
	}
	b.WriteString(": ")
	b.WriteString(e.Reason)
	b.WriteString("; no specs were run for this shard")
	if e.Remedy != "" {
		b.WriteString(" — ")
		b.WriteString(e.Remedy)
	}
	return b.String()
}

// Sources a shard configuration can come from. They double as the exact name printed in the
// diagnostic, which is why they are spelled as the user writes them.
const (
	shardSourceFlag     = "-shard"
	shardSourceEnv      = "SHARD"
	shardSourceEnvIndex = "SHARD_INDEX"
	shardSourceEnvTotal = "SHARD_TOTAL"
)

// shardRemedy returns the remedy for a fault in source. Every remedy also states how to opt out of
// sharding entirely, because "unset it" is the correct fix whenever the sharding was unintended.
func shardRemedy(source string) string {
	switch source {
	case shardSourceFlag:
		return "pass -shard index/total (e.g. -shard 2/10), or omit the flag to run the whole suite"
	case shardSourceEnv:
		return "set SHARD to index/total (e.g. SHARD=2/10), or unset it to run the whole suite"
	case shardSourceEnvIndex, shardSourceEnvTotal:
		return "set SHARD_INDEX to a 0-based index and SHARD_TOTAL to the number of shards, or unset both to run the whole suite"
	default:
		return "use a total >= 1 and a 0-based index below it"
	}
}

// validateShardPartition checks an already-parsed shard/total pair against the partition contract:
// total >= 1 and 0 <= shard < total. It returns the reason the pair is unusable, phrased in terms of
// the field at fault, or ok when the pair describes a real partition.
//
// This is the single definition of "valid shard configuration". ShardSpecs, ShardBCProgram, the
// parsers and RunShard all route through it, so they cannot drift apart into disagreeing about
// which values are usable.
func validateShardPartition(shard, total int) (reason string, ok bool) {
	switch {
	case total <= 0:
		return fmt.Sprintf("total must be >= 1, got %d", total), false
	case shard < 0:
		return fmt.Sprintf("index must be >= 0, got %d", shard), false
	case shard >= total:
		return fmt.Sprintf("index %d is out of range for total %d; indices are 0-based, so the valid range is 0..%d", shard, total, total-1), false
	default:
		return "", true
	}
}

// shardConfigMessage renders the diagnostic for a shard/total pair supplied as arguments rather than
// parsed from a flag or the environment, so there is no Source to name — the values themselves are
// the evidence.
func shardConfigMessage(shard, total int, reason string) string {
	return (&ShardConfigError{
		Value:  fmt.Sprintf("index %d, total %d", shard, total),
		Reason: reason,
		Remedy: shardRemedy(""),
	}).Error()
}

// panicShardConfig reports an unusable shard configuration from a call site that has no testing.TB
// to report through. It is split out and marked noinline so the sharding functions carry only the
// branch, not the panic setup — the same shape panicReused uses for a spent Expectation.
//
// A panic rather than a silent passthrough is the point: the runner recovers panics into a failed
// spec, so the misconfiguration surfaces as a red build instead of a green one that ran the whole
// suite on every worker.
//
//go:noinline
func panicShardConfig(shard, total int, reason string) {
	panic(shardConfigMessage(shard, total, reason))
}

// ShardSpecs returns the subset of specs belonging to the given shard (deterministic).
// Spec index i is included when i%total == shard. total must be >= 1 and 0 <= shard < total;
// anything else panics with an actionable diagnostic rather than returning the whole suite, because
// a shard that silently widened to every spec is a green build that proved nothing (issue #174).
// Allocations happen here (filtered slice); execution loop over the result has no extra allocations.
func ShardSpecs(specs []RunSpec, shard, total int) []RunSpec {
	if reason, ok := validateShardPartition(shard, total); !ok {
		panicShardConfig(shard, total, reason)
	}
	out := make([]RunSpec, 0, len(specs)/total+1)
	for i := range specs {
		if i%total == shard {
			out = append(out, specs[i])
		}
	}
	return out
}

// ShardBCProgram returns a BCProgram containing only specs whose index s satisfies s%total == shard.
// Deterministic; allocates new Code and SpecStarts; execution loop over the result is unchanged.
// total must be >= 1 and 0 <= shard < total; anything else panics, for the same reason ShardSpecs
// does. An empty result for a valid shard is not an error: with more shards than specs, some shards
// legitimately draw nothing.
func ShardBCProgram(prog BCProgram, shard, total int) BCProgram {
	if reason, ok := validateShardPartition(shard, total); !ok {
		panicShardConfig(shard, total, reason)
	}
	nSpecs := prog.NumSpecs()
	if nSpecs == 0 {
		return prog
	}
	code := prog.Code
	starts := prog.SpecStarts

	var newCode []instruction
	newStarts := make([]int, 0, nSpecs/total+2)
	newStarts = append(newStarts, 0)

	for s := 0; s < nSpecs; s++ {
		if s%total != shard {
			continue
		}
		start, end := starts[s], starts[s+1]
		for j := start; j < end; j++ {
			newCode = append(newCode, code[j])
		}
		newStarts = append(newStarts, len(newCode))
	}

	if len(newStarts) == 1 {
		return BCProgram{Code: nil, SpecStarts: nil}
	}
	return BCProgram{Code: newCode, SpecStarts: newStarts}
}

// ParseShardFlag parses -shard <shard>/<total> from args (e.g. -shard 2/10). Both -shard 2/10 and
// -shard=2/10 are accepted, with or without a second leading dash, because Go's flag package accepts
// all four spellings and a form this parser skipped would silently mean "no sharding".
//
// Returns ErrShardNotConfigured when no -shard flag is present, and a *ShardConfigError when one is
// present but its value is unusable — including when it is the last argument and carries no value.
// A malformed flag is never reported as an absent one.
func ParseShardFlag(args []string) (shard, total int, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if value, found := cutShardFlagValue(arg); found {
			return parseShardPair(value, shardSourceFlag)
		}
		if arg != "-shard" && arg != "--shard" {
			continue
		}
		if i == len(args)-1 {
			return 0, 0, &ShardConfigError{
				Source: shardSourceFlag,
				Reason: "the flag carries no value",
				Remedy: shardRemedy(shardSourceFlag),
			}
		}
		return parseShardPair(args[i+1], shardSourceFlag)
	}
	return 0, 0, ErrShardNotConfigured
}

// cutShardFlagValue returns the value of an -shard=VALUE / --shard=VALUE argument.
func cutShardFlagValue(arg string) (value string, found bool) {
	if v, ok := strings.CutPrefix(arg, "-shard="); ok {
		return v, true
	}
	return strings.CutPrefix(arg, "--shard=")
}

// ParseShardEnv reads shard index and total from environment (e.g. CI sets SHARD_INDEX and SHARD_TOTAL).
// Alternatively supports SHARD=2/10, which takes precedence when set.
//
// Returns ErrShardNotConfigured only when none of the three variables carries a value. Anything else
// that cannot be used is a *ShardConfigError naming the variable at fault — including the
// half-configured case where exactly one of SHARD_INDEX / SHARD_TOTAL is set, which is a CI typo
// rather than a request to run the whole suite.
func ParseShardEnv() (shard, total int, err error) {
	if s := strings.TrimSpace(os.Getenv(shardSourceEnv)); s != "" {
		return parseShardPair(s, shardSourceEnv)
	}
	idx := strings.TrimSpace(os.Getenv(shardSourceEnvIndex))
	tot := strings.TrimSpace(os.Getenv(shardSourceEnvTotal))

	switch {
	case idx == "" && tot == "":
		return 0, 0, ErrShardNotConfigured
	case tot == "":
		return 0, 0, &ShardConfigError{
			Source: shardSourceEnvTotal,
			Reason: "SHARD_INDEX is set but SHARD_TOTAL is empty, so the number of shards is unknown",
			Remedy: shardRemedy(shardSourceEnvTotal),
		}
	case idx == "":
		return 0, 0, &ShardConfigError{
			Source: shardSourceEnvIndex,
			Reason: "SHARD_TOTAL is set but SHARD_INDEX is empty, so this worker's shard is unknown",
			Remedy: shardRemedy(shardSourceEnvIndex),
		}
	}

	sh, errIdx := strconv.Atoi(idx)
	if errIdx != nil {
		return 0, 0, notAnInteger(shardSourceEnvIndex, "index", idx)
	}
	totN, errTot := strconv.Atoi(tot)
	if errTot != nil {
		return 0, 0, notAnInteger(shardSourceEnvTotal, "total", tot)
	}
	if reason, ok := validateShardPartition(sh, totN); !ok {
		source, value := envFaultAt(totN, idx, tot)
		return 0, 0, &ShardConfigError{
			Source: source,
			Value:  strconv.Quote(value),
			Reason: reason,
			Remedy: shardRemedy(source),
		}
	}
	return sh, totN, nil
}

// envFaultAt names which of SHARD_INDEX / SHARD_TOTAL to blame for an invalid pair, and returns its
// raw text. A total below 1 is the total's fault; every other rejection is about where the index
// sits in the range, so it names the index.
func envFaultAt(total int, idxText, totText string) (source, value string) {
	if total <= 0 {
		return shardSourceEnvTotal, totText
	}
	return shardSourceEnvIndex, idxText
}

// notAnInteger builds the diagnostic for a value that is not a number at all.
func notAnInteger(source, field, text string) *ShardConfigError {
	return &ShardConfigError{
		Source: source,
		Value:  strconv.Quote(text),
		Reason: field + " is not an integer",
		Remedy: shardRemedy(source),
	}
}

// ParseShardString parses "N/M" (e.g. "2/10") into (shard, total). An empty or blank string is
// ErrShardNotConfigured; anything else that is not a usable partition is a *ShardConfigError.
func ParseShardString(s string) (shard, total int, err error) {
	return parseShardPair(s, "")
}

// parseShardPair parses "index/total" from source. source is only used to build the diagnostic; an
// empty source means the value did not come from a named flag or variable.
func parseShardPair(s, source string) (shard, total int, err error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		if source == "" {
			return 0, 0, ErrShardNotConfigured
		}
		return 0, 0, &ShardConfigError{
			Source: source,
			Reason: "the value is empty",
			Remedy: shardRemedy(source),
		}
	}

	quoted := strconv.Quote(s)
	idxText, totText, found := strings.Cut(trimmed, "/")
	if !found {
		return 0, 0, &ShardConfigError{
			Source: source,
			Value:  quoted,
			Reason: `expected "index/total"`,
			Remedy: shardRemedy(source),
		}
	}
	idxText = strings.TrimSpace(idxText)
	totText = strings.TrimSpace(totText)

	sh, errIdx := strconv.Atoi(idxText)
	if errIdx != nil {
		return 0, 0, &ShardConfigError{
			Source: source,
			Value:  quoted,
			Reason: "index " + strconv.Quote(idxText) + " is not an integer",
			Remedy: shardRemedy(source),
		}
	}
	tot, errTot := strconv.Atoi(totText)
	if errTot != nil {
		return 0, 0, &ShardConfigError{
			Source: source,
			Value:  quoted,
			Reason: "total " + strconv.Quote(totText) + " is not an integer",
			Remedy: shardRemedy(source),
		}
	}
	if reason, ok := validateShardPartition(sh, tot); !ok {
		return 0, 0, &ShardConfigError{
			Source: source,
			Value:  quoted,
			Reason: reason,
			Remedy: shardRemedy(source),
		}
	}
	return sh, tot, nil
}

// ShardFromArgsOrEnv returns the shard requested via the -shard flag or the SHARD / SHARD_INDEX /
// SHARD_TOTAL environment, for use in TestMain to decide whether to call ShardSpecs, ShardBCProgram
// or RunShard.
//
// Precedence is strict and fail-closed. The -shard flag, once present, is authoritative: if its
// value is unusable this returns that *ShardConfigError and does not consult the environment.
// Falling back would silently run a different partition than the one CI asked for — the same class
// of silent degradation as running everything. Only an absent flag defers to the environment, where
// SHARD in turn takes precedence over SHARD_INDEX / SHARD_TOTAL on the same terms.
//
// Returns ErrShardNotConfigured when nothing requested sharding at all.
func ShardFromArgsOrEnv() (shard, total int, err error) {
	return shardFromArgsOrEnv(os.Args)
}

// shardFromArgsOrEnv is ShardFromArgsOrEnv against an explicit argument list, so the precedence
// contract can be tested without mutating os.Args.
func shardFromArgsOrEnv(args []string) (shard, total int, err error) {
	shard, total, err = ParseShardFlag(args)
	if !errors.Is(err, ErrShardNotConfigured) {
		return shard, total, err
	}
	return ParseShardEnv()
}

// FormatShardFlag returns a flag string for the given shard and total, e.g. "-shard 2/10".
func FormatShardFlag(shard, total int) string {
	return fmt.Sprintf("-shard %d/%d", shard, total)
}
