package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type tableRow struct {
	name string
	a, b int
	want int
}

func tableRowName(r tableRow) string { return r.name }

// mustPanicWith runs fn and fails unless it panics with a message containing every want fragment.
func mustPanicWith(t *testing.T, fn func(), want ...string) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Fatalf("expected a panic mentioning %q, got none", want)
		}
		msg := fmt.Sprint(r)
		for _, w := range want {
			if !strings.Contains(msg, w) {
				t.Fatalf("panic %q does not mention %q", msg, w)
			}
		}
	}()
	fn()
}

// TestTableRegistersEachRowAsItsOwnSpec pins row attribution: every row is reported as its own
// spec, in declaration order, with no extra hierarchy segment, and its body receives its own row.
func TestTableRegistersEachRowAsItsOwnSpec(t *testing.T) {
	var rep recordingReporter
	rows := []tableRow{{"one", 1, 1, 2}, {"two", 2, 2, 4}, {"three", 3, 3, 6}}
	var seen []string
	DescribeWithReporter(t, "suite", &rep, func(s *Spec) {
		Table(s, rows, tableRowName, func(ctx *Context, r tableRow) {
			seen = append(seen, r.name)
			ctx.Expect(r.a + r.b).ToEqual(r.want)
		})
	})
	if got, want := strings.Join(seen, ","), "one,two,three"; got != want {
		t.Fatalf("bodies ran for %q, want %q", got, want)
	}
	if len(rep.specStarted) != 3 {
		t.Fatalf("got %d SpecStarted events, want 3", len(rep.specStarted))
	}
	for i, r := range rows {
		if got := rep.specStarted[i].Name; got != r.name {
			t.Fatalf("spec %d reported as %q, want %q", i, got, r.name)
		}
		if got, want := strings.Join(rep.specStarted[i].Path, "|"), "suite|"+r.name; got != want {
			t.Fatalf("spec %d path %q, want %q (no extra hierarchy segment)", i, got, want)
		}
		if rep.specFinished[i].Failed {
			t.Fatalf("spec %q unexpectedly failed", r.name)
		}
	}
}

// TestTableFailingRowIsReportedOnItsOwn pins that one failing row fails only its own spec.
func TestTableFailingRowIsReportedOnItsOwn(t *testing.T) {
	var rep recordingReporter
	DescribeWithReporter(t, "suite", &rep, func(s *Spec) {
		Table(s, []string{"good", "bad", "also-good"}, func(n string) string { return n }, func(ctx *Context, n string) {
			if n == "bad" {
				ctx.recordFailure() // marks the case failed without failing this outer test
			}
		})
	})
	failed := map[string]bool{}
	for _, f := range rep.specFinished {
		failed[f.Name] = f.Failed
	}
	if len(failed) != 3 || failed["good"] || !failed["bad"] || failed["also-good"] {
		t.Fatalf("expected only %q to fail, got %v", "bad", failed)
	}
}

// TestTableRunsHooksAroundEveryRow pins that BeforeEach/AfterEach wrap each row like any spec.
func TestTableRunsHooksAroundEveryRow(t *testing.T) {
	var log []string
	DescribeWithReporter(t, "suite", &recordingReporter{}, func(s *Spec) {
		s.BeforeEach(func(*Context) { log = append(log, "before") })
		s.AfterEach(func(*Context) { log = append(log, "after") })
		Table(s, []string{"x", "y"}, func(n string) string { return n }, func(_ *Context, n string) {
			log = append(log, "body:"+n)
		})
	})
	want := "before,body:x,after,before,body:y,after"
	if got := strings.Join(log, ","); got != want {
		t.Fatalf("hook order %q, want %q", got, want)
	}
}

// TestTableGroupingIsTheCallers pins that grouping is opt-in: wrapping Table in When adds exactly
// the caller's segment.
func TestTableGroupingIsTheCallers(t *testing.T) {
	var rep recordingReporter
	DescribeWithReporter(t, "suite", &rep, func(s *Spec) {
		s.When("adding", func(s *Spec) {
			Table(s, []string{"x"}, func(n string) string { return n }, func(*Context, string) {})
		})
	})
	if got, want := strings.Join(rep.specStarted[0].Path, "|"), "suite|adding|x"; got != want {
		t.Fatalf("path %q, want %q", got, want)
	}
}

// TestTableSnapshotsRows pins row ownership: the slice is copied at registration, so mutating it
// afterwards does not change what runs.
func TestTableSnapshotsRows(t *testing.T) {
	rows := []tableRow{{"a", 1, 1, 2}}
	var got []int
	DescribeWithReporter(t, "suite", &recordingReporter{}, func(s *Spec) {
		Table(s, rows, tableRowName, func(_ *Context, r tableRow) { got = append(got, r.a) })
		rows[0].a = 99
	})
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("body saw %v, want [1]", got)
	}
}

func TestTableEmptyRowsRegistersNothing(t *testing.T) {
	var rep recordingReporter
	DescribeWithReporter(t, "suite", &rep, func(s *Spec) {
		s.It("anchor", func(*Context) {})
		Table(s, []tableRow(nil), tableRowName, func(*Context, tableRow) {})
	})
	if len(rep.specStarted) != 1 {
		t.Fatalf("got %d specs, want only the anchor", len(rep.specStarted))
	}
}

func TestTableRejectsInvalidInput(t *testing.T) {
	body := func(*Context, string) {}
	id := func(s string) string { return s }
	describe := func(fn func(s *Spec)) func() {
		return func() { DescribeWithReporter(t, "suite", &recordingReporter{}, fn) }
	}
	t.Run("empty name", func(t *testing.T) {
		mustPanicWith(t, describe(func(s *Spec) { Table(s, []string{"a", ""}, id, body) }),
			"Table", "row 1", "empty")
	})
	t.Run("duplicate name", func(t *testing.T) {
		mustPanicWith(t, describe(func(s *Spec) { Table(s, []string{"a", "b", "a"}, id, body) }),
			"Table", `"a"`, "row 0", "row 2")
	})
	t.Run("names colliding under go test rewriting", func(t *testing.T) {
		mustPanicWith(t, describe(func(s *Spec) { Table(s, []string{"a b", "a_b"}, id, body) }),
			"Table", "row 0", "row 1")
	})
	t.Run("nil name func", func(t *testing.T) {
		mustPanicWith(t, describe(func(s *Spec) { Table[string](s, []string{"a"}, nil, body) }),
			"Table", "name")
	})
	t.Run("nil body", func(t *testing.T) {
		mustPanicWith(t, describe(func(s *Spec) { Table[string](s, []string{"a"}, id, nil) }),
			"Table", "body")
	})
	t.Run("nil spec", func(t *testing.T) {
		mustPanicWith(t, func() { Table[string](nil, []string{"a"}, id, body) }, "Table", "Spec")
	})
}

// TestTableRejectionRegistersNoRow pins that validation is all-or-nothing: a rejected table leaves
// no partial rows behind.
func TestTableRejectionRegistersNoRow(t *testing.T) {
	var rep recordingReporter
	DescribeWithReporter(t, "suite", &rep, func(s *Spec) {
		func() {
			defer func() { _ = recover() }()
			Table(s, []string{"a", "a"}, func(n string) string { return n }, func(*Context, string) {})
		}()
		s.It("anchor", func(*Context) {})
	})
	if len(rep.specStarted) != 1 || rep.specStarted[0].Name != "anchor" {
		t.Fatalf("expected only the anchor spec, got %d specs", len(rep.specStarted))
	}
}

// TestTableParallelRunsRowsConcurrently pins that every row runs in its own goroutine: all rows
// rendezvous at a barrier that only opens when they are all running at once.
func TestTableParallelRunsRowsConcurrently(t *testing.T) {
	var rep recordingReporter
	rows := []string{"p1", "p2", "p3"}
	var arrived sync.WaitGroup
	arrived.Add(len(rows))
	var mu sync.Mutex
	got := map[string]int{}
	DescribeWithReporter(t, "suite", &rep, func(s *Spec) {
		TableParallel(s, rows, func(n string) string { return n }, func(ctx *Context, n string) {
			arrived.Done()
			done := make(chan struct{})
			go func() { arrived.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				ctx.recordFailure() // rows did not overlap
				return
			}
			mu.Lock()
			got[n]++
			mu.Unlock()
		})
	})
	for _, n := range rows {
		if got[n] != 1 {
			t.Fatalf("row %q ran %d times, want 1 (%v)", n, got[n], got)
		}
	}
	if len(rep.specStarted) != len(rows) {
		t.Fatalf("got %d reported specs, want %d", len(rep.specStarted), len(rows))
	}
	for _, f := range rep.specFinished {
		if f.Failed {
			t.Fatalf("row %q failed", f.Name)
		}
	}
}

// TestTableParallelValidatesLikeTable pins that TableParallel shares Table's name rules.
func TestTableParallelValidatesLikeTable(t *testing.T) {
	mustPanicWith(t, func() {
		DescribeWithReporter(t, "suite", &recordingReporter{}, func(s *Spec) {
			TableParallel(s, []string{"a", "a"}, func(n string) string { return n }, func(*Context, string) {})
		})
	}, "TableParallel", "row 0", "row 1")
}

// TestTableParallelRowsOwnTheirValue pins that each parallel row reads its own copy of the row.
func TestTableParallelRowsOwnTheirValue(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	rows := []tableRow{{"a", 1, 0, 0}, {"b", 2, 0, 0}, {"c", 3, 0, 0}}
	DescribeWithReporter(t, "suite", &recordingReporter{}, func(s *Spec) {
		TableParallel(s, rows, tableRowName, func(_ *Context, r tableRow) {
			mu.Lock()
			seen[r.name] = r.a
			mu.Unlock()
		})
	})
	if seen["a"] != 1 || seen["b"] != 2 || seen["c"] != 3 {
		t.Fatalf("rows saw %v", seen)
	}
}

const tableHelperEnv = "GO_SPECS_TABLE_HELPER"

func tableHelperRows() []tableRow {
	return []tableRow{{"alpha", 1, 1, 2}, {"beta", 1, 1, 3}, {"gamma", 2, 2, 4}}
}

// TestTableRealProcess re-executes the test binary to observe what `go test` itself prints: -run
// targets one row, and a failing row is its own FAIL line with its siblings passing.
func TestTableRealProcess(t *testing.T) {
	switch os.Getenv(tableHelperEnv) {
	case "seq":
		Describe(t, "suite", func(s *Spec) {
			Table(s, tableHelperRows(), tableRowName, func(ctx *Context, r tableRow) {
				fmt.Printf("RAN %s\n", r.name)
				ctx.Expect(r.a + r.b).ToEqual(r.want)
			})
		})
		return
	case "par":
		Describe(t, "suite", func(s *Spec) {
			TableParallel(s, tableHelperRows(), tableRowName, func(ctx *Context, r tableRow) {
				fmt.Printf("RAN %s\n", r.name)
				ctx.Expect(r.a + r.b).ToEqual(r.want)
			})
		})
		return
	}
	run := func(mode string, args ...string) (string, error) {
		cmd := exec.Command(os.Args[0], append([]string{"-test.v"}, args...)...)
		cmd.Env = append(os.Environ(), tableHelperEnv+"="+mode)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	const sel = "-test.run=^TestTableRealProcess$"
	for _, mode := range []string{"seq", "par"} {
		t.Run(mode+"/failing row is its own FAIL", func(t *testing.T) {
			out, err := run(mode, sel)
			if err == nil {
				t.Fatalf("expected the helper to fail:\n%s", out)
			}
			for _, want := range []string{
				"--- FAIL: TestTableRealProcess/suite/beta",
				"--- PASS: TestTableRealProcess/suite/alpha",
				"--- PASS: TestTableRealProcess/suite/gamma",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("missing %q in:\n%s", want, out)
				}
			}
		})
		t.Run(mode+"/-run targets one row", func(t *testing.T) {
			out, err := run(mode, "-test.run=^TestTableRealProcess$/^suite$/^gamma$")
			if err != nil {
				t.Fatalf("helper failed: %v\n%s", err, out)
			}
			if !strings.Contains(out, "RAN gamma\n") || strings.Contains(out, "RAN alpha") || strings.Contains(out, "RAN beta") {
				t.Fatalf("only gamma should run:\n%s", out)
			}
		})
	}
}
