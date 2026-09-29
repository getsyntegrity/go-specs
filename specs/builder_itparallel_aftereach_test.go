// builder_itparallel_aftereach_test.go pins that Builder.ItParallel runs a spec's AfterEach hooks
// however the spec ends, exactly like Builder.It does (#334). Before the fix, a fatal assertion or a
// panic in a parallel spec unwound out of the flat before/body/after sequence and the hooks never ran.
//
// One table drives both registration kinds (It is the contract, ItParallel must match it) on both
// parallelStep paths: a fake non-*testing.T backend, where each spec runs inline (the *testing.B
// shape), and a real *testing.T, where each spec runs in its own subtest. The specs fail on purpose,
// so the real-T path can only be observed from a subprocess.
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

// aeRec records the events of one spec run in call order; ItParallel bodies, hooks and ctx.Go tasks
// run on different goroutines, so it is mutex-guarded.
type aeRec struct {
	mu     sync.Mutex
	events []string
}

func (r *aeRec) add(e string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *aeRec) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.events, ",")
}

// aeFakeTB is a testing.TB that is not a *testing.T, so both engines run every spec inline on the
// calling goroutine, and that swallows failures so the outer test stays green. The embedded TB is nil
// on purpose: a method this file does not override panics instead of silently reaching a real test.
type aeFakeTB struct{ testing.TB }

func (aeFakeTB) Helper()               {}
func (aeFakeTB) Name() string          { return "aeFakeTB" }
func (aeFakeTB) Cleanup(func())        {}
func (aeFakeTB) Error(...any)          {}
func (aeFakeTB) Errorf(string, ...any) {}
func (aeFakeTB) Fatal(...any)          {}
func (aeFakeTB) Fatalf(string, ...any) {}
func (aeFakeTB) FailNow()              {}
func (aeFakeTB) Fail()                 {}
func (aeFakeTB) Log(...any)            {}
func (aeFakeTB) Logf(string, ...any)   {}
func (aeFakeTB) Failed() bool          { return false }

// aeCase is one spec, described by the hooks and body it registers through reg (Builder.It or
// Builder.ItParallel) and by what the run must record and report.
type aeCase struct {
	name       string
	build      func(b *Builder, reg func(string, func(*Context)), rec *aeRec)
	wantEvents string // every event, in order
	wantStatus string // classifyOutcome of the single spec
	wantMsg    string // substring of the reported message ("" = no check)

	// Builder.It differs from Builder.ItParallel in two observed ways, pinned here rather than hidden:
	// on a fake backend whose Fatalf returns (a real testing.T's ends the goroutine), a sequential spec
	// keeps running past a failed assertion (seqInlineEvents); and a later assertion failure overwrites
	// the message of an earlier one on Builder.It (seqMsg), where ItParallel keeps the first.
	seqInlineEvents string
	seqMsg          string
}

func aeHook(rec *aeRec, name string, then func(*Context)) func(*Context) {
	return func(c *Context) {
		rec.add(name)
		if then != nil {
			then(c)
		}
	}
}

func aeFail(c *Context)  { c.Expect(1).ToEqual(2) }
func aePanic(c *Context) { panic("kaboom") }

var aeCases = []aeCase{
	{
		name: "passing spec",
		build: func(b *Builder, reg func(string, func(*Context)), rec *aeRec) {
			b.BeforeEach(aeHook(rec, "before", nil))
			b.AfterEach(aeHook(rec, "after", nil))
			reg("spec", aeHook(rec, "body", nil))
		},
		wantEvents: "before,body,after",
		wantStatus: "passed",
	},
	{
		name: "failing assertion",
		build: func(b *Builder, reg func(string, func(*Context)), rec *aeRec) {
			b.AfterEach(aeHook(rec, "after", nil))
			reg("spec", aeHook(rec, "body", aeFail))
		},
		wantEvents: "body,after",
		wantStatus: "failed",
		wantMsg:    "2",
	},
	{
		name: "panic in body",
		build: func(b *Builder, reg func(string, func(*Context)), rec *aeRec) {
			b.AfterEach(aeHook(rec, "after", nil))
			reg("spec", aeHook(rec, "body", aePanic))
		},
		wantEvents: "body,after",
		wantStatus: "error",
		wantMsg:    "kaboom",
	},
	{
		name: "failing BeforeEach",
		build: func(b *Builder, reg func(string, func(*Context)), rec *aeRec) {
			b.BeforeEach(aeHook(rec, "before", aeFail))
			b.AfterEach(aeHook(rec, "after", nil))
			reg("spec", aeHook(rec, "body", nil))
		},
		wantEvents: "before,after",
		wantStatus: "failed",

		seqInlineEvents: "before,body,after",
	},
	{
		name: "failing ctx.Go task runs AfterEach after the task finished",
		build: func(b *Builder, reg func(string, func(*Context)), rec *aeRec) {
			b.AfterEach(aeHook(rec, "after", nil))
			reg("spec", func(c *Context) {
				rec.add("body")
				c.Go(func(tc *Context) {
					time.Sleep(20 * time.Millisecond)
					rec.add("task")
					aeFail(tc)
				})
			})
		},
		wantEvents: "body,task,after",
		wantStatus: "failed",
	},
	{
		name: "nested Describe with AfterEach at two levels, passing",
		build: func(b *Builder, reg func(string, func(*Context)), rec *aeRec) {
			b.AfterEach(aeHook(rec, "after-outer", nil))
			b.Describe("inner", func() {
				b.AfterEach(aeHook(rec, "after-inner", nil))
				reg("spec", aeHook(rec, "body", nil))
			})
		},
		wantEvents: "body,after-outer,after-inner",
		wantStatus: "passed",
	},
	{
		name: "nested Describe with AfterEach at two levels",
		build: func(b *Builder, reg func(string, func(*Context)), rec *aeRec) {
			b.BeforeEach(aeHook(rec, "before-outer", nil))
			b.AfterEach(aeHook(rec, "after-outer", nil))
			b.Describe("inner", func() {
				b.BeforeEach(aeHook(rec, "before-inner", nil))
				b.AfterEach(aeHook(rec, "after-inner", nil))
				reg("spec", aeHook(rec, "body", aeFail))
			})
		},
		wantEvents: "before-outer,before-inner,body,after-outer,after-inner",
		wantStatus: "failed",
	},
	{
		name: "body failure message wins over a later AfterEach failure",
		build: func(b *Builder, reg func(string, func(*Context)), rec *aeRec) {
			b.AfterEach(aeHook(rec, "after", func(c *Context) { c.Expect("after-value").ToEqual("other") }))
			reg("spec", aeHook(rec, "body", aeFail))
		},
		wantEvents: "body,after",
		wantStatus: "failed",
		wantMsg:    "2",

		seqMsg: "other",
	},
	{
		name: "panic in AfterEach is an error",
		build: func(b *Builder, reg func(string, func(*Context)), rec *aeRec) {
			b.AfterEach(aeHook(rec, "after", func(*Context) { panic("after-boom") }))
			reg("spec", aeHook(rec, "body", nil))
		},
		wantEvents: "body,after",
		wantStatus: "error",
		wantMsg:    "after-boom",
	},
	{
		name: "fatal assertion in AfterEach is a failure, not a panic",
		build: func(b *Builder, reg func(string, func(*Context)), rec *aeRec) {
			b.AfterEach(aeHook(rec, "after", func(c *Context) { c.Expect("after-value").ToEqual("other") }))
			reg("spec", aeHook(rec, "body", nil))
		},
		wantEvents: "body,after",
		wantStatus: "failed",
		wantMsg:    "other",
	},
	{
		name: "remaining AfterEach hooks still run after one panics",
		build: func(b *Builder, reg func(string, func(*Context)), rec *aeRec) {
			b.AfterEach(aeHook(rec, "after-1", nil))
			b.AfterEach(aeHook(rec, "after-2", func(*Context) { panic("after-boom") }))
			reg("spec", aeHook(rec, "body", nil))
		},
		wantEvents: "body,after-2,after-1",
		wantStatus: "error",
		wantMsg:    "after-boom",
	},
}

// runAeCase runs c once and returns what it recorded and how its only spec was reported.
func runAeCase(tb testing.TB, c aeCase, parallel bool) (events, status, msg string) {
	rec := &aeRec{}
	rep := &recordingReporter{}
	b := NewBuilder()
	reg := b.It
	if parallel {
		reg = b.ItParallel
	}
	b.Describe("suite", func() { c.build(b, reg, rec) })
	NewRunnerWithReporter(b.Build(), "suite", rep).Run(tb)
	if len(rep.specFinished) != 1 {
		return rec.String(), fmt.Sprintf("%d spec events", len(rep.specFinished)), ""
	}
	e := rep.specFinished[0]
	return rec.String(), classifyOutcome(e), e.Message
}

func checkAeCase(t *testing.T, label string, c aeCase, events, status, msg string, parallel, inline bool) {
	t.Helper()
	if !parallel {
		if c.seqMsg != "" {
			c.wantMsg = c.seqMsg
		}
		if inline && c.seqInlineEvents != "" {
			c.wantEvents = c.seqInlineEvents
		}
	}
	if events != c.wantEvents {
		t.Errorf("%s: events = %q, want %q", label, events, c.wantEvents)
	}
	if status != c.wantStatus {
		t.Errorf("%s: status = %q, want %q (message %q)", label, status, c.wantStatus, msg)
	}
	if c.wantMsg != "" && !strings.Contains(msg, c.wantMsg) {
		t.Errorf("%s: message = %q, want it to contain %q", label, msg, c.wantMsg)
	}
}

// aeHelperEnv gates the subprocess mode that runs the table against a real *testing.T.
const aeHelperEnv = "GO_SPECS_AFTEREACH_HELPER"

// TestBuilderAfterEachRunsForFailingSpecs (#334): the table above, on a non-*testing.T backend
// (inline) and, through a subprocess, on a real *testing.T (one subtest per spec), for Builder.It
// (the contract) and Builder.ItParallel (which must match it).
func TestBuilderAfterEachRunsForFailingSpecs(t *testing.T) {
	if mode := os.Getenv(aeHelperEnv); mode != "" {
		for _, c := range aeCases {
			events, status, msg := runAeCase(t, c, mode == "parallel")
			fmt.Printf("AERESULT|%s|%s|%s|%s\n", c.name, events, status, strings.ReplaceAll(msg, "\n", " "))
		}
		return
	}
	for _, mode := range []string{"sequential", "parallel"} {
		parallel := mode == "parallel"
		t.Run(mode+"/fake backend", func(t *testing.T) {
			for _, c := range aeCases {
				events, status, msg := runAeCase(aeFakeTB{}, c, parallel)
				checkAeCase(t, c.name, c, events, status, msg, parallel, true)
			}
		})
		t.Run(mode+"/real testing.T", func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestBuilderAfterEachRunsForFailingSpecs$")
			cmd.Env = append(os.Environ(), aeHelperEnv+"="+mode)
			raw, _ := cmd.CombinedOutput() // exits non-zero: the specs fail on purpose
			got := map[string][3]string{}
			for _, line := range strings.Split(string(raw), "\n") {
				if p := strings.SplitN(line, "|", 5); len(p) == 5 && p[0] == "AERESULT" {
					got[p[1]] = [3]string{p[2], p[3], p[4]}
				}
			}
			for _, c := range aeCases {
				r, ok := got[c.name]
				if !ok {
					t.Errorf("%s: no result from the subprocess:\n%s", c.name, raw)
					continue
				}
				checkAeCase(t, c.name, c, r[0], r[1], r[2], parallel, false)
			}
		})
	}
}
