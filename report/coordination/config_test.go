package coordination

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeEnv records which of the two deliberately different access paths each variable was read
// through. Contract v1.2.6 §5 makes the choice normative in both directions, and neither rule may
// be "simplified" into the other, so the tests below assert the access pattern itself rather than
// only its observable effect.
type fakeEnv struct {
	vars     map[string]string
	lookedUp []string
	scanned  []string
}

func newFakeEnv(kv map[string]string) *fakeEnv {
	return &fakeEnv{vars: kv}
}

func (f *fakeEnv) Lookup(name string) (string, bool) {
	f.lookedUp = append(f.lookedUp, name)
	v, ok := f.vars[name]
	return v, ok
}

func (f *fakeEnv) Scan(name string) (string, bool) {
	f.scanned = append(f.scanned, name)
	v, ok := f.vars[name]
	return v, ok
}

func (f *fakeEnv) didLookUp(name string) bool { return contains(f.lookedUp, name) }
func (f *fakeEnv) didScan(name string) bool   { return contains(f.scanned, name) }

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func resolveWith(t *testing.T, env *fakeEnv) (ShardConfig, string, error) {
	t.Helper()
	var warn strings.Builder
	r := &resolver{env: env, warn: &warn, once: &sync.Once{}}
	cfg, err := r.resolve("example.com/m/pkg")
	return cfg, warn.String(), err
}

const (
	validToken   = "0a1b2c3d4e5f60718293a4b5c6d7e8f9"
	validTokenUp = "0A1B2C3D4E5F60718293A4B5C6D7E8F9"
)

func TestGateAbsentOrFalseDisablesReportingSilently(t *testing.T) {
	// The case that protects a developer shell with a leftover export: no shard, no diagnostic,
	// no effect on exit status whatsoever (contract v1.2.6 §5, row 1).
	for name, tc := range map[string]struct {
		gate string
		set  bool
	}{
		"absent":         {"", false},
		"empty string":   {"", true},
		"zero":           {"0", true},
		"false":          {"false", true},
		"FALSE":          {"FALSE", true},
		"mixed case off": {"False", true},
	} {
		t.Run(name, func(t *testing.T) {
			gate := tc.gate
			vars := map[string]string{EnvRunID: "run-1", EnvRunToken: validToken}
			if tc.set {
				vars[EnvGate] = gate
			}
			cfg, warned, err := resolveWith(t, newFakeEnv(vars))
			if err != nil {
				t.Fatalf("gate %q returned an error: %v", gate, err)
			}
			if cfg.Activated || cfg.Enabled() {
				t.Fatalf("gate %q activated reporting", gate)
			}
			if warned != "" {
				t.Fatalf("gate %q emitted a diagnostic for a valid leftover export: %q", gate, warned)
			}
		})
	}
}

func TestGateOnActivatesReporting(t *testing.T) {
	for _, gate := range []string{"1", "true", "TRUE", "True"} {
		t.Run(gate, func(t *testing.T) {
			cfg, warned, err := resolveWith(t, newFakeEnv(map[string]string{
				EnvGate:     gate,
				EnvRunID:    "run-1",
				EnvRunToken: validToken,
			}))
			if err != nil {
				t.Fatalf("gate %q: %v", gate, err)
			}
			if !cfg.Activated {
				t.Fatalf("gate %q did not activate reporting", gate)
			}
			if cfg.RunID != "run-1" || cfg.Token != validToken {
				t.Fatalf("resolved identity = %q/%q", cfg.RunID, cfg.Token)
			}
			if cfg.PackagePath != "example.com/m/pkg" {
				t.Fatalf("PackagePath = %q", cfg.PackagePath)
			}
			if warned != "" {
				t.Fatalf("gate-on path emitted a warn-only diagnostic: %q", warned)
			}
		})
	}
}

func TestUnparseableGateIsAConfigurationErrorAndIsNotGuessed(t *testing.T) {
	// Contract v1.2.6 §5: "an unparseable gate must not be guessed either way".
	for _, gate := range []string{"yes", "on", "2", "maybe", " 1"} {
		t.Run(gate, func(t *testing.T) {
			_, _, err := resolveWith(t, newFakeEnv(map[string]string{EnvGate: gate}))
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("gate %q returned %v, want a *ConfigError", gate, err)
			}
			if cfgErr.Reason != ReasonInvalidGate {
				t.Fatalf("Reason = %q, want %q", cfgErr.Reason, ReasonInvalidGate)
			}
			if !strings.Contains(cfgErr.Error(), EnvGate) {
				t.Fatalf("diagnostic %q does not name %s", cfgErr, EnvGate)
			}
		})
	}
}

func TestGateOffReadsIdentityVariablesOnlyByScanningEnviron(t *testing.T) {
	// The load-bearing half of contract v1.2.6 §5: on the disabled path EVERY variable this
	// contract introduces must come from an os.Environ() scan. os.Getenv/os.LookupEnv route
	// through testlog, which enrolls the variable in go test's cache key for every package in the
	// module — so a per-invocation GO_SPECS_RUN_ID sitting in a shell defeats caching module-wide
	// while producing no reporting in exchange, because the gate is off.
	//
	// GO_SPECS_REPORT_DIR is the one most likely to be missed, and CI routinely points it at a
	// run-scoped temp directory, so it is checked explicitly.
	env := newFakeEnv(map[string]string{
		EnvRunID:     "run-1",
		EnvRunToken:  validToken,
		EnvReportDir: "/tmp/scoped",
	})
	if _, _, err := resolveWith(t, env); err != nil {
		t.Fatalf("disabled path returned an error: %v", err)
	}
	for _, name := range []string{EnvRunID, EnvRunToken, EnvReportDir} {
		if env.didLookUp(name) {
			t.Fatalf("%s was read with Lookup on the disabled path; contract v1.2.6 §5 requires an os.Environ() scan", name)
		}
		if !env.didScan(name) {
			t.Fatalf("%s was never read on the disabled path; the warn-only check needs it", name)
		}
	}
}

func TestGateOnReadsIdentityVariablesThroughLookupSoTheyEnrollInTheCacheKey(t *testing.T) {
	// The other half, and the one no other test in this package pins. If this path were
	// "simplified" into an os.Environ() scan for symmetry, the disabled-path test still passes,
	// the code looks tidier, and a cached package silently stops republishing its shard — which is
	// indistinguishable from a crash (contract v1.2.6 §5, F4).
	env := newFakeEnv(map[string]string{
		EnvGate:      "1",
		EnvRunID:     "run-1",
		EnvRunToken:  validToken,
		EnvReportDir: "/tmp/scoped",
	})
	if _, _, err := resolveWith(t, env); err != nil {
		t.Fatalf("enabled path returned an error: %v", err)
	}
	for _, name := range []string{EnvRunID, EnvRunToken, EnvReportDir} {
		if !env.didLookUp(name) {
			t.Fatalf("%s was not read with Lookup on the enabled path; a new %s must invalidate the cached result", name, EnvRunID)
		}
	}
}

func TestGateOffWarnsOnceForAMalformedIdentityWithoutFailing(t *testing.T) {
	env := newFakeEnv(map[string]string{EnvRunID: "../escape", EnvRunToken: validToken})
	cfg, warned, err := resolveWith(t, env)
	if err != nil {
		t.Fatalf("warn-only path returned an error: %v", err)
	}
	if cfg.Enabled() {
		t.Fatal("warn-only path enabled reporting")
	}
	if !strings.Contains(warned, EnvRunID) {
		t.Fatalf("diagnostic %q does not name %s", warned, EnvRunID)
	}
	if !strings.Contains(warned, "go-specs:") {
		t.Fatalf("diagnostic %q is not prefixed distinctly, so it is not attributable or greppable", warned)
	}
	if n := strings.Count(strings.TrimSuffix(warned, "\n"), "\n"); n != 0 {
		t.Fatalf("diagnostic spans %d lines, want exactly one:\n%s", n+1, warned)
	}
}

func TestGateOffWarnsAtMostOncePerProcess(t *testing.T) {
	var warn strings.Builder
	r := &resolver{
		env:  newFakeEnv(map[string]string{EnvRunID: "../escape"}),
		warn: &warn,
		once: &sync.Once{},
	}
	for i := 0; i < 3; i++ {
		if _, err := r.resolve("example.com/m/pkg"); err != nil {
			t.Fatalf("resolve %d: %v", i, err)
		}
	}
	if got := strings.Count(warn.String(), "go-specs:"); got != 1 {
		t.Fatalf("emitted %d diagnostics across 3 resolves, want exactly 1", got)
	}
}

func TestGateOffWarningNamesBothVariablesAndNeverEchoesTheToken(t *testing.T) {
	secret := "deadbeef" // too short to be valid, and must not reach the log
	_, warned, err := resolveWith(t, newFakeEnv(map[string]string{
		EnvRunID:    "bad/id",
		EnvRunToken: secret,
	}))
	if err != nil {
		t.Fatalf("warn-only path returned an error: %v", err)
	}
	for _, want := range []string{EnvRunID, EnvRunToken} {
		if !strings.Contains(warned, want) {
			t.Fatalf("diagnostic %q does not name %s", warned, want)
		}
	}
	if strings.Contains(warned, secret) {
		t.Fatalf("diagnostic %q echoes the token value", warned)
	}
	if n := strings.Count(strings.TrimSuffix(warned, "\n"), "\n"); n != 0 {
		t.Fatalf("diagnostic spans %d lines, want exactly one", n+1)
	}
}

func TestGateOffIsSilentWhenLeftoverIdentityIsWellFormed(t *testing.T) {
	// "MUST NOT fire when the value is present and valid — a correctly-formed leftover export is
	// silent, because there is nothing to tell the user."
	_, warned, err := resolveWith(t, newFakeEnv(map[string]string{
		EnvRunID:    "run-1",
		EnvRunToken: validTokenUp,
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if warned != "" {
		t.Fatalf("emitted a diagnostic for a well-formed leftover export: %q", warned)
	}
}

func TestGateOnRejectsEveryPartialOrInvalidIdentityCombination(t *testing.T) {
	for name, tc := range map[string]struct {
		vars   map[string]string
		reason ConfigErrorReason
		names  string
	}{
		"run id absent": {
			vars:   map[string]string{EnvGate: "1", EnvRunToken: validToken},
			reason: ReasonMissingRunID, names: EnvRunID,
		},
		"token absent": {
			vars:   map[string]string{EnvGate: "1", EnvRunID: "run-1"},
			reason: ReasonMissingRunToken, names: EnvRunToken,
		},
		"both absent": {
			vars:   map[string]string{EnvGate: "1"},
			reason: ReasonMissingRunID, names: EnvRunID,
		},
		"run id invalid": {
			vars:   map[string]string{EnvGate: "1", EnvRunID: "../escape", EnvRunToken: validToken},
			reason: ReasonInvalidRunID, names: EnvRunID,
		},
		"token invalid": {
			vars:   map[string]string{EnvGate: "1", EnvRunID: "run-1", EnvRunToken: "zz"},
			reason: ReasonInvalidRunToken, names: EnvRunToken,
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, warned, err := resolveWith(t, newFakeEnv(tc.vars))
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("got %v, want a *ConfigError", err)
			}
			if cfgErr.Reason != tc.reason {
				t.Fatalf("Reason = %q, want %q", cfgErr.Reason, tc.reason)
			}
			if !strings.Contains(cfgErr.Error(), tc.names) {
				t.Fatalf("diagnostic %q does not name %s", cfgErr, tc.names)
			}
			// Invalid configuration must never be downgraded to disabled reporting.
			if cfg.Enabled() {
				t.Fatal("a configuration error returned an enabled config")
			}
			if warned != "" {
				t.Fatalf("the gate-on path emitted a warn-only diagnostic instead of failing: %q", warned)
			}
		})
	}
}

func TestMissingTokenDiagnosticPointsAtThePreflightStep(t *testing.T) {
	// Contract v1.2.6 §5: the message MUST state that the token is generated by the same preflight
	// step that created the run marker — otherwise the reader has no idea where it comes from.
	_, _, err := resolveWith(t, newFakeEnv(map[string]string{EnvGate: "1", EnvRunID: "run-1"}))
	if err == nil {
		t.Fatal("missing token was accepted")
	}
	if !strings.Contains(err.Error(), "preflight") {
		t.Fatalf("diagnostic %q does not say where %s comes from", err, EnvRunToken)
	}
}

func TestBaseDirDefaultsAndIsHonouredWhenSet(t *testing.T) {
	cfg, _, err := resolveWith(t, newFakeEnv(map[string]string{
		EnvGate: "1", EnvRunID: "run-1", EnvRunToken: validToken,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseDir != DefaultBaseDir {
		t.Fatalf("BaseDir = %q, want the default %q", cfg.BaseDir, DefaultBaseDir)
	}

	cfg, _, err = resolveWith(t, newFakeEnv(map[string]string{
		EnvGate: "1", EnvRunID: "run-1", EnvRunToken: validToken, EnvReportDir: "/srv/runs",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseDir != "/srv/runs" {
		t.Fatalf("BaseDir = %q, want the configured value", cfg.BaseDir)
	}
}
