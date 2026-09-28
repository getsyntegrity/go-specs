// builtin_assertion_message_report_test.go pins issue #272: a built-in ctx.Expect(...)/
// Context.Snapshot assertion failure's formatted message must reach report.SpecResultEvent.Message,
// not just Failed=true, on every execution model — including one that hands the spec body a real
// *testing.T subtest, where the old behavior lost it. Before the fix, Context.failf recorded only
// the failure bit and sent the formatted text straight to backend.Fatalf; on a real *testing.T,
// Fatalf ends in runtime.Goexit before any of these engines could read that text back, so every one
// of them reported Failed=true with an empty Message — and, downstream, a blank JUnit <failure>
// element (see report/render_xml.go's junitFailure.Message).
package specs

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// wantEqualFailureMessage is the exact text assert.Equal(43).FailureMessage(42) produces (pinned by
// assert/matcher_test.go); every test below drives the same ctx.Expect(42).To(Equal(43)) failure so
// they can all assert this one literal string.
const wantEqualFailureMessage = "expected 42 to equal 43"

// builtInAssertionMessageHelperSuite declares one failing built-in assertion and one passing spec,
// so the RealProcess helpers below can pin the failing message alongside a passing spec's empty one
// in a single run.
func builtInAssertionMessageHelperSuite(s *Spec) {
	s.It("fails", func(ctx *Context) { ctx.Expect(42).To(Equal(43)) })
	s.It("passes", func(*Context) {})
}

func printBuiltInAssertionMessageReport(rep *recordingReporter) {
	for _, e := range rep.specFinished {
		fmt.Printf("SPEC_FINISHED name=%s failed=%t message=%q\n", e.Name, e.Failed, e.Message)
	}
}

// TestBuiltInAssertionMessageReachesSpecResultEventRealProcess proves #272 for every execution model
// that can run a spec body against a real *testing.T subtest: Describe/ExecutionPlan flat
// (runSpecProgramIsolated), Describe/ExecutionPlan with a BeforeAll (routes through
// group_hooks.go's runSpec, which calls the same function), Builder/Runner (runSpecIsolated), and
// Spec.ItParallel (group_hooks.go's runParallelSpec — the only ItParallel model backed by a real
// subtest; see spec_body_parallel.go). A subprocess is required: the real subtest failures these
// engines produce would otherwise fail this test itself.
func TestBuiltInAssertionMessageReachesSpecResultEventRealProcess(t *testing.T) {
	engines := map[string]func(t *testing.T, rep *recordingReporter){
		"describe": func(t *testing.T, rep *recordingReporter) {
			DescribeWithReporter(t, "suite", rep, builtInAssertionMessageHelperSuite)
		},
		"describe-with-before-all": func(t *testing.T, rep *recordingReporter) {
			DescribeWithReporter(t, "suite", rep, func(s *Spec) {
				s.BeforeAll(func(*Context) {})
				builtInAssertionMessageHelperSuite(s)
			})
		},
		"runner": func(t *testing.T, rep *recordingReporter) {
			g := group{
				specs: []step{
					func(ctx *Context) { ctx.Expect(42).To(Equal(43)) },
					func(*Context) {},
				},
				names: []string{"fails", "passes"},
			}
			NewRunnerWithReporter(&Program{Groups: []group{g}}, "suite", rep).Run(t)
		},
		"it-parallel": func(t *testing.T, rep *recordingReporter) {
			DescribeWithReporter(t, "suite", rep, func(s *Spec) {
				s.ItParallel("fails", func(ctx *Context) { ctx.Expect(42).To(Equal(43)) })
				s.ItParallel("passes", func(*Context) {})
			})
		},
	}

	const helperEnv = "GO_SPECS_BUILTIN_ASSERTION_MESSAGE_HELPER"
	if engine := os.Getenv(helperEnv); engine != "" {
		var rep recordingReporter
		engines[engine](t, &rep)
		printBuiltInAssertionMessageReport(&rep)
		return
	}

	for engine := range engines {
		t.Run(engine, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestBuiltInAssertionMessageReachesSpecResultEventRealProcess$")
			cmd.Env = append(os.Environ(), helperEnv+"="+engine)
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("expected the failing spec to fail the process, but it passed: %s", output)
			}
			wantFailed := fmt.Sprintf("SPEC_FINISHED name=fails failed=true message=%q", wantEqualFailureMessage)
			wantPassed := `SPEC_FINISHED name=passes failed=false message=""`
			if !strings.Contains(string(output), wantFailed) {
				t.Fatalf("expected %s, got:\n%s", wantFailed, output)
			}
			if !strings.Contains(string(output), wantPassed) {
				t.Fatalf("expected %s, got:\n%s", wantPassed, output)
			}
		})
	}
}

// TestBuiltInAssertionMessageReachesSpecResultEventOnTheFakeBackendFastPath proves #272 on the
// non-*testing.T fast path too (runSpecProgram/runSpecRecovered's first two branches): a fake
// backend's Fatalf neither panics nor calls runtime.Goexit, so the spec body returns normally and the
// old recover()-only message capture (runStepRecovered) saw nothing to recover there either — the
// bug was never only about Goexit.
func TestBuiltInAssertionMessageReachesSpecResultEventOnTheFakeBackendFastPath(t *testing.T) {
	rep, _ := runSpecThroughPlan(t, func(ctx *Context) { ctx.Expect(42).To(Equal(43)) })

	if len(rep.specFinished) != 1 {
		t.Fatalf("expected one SpecFinished event, got %+v", rep.specFinished)
	}
	got := rep.specFinished[0]
	if !got.Failed {
		t.Fatalf("expected Failed=true, got %+v", got)
	}
	if got.Message != wantEqualFailureMessage {
		t.Errorf("expected Message %q, got %q", wantEqualFailureMessage, got.Message)
	}
}

// TestRecoveredPanicMessageTakesPriorityOverABuiltInAssertionMessage pins the priority #272 asks for:
// when a spec both fails a built-in assertion and then panics — only reachable on a backend whose
// Fatalf does not abort, like planBackend below (a real *testing.T would Goexit right at the
// assertion and never reach the panic) — the reported Message is the panic's, not the assertion's
// that preceded it, because the panic is the more specific, later event that actually ended the spec
// (see Context.assertionMessage's doc comment).
func TestRecoveredPanicMessageTakesPriorityOverABuiltInAssertionMessage(t *testing.T) {
	rep, _ := runSpecThroughPlan(t, func(ctx *Context) {
		ctx.Expect(42).To(Equal(43))
		panic("boom")
	})

	if len(rep.specFinished) != 1 {
		t.Fatalf("expected one SpecFinished event, got %+v", rep.specFinished)
	}
	got := rep.specFinished[0]
	if !got.Failed {
		t.Fatalf("expected Failed=true, got %+v", got)
	}
	if got.Message != "panic: boom" {
		t.Errorf("expected the panic message to win over the preceding assertion message, got %q", got.Message)
	}
}

// TestFailfRecordsTheFormattedMessageBeforeReporting pins #272's fix at its source: failf writes the
// formatted message to c.failure.Message before calling backend.Fatalf, using the exact same text the
// backend receives — not a second, independent format call that could drift from it.
func TestFailfRecordsTheFormattedMessageBeforeReporting(t *testing.T) {
	backend := &capturingBackend{}
	ctx := &Context{backend: backend}

	ctx.failf("expected %d to equal %d", 42, 43)

	if !ctx.failure.Failed {
		t.Fatal("expected the failure bit to be set")
	}
	if ctx.failure.Message != wantEqualFailureMessage {
		t.Errorf("expected c.failure.Message %q, got %q", wantEqualFailureMessage, ctx.failure.Message)
	}
	if backend.message != ctx.failure.Message {
		t.Errorf("expected the backend to receive the exact same text, got backend=%q record=%q", backend.message, ctx.failure.Message)
	}
}

// TestBuiltInAssertionMessageRendersInEveryReportFormat is #272's end-to-end acceptance check: a
// real built-in assertion failure's message, collected off the exact events Describe/ExecutionPlan
// produces, appears in every rendered report format — JSON, XML, HTML and TXT — not just in the
// in-memory SpecResultEvent. The renderers themselves already forward report.Case.Message verbatim
// (see report/render_*.go); what #272 fixes is that Message is no longer empty by the time it
// reaches them for this failure shape.
func TestBuiltInAssertionMessageRendersInEveryReportFormat(t *testing.T) {
	plan := func() *ExecutionPlan {
		c := newBytecodeCompiler()
		c.PushScope("RenderSuite")
		s := &Spec{name: "RenderSuite", compiler: c}
		s.It("fails", func(ctx *Context) { ctx.Expect(42).To(Equal(43)) })
		return c.TakePlan()
	}()

	collector := report.NewCollector()
	collector.SuiteStarted(report.SuiteStartEvent{Name: "RenderSuite"})
	runPlanSpecsInOrder(&planBackend{}, collector, plan, false, nil)
	collector.SuiteFinished(report.SuiteEndEvent{Name: "RenderSuite"})
	rendered := collector.Report()

	renderers := map[string]func(*bytes.Buffer) error{
		"json": func(buf *bytes.Buffer) error { return report.RenderJSON(buf, rendered) },
		"xml":  func(buf *bytes.Buffer) error { return report.RenderXML(buf, rendered) },
		"html": func(buf *bytes.Buffer) error { return report.RenderHTML(buf, rendered) },
		"txt":  func(buf *bytes.Buffer) error { return report.RenderTXT(buf, rendered) },
	}
	for format, render := range renderers {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			if err := render(&buf); err != nil {
				t.Fatalf("render %s: %v", format, err)
			}
			if !strings.Contains(buf.String(), wantEqualFailureMessage) {
				t.Fatalf("expected %s output to contain the assertion message %q, got:\n%s", format, wantEqualFailureMessage, buf.String())
			}
		})
	}
}
