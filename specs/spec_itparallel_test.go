// spec_itparallel_test.go pins issue #245's second spec (T1): Spec.ItParallel on both build paths
// (the bytecode compiler and the Analyze/registry path). Every ItParallel spec runs concurrently
// with its adjacent ItParallel siblings, each as its own real Go subtest with its own pooled
// *Context and a live ctx.T; BeforeEach/AfterEach still run per spec; the group is dropped whole
// under focus; and the ctx.T.Parallel() guard from spec_body_parallel.go still fires inside it.
package specs

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// TestSpecItParallel_CompilerPath_RunsAndReports registers three ItParallel specs on the bytecode
// compiler path and checks every one ran and reported passed.
func TestSpecItParallel_CompilerPath_RunsAndReports(t *testing.T) {
	var mu sync.Mutex
	ran := map[string]bool{}
	rep := &recordingReporter{}
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.ItParallel("a", func(*Context) { mu.Lock(); ran["a"] = true; mu.Unlock() })
		s.ItParallel("b", func(*Context) { mu.Lock(); ran["b"] = true; mu.Unlock() })
		s.ItParallel("c", func(*Context) { mu.Lock(); ran["c"] = true; mu.Unlock() })
	})
	suite.Reporter = rep
	suite.Run(t)

	for _, name := range []string{"a", "b", "c"} {
		if !ran[name] {
			t.Errorf("spec %q did not run", name)
		}
	}
	if len(rep.specFinished) != 3 {
		t.Fatalf("got %d SpecFinished events, want 3: %+v", len(rep.specFinished), rep.specFinished)
	}
	for _, e := range rep.specFinished {
		if e.Failed {
			t.Errorf("spec %q reported Failed, want passed", e.Name)
		}
	}
}

// TestSpecItParallel_RegistryPath_RunsAndReports is the Analyze/registry-path equivalent.
func TestSpecItParallel_RegistryPath_RunsAndReports(t *testing.T) {
	var mu sync.Mutex
	ran := map[string]bool{}
	rep := &recordingReporter{}
	var suite *CompiledSuite
	Analyze(func() {
		suite = BuildSuite(nil, "suite", func(s *Spec) {
			s.ItParallel("a", func(*Context) { mu.Lock(); ran["a"] = true; mu.Unlock() })
			s.ItParallel("b", func(*Context) { mu.Lock(); ran["b"] = true; mu.Unlock() })
			s.ItParallel("c", func(*Context) { mu.Lock(); ran["c"] = true; mu.Unlock() })
		})
	})
	suite.Reporter = rep
	suite.Run(t)

	for _, name := range []string{"a", "b", "c"} {
		if !ran[name] {
			t.Errorf("spec %q did not run", name)
		}
	}
	if len(rep.specFinished) != 3 {
		t.Fatalf("got %d SpecFinished events, want 3: %+v", len(rep.specFinished), rep.specFinished)
	}
}

// TestSpecItParallel_SpecsOverlapInTime proves the specs actually run concurrently rather than one
// at a time: every goroutine blocks on a shared barrier until all of them have arrived, with a
// timeout so a sequential implementation (which would deadlock instead of overlapping) fails fast
// instead of hanging the suite.
func TestSpecItParallel_SpecsOverlapInTime(t *testing.T) {
	const n = 4
	arrive := make(chan struct{}, n)
	release := make(chan struct{})
	var once sync.Once

	body := func(*Context) {
		arrive <- struct{}{}
		select {
		case <-release:
		case <-time.After(5 * time.Second):
			t.Error("timed out waiting for every parallel spec to arrive at the barrier; specs did not overlap")
		}
	}

	go func() {
		for i := 0; i < n; i++ {
			<-arrive
		}
		once.Do(func() { close(release) })
	}()

	suite := BuildSuite(nil, "suite", func(s *Spec) {
		for i := 0; i < n; i++ {
			s.ItParallel(fmt.Sprintf("spec-%d", i), body)
		}
	})
	suite.Run(t)
}

// TestSpecItParallel_EachSpecHasOwnContextAndRealT pins that ctx.T is a real, non-nil *testing.T
// (unlike Builder.ItParallel's nil ctx.T) and that BeforeEach/AfterEach still run once per parallel
// spec, around its own body.
func TestSpecItParallel_EachSpecHasOwnContextAndRealT(t *testing.T) {
	var mu sync.Mutex
	var beforeCount, afterCount int
	names := map[string]bool{}

	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.BeforeEach(func(*Context) { mu.Lock(); beforeCount++; mu.Unlock() })
		s.AfterEach(func(*Context) { mu.Lock(); afterCount++; mu.Unlock() })
		s.ItParallel("a", func(ctx *Context) {
			if ctx.T == nil {
				t.Error("expected ctx.T to be a real *testing.T inside ItParallel, got nil")
				return
			}
			mu.Lock()
			names[ctx.T.Name()] = true
			mu.Unlock()
		})
		s.ItParallel("b", func(ctx *Context) {
			if ctx.T == nil {
				t.Error("expected ctx.T to be a real *testing.T inside ItParallel, got nil")
				return
			}
			mu.Lock()
			names[ctx.T.Name()] = true
			mu.Unlock()
		})
	})
	suite.Run(t)

	if beforeCount != 2 {
		t.Errorf("BeforeEach ran %d times, want 2 (once per parallel spec)", beforeCount)
	}
	if afterCount != 2 {
		t.Errorf("AfterEach ran %d times, want 2 (once per parallel spec)", afterCount)
	}
	if len(names) != 2 {
		t.Errorf("got %d distinct subtest names for 2 parallel specs, want 2: %v", len(names), names)
	}
}

// TestSpecItParallel_GroupingConsecutiveVsSeparated pins the grouping rule directly against the
// compiled plan: consecutive ItParallel specs form one group; a plain It in between ends it; and a
// Describe/When scope boundary ends it even when the enclosing scope has no BeforeAll/AfterAll of
// its own (this engine's stricter, always-safe rule — see compiler.go's closeOpenParallelRun).
func TestSpecItParallel_GroupingConsecutiveVsSeparated(t *testing.T) {
	build := func(s *Spec) {
		s.ItParallel("a", func(*Context) {})
		s.ItParallel("b", func(*Context) {})
		s.It("mid", func(*Context) {})
		s.ItParallel("c", func(*Context) {})
		s.When("nested", func(w *Spec) {
			w.ItParallel("d", func(*Context) {})
		})
	}

	t.Run("compiler path", func(t *testing.T) {
		suite := BuildSuite(nil, "suite", build)
		pg := suite.groups
		if pg == nil {
			t.Fatal("expected parallel group bookkeeping, got nil")
		}
		assertParallelRanges(t, "compiler path", pg.parallel, [][2]int{{0, 1}, {3, 3}, {4, 4}})
	})

	t.Run("registry path", func(t *testing.T) {
		var suite *CompiledSuite
		Analyze(func() { suite = BuildSuite(nil, "suite", build) })
		pg := suite.groups
		if pg == nil {
			t.Fatal("expected parallel group bookkeeping, got nil")
		}
		assertParallelRanges(t, "registry path", pg.parallel, [][2]int{{0, 1}, {3, 3}, {4, 4}})
	})
}

func assertParallelRanges(t *testing.T, label string, got []parallelRange, want [][2]int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d parallel ranges, want %d: %+v", label, len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Start != w[0] || got[i].End != w[1] {
			t.Errorf("%s: range[%d] = [%d,%d], want [%d,%d]", label, i, got[i].Start, got[i].End, w[0], w[1])
		}
	}
}

// TestSpecItParallel_DroppedUnderFocus pins that ItParallel is dropped whole under a suite-wide
// FIt, exactly like an unfocused It — on both build paths.
func TestSpecItParallel_DroppedUnderFocus(t *testing.T) {
	t.Run("compiler path", func(t *testing.T) {
		var parallelRan bool
		rep := &recordingReporter{}
		suite := BuildSuite(nil, "suite", func(s *Spec) {
			s.ItParallel("parallel", func(*Context) { parallelRan = true })
			s.FIt("focused", func(*Context) {})
		})
		suite.Reporter = rep
		suite.Run(t)
		if parallelRan {
			t.Error("expected the unfocused ItParallel to be dropped, but it ran")
		}
		if len(rep.specFinished) != 1 || rep.specFinished[0].Name != "focused" {
			t.Errorf("expected exactly [focused] reported, got %+v", rep.specFinished)
		}
	})

	t.Run("registry path", func(t *testing.T) {
		var parallelRan bool
		rep := &recordingReporter{}
		var suite *CompiledSuite
		Analyze(func() {
			suite = BuildSuite(nil, "suite", func(s *Spec) {
				s.ItParallel("parallel", func(*Context) { parallelRan = true })
				s.FIt("focused", func(*Context) {})
			})
		})
		suite.Reporter = rep
		suite.Run(t)
		if parallelRan {
			t.Error("expected the unfocused ItParallel to be dropped, but it ran")
		}
		if len(rep.specFinished) != 1 || rep.specFinished[0].Name != "focused" {
			t.Errorf("expected exactly [focused] reported, got %+v", rep.specFinished)
		}
	})
}

// TestSpecItParallel_NonRealBackendRunsSequentially pins the documented fallback: without a real
// *testing.T (here a zero-value *testing.B, which never opens subtests), ItParallel specs run one
// at a time, exactly like ordinary sequential specs in that mode — mirroring what *Spec already
// does for It.
func TestSpecItParallel_NonRealBackendRunsSequentially(t *testing.T) {
	var mu sync.Mutex
	var order []string
	b := &testing.B{}
	Describe(b, "suite", func(s *Spec) {
		s.ItParallel("a", func(*Context) { mu.Lock(); order = append(order, "a"); mu.Unlock() })
		s.ItParallel("b", func(*Context) { mu.Lock(); order = append(order, "b"); mu.Unlock() })
	})
	if len(order) != 2 {
		t.Fatalf("got order %v, want 2 entries", order)
	}
}

// TestSpecItParallelBodyCallingTParallelStillFails proves the ctx.T.Parallel() guard
// (spec_body_parallel.go) still fires inside an ItParallel body's own real subtest, run as a real
// subprocess: the guarantee under test is that the whole run fails and says why, which can only be
// observed from outside the failing process (see spec_body_parallel_test.go's runParallelHelper).
func TestSpecItParallelBodyCallingTParallelStillFails(t *testing.T) {
	if os.Getenv(parallelHelperEnv) == "1" {
		Describe(t, "suite", func(s *Spec) {
			s.ItParallel("alpha", func(*Context) {})
			s.ItParallel("bravo", func(ctx *Context) {
				ctx.T.Parallel()
				ctx.Expect(1).ToEqual(2)
			})
			s.ItParallel("charlie", func(*Context) {})
		})
		return
	}
	output, failed := runParallelHelper(t, "TestSpecItParallelBodyCallingTParallelStillFails")
	assertParallelRejected(t, output, failed)
}
