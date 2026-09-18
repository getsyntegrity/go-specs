package coordination

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// ShardConfig is resolved once per package process, typically inside TestMain
// (contract v1.2.6 §7).
type ShardConfig struct {
	// Activated reports whether EnvGate asked for shard emission. When false every field below
	// is ignored and nothing is ever written.
	Activated bool
	// RunID identifies the invocation. Populated only when Activated.
	RunID RunID
	// Token is this process's claim to the run. Populated only when Activated. It is never
	// written to disk in this form — only its digest is.
	Token RunToken
	// BaseDir is the base directory for run subdirectories, defaulted from DefaultBaseDir.
	BaseDir string
	// PackagePath is this package's own import path, supplied by the caller.
	PackagePath string
}

// Enabled reports whether this process should publish a shard.
func (c ShardConfig) Enabled() bool { return c.Activated }

// environment abstracts the two deliberately different ways this package reads variables. The
// distinction is normative, not stylistic (contract v1.2.6 §5), and an interface makes it
// testable: a fake can assert which path a variable was read through, which is the only way to
// notice one rule being "simplified" into the other.
type environment interface {
	// Lookup reads through os.LookupEnv, which routes via testlog and therefore enrolls the
	// variable in `go test`'s cache key.
	Lookup(name string) (string, bool)
	// Scan reads by walking os.Environ(), which records nothing in the testlog.
	Scan(name string) (string, bool)
}

type osEnvironment struct{}

func (osEnvironment) Lookup(name string) (string, bool) { return os.LookupEnv(name) }

// Scan walks os.Environ() instead of calling os.LookupEnv, and that is a deliberate use of an
// implementation detail rather than a stylistic choice — DO NOT "simplify" it to os.Getenv.
//
// `cmd/go` derives test-cache validity from the testlog, which records every getenv event a
// binary emits. os.Getenv and os.LookupEnv both call testlog.Getenv; os.Environ delegates
// straight to syscall.Environ and records nothing. So reading the coordination variables with
// Getenv on the DISABLED path enrolls them in the inputs ID of every package in the module, and a
// stale or per-invocation GO_SPECS_RUN_ID sitting in a developer's environment then invalidates
// the cache for every package on every run — while producing no reporting at all in exchange,
// because the gate is off. That regression is silent, cumulative, and typically diagnosed months
// later as "our CI got slow" (contract v1.2.6 §5).
//
// It is not a documented API guarantee, which is exactly why this comment exists: without it the
// first person tidying this code reintroduces the problem with a change that looks like a pure
// simplification.
func (osEnvironment) Scan(name string) (string, bool) {
	prefix := name + "="
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):], true
		}
	}
	return "", false
}

type resolver struct {
	env  environment
	warn io.Writer
	once *sync.Once
}

var defaultResolver = &resolver{
	env:  osEnvironment{},
	warn: os.Stderr,
	once: &sync.Once{},
}

// ShardConfigFromEnv resolves this package process's coordination configuration.
//
// It reads EnvGate first. When the gate is absent or false it returns a disabled config with no
// error — a stale export in a developer shell must never hard-fail a test run. In that state it
// may still emit the single warn-only stderr diagnostic for a present-but-malformed identity
// value, which never affects the returned error or the process's exit status.
//
// When the gate is on, the invocation has explicitly asked for coordination, so a missing or
// invalid value is a configuration error that fails loudly. Invalid configuration is never
// downgraded to disabled reporting (contract v1.2.6 §5, §8).
func ShardConfigFromEnv(packagePath string) (ShardConfig, error) {
	return defaultResolver.resolve(packagePath)
}

func (r *resolver) resolve(packagePath string) (ShardConfig, error) {
	// The gate itself may be read either way: it is stable within an environment, so enrolling it
	// in the cache key costs nothing. Only the per-invocation identity variables must avoid the
	// testlog (contract v1.2.6 §5).
	gate, _ := r.env.Lookup(EnvGate)
	on, err := parseGate(gate)
	if err != nil {
		return ShardConfig{}, err
	}
	if !on {
		return r.resolveDisabled(packagePath)
	}
	return r.resolveEnabled(packagePath)
}

func parseGate(raw string) (bool, error) {
	switch strings.ToLower(raw) {
	case "", "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	default:
		return false, &ConfigError{
			Source: EnvGate,
			Value:  fmt.Sprintf("%q", raw),
			Reason: ReasonInvalidGate,
			Detail: `the activation gate must be "1"/"true" or "0"/"false"; an unparseable gate is not guessed either way, because guessing "on" can fail a test run nobody asked to observe and guessing "off" silently discards a report CI is waiting for`,
			Remedy: remedyFor(EnvGate),
		}
	}
}

// resolveDisabled implements the gate-off path: nothing is written, nothing can fail, and a
// malformed identity value produces at most one stderr line naming the variables at fault.
//
// Every variable here is read with Scan. See osEnvironment.Scan for why that is normative.
func (r *resolver) resolveDisabled(packagePath string) (ShardConfig, error) {
	rawID, _ := r.env.Scan(EnvRunID)
	rawToken, _ := r.env.Scan(EnvRunToken)
	rawDir, _ := r.env.Scan(EnvReportDir)

	var faults []string
	if rawID != "" {
		if _, err := ValidateRunID(rawID); err != nil {
			faults = append(faults, fmt.Sprintf("%s=%q does not match %s", EnvRunID, rawID, runIDPattern))
		}
	}
	if rawToken != "" {
		if _, err := ValidateRunToken(rawToken); err != nil {
			// The value is never echoed, not even when malformed (contract v1.2.6 §5).
			faults = append(faults, fmt.Sprintf("%s does not match %s", EnvRunToken, runTokenPattern))
		}
	}
	if len(faults) > 0 {
		r.warnOnce(faults)
	}

	return ShardConfig{
		Activated:   false,
		BaseDir:     baseDirOrDefault(rawDir),
		PackagePath: packagePath,
	}, nil
}

// warnOnce emits the warn-only diagnostic at most once per process, on one line, prefixed
// distinctly so it is attributable and greppable. It is a diagnostic, not a contract signal: no
// tooling may key behaviour off it, and it never affects exit status, the filesystem, or
// coordination (contract v1.2.6 §5).
func (r *resolver) warnOnce(faults []string) {
	r.once.Do(func() {
		// The write error is deliberately discarded. This path MUST NOT affect the process's
		// exit status, and a diagnostic that failed to reach a closed stderr is not a reason to
		// fail a test run the gate never asked to observe.
		_, _ = fmt.Fprintf(r.warn, "go-specs: %s; ignored because %s is not enabled\n",
			strings.Join(faults, "; "), EnvGate)
	})
}

// resolveEnabled implements the gate-on path. Every read goes through Lookup precisely so that
// `go test` records these variables in its cache key and a new GO_SPECS_RUN_ID invalidates a
// stale cached result — the inverse of the disabled path, and equally normative
// (contract v1.2.6 §5).
func (r *resolver) resolveEnabled(packagePath string) (ShardConfig, error) {
	rawID, _ := r.env.Lookup(EnvRunID)
	rawToken, _ := r.env.Lookup(EnvRunToken)
	rawDir, _ := r.env.Lookup(EnvReportDir)

	if rawID == "" {
		return ShardConfig{}, &ConfigError{
			Source: EnvRunID,
			Reason: ReasonMissingRunID,
			Detail: EnvGate + " is on, so coordination was explicitly requested, but no run identity was supplied; an explicit request that carries no run identity is a misconfiguration, never silence",
			Remedy: remedyFor(EnvRunID),
		}
	}
	runID, err := ValidateRunID(rawID)
	if err != nil {
		return ShardConfig{}, err
	}

	if rawToken == "" {
		return ShardConfig{}, &ConfigError{
			Source: EnvRunToken,
			Reason: ReasonMissingRunToken,
			Detail: EnvGate + " is on but no ownership token was supplied; " + EnvRunToken +
				" is generated by the same preflight step that created the run marker, so a run started without that step cannot prove it owns its run directory",
			Remedy: remedyFor(EnvRunToken),
		}
	}
	token, err := ValidateRunToken(rawToken)
	if err != nil {
		return ShardConfig{}, err
	}

	return ShardConfig{
		Activated:   true,
		RunID:       runID,
		Token:       token,
		BaseDir:     baseDirOrDefault(rawDir),
		PackagePath: packagePath,
	}, nil
}

func baseDirOrDefault(raw string) string {
	if raw == "" {
		return DefaultBaseDir
	}
	return raw
}
