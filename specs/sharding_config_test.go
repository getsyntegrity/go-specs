package specs

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// shardCapturingTB is a testing.TB that records Fatalf instead of aborting the goroutine.
//
// testing.TB has an unexported private() method precisely so external types cannot satisfy it, so
// the interface is embedded rather than implemented: the embedded value supplies private() and the
// methods RunShard actually calls are overridden here. The embedded TB stays nil on purpose — any
// call RunShard makes that is not overridden below panics with a nil dereference instead of
// silently reaching a real *testing.T, which keeps this fake honest about what it covers.
type shardCapturingTB struct {
	testing.TB
	failed  bool
	message string
}

func (c *shardCapturingTB) Helper() {}

func (c *shardCapturingTB) Fatalf(format string, args ...any) {
	c.failed = true
	c.message = fmt.Sprintf(format, args...)
}

// asShardConfigError extracts a *ShardConfigError from err, failing the test when err is not one.
func asShardConfigError(t *testing.T, err error) *ShardConfigError {
	t.Helper()
	var cfgErr *ShardConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *ShardConfigError, got %T (%v)", err, err)
	}
	return cfgErr
}

// TestShardingDistinguishesUnconfiguredFromInvalid is the core of #174: "no sharding requested" and
// "sharding requested but unusable" are different states and must not collapse into each other. The
// second one used to be reported as the first, which is what let SHARD_TOTAL=0 run 100% of the
// suite on every CI worker and still report green.
func TestShardingDistinguishesUnconfiguredFromInvalid(t *testing.T) {
	t.Setenv("SHARD", "")
	t.Setenv("SHARD_INDEX", "")
	t.Setenv("SHARD_TOTAL", "")

	_, _, err := ParseShardEnv()
	if !errors.Is(err, ErrShardNotConfigured) {
		t.Fatalf("no shard env set: got %v, want ErrShardNotConfigured", err)
	}

	t.Setenv("SHARD_INDEX", "0")
	t.Setenv("SHARD_TOTAL", "0")

	_, _, err = ParseShardEnv()
	if errors.Is(err, ErrShardNotConfigured) {
		t.Fatal("SHARD_TOTAL=0 reported as 'not configured'; a CI typo must not mean 'run everything'")
	}
	cfgErr := asShardConfigError(t, err)
	if !strings.Contains(cfgErr.Reason, "total") {
		t.Errorf("reason %q does not name the offending field", cfgErr.Reason)
	}
}

func TestParseShardStringRejectsMalformedValues(t *testing.T) {
	for _, tc := range []struct {
		name         string
		in           string
		wantShard    int
		wantTotal    int
		unconfigured bool
	}{
		{name: "valid", in: "2/10", wantShard: 2, wantTotal: 10},
		{name: "single shard", in: "0/1", wantTotal: 1},
		{name: "surrounding spaces", in: " 3 / 5 ", wantShard: 3, wantTotal: 5},
		{name: "empty is unconfigured", in: "", unconfigured: true},
		{name: "missing separator", in: "2"},
		{name: "too many parts", in: "2/10/1"},
		{name: "negative index", in: "-1/5"},
		{name: "index equals total", in: "2/2"},
		{name: "index above total", in: "10/10"},
		{name: "zero total", in: "0/0"},
		{name: "negative total", in: "0/-3"},
		{name: "non numeric index", in: "abc/10"},
		{name: "non numeric total", in: "2/abc"},
		{name: "empty index", in: "/10"},
		{name: "empty total", in: "2/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shard, total, err := ParseShardString(tc.in)

			switch {
			case tc.unconfigured:
				if !errors.Is(err, ErrShardNotConfigured) {
					t.Fatalf("ParseShardString(%q): got %v, want ErrShardNotConfigured", tc.in, err)
				}
			case tc.wantTotal != 0:
				if err != nil {
					t.Fatalf("ParseShardString(%q): unexpected error %v", tc.in, err)
				}
				if shard != tc.wantShard || total != tc.wantTotal {
					t.Errorf("ParseShardString(%q): got (%d,%d), want (%d,%d)", tc.in, shard, total, tc.wantShard, tc.wantTotal)
				}
			default:
				if errors.Is(err, ErrShardNotConfigured) {
					t.Fatalf("ParseShardString(%q): malformed value reported as 'not configured'", tc.in)
				}
				cfgErr := asShardConfigError(t, err)
				if cfgErr.Reason == "" {
					t.Error("ShardConfigError carries no reason; the diagnostic is not actionable")
				}
			}
		})
	}
}

// TestParseShardEnvRejectsPartialConfiguration pins the half-configured case. Setting only one of
// SHARD_INDEX / SHARD_TOTAL is a CI typo, not an intent to run the whole suite, so it must be an
// error rather than ErrShardNotConfigured.
func TestParseShardEnvRejectsPartialConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		index string
		total string
	}{
		{name: "only index", index: "1", total: ""},
		{name: "only total", index: "", total: "4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SHARD", "")
			t.Setenv("SHARD_INDEX", tc.index)
			t.Setenv("SHARD_TOTAL", tc.total)

			_, _, err := ParseShardEnv()
			if errors.Is(err, ErrShardNotConfigured) {
				t.Fatal("half-configured shard env reported as 'not configured'")
			}
			cfgErr := asShardConfigError(t, err)
			if !strings.Contains(cfgErr.Source, "SHARD_") {
				t.Errorf("source %q does not name the environment variables involved", cfgErr.Source)
			}
		})
	}
}

func TestParseShardEnvAcceptsValidConfiguration(t *testing.T) {
	t.Setenv("SHARD", "3/7")
	shard, total, err := ParseShardEnv()
	if err != nil || shard != 3 || total != 7 {
		t.Fatalf("SHARD=3/7: got (%d,%d,%v), want (3,7,nil)", shard, total, err)
	}

	t.Setenv("SHARD", "")
	t.Setenv("SHARD_INDEX", "1")
	t.Setenv("SHARD_TOTAL", "4")
	shard, total, err = ParseShardEnv()
	if err != nil || shard != 1 || total != 4 {
		t.Fatalf("SHARD_INDEX=1 SHARD_TOTAL=4: got (%d,%d,%v), want (1,4,nil)", shard, total, err)
	}
}

// TestParseShardEnvRejectsMalformedSHARD pins that SHARD, once set, is authoritative: a malformed
// value is an error and never falls through to SHARD_INDEX/SHARD_TOTAL.
func TestParseShardEnvRejectsMalformedSHARD(t *testing.T) {
	t.Setenv("SHARD", "0/0")
	t.Setenv("SHARD_INDEX", "1")
	t.Setenv("SHARD_TOTAL", "4")

	shard, total, err := ParseShardEnv()
	if err == nil {
		t.Fatalf("SHARD=0/0: got (%d,%d,nil); malformed SHARD silently fell through to SHARD_INDEX/SHARD_TOTAL", shard, total)
	}
	cfgErr := asShardConfigError(t, err)
	if cfgErr.Source != "SHARD" {
		t.Errorf("source = %q, want %q", cfgErr.Source, "SHARD")
	}
}

func TestParseShardFlagRejectsMalformedValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "zero total", args: []string{"prog", "-shard", "0/0"}},
		{name: "index out of range", args: []string{"prog", "-shard", "5/5"}},
		{name: "not a pair", args: []string{"prog", "-shard", "abc"}},
		{name: "missing value", args: []string{"prog", "-shard"}},
		{name: "equals form, zero total", args: []string{"prog", "-shard=0/0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseShardFlag(tc.args)
			if errors.Is(err, ErrShardNotConfigured) {
				t.Fatalf("%v: malformed -shard reported as 'not configured'", tc.args)
			}
			cfgErr := asShardConfigError(t, err)
			if !strings.Contains(cfgErr.Source, "-shard") {
				t.Errorf("source %q does not name the -shard flag", cfgErr.Source)
			}
		})
	}
}

func TestParseShardFlagAcceptsValidForms(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "separate value", args: []string{"prog", "-shard", "2/10", "other"}},
		{name: "equals form", args: []string{"prog", "-shard=2/10"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shard, total, err := ParseShardFlag(tc.args)
			if err != nil || shard != 2 || total != 10 {
				t.Fatalf("got (%d,%d,%v), want (2,10,nil)", shard, total, err)
			}
		})
	}
}

func TestParseShardFlagUnconfiguredWhenFlagAbsent(t *testing.T) {
	_, _, err := ParseShardFlag([]string{"prog", "run"})
	if !errors.Is(err, ErrShardNotConfigured) {
		t.Fatalf("got %v, want ErrShardNotConfigured", err)
	}
}

// TestShardFromArgsOrEnvDoesNotFallBackFromInvalidFlag pins the args-vs-env precedence contract: the
// -shard flag, once present, is authoritative. Falling back to a valid environment after a malformed
// flag would silently run a different partition than the one CI asked for.
func TestShardFromArgsOrEnvDoesNotFallBackFromInvalidFlag(t *testing.T) {
	t.Setenv("SHARD", "1/2")

	shard, total, err := shardFromArgsOrEnv([]string{"prog", "-shard", "0/0"})
	if err == nil {
		t.Fatalf("got (%d,%d,nil); a malformed -shard flag silently fell back to SHARD=1/2", shard, total)
	}
	cfgErr := asShardConfigError(t, err)
	if !strings.Contains(cfgErr.Source, "-shard") {
		t.Errorf("source %q does not name the -shard flag that actually failed", cfgErr.Source)
	}
}

func TestShardFromArgsOrEnvPrefersFlagThenEnv(t *testing.T) {
	t.Setenv("SHARD", "1/2")

	shard, total, err := shardFromArgsOrEnv([]string{"prog", "-shard", "3/4"})
	if err != nil || shard != 3 || total != 4 {
		t.Fatalf("flag present: got (%d,%d,%v), want (3,4,nil)", shard, total, err)
	}

	shard, total, err = shardFromArgsOrEnv([]string{"prog"})
	if err != nil || shard != 1 || total != 2 {
		t.Fatalf("flag absent: got (%d,%d,%v), want (1,2,nil)", shard, total, err)
	}
}

func TestShardFromArgsOrEnvUnconfigured(t *testing.T) {
	t.Setenv("SHARD", "")
	t.Setenv("SHARD_INDEX", "")
	t.Setenv("SHARD_TOTAL", "")

	_, _, err := shardFromArgsOrEnv([]string{"prog"})
	if !errors.Is(err, ErrShardNotConfigured) {
		t.Fatalf("got %v, want ErrShardNotConfigured", err)
	}
}

// invalidShardConfigs are the boundary values that used to be silently treated as "no sharding".
var invalidShardConfigs = []struct {
	name  string
	shard int
	total int
}{
	{name: "zero total", shard: 0, total: 0},
	{name: "negative total", shard: 0, total: -1},
	{name: "negative index", shard: -1, total: 5},
	{name: "index equals total", shard: 5, total: 5},
	{name: "index above total", shard: 6, total: 5},
}

// TestShardSpecsPanicsOnInvalidConfiguration pins that ShardSpecs no longer returns the whole suite
// when the configuration is unusable. ShardSpecs has no testing.TB to report through, so it panics
// with an actionable message — the same fail-closed treatment #179 gave a reused Expectation.
func TestShardSpecsPanicsOnInvalidConfiguration(t *testing.T) {
	specs := make([]RunSpec, 10)
	for i := range specs {
		specs[i] = RunSpec{Name: "spec", Fn: func(*Context) {}}
	}

	for _, tc := range invalidShardConfigs {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("ShardSpecs(specs, %d, %d) did not panic; invalid configuration must never mean 'run everything'", tc.shard, tc.total)
				}
				if !strings.Contains(fmt.Sprint(r), "invalid shard configuration") {
					t.Errorf("panic value %v is not an actionable shard diagnostic", r)
				}
			}()
			ShardSpecs(specs, tc.shard, tc.total)
		})
	}
}

func TestShardBCProgramPanicsOnInvalidConfiguration(t *testing.T) {
	b := NewBCBuilder(32)
	b.AddSpec(func(*Context) {})
	b.AddSpec(func(*Context) {})
	prog := b.BuildBC()

	for _, tc := range invalidShardConfigs {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("ShardBCProgram(prog, %d, %d) did not panic", tc.shard, tc.total)
				}
				if !strings.Contains(fmt.Sprint(r), "invalid shard configuration") {
					t.Errorf("panic value %v is not an actionable shard diagnostic", r)
				}
			}()
			ShardBCProgram(prog, tc.shard, tc.total)
		})
	}
}

// TestShardSpecsPartitionContractPreserved pins that the valid behaviour is untouched: every shard
// is disjoint and their union is the whole suite, for several totals including total == len(specs)
// and total > len(specs).
func TestShardSpecsPartitionContractPreserved(t *testing.T) {
	const n = 10
	specs := make([]RunSpec, n)
	for i := range specs {
		specs[i] = RunSpec{Name: fmt.Sprintf("spec-%d", i), Fn: func(*Context) {}}
	}

	for _, total := range []int{1, 3, 10, 13} {
		t.Run(fmt.Sprintf("total-%d", total), func(t *testing.T) {
			seen := make(map[string]int, n)
			for shard := 0; shard < total; shard++ {
				for _, s := range ShardSpecs(specs, shard, total) {
					seen[s.Name]++
				}
			}
			if len(seen) != n {
				t.Fatalf("union of shards covered %d specs, want %d", len(seen), n)
			}
			for name, count := range seen {
				if count != 1 {
					t.Errorf("spec %s ran in %d shards, want exactly 1", name, count)
				}
			}
		})
	}
}

// TestRunShardFailsVisiblyOnInvalidConfiguration pins the CI-facing half of #174: RunShard holds a
// testing.TB, so it reports an unusable configuration as a test failure instead of quietly running
// every group on every worker.
func TestRunShardFailsVisiblyOnInvalidConfiguration(t *testing.T) {
	noop := func(*Context) {}
	prog := &Program{Groups: []group{
		{specs: []step{noop}, names: []string{"a"}},
		{specs: []step{noop}, names: []string{"b"}},
	}}

	for _, tc := range invalidShardConfigs {
		t.Run(tc.name, func(t *testing.T) {
			tb := &shardCapturingTB{}
			RunShard(prog, tb, tc.shard, tc.total)
			if !tb.failed {
				t.Fatalf("RunShard(prog, tb, %d, %d) reported no failure", tc.shard, tc.total)
			}
			if !strings.Contains(tb.message, "invalid shard configuration") {
				t.Errorf("failure message %q is not an actionable shard diagnostic", tb.message)
			}
		})
	}
}

func TestRunShardWithReporterFailsVisiblyOnInvalidConfiguration(t *testing.T) {
	noop := func(*Context) {}
	prog := &Program{Groups: []group{{specs: []step{noop}, names: []string{"a"}}}}

	for _, tc := range invalidShardConfigs {
		t.Run(tc.name, func(t *testing.T) {
			tb := &shardCapturingTB{}
			rep := &recordingReporter{}
			RunShardWithReporter(prog, tb, tc.shard, tc.total, "Shard", rep)
			if !tb.failed {
				t.Fatalf("RunShardWithReporter(prog, tb, %d, %d) reported no failure", tc.shard, tc.total)
			}
			if len(rep.specStarted) != 0 {
				t.Errorf("an unusable configuration still ran %d specs", len(rep.specStarted))
			}
		})
	}
}

// TestRunShardEmptyValidShardIsNotAFailure pins the boundary the issue deliberately leaves alone: a
// shard that legitimately draws no groups (more shards than groups) is valid configuration and an
// empty partition, not a configuration error.
func TestRunShardEmptyValidShardIsNotAFailure(t *testing.T) {
	noop := func(*Context) {}
	prog := &Program{Groups: []group{{specs: []step{noop}, names: []string{"a"}}}}

	tb := &shardCapturingTB{}
	RunShard(prog, tb, 2, 3)
	if tb.failed {
		t.Fatalf("an empty but valid shard was reported as a failure: %s", tb.message)
	}
}

// TestShardConfigErrorMessageIsActionable pins that the diagnostic names where the value came from,
// what it was, and what to do about it — the difference between a CI log that explains the red build
// and one that just says "invalid".
func TestShardConfigErrorMessageIsActionable(t *testing.T) {
	t.Setenv("SHARD", "")
	t.Setenv("SHARD_INDEX", "0")
	t.Setenv("SHARD_TOTAL", "0")

	_, _, err := ParseShardEnv()
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, want := range []string{"SHARD_TOTAL", "0", "total must be", "unset"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %q", msg, want)
		}
	}
}
