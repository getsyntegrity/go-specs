// compiled_suite_shard_test.go proves CompiledSuite.RunShard (issue #251) on the flat path — a
// suite with no BeforeAll/AfterAll group and no ItParallel batch, where every spec is its own
// sharding unit and its own plan index (buildShardSelection). See group_hooks_shard_test.go for the
// group-path equivalent (T2: hook-group and ItParallel batch integrity).
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
)

// shardFakeTB is shardCapturingTB (sharding_config_test.go) plus a Name() override. It is needed
// only where a test attaches a Reporter to a suite built through BuildSuite's bytecode-compiler
// path: that path never sets CompiledSuite.Name from its own name argument (see failFastFakeTB's
// doc comment in compiled_suite_failfast_test.go), so CompiledSuite.run falls back to
// backend.Name() whenever s.Reporter != nil — a call shardCapturingTB's embedded nil testing.TB
// cannot answer.
type shardFakeTB struct {
	testing.TB
	failed  bool
	message string
}

func (c *shardFakeTB) Helper()      {}
func (c *shardFakeTB) Name() string { return "shardFakeTB" }
func (c *shardFakeTB) Fatalf(format string, args ...any) {
	c.failed = true
	c.message = fmt.Sprintf(format, args...)
}

// flatShardBody registers n flat specs ("spec-0".."spec-(n-1)") on s, each recording its own name
// into *ran (guarded by mu). Shared by both CompiledSuite build paths (buildFlatShardSuiteCompilerPath
// / buildFlatShardSuiteRegistryPath) so a stability test can prove the same declared tree yields the
// same shard assignment on either one (RunShard's deterministic-rule decision).
func flatShardBody(s *Spec, n int, ran *[]string, mu *sync.Mutex) {
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("spec-%d", i)
		s.It(name, func(*Context) {
			mu.Lock()
			*ran = append(*ran, name)
			mu.Unlock()
		})
	}
}

// buildFlatShardSuiteCompilerPath builds a flat n-spec suite through the top-level bytecode
// compiler path (no active Analyze registry) — the path BuildSuite takes when called outside
// Analyze(fn).
func buildFlatShardSuiteCompilerPath(n int, ran *[]string, mu *sync.Mutex) *CompiledSuite {
	return BuildSuite(nil, "suite", func(s *Spec) { flatShardBody(s, n, ran, mu) })
}

// buildFlatShardSuiteRegistryPath builds the same flat n-spec suite through the Analyze/registry
// path (arena-backed), so RunShard's assignment can be proved identical on both compile paths.
func buildFlatShardSuiteRegistryPath(n int, ran *[]string, mu *sync.Mutex) *CompiledSuite {
	var suite *CompiledSuite
	Analyze(func() {
		suite = BuildSuite(nil, "suite", func(s *Spec) { flatShardBody(s, n, ran, mu) })
	})
	return suite
}

// TestRunShardFlatPathCoversEverySpecExactlyOnceNoDuplicates pins the core partition contract
// (mirroring TestShardSpecsPartitionContractPreserved for the Builder engine, but through
// CompiledSuite.RunShard): the union of every shard's specs is the whole suite, and no spec runs on
// more than one shard, for several shard counts including 1 and more shards than specs — on both
// compile paths.
func TestRunShardFlatPathCoversEverySpecExactlyOnceNoDuplicates(t *testing.T) {
	const n = 10
	builders := []struct {
		name  string
		build func(n int, ran *[]string, mu *sync.Mutex) *CompiledSuite
	}{
		{"compiler path", buildFlatShardSuiteCompilerPath},
		{"registry path", buildFlatShardSuiteRegistryPath},
	}
	for _, b := range builders {
		t.Run(b.name, func(t *testing.T) {
			for _, shardCount := range []int{1, 3, n, n + 2} {
				t.Run(fmt.Sprintf("shardCount-%d", shardCount), func(t *testing.T) {
					seen := make(map[string]int, n)
					for shard := 0; shard < shardCount; shard++ {
						var ran []string
						var mu sync.Mutex
						suite := b.build(n, &ran, &mu)
						suite.RunShard(&shardCapturingTB{}, shard, shardCount)
						for _, name := range ran {
							seen[name]++
						}
					}
					if len(seen) != n {
						t.Fatalf("union of shards covered %d specs, want %d: %v", len(seen), n, seen)
					}
					for name, count := range seen {
						if count != 1 {
							t.Errorf("spec %s ran in %d shards, want exactly 1", name, count)
						}
					}
				})
			}
		})
	}
}

// TestRunShardFlatPathAssignmentIsStableAcrossBuilds pins RunShard's determinism decision: the same
// declared tree always yields the same units and the same shard assignment, both across independent
// builds of the same tree and across the two compile paths (bytecode compiler vs. Analyze/registry).
func TestRunShardFlatPathAssignmentIsStableAcrossBuilds(t *testing.T) {
	const n = 9
	const shardCount = 4
	assignment := func(build func(n int, ran *[]string, mu *sync.Mutex) *CompiledSuite) map[int][]string {
		result := make(map[int][]string, shardCount)
		for shard := 0; shard < shardCount; shard++ {
			var ran []string
			var mu sync.Mutex
			suite := build(n, &ran, &mu)
			suite.RunShard(&shardCapturingTB{}, shard, shardCount)
			sort.Strings(ran)
			result[shard] = ran
		}
		return result
	}

	first := assignment(buildFlatShardSuiteCompilerPath)
	second := assignment(buildFlatShardSuiteCompilerPath)
	for shard := 0; shard < shardCount; shard++ {
		if !slices.Equal(first[shard], second[shard]) {
			t.Fatalf("compiler path: shard %d assignment changed across builds of the same tree: %v vs %v",
				shard, first[shard], second[shard])
		}
	}

	registryAssignment := assignment(buildFlatShardSuiteRegistryPath)
	for shard := 0; shard < shardCount; shard++ {
		if !slices.Equal(first[shard], registryAssignment[shard]) {
			t.Fatalf("registry path assignment differs from the compiler path for shard %d: %v vs %v",
				shard, first[shard], registryAssignment[shard])
		}
	}
}

// TestRunShardFlatPathReporterCountsOnlyThisShardsSpecs proves RunShard's reporting decision: a
// shard's SuiteEndEvent.TotalSpecs/FailedSpecs counts only its own specs, and specs of other shards
// emit no SpecStarted/SpecFinished events at all (mirroring Builder's RunShardWithReporter).
func TestRunShardFlatPathReporterCountsOnlyThisShardsSpecs(t *testing.T) {
	const n = 6
	const shardCount = 3
	const wantSpecs = n / shardCount // evenly divisible: 2 specs per shard
	for shard := 0; shard < shardCount; shard++ {
		t.Run(fmt.Sprintf("shard-%d", shard), func(t *testing.T) {
			var ran []string
			var mu sync.Mutex
			suite := buildFlatShardSuiteCompilerPath(n, &ran, &mu)
			rep := &recordingReporter{}
			suite.Reporter = rep
			suite.RunShard(&shardFakeTB{}, shard, shardCount)

			if len(rep.suiteFinished) != 1 {
				t.Fatalf("shard %d: got %d SuiteFinished events, want 1", shard, len(rep.suiteFinished))
			}
			end := rep.suiteFinished[0]
			if end.TotalSpecs != wantSpecs {
				t.Errorf("shard %d: TotalSpecs = %d, want %d", shard, end.TotalSpecs, wantSpecs)
			}
			if end.FailedSpecs != 0 {
				t.Errorf("shard %d: FailedSpecs = %d, want 0", shard, end.FailedSpecs)
			}
			if len(rep.specStarted) != wantSpecs {
				t.Errorf("shard %d: %d SpecStarted events, want %d (another shard's specs must emit none)",
					shard, len(rep.specStarted), wantSpecs)
			}
		})
	}
}

// TestRunShardReportsSkipPendingMarksOnlyOnShardZero pins RunShard's decision that compile-time
// SkipIt/PendingIt marks have no plan index and belong to no shard's units: shard 0 alone reports
// them, so the union across every shard reports each one exactly once.
func TestRunShardReportsSkipPendingMarksOnlyOnShardZero(t *testing.T) {
	build := func() *CompiledSuite {
		return BuildSuite(nil, "suite", func(s *Spec) {
			s.SkipIt("skipped", func(*Context) {})
			s.PendingIt("pending", func(*Context) {})
			s.It("one", func(*Context) {})
			s.It("two", func(*Context) {})
		})
	}
	const shardCount = 2
	var totalSkipMarks, totalPendingMarks int
	for shard := 0; shard < shardCount; shard++ {
		suite := build()
		rep := &recordingReporter{}
		suite.Reporter = rep
		suite.RunShard(&shardFakeTB{}, shard, shardCount)
		for _, e := range rep.specFinished {
			switch {
			case e.Name == "skipped" && e.Skipped:
				totalSkipMarks++
				if shard != 0 {
					t.Errorf("skip mark reported by shard %d, want shard 0 only", shard)
				}
			case e.Name == "pending" && e.Pending:
				totalPendingMarks++
				if shard != 0 {
					t.Errorf("pending mark reported by shard %d, want shard 0 only", shard)
				}
			}
		}
	}
	if totalSkipMarks != 1 {
		t.Errorf("skip mark reported %d times across all shards, want exactly 1", totalSkipMarks)
	}
	if totalPendingMarks != 1 {
		t.Errorf("pending mark reported %d times across all shards, want exactly 1", totalPendingMarks)
	}
}

// TestCompiledSuiteRunShardFailsVisiblyOnInvalidConfiguration is
// TestRunShardFailsVisiblyOnInvalidConfiguration (sharding_test.go) for CompiledSuite.RunShard:
// reuses invalidShardConfigs and shardCapturingTB so both RunShard entry points are held to the same
// #174 diagnostic.
func TestCompiledSuiteRunShardFailsVisiblyOnInvalidConfiguration(t *testing.T) {
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.It("one", func(*Context) {})
	})
	for _, tc := range invalidShardConfigs {
		t.Run(tc.name, func(t *testing.T) {
			tb := &shardCapturingTB{}
			suite.RunShard(tb, tc.shard, tc.total)
			if !tb.failed {
				t.Fatalf("RunShard(tb, %d, %d) reported no failure", tc.shard, tc.total)
			}
			if !strings.Contains(tb.message, "invalid shard configuration") {
				t.Errorf("failure message %q is not an actionable shard diagnostic", tb.message)
			}
		})
	}
}

// TestCompiledSuiteRunShardEmptyValidShardRunsNothingAndIsNotAFailure pins the boundary decision #5
// deliberately leaves alone: a shard that legitimately draws no unit (more shards than units) is
// valid configuration and an empty partition, not a configuration error — and, unlike a suite whose
// whole plan is empty, it must not even emit SuiteStarted/SuiteFinished, mirroring how Builder's
// RunShard/RunShardWithReporter never build a Runner at all for an empty shard.
func TestCompiledSuiteRunShardEmptyValidShardRunsNothingAndIsNotAFailure(t *testing.T) {
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.It("one", func(*Context) {})
	})
	rep := &recordingReporter{}
	suite.Reporter = rep
	tb := &shardFakeTB{}
	suite.RunShard(tb, 2, 3) // 1 spec (1 unit), 3 shards: shard 2 draws nothing
	if tb.failed {
		t.Fatalf("an empty but valid shard was reported as a failure: %s", tb.message)
	}
	if len(rep.suiteStarted) != 0 || len(rep.suiteFinished) != 0 {
		t.Errorf("an empty valid shard emitted suite events: started=%d finished=%d",
			len(rep.suiteStarted), len(rep.suiteFinished))
	}
}

// TestRunShardFlatPathFailFastAppliesWithinShardOnly proves FailFast (CompiledSuite.SetFailFast)
// stops only the shard that is running: a failure in shard 0 must never affect shard 1, a separate
// RunShard call entirely.
func TestRunShardFlatPathFailFastAppliesWithinShardOnly(t *testing.T) {
	newSuite := func(log *[]string) *CompiledSuite {
		suite := BuildSuite(nil, "suite", func(s *Spec) {
			s.It("spec-0", func(ctx *Context) { *log = append(*log, "spec-0"); EqualTo(ctx, 1, 2) })
			s.It("spec-1", func(*Context) { *log = append(*log, "spec-1") })
			s.It("spec-2", func(*Context) { *log = append(*log, "spec-2") })
			s.It("spec-3", func(*Context) { *log = append(*log, "spec-3") })
		})
		suite.SetFailFast(true)
		return suite
	}
	// units: u0=spec-0, u1=spec-1, u2=spec-2, u3=spec-3 (flat path: unit == plan index).
	// shardCount 2: shard 0 = {spec-0, spec-2}; shard 1 = {spec-1, spec-3}.

	var log0 []string
	suite0 := newSuite(&log0)
	suite0.RunShard(&shardCapturingTB{}, 0, 2)
	if got := strings.Join(log0, ","); got != "spec-0" {
		t.Fatalf("shard 0 = %q, want %q (spec-2, this shard's second unit, must never run after spec-0 fails)",
			got, "spec-0")
	}

	var log1 []string
	suite1 := newSuite(&log1)
	suite1.RunShard(&shardCapturingTB{}, 1, 2)
	if got := strings.Join(log1, ","); got != "spec-1,spec-3" {
		t.Fatalf("shard 1 = %q, want %q (FailFast on shard 0 must not affect shard 1, a different RunShard call)",
			got, "spec-1,spec-3")
	}
}

// TestRunShardFlatPathDashRunFilterAppliesWithinTheShardRealProcess proves -run filtering composes
// with sharding: a spec belonging to a different shard never runs regardless of -run, while -run
// still filters within the specs this shard actually draws (mirroring
// TestCompiledSuiteSetFailFastDoesNotStopOnAFilteredSpecRealProcess's pattern). A real *testing.T and
// a subprocess are required: only a real subtest can be filtered by -run at all.
func TestRunShardFlatPathDashRunFilterAppliesWithinTheShardRealProcess(t *testing.T) {
	const helperEnv = "GO_SPECS_RUNSHARD_FILTER_HELPER"
	if os.Getenv(helperEnv) == "1" {
		var rep recordingReporter
		// units: u0=zero (shard 0), u1=one (shard 1), u2=two (shard 0). shard 0 draws {zero, two}.
		suite := BuildSuite(t, "suite", func(s *Spec) {
			s.It("zero", func(ctx *Context) { EqualTo(ctx, 1, 2) }) // filtered out below, so never runs
			s.It("one", func(*Context) { fmt.Println("one ran") })  // a different shard: must never run
			s.It("two", func(*Context) { fmt.Println("two ran") })
		})
		suite.Reporter = &rep
		suite.RunShard(t, 0, 2)
		fmt.Printf("suite finished total=%d failed=%d filtered=%d\n",
			rep.suiteFinished[0].TotalSpecs, rep.suiteFinished[0].FailedSpecs, rep.suiteFinished[0].FilteredSpecs)
		return
	}

	cmd := exec.Command(os.Args[0],
		"-test.run=^TestRunShardFlatPathDashRunFilterAppliesWithinTheShardRealProcess$/^suite$/^two$",
	)
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "two ran") {
		t.Fatalf("expected the -run-selected spec to still run, got: %s", output)
	}
	if strings.Contains(string(output), "one ran") {
		t.Fatalf("expected the other shard's spec never to run regardless of -run, got: %s", output)
	}
	if !strings.Contains(string(output), "suite finished total=2 failed=0 filtered=1") {
		t.Fatalf("expected SuiteFinished total=2 failed=0 filtered=1 (this shard's 2 specs: "+
			"zero filtered, two ran), got: %s", output)
	}
}
