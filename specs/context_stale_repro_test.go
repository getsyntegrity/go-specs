// context_stale_repro_test.go is the deterministic reproducer for issue #324: what happens when a
// goroutine keeps a spec's pooled *Context after that spec ended and uses it once a later spec owns
// the same pointer. It PINS the current, documented-as-unsupported behavior so a design change
// (docs/investigations/stale-context-handles-324.md) shows up as a deliberate test change.
//
// Every stale access here is ordered by channels: the later spec ("b") is parked while the stale
// goroutine acts, so the run is deterministic and race-free under `go test -race`. The truly
// concurrent variant is a data race by construction and only runs on request; see
// TestStaleContextConcurrentAccessIsADataRace.
//
// What the tests record (identical on the compiled Describe/Spec engine and the Builder engine):
//
//   - ctx.Expect through the stale handle fails the spec that owns the Context now (b), with the
//     assertion's own message; the spec that leaked ctx (a) stays passed.
//   - ctx.T.Errorf through the stale handle (real *testing.T only) lands on b's subtest T.
//   - ctx.Go through the stale handle does not panic and its task is charged to b.
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

// TestStaleContextAttributionAfterPoolReuse pins, on both engines, that stale Expect and stale Go are
// charged to the spec that owns the Context now and never to the spec that leaked it.
func TestStaleContextAttributionAfterPoolReuse(t *testing.T) {
	forEachGoEngine(t, func(t *testing.T, run goRunFn) {
		for _, op := range []string{"expect", "go"} {
			t.Run(op, func(t *testing.T) {
				var leaked *Context
				var reused atomic.Bool
				var panicked atomic.Value
				gate := make(chan struct{}) // closed by b once it owns the Context
				done := make(chan struct{}) // closed by the stale goroutine when it is finished
				rep := run(t, false, nil,
					goDeclSpec{"a", func(ctx *Context) {
						leaked = ctx
						go func() {
							defer close(done)
							<-gate
							defer func() {
								// A fatal assertion on a fake backend aborts with a sentinel panic.
								if r := recover(); r != nil {
									panicked.Store(fmt.Sprint(r))
								}
							}()
							switch op {
							case "expect":
								leaked.Expect("stale-expect").ToEqual("other")
							case "go":
								leaked.Go(func(c *Context) { c.Expect(1).ToEqual(2) })
							}
						}()
					}},
					goDeclSpec{"b", func(ctx *Context) {
						reused.Store(ctx == leaked)
						close(gate)
						<-done
					}},
				)
				if !reused.Load() {
					t.Skip("the engine did not reuse the Context between specs, so there is no stale handle")
				}
				got := finishedByName(t, rep)
				if o := classifyOutcome(got["a"]); o != "passed" {
					t.Errorf("a outcome = %q, want passed: the spec that leaked ctx is never charged", o)
				}
				if o := classifyOutcome(got["b"]); o != "failed" {
					t.Errorf("b outcome = %q, want failed: a stale %s is attributed to the spec that owns the Context now", o, op)
				}
				if op == "go" && panicked.Load() != nil {
					t.Errorf("stale ctx.Go panicked with %v, want it accepted (current, unsupported behavior)", panicked.Load())
				}
			})
		}
	})
}

// --- stale T operation, Expect and Go against a real *testing.T (subprocess) ---

const staleReproHelperEnv = "GO_SPECS_STALE324_HELPER"

func staleReproRealTBody(t *testing.T, engine string) {
	var leaked *Context
	gate := make(chan struct{})
	done := make(chan struct{})
	a := func(ctx *Context) {
		leaked = ctx
		go func() {
			defer close(done) // also runs when the fatal Expect below ends this goroutine
			<-gate
			leaked.T.Errorf("stale-t-op")
			leaked.Go(func(c *Context) { c.T.Errorf("stale-go-task") })
			leaked.Expect("stale-expect").ToEqual("other")
		}()
	}
	b := func(ctx *Context) {
		fmt.Printf("REUSED=%v\n", ctx == leaked)
		close(gate)
		<-done
	}
	switch engine {
	case "spec":
		Describe(t, "suite", func(s *Spec) {
			s.It("a", a)
			s.It("b", b)
		})
	case "builder":
		bl := NewBuilder()
		bl.Describe("suite", func() {
			bl.It("a", a)
			bl.It("b", b)
		})
		NewRunner(bl.Build()).Run(t)
	}
}

func runStaleReproRealT(t *testing.T, testName, engine string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), staleReproHelperEnv+"="+engine)
	raw, _ := cmd.CombinedOutput() // the run fails on purpose: b is charged with the stale failures
	out := string(raw)
	if strings.Contains(out, "REUSED=false") || !strings.Contains(out, "REUSED=true") {
		t.Skipf("the engine did not reuse the Context between specs:\n%s", out)
	}
	if regexp.MustCompile(`(?m)^panic: `).MatchString(out) {
		t.Fatalf("a stale handle crashed the test binary:\n%s", out)
	}
	if !regexp.MustCompile(`--- PASS: ` + testName + `/\S*a \(`).MatchString(out) {
		t.Errorf("spec a (the one that leaked ctx) must pass:\n%s", out)
	}
	if !regexp.MustCompile(`--- FAIL: ` + testName + `/\S*b \(`).MatchString(out) {
		t.Errorf("spec b (the current owner) must fail with the stale failures:\n%s", out)
	}
	for _, want := range []string{"stale-t-op", "stale-go-task", "stale-expect"} {
		if !strings.Contains(out, want) {
			t.Errorf("stale %q was not delivered to the current owner's transcript:\n%s", want, out)
		}
	}
}

func TestStaleContextRealT_Spec(t *testing.T) {
	if os.Getenv(staleReproHelperEnv) == "spec" {
		staleReproRealTBody(t, "spec")
		return
	}
	runStaleReproRealT(t, "TestStaleContextRealT_Spec", "spec")
}

func TestStaleContextRealT_Builder(t *testing.T) {
	if os.Getenv(staleReproHelperEnv) == "builder" {
		staleReproRealTBody(t, "builder")
		return
	}
	runStaleReproRealT(t, "TestStaleContextRealT_Builder", "builder")
}

// --- gated: the concurrent stale access ---

const staleReproEnv = "GO_SPECS_REPRO_324"

// TestStaleContextConcurrentAccessIsADataRace shows the part that cannot be made deterministic: a stale
// goroutine that touches a Context while its new owner is running is an unsynchronized read and write
// of the same fields. It is skipped unless GO_SPECS_REPRO_324=1. Run it with the race detector to see
// the report (`GO_SPECS_REPRO_324=1 go test -race ./specs -run StaleContextConcurrent`); it is not
// part of the default suite because the detector would rightly fail it. Without -race it only records
// the attribution and terminates.
func TestStaleContextConcurrentAccessIsADataRace(t *testing.T) {
	if os.Getenv(staleReproEnv) != "1" {
		t.Skip("set " + staleReproEnv + "=1 to run the concurrent stale-access reproducer (a data race by design)")
	}
	var leaked *Context
	var reused atomic.Bool
	gate := make(chan struct{})
	done := make(chan struct{})
	rep := runCompiledGoSpecs(t, false, nil,
		goDeclSpec{"a", func(ctx *Context) {
			leaked = ctx
			go func() {
				defer close(done)
				defer func() { _ = recover() }()
				<-gate
				for i := 0; i < 1000; i++ {
					leaked.Expect(i).ToEqual(-1) // stale write of failure state while b runs
				}
			}()
		}},
		goDeclSpec{"b", func(ctx *Context) {
			reused.Store(ctx == leaked)
			close(gate)
			for i := 0; i < 1000; i++ {
				ctx.Expect(i).ToEqual(i) // b's own assertions race with the stale ones
			}
			<-done
		}},
	)
	if !reused.Load() {
		t.Skip("the engine did not reuse the Context between specs")
	}
	got := finishedByName(t, rep)
	t.Logf("a=%s b=%s (b is charged for the stale goroutine's failures)", classifyOutcome(got["a"]), classifyOutcome(got["b"]))
}
