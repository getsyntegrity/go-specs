// builder_itparallel_report_test.go pins that a recovered panic in Builder.ItParallel's compiled
// group step (parallelStep) is classified as an error, like every other engine (#314).
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// orderedReporter records every event as one string, in call order, so a test can assert the exact
// interleaving of SpecStarted and SpecFinished.
type orderedReporter struct {
	mu     sync.Mutex
	events []string
	final  []report.SpecResultEvent
}

func (r *orderedReporter) SuiteStarted(report.SuiteStartEvent) {}
func (r *orderedReporter) SuiteFinished(report.SuiteEndEvent)  {}
func (r *orderedReporter) SpecStarted(e report.SpecStartEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "start:"+strings.Join(e.Path, "/"))
}
func (r *orderedReporter) SpecFinished(e report.SpecResultEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "finish:"+strings.Join(e.Path, "/"))
	r.final = append(r.final, e)
}

// TestParallelStepPanicIsErrorNotFailed (#314): a recovered panic carries the stack trace in Output
// (the marker report classifies as StatusError); an ordinary assertion failure carries none.
func TestParallelStepPanicIsErrorNotFailed(t *testing.T) {
	rep := &orderedReporter{}
	ctx := &Context{backend: &collectingBackend{}, execObserver: &reporterObserver{rep: rep}}

	parallelStep([]step{
		func(*Context) {},
		func(*Context) { panic("kaboom") },
		func(c *Context) { c.backend.Error("plain failure") },
	}, []string{"ok", "boom", "bad"}, [][]string{{"s"}, {"s"}, {"s"}})(ctx)

	if len(rep.final) != 3 {
		t.Fatalf("got %d finished events, want 3", len(rep.final))
	}
	byName := map[string]report.SpecResultEvent{}
	for _, e := range rep.final {
		byName[e.Name] = e
	}
	if e := byName["ok"]; e.Failed || e.Output != "" {
		t.Errorf("ok: %+v, want passed", e)
	}
	if e := byName["boom"]; !e.Failed || e.Output == "" || !strings.Contains(e.Message, "kaboom") {
		t.Errorf("boom: Failed=%v Message=%q Output empty=%v, want failed with panic message and stack output", e.Failed, e.Message, e.Output == "")
	}
	if e := byName["bad"]; !e.Failed || e.Output != "" || e.Message != "plain failure" {
		t.Errorf("bad: %+v, want plain failure without output", e)
	}
}

// builderParallelHelperEnv gates the subprocess mode of TestBuilderItParallelPanicEquivalence.
const builderParallelHelperEnv = "GO_SPECS_BUILDER_PARALLEL_PANIC_HELPER"

func printOutcomes(engine string, rep *recordingReporter) {
	for _, e := range rep.specFinished {
		fmt.Printf("OUTCOME|%s|%s|%s\n", engine, outcomeKey(e), classifyOutcome(e))
	}
}

// TestBuilderItParallelPanicEquivalence (#314): Builder and *Spec report the same statuses for a
// parallel batch holding a passing spec, an assertion failure and a panic. The failing run can only
// be observed from a subprocess.
func TestBuilderItParallelPanicEquivalence(t *testing.T) {
	if os.Getenv(builderParallelHelperEnv) == "1" {
		{
			rep := &recordingReporter{}
			b := NewBuilder()
			b.Describe("suite", func() {
				b.ItParallel("ok", func(*Context) {})
				b.ItParallel("bad", func(c *Context) { c.Expect(1).ToEqual(2) })
				b.ItParallel("boom", func(*Context) { panic("kaboom") })
			})
			NewRunnerWithReporter(b.Build(), "suite", rep).Run(t)
			printOutcomes("builder", rep)
		}
		{
			rep := &recordingReporter{}
			DescribeWithReporter(t, "suite", rep, func(s *Spec) {
				s.ItParallel("ok", func(*Context) {})
				s.ItParallel("bad", func(c *Context) { c.Expect(1).ToEqual(2) })
				s.ItParallel("boom", func(*Context) { panic("kaboom") })
			})
			printOutcomes("spec", rep)
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestBuilderItParallelPanicEquivalence$")
	cmd.Env = append(os.Environ(), builderParallelHelperEnv+"=1")
	raw, _ := cmd.CombinedOutput()
	got := map[string]map[string]string{"builder": {}, "spec": {}}
	for _, line := range strings.Split(string(raw), "\n") {
		if p := strings.Split(line, "|"); len(p) == 4 && p[0] == "OUTCOME" {
			got[p[1]][p[2]] = p[3]
		}
	}
	want := map[string]string{"suite/ok": "passed", "suite/bad": "failed", "suite/boom": "error"}
	for engine, outcomes := range got {
		assertSameOutcomes(t, engine, outcomes, want)
	}
}
