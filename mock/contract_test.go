// contract_test.go runs the mock.Controller contract under go-specs on every engine (Spec.It,
// Spec.ItParallel, Builder.It, Builder.ItParallel). It is an external test package: mock's production
// code must not import specs, but its tests may, and they are the proof that *specs.Context really is
// the TB the mock asks for.
//
// Scenarios that pass run in this process. Scenarios that fail on purpose (unmet expectations after a
// fatal or panicking body, unexpected calls, order violations) run in a subprocess with a real
// *testing.T, because their failure output, and the file:line it is attributed to, is the contract.
package mock_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/report"
	"github.com/getsyntegrity/go-specs/specs"
)

// store is the hand-written typed adapter every scenario uses: the documented way to mock an
// interface, with no code generation. The comments are markers the tests look up to learn which
// line a diagnostic must be attributed to.
type store struct{ c *mock.Controller }

func (s store) Get(ctx context.Context, key string) (string, error) {
	r := s.c.Method("Get").Call(ctx, key) // ADAPTER_GET
	return mock.Value[string](r, 0), r.Err(1)
}

func (s store) Put(key, value string) error {
	r := s.c.Method("Put").Call(key, value) // ADAPTER_PUT
	return r.Err(0)
}

// probeTB wraps the case's Context and prints every Errorf the mock reports before forwarding it. It
// embeds *specs.Context, so Testing() is promoted and the mock's frame marking works as with the
// bare Context. Builder.ItParallel keeps only the first failure of a spec, so this is how the
// contract sees that Verify ran (and what it reported) after a fatal or panicking body.
type probeTB struct {
	*specs.Context
	label string
}

func (p probeTB) Errorf(format string, args ...any) {
	fmt.Printf("PROBE %s %s\n", p.label, strings.ReplaceAll(fmt.Sprintf(format, args...), "\n", " "))
	p.Context.Errorf(format, args...)
}

type contractCase struct {
	name string
	body func(*specs.Context)
}

type engine struct {
	name string
	run  func(t *testing.T, rep report.EventReporter, cases []contractCase)
}

var engines = []engine{
	{"Spec.It", func(t *testing.T, rep report.EventReporter, cases []contractCase) {
		specs.DescribeWithReporter(t, "suite", rep, func(s *specs.Spec) {
			for _, c := range cases {
				s.It(c.name, c.body)
			}
		})
	}},
	{"Spec.ItParallel", func(t *testing.T, rep report.EventReporter, cases []contractCase) {
		specs.DescribeWithReporter(t, "suite", rep, func(s *specs.Spec) {
			for _, c := range cases {
				s.ItParallel(c.name, c.body)
			}
		})
	}},
	{"Builder.It", func(t *testing.T, rep report.EventReporter, cases []contractCase) {
		b := specs.NewBuilder()
		b.Describe("suite", func() {
			for _, c := range cases {
				b.It(c.name, c.body)
			}
		})
		specs.NewRunnerWithReporter(b.Build(), "suite", rep).Run(t)
	}},
	{"Builder.ItParallel", func(t *testing.T, rep report.EventReporter, cases []contractCase) {
		b := specs.NewBuilder()
		b.Describe("suite", func() {
			for _, c := range cases {
				b.ItParallel(c.name, c.body)
			}
		})
		specs.NewRunnerWithReporter(b.Build(), "suite", rep).Run(t)
	}},
}

// recorder is a report.EventReporter that keeps the finished specs.
type recorder struct {
	mu       sync.Mutex
	finished []report.SpecResultEvent
}

func (r *recorder) SuiteStarted(report.SuiteStartEvent) {}
func (r *recorder) SuiteFinished(report.SuiteEndEvent)  {}
func (r *recorder) SpecStarted(report.SpecStartEvent)   {}
func (r *recorder) SpecFinished(e report.SpecResultEvent) {
	r.mu.Lock()
	r.finished = append(r.finished, e)
	r.mu.Unlock()
}

var _ report.EventReporter = (*recorder)(nil)

func forEachEngine(t *testing.T, fn func(t *testing.T, e engine)) {
	t.Helper()
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) { fn(t, e) })
	}
}

// --- passing scenarios: run in this process on every engine ---

var passingCases = []contractCase{
	{"counts", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		st := store{ctrl}
		ctrl.Method("Put").Expect("a", mock.Any()).Times(2)
		ctrl.Method("Put").Expect("b", mock.Any()).AtLeast(1).AtMost(3)
		ctrl.Method("Put").Expect("never", mock.Any()).Never()
		ctrl.Method("Put").Expect("any", mock.Any()).AnyTimes()

		for range 2 {
			_ = st.Put("a", "1")
		}
		for range 3 {
			_ = st.Put("b", "2")
		}
		ctx.Expect(len(ctrl.Method("Put").Calls())).ToEqual(5)
		// Verify runs by itself at cleanup; the case fails there if a count is off.
	}},
	{"arguments", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		st := store{ctrl}
		bg := context.Background()
		// First matching expectation with capacity takes the call.
		ctrl.Method("Get").Expect(mock.Any(), "a").Times(1).Return("first", nil)
		ctrl.Method("Get").Expect(mock.Any(), mock.MatchT("a non-empty key", func(k string) bool { return k != "" })).AnyTimes().Return("rest", nil)

		v1, _ := st.Get(bg, "a")
		v2, _ := st.Get(bg, "a")
		v3, _ := st.Get(bg, "zz")
		ctx.Expect([]string{v1, v2, v3}).ToEqual([]string{"first", "rest", "rest"})
	}},
	{"captures", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		st := store{ctrl}
		keys := mock.NewCaptor[string]()
		ctrl.Method("Put").Expect(keys.Matcher(), mock.Any()).Times(1)
		ctrl.Method("Put").Expect(mock.Any(), mock.Any()).AnyTimes()

		_ = st.Put("k1", "v")
		_ = st.Put("k2", "v")
		_ = st.Put("k3", "v")
		// A captor records only the call its expectation claimed.
		ctx.Expect(keys.Values()).ToEqual([]string{"k1"})
		ctx.Expect(keys.Last()).ToEqual("k1")
	}},
	{"global order", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		st := store{ctrl}
		audit := ctrl.Spy("audit")
		put := ctrl.Method("Put").Expect("k", "v")
		get := ctrl.Method("Get").Expect(mock.Any(), "k").Return("v", nil)
		ctrl.InOrder(put, get)

		_ = st.Put("k", "v")
		audit.Call("saved")
		_, _ = st.Get(context.Background(), "k")

		var seq []string
		for _, rc := range ctrl.Calls() {
			seq = append(seq, fmt.Sprintf("%d:%s", rc.Seq, rc.Method))
		}
		ctx.Expect(seq).ToEqual([]string{"1:Put", "2:audit", "3:Get"})
	}},
	{"stubbing", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		st := store{ctrl}
		bg := context.Background()
		boom := fmt.Errorf("boom")
		ctrl.Method("Get").Expect(mock.Any(), "seq").Times(3).Return("one", nil).Return("two", nil).Return("", boom)
		ctrl.Method("Get").Expect(mock.Any(), "repeat").Times(3).Return("same", nil)
		ctrl.Method("Get").Expect(mock.Any(), "do").Times(1).Return("ignored", nil).Do(func(args []any) []any {
			return []any{"do:" + args[1].(string), nil}
		})
		ctrl.Method("Put").Expect(mock.Any(), mock.Any()).Times(1).Return(boom)

		var got []string
		for range 3 {
			v, err := st.Get(bg, "seq")
			got = append(got, fmt.Sprint(v, "/", err))
		}
		for range 3 {
			v, _ := st.Get(bg, "repeat")
			got = append(got, v)
		}
		v, _ := st.Get(bg, "do")
		got = append(got, v)
		ctx.Expect(got).ToEqual([]string{"one/<nil>", "two/<nil>", "/boom", "same", "same", "same", "do:do"})
		ctx.Expect(st.Put("k", "v")).ToEqual(boom)
	}},
	{"concurrent ctx.Go", func(ctx *specs.Context) {
		const workers, per = 8, 25
		ctrl := mock.NewController(ctx)
		st := store{ctrl}
		ctrl.Method("Put").Expect(mock.Any(), mock.Any()).Times(workers * per)
		for w := range workers {
			ctx.Go(func(*specs.Context) {
				for i := range per {
					_ = st.Put("w"+strconv.Itoa(w), strconv.Itoa(i))
				}
			})
		}
	}},
}

// checkDenseSequence fails t unless calls carry the sequence numbers 1..len(calls), each once, in order.
func checkDenseSequence(t *testing.T, calls []mock.RecordedCall) {
	t.Helper()
	for i, rc := range calls {
		if rc.Seq != uint64(i+1) {
			t.Fatalf("call %d has Seq %d, want %d: the global sequence must be dense and ordered", i, rc.Seq, i+1)
		}
	}
}

func TestContractPassingScenariosOnEveryEngine(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e engine) {
		rep := &recorder{}
		e.run(t, rep, passingCases)
		t.Cleanup(func() {
			if len(rep.finished) != len(passingCases) {
				t.Errorf("%d specs finished, want %d", len(rep.finished), len(passingCases))
			}
			for _, f := range rep.finished {
				if f.Failed {
					t.Errorf("spec %q failed: %s", f.Name, f.Message)
				}
			}
		})
	})
}

// TestContractConcurrentParallelSpecsShareOneController drives one controller from many parallel specs
// at once: the global sequence stays dense and every call is counted.
func TestContractConcurrentParallelSpecsShareOneController(t *testing.T) {
	const specsN, per = 6, 50
	for _, e := range engines {
		if !strings.Contains(e.name, "Parallel") {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			ctrl := mock.NewController(t)
			st := store{ctrl}
			ctrl.Method("Put").Expect(mock.Any(), mock.Any()).Times(specsN * per)
			cases := make([]contractCase, specsN)
			for i := range cases {
				cases[i] = contractCase{fmt.Sprintf("caller %d", i), func(*specs.Context) {
					for j := range per {
						_ = st.Put("k", strconv.Itoa(j))
					}
				}}
			}
			t.Cleanup(func() {
				calls := ctrl.Calls()
				if len(calls) != specsN*per {
					t.Errorf("recorded %d calls, want %d", len(calls), specsN*per)
				}
				checkDenseSequence(t, calls)
			})
			e.run(t, &recorder{}, cases)
		})
	}
}

// --- scenarios that fail on purpose: run in a subprocess on a real *testing.T ---

var failingCases = []contractCase{
	{"unmet", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		ctrl.Method("Put").Expect("k", "v") // DECL_UNMET
	}},
	{"fatal", func(ctx *specs.Context) {
		ctrl := mock.NewController(probeTB{ctx, "fatal"})
		ctrl.Method("Put").Expect("k", "v") // DECL_FATAL
		ctx.Expect(1).ToEqual(2)
	}},
	{"panic", func(ctx *specs.Context) {
		ctrl := mock.NewController(probeTB{ctx, "panic"})
		ctrl.Method("Put").Expect("k", "v") // DECL_PANIC
		panic("body-boom")
	}},
	{"unexpected", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		st := store{ctrl}
		ctrl.Method("Get").Expect(mock.Any(), "a").Return("x", nil) // DECL_UNEXPECTED
		_, _ = st.Get(context.Background(), "b")
		_, _ = st.Get(context.Background(), "a")
	}},
	{"no expectations", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		_ = store{ctrl}.Put("k", "v")
	}},
	{"capacity", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		st := store{ctrl}
		ctrl.Method("Put").Expect("k", "v") // DECL_CAPACITY
		_ = st.Put("k", "v")
		_ = st.Put("k", "v")
	}},
	{"order", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		st := store{ctrl}
		put := ctrl.Method("Put").Expect("k", "v")                         // DECL_ORDER_PUT
		get := ctrl.Method("Get").Expect(mock.Any(), "k").Return("v", nil) // DECL_ORDER_GET
		ctrl.InOrder(put, get)
		_, _ = st.Get(context.Background(), "k")
		_ = st.Put("k", "v")
	}},
}

const contractEnv = "GO_SPECS_MOCK_CONTRACT"

func TestContractFailures(t *testing.T) {
	if name := os.Getenv(contractEnv); name != "" {
		for _, e := range engines {
			if e.name == name {
				rep := &recorder{}
				e.run(t, rep, failingCases)
				for _, f := range rep.finished {
					fmt.Printf("RESULT %s failed=%v msg=%q\n", f.Name, f.Failed, f.Message)
				}
			}
		}
		return
	}
	forEachEngine(t, func(t *testing.T, e engine) { checkFailureOutput(t, e.name, runFailures(t, e.name)) })
}

func runFailures(t *testing.T, engineName string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^TestContractFailures$")
	cmd.Env = append(os.Environ(), contractEnv+"="+engineName)
	raw, err := cmd.CombinedOutput()
	out := string(raw)
	if err == nil {
		t.Fatalf("expected the run to fail (every case fails on purpose):\n%s", out)
	}
	if regexp.MustCompile(`(?m)^panic: `).MatchString(out) {
		t.Fatalf("the run crashed the test binary:\n%s", out)
	}
	return out
}

// markerLine returns the 1-based line of this file that carries the marker comment.
func markerLine(t *testing.T, marker string) int {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range strings.Split(string(src), "\n") {
		if strings.HasSuffix(strings.TrimSpace(l), "// "+marker) {
			return i + 1
		}
	}
	t.Fatalf("marker %s not found", marker)
	return 0
}

func checkFailureOutput(t *testing.T, engineName, out string) {
	t.Helper()
	result := func(name string) string {
		m := regexp.MustCompile(`(?m)^RESULT ` + regexp.QuoteMeta(name) + ` (.*)$`).FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("no RESULT for case %q:\n%s", name, out)
		}
		// The message was printed with %q: turn it back into the text the reporter received.
		if i := strings.Index(m[1], " msg="); i >= 0 {
			if msg, err := strconv.Unquote(m[1][i+len(" msg="):]); err == nil {
				return m[1][:i] + " msg=" + msg
			}
		}
		return m[1]
	}
	contains := func(what, hay string, wants ...string) {
		t.Helper()
		for _, w := range wants {
			if !strings.Contains(hay, w) {
				t.Errorf("%s: missing %q in:\n%s", what, w, hay)
			}
		}
	}
	decl := func(marker string) string { return "contract_test.go:" + strconv.Itoa(markerLine(t, marker)) }

	for _, c := range failingCases {
		if r := result(c.name); !strings.Contains(r, "failed=true") {
			t.Errorf("case %q must fail: %s", c.name, r)
		}
	}

	// Unmet expectations: method, arguments, count and declaration site; SpecResultEvent carries them.
	contains("unmet", result("unmet"),
		"mock: unmet expectation Put(", `equal to "k"`, "want 1, got 0", "declared at "+decl("DECL_UNMET"))

	// Failure cleanup: Verify still runs, and reports the unmet expectation, although the body failed
	// fatally or panicked. The probe sees it on every engine; Builder.ItParallel then keeps only the
	// first failure of a spec as its message (the engine's contract), the others show it as well.
	for label, marker := range map[string]string{"fatal": "DECL_FATAL", "panic": "DECL_PANIC"} {
		probe := regexp.MustCompile(`(?m)^PROBE ` + label + ` (.*)$`).FindStringSubmatch(out)
		if probe == nil {
			t.Errorf("%s: Verify did not report after the body failed:\n%s", label, out)
			continue
		}
		contains("failure cleanup "+label, probe[1], "mock: unmet expectation Put(", "want 1, got 0", "declared at "+decl(marker))
	}
	if r := result("fatal"); !strings.Contains(r, "failed=true") {
		t.Errorf("fatal: %s", r)
	}
	if r := result("panic"); !strings.Contains(r, "failed=true") {
		t.Errorf("panic: %s", r)
	}

	// Unexpected call: method, argument, why each expectation did not match, declaration site.
	contains("unexpected", result("unexpected"),
		`mock: unexpected call Get(context.Background, "b")`, `argument 1: got "b", want equal to "a"`, "declared at "+decl("DECL_UNEXPECTED"))
	contains("no expectations", result("no expectations"),
		`unexpected call Put("k", "v")`, "no expectations declared for this method")
	contains("capacity", result("capacity"),
		"unexpected call Put(", "matched but at capacity (want 1, got 1)", "declared at "+decl("DECL_CAPACITY"))
	contains("order", result("order"),
		"mock: order violation: expected Put(", "before Get(", "observed sequence: #1 Get(", "#2 Put(")

	// Attribution: a call reported through mock points at the adapter line, never into mock/. On real
	// testing.T engines this is testing's own "file:line:" prefix (Helper marking through
	// ctx.Testing()); on Builder.ItParallel it is the stack walk that skips go-specs and mock frames.
	getLine, putLine := markerLine(t, "ADAPTER_GET"), markerLine(t, "ADAPTER_PUT")
	adapterGet := "contract_test.go:" + strconv.Itoa(getLine) + ": mock: unexpected call Get("
	adapterPut := "contract_test.go:" + strconv.Itoa(putLine) + ": mock: unexpected call Put("
	contains("attribution", out, adapterGet, adapterPut)
	for _, bad := range []string{"controller.go:", "expectation.go:", "counts.go:", "stub.go:"} {
		if strings.Contains(out, bad) {
			t.Errorf("a report is attributed into the mock package (%q):\n%s", bad, out)
		}
	}
}
