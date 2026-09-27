// group_hooks_shard_test.go proves CompiledSuite.RunShard (issue #251) on the group path — a suite
// that registers at least one BeforeAll/AfterAll group or ItParallel batch, which runs through
// runPlanWithGroups/runRange instead of the flat path. See compiled_suite_shard_test.go for the
// flat-path equivalent (T1).
package specs

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// buildShardGroupSuite builds a suite with five top-level units, in declaration order:
//
//	u0 = "s0"                     (single spec)
//	u1 = When("G") { ... }        (a whole hook group, including its nested When("N"))
//	u2 = ItParallel batch {p1,p2,p3}
//	u3 = "s3"                     (single spec)
//	u4 = "s4"                     (single spec)
//
// Every executed spec/hook records its own name into *log (guarded by mu). This is the fixture every
// test in this file shares, so a change to the declared shape only has to be reasoned about once.
func buildShardGroupSuite(log *[]string, mu *sync.Mutex) *CompiledSuite {
	logAppend := func(s string) {
		mu.Lock()
		*log = append(*log, s)
		mu.Unlock()
	}
	return BuildSuite(nil, "suite", func(s *Spec) {
		s.It("s0", func(*Context) { logAppend("s0") })
		s.When("G", func(w *Spec) {
			w.BeforeAll(func(*Context) { logAppend("G.BeforeAll") })
			w.AfterAll(func(*Context) { logAppend("G.AfterAll") })
			w.It("g1", func(*Context) { logAppend("G.g1") })
			w.When("N", func(n *Spec) {
				n.It("nested", func(*Context) { logAppend("G.N.nested") })
			})
		})
		s.ItParallel("p1", func(*Context) { logAppend("p1") })
		s.ItParallel("p2", func(*Context) { logAppend("p2") })
		s.ItParallel("p3", func(*Context) { logAppend("p3") })
		s.It("s3", func(*Context) { logAppend("s3") })
		s.It("s4", func(*Context) { logAppend("s4") })
	})
}

// TestRunShardKeepsHookGroupWholeIncludingNestedGroup proves RunShard's assignment-unit decision for
// a hook group: When("G") — including its nested When("N"), which registers no hooks of its own —
// is one indivisible unit. Its BeforeAll/AfterAll run exactly once, on exactly the shard the group's
// unit number assigns it to, and never on any other shard; both of its specs ("g1" and the nested
// "nested") land on that same shard.
func TestRunShardKeepsHookGroupWholeIncludingNestedGroup(t *testing.T) {
	for _, shardCount := range []int{1, 2, 3, 5, 7} {
		t.Run(fmt.Sprintf("shardCount-%d", shardCount), func(t *testing.T) {
			wantShard := 1 % shardCount // u1 = the "G" hook group, in declaration order
			var beforeAllCount, afterAllCount int
			groupShard := -1
			var sawG1, sawNested bool

			for shard := 0; shard < shardCount; shard++ {
				var log []string
				var mu sync.Mutex
				suite := buildShardGroupSuite(&log, &mu)
				suite.RunShard(&shardCapturingTB{}, shard, shardCount)
				for _, e := range log {
					switch e {
					case "G.BeforeAll":
						beforeAllCount++
						groupShard = shard
					case "G.AfterAll":
						afterAllCount++
					case "G.g1":
						sawG1 = true
						if shard != wantShard {
							t.Errorf("G.g1 ran on shard %d, want shard %d", shard, wantShard)
						}
					case "G.N.nested":
						sawNested = true
						if shard != wantShard {
							t.Errorf("G.N.nested ran on shard %d, want shard %d", shard, wantShard)
						}
					}
				}
			}
			if beforeAllCount != 1 || afterAllCount != 1 {
				t.Fatalf("BeforeAll ran %d times, AfterAll ran %d times, want exactly 1 each (across all shards)",
					beforeAllCount, afterAllCount)
			}
			if groupShard != wantShard {
				t.Fatalf("group hooks ran on shard %d, want shard %d", groupShard, wantShard)
			}
			if !sawG1 || !sawNested {
				t.Fatalf("the group's own specs did not all run: g1=%v nested=%v", sawG1, sawNested)
			}
		})
	}
}

// TestRunShardKeepsItParallelBatchWholeAndRunsAllSiblings proves RunShard's assignment-unit decision
// for an ItParallel batch: {p1,p2,p3} is one indivisible unit — every sibling lands on the same
// shard, the one the batch's unit number assigns it to, and none of them ever runs on any other
// shard.
func TestRunShardKeepsItParallelBatchWholeAndRunsAllSiblings(t *testing.T) {
	for _, shardCount := range []int{1, 2, 3, 5, 7} {
		t.Run(fmt.Sprintf("shardCount-%d", shardCount), func(t *testing.T) {
			wantShard := 2 % shardCount // u2 = the ItParallel batch {p1,p2,p3}
			seen := map[string]int{}

			for shard := 0; shard < shardCount; shard++ {
				var log []string
				var mu sync.Mutex
				suite := buildShardGroupSuite(&log, &mu)
				suite.RunShard(&shardCapturingTB{}, shard, shardCount)
				for _, e := range log {
					if e == "p1" || e == "p2" || e == "p3" {
						seen[e]++
						if shard != wantShard {
							t.Errorf("%s ran on shard %d, want shard %d", e, shard, wantShard)
						}
					}
				}
			}
			for _, name := range []string{"p1", "p2", "p3"} {
				if seen[name] != 1 {
					t.Errorf("%s ran %d times across all shards, want exactly 1", name, seen[name])
				}
			}
		})
	}
}

// TestRunShardGroupPathCoversEveryUnitExactlyOnce is
// TestRunShardFlatPathCoversEverySpecExactlyOnceNoDuplicates for the group-path fixture: the union
// of every shard's log is every spec and every hook exactly once, whatever the shard count.
func TestRunShardGroupPathCoversEveryUnitExactlyOnce(t *testing.T) {
	wantEvents := []string{"s0", "G.BeforeAll", "G.g1", "G.N.nested", "G.AfterAll", "p1", "p2", "p3", "s3", "s4"}
	for _, shardCount := range []int{1, 2, 5} {
		t.Run(fmt.Sprintf("shardCount-%d", shardCount), func(t *testing.T) {
			seen := map[string]int{}
			for shard := 0; shard < shardCount; shard++ {
				var log []string
				var mu sync.Mutex
				suite := buildShardGroupSuite(&log, &mu)
				suite.RunShard(&shardCapturingTB{}, shard, shardCount)
				for _, e := range log {
					seen[e]++
				}
			}
			for _, name := range wantEvents {
				if seen[name] != 1 {
					t.Errorf("%s ran %d times across all shards, want exactly 1: %v", name, seen[name], seen)
				}
			}
		})
	}
}

// TestRunShardGroupPathReporterCountsOnlyThisShardsSpecs proves RunShard's reporting decision holds
// on the group path too: SuiteEndEvent.TotalSpecs and the number of SpecStarted events count only
// this shard's own real specs — a hook group contributes its specs (not a synthetic [BeforeAll]/
// [AfterAll] case, which is only ever emitted for a *failed* hook), and a shard that draws no part
// of the group emits none of its events.
func TestRunShardGroupPathReporterCountsOnlyThisShardsSpecs(t *testing.T) {
	const shardCount = 5
	// u0=s0(1) -> shard0, u1=G(g1+nested=2) -> shard1, u2=parallel(p1,p2,p3=3) -> shard2,
	// u3=s3(1) -> shard3, u4=s4(1) -> shard4.
	wantSpecs := map[int]int{0: 1, 1: 2, 2: 3, 3: 1, 4: 1}
	for shard := 0; shard < shardCount; shard++ {
		t.Run(fmt.Sprintf("shard-%d", shard), func(t *testing.T) {
			var log []string
			var mu sync.Mutex
			suite := buildShardGroupSuite(&log, &mu)
			rep := &recordingReporter{}
			suite.Reporter = rep
			suite.RunShard(&shardFakeTB{}, shard, shardCount)

			if len(rep.suiteFinished) != 1 {
				t.Fatalf("shard %d: got %d SuiteFinished events, want 1", shard, len(rep.suiteFinished))
			}
			want := wantSpecs[shard]
			if end := rep.suiteFinished[0]; end.TotalSpecs != want {
				t.Errorf("shard %d: TotalSpecs = %d, want %d", shard, end.TotalSpecs, want)
			}
			if len(rep.specStarted) != want {
				t.Errorf("shard %d: %d SpecStarted events, want %d", shard, len(rep.specStarted), want)
			}
		})
	}
}

// TestRunShardGroupPathFailFastAppliesWithinShardOnly proves FailFast on the group path stops only
// the shard that is running, exactly like the flat-path equivalent
// (TestRunShardFlatPathFailFastAppliesWithinShardOnly): a failure in shard 0's own units must never
// affect shard 1's units, a separate RunShard call entirely.
func TestRunShardGroupPathFailFastAppliesWithinShardOnly(t *testing.T) {
	newSuite := func(log *[]string, mu *sync.Mutex) *CompiledSuite {
		logAppend := func(s string) { mu.Lock(); *log = append(*log, s); mu.Unlock() }
		suite := BuildSuite(nil, "suite", func(s *Spec) {
			s.It("s0", func(ctx *Context) { logAppend("s0"); EqualTo(ctx, 1, 2) })
			s.When("G", func(w *Spec) {
				w.BeforeAll(func(*Context) { logAppend("G.BeforeAll") })
				w.AfterAll(func(*Context) { logAppend("G.AfterAll") })
				w.It("g1", func(*Context) { logAppend("G.g1") })
			})
			s.ItParallel("p1", func(*Context) { logAppend("p1") })
			s.It("s3", func(*Context) { logAppend("s3") })
			s.It("s4", func(*Context) { logAppend("s4") })
		})
		suite.SetFailFast(true)
		return suite
	}
	// units: u0=s0, u1=G, u2=p1 (a batch of one is still its own unit), u3=s3, u4=s4.
	// shardCount 2: shard 0 = {s0, p1, s4}; shard 1 = {G, s3}.

	var log0 []string
	var mu0 sync.Mutex
	suite0 := newSuite(&log0, &mu0)
	suite0.RunShard(&shardCapturingTB{}, 0, 2)
	if got := strings.Join(log0, ","); got != "s0" {
		t.Fatalf("shard 0 = %q, want %q (p1 and s4, this shard's other units, must never run after s0 fails)",
			got, "s0")
	}

	var log1 []string
	var mu1 sync.Mutex
	suite1 := newSuite(&log1, &mu1)
	suite1.RunShard(&shardCapturingTB{}, 1, 2)
	if got := strings.Join(log1, ","); got != "G.BeforeAll,G.g1,G.AfterAll,s3" {
		t.Fatalf("shard 1 = %q, want %q (FailFast on shard 0 must not affect shard 1, a different RunShard call)",
			got, "G.BeforeAll,G.g1,G.AfterAll,s3")
	}
}
