// context_errorf_test.go pins ctx.Errorf and ctx.Helper of issue #357 on every engine of the table in
// context_cleanup_test.go, plus, in a subprocess, the behavior that needs a real *testing.T: subtest
// reporting and the reported file:line.
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Compile-time proof that *Context is what the mock package needs from a test (#357).
var _ interface {
	Helper()
	Cleanup(func())
	Errorf(string, ...any)
} = (*Context)(nil)

func TestCtxErrorfIsANonFatalFailureThatKeepsTheFirstMessage(t *testing.T) {
	forEachCleanupEngine(t, func(t *testing.T, e cleanupEngine) {
		var ev goEvents
		out := e.run(t, cleanupCase{body: func(ctx *Context) {
			ctx.Errorf("first %d", 1)
			ev.add("continued")
			ctx.Errorf("second")
		}})
		if !out.failed {
			t.Fatalf("Errorf did not fail the case: %+v", out)
		}
		if got := fmt.Sprint(ev.list); got != "[continued]" {
			t.Errorf("the case did not continue after Errorf: %v", ev.list)
		}
		checkFirstMessage(t, e, out, "first 1", "second")
	})
}

func TestCtxErrorfFromACleanupFailsTheCase(t *testing.T) {
	forEachCleanupEngine(t, func(t *testing.T, e cleanupEngine) {
		out := e.run(t, cleanupCase{body: func(ctx *Context) {
			ctx.Cleanup(func() { ctx.Errorf("from cleanup") })
		}})
		if !out.failed {
			t.Fatalf("an Errorf raised by a cleanup did not fail the case: %+v", out)
		}
		checkFirstMessage(t, e, out, "from cleanup", "")
	})
}

func checkFirstMessage(t *testing.T, e cleanupEngine, out cleanupOutcome, want, notWant string) {
	t.Helper()
	if e.rawMessage {
		if out.message != want {
			t.Errorf("message = %q, want exactly %q", out.message, want)
		}
		return
	}
	if !strings.HasSuffix(out.message, want) {
		t.Errorf("message = %q, want it to end with %q", out.message, want)
	}
	if notWant != "" && strings.Contains(out.message, notWant) {
		t.Errorf("message = %q must not contain the later failure %q", out.message, notWant)
	}
}

func TestCtxHelperIsASafeNoOpWithoutABackend(t *testing.T) {
	var nilCtx *Context
	nilCtx.Helper()
	(&Context{}).Helper()
	ctx := acquireContext(&controlledBackend{})
	defer releaseContext(ctx)
	ctx.Helper()
}

// TestGoSpecsInternalFramesIncludeTheMockPackage pins the attribution prefix list: a failure the mock
// package reports from inside parallelBackend must skip its frames and land on the user's code.
func TestGoSpecsInternalFramesIncludeTheMockPackage(t *testing.T) {
	for fn, want := range map[string]bool{
		"github.com/getsyntegrity/go-specs/mock.(*Controller).Verify": true,
		"github.com/getsyntegrity/go-specs/mock.NewController":        true,
		"github.com/getsyntegrity/go-specs/specs.(*Context).Errorf":   true,
		"github.com/getsyntegrity/go-specs/mock/testdata/x.Body":      false,
		"github.com/getsyntegrity/go-specs/mockery.Fn":                false,
		"example.com/user.Test":                                       false,
	} {
		if got := isGoSpecsInternalFrame(fn); got != want {
			t.Errorf("isGoSpecsInternalFrame(%q) = %v, want %v", fn, got, want)
		}
	}
}

// --- real *testing.T behavior (subprocess) ---

const cleanupHelperEnv = "GO_SPECS_CLEANUP_HELPER"

func cleanupRealTBody(t *testing.T, engine string) {
	mark := func(s string) { fmt.Println("MARK " + s) }
	order := func(ctx *Context) {
		mark("order body")
		ctx.Cleanup(func() { mark("order cleanup1") })
		ctx.Cleanup(func() { mark("order cleanup2") })
		ctx.Go(func(*Context) {
			time.Sleep(20 * time.Millisecond)
			mark("order task")
		})
	}
	orderAfter := func(*Context) { mark("order after") }
	fatal := func(ctx *Context) {
		ctx.Cleanup(func() { mark("fatal cleanup") })
		ctx.Expect(1).ToEqual(2)
	}
	panics := func(ctx *Context) {
		ctx.Cleanup(func() { mark("panic cleanup") })
		panic("body-boom")
	}
	cleanupPanics := func(ctx *Context) {
		ctx.Cleanup(func() { mark("cleanuppanic first") })
		ctx.Cleanup(func() { panic("cleanup-boom") })
	}
	errorf := func(ctx *Context) {
		_, _, line, _ := runtime.Caller(0)
		fmt.Printf("ERRORF_LINE=%d\n", line+2) // Errorf is two lines below Caller(0)
		ctx.Errorf("errorf-msg %d", 7)
		mark("errorf continued")
	}
	rep := &recordingReporter{}
	switch engine {
	case "spec", "specparallel":
		it := func(s *Spec, name string, fn func(*Context)) { s.It(name, fn) }
		if engine == "specparallel" {
			it = func(s *Spec, name string, fn func(*Context)) { s.ItParallel(name, fn) }
		}
		DescribeWithReporter(t, "suite", rep, func(s *Spec) {
			s.Describe("ordered", func(s *Spec) {
				s.AfterEach(orderAfter)
				it(s, "order", order)
			})
			it(s, "fatal", fatal)
			it(s, "panic", panics)
			it(s, "cleanuppanic", cleanupPanics)
			it(s, "errorf", errorf)
		})
	case "builder", "builderparallel":
		b := NewBuilder()
		it := b.It
		if engine == "builderparallel" {
			it = b.ItParallel
		}
		b.Describe("suite", func() {
			b.Describe("ordered", func() {
				b.AfterEach(orderAfter)
				it("order", order)
			})
			it("fatal", fatal)
			it("panic", panics)
			it("cleanuppanic", cleanupPanics)
			it("errorf", errorf)
		})
		NewRunnerWithReporter(b.Build(), "suite", rep).Run(t)
	}
	for _, e := range rep.specFinished {
		fmt.Printf("RESULT %s failed=%v error=%v msg=%q\n", e.Name, e.Failed, classifyOutcome(e) == "error", e.Message)
	}
}

func runCleanupRealT(t *testing.T, testName, engine string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), cleanupHelperEnv+"="+engine)
	raw, err := cmd.CombinedOutput()
	out := string(raw)
	if err == nil {
		t.Fatalf("expected the run to fail (four cases fail on purpose), got a passing run:\n%s", out)
	}
	if regexp.MustCompile(`(?m)^panic: `).MatchString(out) {
		t.Fatalf("the run crashed the test binary:\n%s", out)
	}
	pos := func(s string) int { return strings.Index(out, "MARK "+s+"\n") }
	// Order: body, then the ctx.Go task, then AfterEach, then cleanups last registered first.
	seq := []string{"order body", "order task", "order after", "order cleanup2", "order cleanup1"}
	prev := -1
	for _, s := range seq {
		p := pos(s)
		if p < 0 {
			t.Fatalf("marker %q missing:\n%s", s, out)
		}
		if p < prev {
			t.Errorf("marker %q ran out of order (want %v):\n%s", s, seq, out)
		}
		prev = p
	}
	for _, s := range []string{"fatal cleanup", "panic cleanup", "cleanuppanic first", "errorf continued"} {
		if pos(s) < 0 {
			t.Errorf("marker %q missing (cleanup did not run, or the case stopped):\n%s", s, out)
		}
	}
	res := func(name string) string {
		m := regexp.MustCompile(`(?m)^RESULT ` + name + ` (.*)$`).FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("no RESULT for %q:\n%s", name, out)
		}
		return m[1]
	}
	if r := res("order"); !strings.Contains(r, "failed=false") {
		t.Errorf("order: %s", r)
	}
	if r := res("fatal"); !strings.Contains(r, "failed=true") || !strings.Contains(r, "error=false") {
		t.Errorf("fatal: %s", r)
	}
	if r := res("panic"); !strings.Contains(r, "failed=true") || !strings.Contains(r, "error=true") {
		t.Errorf("panic: %s", r)
	}
	if r := res("cleanuppanic"); !strings.Contains(r, "failed=true") || !strings.Contains(r, "error=true") || !strings.Contains(r, "cleanup-boom") {
		t.Errorf("cleanuppanic must be an error naming the cleanup panic: %s", r)
	}
	if r := res("errorf"); !strings.Contains(r, "failed=true") || !strings.Contains(r, `msg="errorf-msg 7"`) {
		t.Errorf("errorf must fail with exactly the Errorf text as SpecResultEvent.Message: %s", r)
	}
	m := regexp.MustCompile(`ERRORF_LINE=(\d+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("the errorf case never ran:\n%s", out)
	}
	if engine == "builderparallel" {
		// Builder.ItParallel attributes through a stack walk that skips every go-specs frame, and this
		// test file is itself package specs, so the walk would skip the body too. The attribution of
		// ctx.Errorf on that engine is pinned by testdata/parallel_attribution (TestItParallelCtxErrorf).
		return
	}
	if want := "context_errorf_test.go:" + m[1] + ": "; !strings.Contains(out, want) {
		t.Errorf("Errorf is not attributed to the caller's line (want %q):\n%s", want, out)
	}
}

func TestCtxCleanupAndErrorfRealT(t *testing.T) {
	for _, engine := range []string{"spec", "specparallel", "builder", "builderparallel"} {
		testName := "TestCtxCleanupAndErrorfRealT"
		if os.Getenv(cleanupHelperEnv) == engine {
			cleanupRealTBody(t, engine)
			return
		}
		if os.Getenv(cleanupHelperEnv) != "" {
			continue
		}
		t.Run(engine, func(t *testing.T) { runCleanupRealT(t, testName, engine) })
	}
}
