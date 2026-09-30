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
	"sync/atomic"
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
	{"task controller satisfied", func(ctx *specs.Context) {
		ctx.Go(func(task *specs.Context) {
			ctrl := mock.NewController(task)
			ctrl.Method("Save").Expect("x").Return(nil)
			ctrl.Method("Save").Call("x")
		})
	}},
	{"task controller unused with AnyTimes", func(ctx *specs.Context) {
		ctx.Go(func(task *specs.Context) {
			ctrl := mock.NewController(task)
			ctrl.Method("Save").Expect("x").AnyTimes()
		})
	}},
	{"count bounds green", func(ctx *specs.Context) {
		for _, calls := range []int{3, 5} { // AtLeast(2).AtMost(5).AtLeast(3) is 3..5
			ctrl := mock.NewController(ctx)
			ctrl.Method("Put").Expect(mock.Any(), mock.Any()).AtLeast(2).AtMost(5).AtLeast(3)
			putN(store{ctrl}, calls)
			ctrl.Verify()
		}
		for _, calls := range []int{2, 3} { // AtMost(5).AtLeast(2).AtMost(3) is 2..3
			ctrl := mock.NewController(ctx)
			ctrl.Method("Put").Expect(mock.Any(), mock.Any()).AtMost(5).AtLeast(2).AtMost(3)
			putN(store{ctrl}, calls)
			ctrl.Verify()
		}
		// Zero bounds: no call is the only way to satisfy them.
		ctrl := mock.NewController(ctx)
		ctrl.Method("Put").Expect("a", mock.Any()).AtMost(0)
		ctrl.Method("Put").Expect("b", mock.Any()).AtLeast(0).AtMost(0)
		ctrl.Method("Put").Expect("c", mock.Any()).Never()
		ctrl.Verify()
	}},
	{"count bound declaration panics", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		e := ctrl.Method("Put").Expect(mock.Any(), mock.Any()).AtMost(0)
		msg := func() (msg string) {
			defer func() { msg = fmt.Sprint(recover()) }()
			e.AtLeast(1)
			return "no panic"
		}()
		ctx.Expect(strings.Contains(msg, "AtLeast(1) exceeds the maximum of 0")).To(specs.BeTrue())
		// The panicking call left the expectation unchanged: still "never".
		ctx.Expect(len(ctrl.Method("Put").Calls())).ToEqual(0)
	}},
}

// putN makes n calls to Put through the adapter.
func putN(st store, n int) {
	for range n {
		_ = st.Put("k", "v")
	}
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

// --- prohibitions over permissive expectations ---

// prohibitionSpellings are the ways to declare an expectation whose effective maximum is 0.
var prohibitionSpellings = []struct {
	name  string
	apply func(*mock.Expectation) *mock.Expectation
}{
	{"Never()", func(e *mock.Expectation) *mock.Expectation { return e.Never() }},
	{"Times(0)", func(e *mock.Expectation) *mock.Expectation { return e.Times(0) }},
	{"AtMost(0)", func(e *mock.Expectation) *mock.Expectation { return e.AtMost(0) }},
	{"AtLeast(0).AtMost(0)", func(e *mock.Expectation) *mock.Expectation { return e.AtLeast(0).AtMost(0) }},
}

// prohibitionCase declares the prohibition on Put("protected", any) and a permissive fallback for every
// Put, in either declaration order. The fallback's Do and its captor announce themselves on stdout so
// the contract can see whether they ran. With callProtected the body calls the protected key first,
// then an allowed one.
func prohibitionCase(name string, apply func(*mock.Expectation) *mock.Expectation, prohibitionFirst, callProtected bool) contractCase {
	return contractCase{name, func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		st := store{ctrl}
		keys := mock.NewCaptor[string]()
		prohibit := func() {
			apply(ctrl.Method("Put").Expect("protected", mock.Any())) // DECL_PROHIB
		}
		fallback := func() {
			ctrl.Method("Put").Expect(keys.Matcher(), mock.Any()).AnyTimes().Do(func(args []any) []any {
				fmt.Printf("FALLBACK_RAN %s %v\n", name, args[0])
				return []any{nil}
			})
		}
		if prohibitionFirst {
			prohibit()
			fallback()
		} else {
			fallback()
			prohibit()
		}
		if callProtected {
			_ = st.Put("protected", "v")
		}
		_ = st.Put("ok", "v")
		fmt.Printf("CAPTURED %s %v\n", name, keys.Values())
	}}
}

// prohibitionCases builds one case per spelling and declaration order.
func prohibitionCases(callProtected bool) []contractCase {
	var cases []contractCase
	for _, sp := range prohibitionSpellings {
		for _, first := range []bool{true, false} {
			order := "fallback first"
			if first {
				order = "prohibition first"
			}
			cases = append(cases, prohibitionCase("prohibition "+sp.name+" "+order, sp.apply, first, callProtected))
		}
	}
	return cases
}

// TestContractProhibitionGreenControls: the same setups, calling only allowed arguments, pass on
// every engine and the fallback answers.
func TestContractProhibitionGreenControls(t *testing.T) {
	forEachEngine(t, func(t *testing.T, e engine) {
		rep := &recorder{}
		e.run(t, rep, prohibitionCases(false))
		if want := len(prohibitionSpellings) * 2; len(rep.finished) != want {
			t.Fatalf("finished %d specs, want %d", len(rep.finished), want)
		}
		for _, f := range rep.finished {
			if f.Failed {
				t.Errorf("green control %q failed: %s", f.Name, f.Message)
			}
		}
	})
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
	// P1b: a controller created inside a task with the task's Context. Its cleanup verification
	// still fails the spec.
	{"task unmet", func(ctx *specs.Context) {
		ctx.Go(func(task *specs.Context) {
			ctrl := mock.NewController(task)
			ctrl.Method("Save").Expect("x") // DECL_TASK_UNMET
		})
	}},
	{"task unexpected", func(ctx *specs.Context) {
		ctx.Go(func(task *specs.Context) {
			ctrl := mock.NewController(task)
			ctrl.Method("Save").Call("y")
		})
	}},
	// P2: count bounds through a real spec.
	{"bounds 3..5 with 2 calls", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		ctrl.Method("Put").Expect(mock.Any(), mock.Any()).AtLeast(2).AtMost(5).AtLeast(3) // DECL_B35_2
		putN(store{ctrl}, 2)
	}},
	{"bounds 3..5 with 6 calls", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		ctrl.Method("Put").Expect(mock.Any(), mock.Any()).AtLeast(2).AtMost(5).AtLeast(3) // DECL_B35_6
		putN(store{ctrl}, 6)
	}},
	{"bounds 2..3 with 1 call", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		ctrl.Method("Put").Expect(mock.Any(), mock.Any()).AtMost(5).AtLeast(2).AtMost(3) // DECL_B23_1
		putN(store{ctrl}, 1)
	}},
	{"bounds 2..3 with 4 calls", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		ctrl.Method("Put").Expect(mock.Any(), mock.Any()).AtMost(5).AtLeast(2).AtMost(3) // DECL_B23_4
		putN(store{ctrl}, 4)
	}},
	{"bounds AtMost(0) called", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		ctrl.Method("Put").Expect(mock.Any(), mock.Any()).AtMost(0) // DECL_B0_A
		putN(store{ctrl}, 1)
	}},
	{"bounds AtLeast(0).AtMost(0) called", func(ctx *specs.Context) {
		ctrl := mock.NewController(ctx)
		ctrl.Method("Put").Expect(mock.Any(), mock.Any()).AtLeast(0).AtMost(0) // DECL_B0_B
		putN(store{ctrl}, 1)
	}},
}

func init() { failingCases = append(failingCases, prohibitionCases(true)...) }

const contractEnv = "GO_SPECS_MOCK_CONTRACT"

// runInSubprocess runs cases on the engine named by contractEnv and prints one RESULT line per
// finished spec. It reports false when this process is not the subprocess.
func runInSubprocess(t *testing.T, cases []contractCase) bool {
	t.Helper()
	name := os.Getenv(contractEnv)
	if name == "" {
		return false
	}
	for _, e := range engines {
		if e.name == name {
			rep := &recorder{}
			e.run(t, rep, cases)
			for _, f := range rep.finished {
				fmt.Printf("RESULT %s failed=%v msg=%q\n", f.Name, f.Failed, f.Message)
			}
		}
	}
	return true
}

func TestContractFailures(t *testing.T) {
	if runInSubprocess(t, failingCases) {
		return
	}
	forEachEngine(t, func(t *testing.T, e engine) {
		checkFailureOutput(t, e.name, runSubprocess(t, e.name, "TestContractFailures"))
	})
}

// runSubprocess re-runs this test binary on one test, on one engine, and returns its combined output.
// Every case in it fails on purpose, so a passing run is itself a defect.
func runSubprocess(t *testing.T, engineName, test string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.v", "-test.run=^"+test+"$")
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

// resultOf returns the RESULT line of the named case, with its message unquoted, and fails unless the
// case finished exactly once (a corrupted report would duplicate or lose it).
func resultOf(t *testing.T, out, name string) string {
	t.Helper()
	all := regexp.MustCompile(`(?m)^RESULT `+regexp.QuoteMeta(name)+` (.*)$`).FindAllStringSubmatch(out, -1)
	if len(all) != 1 {
		t.Fatalf("case %q finished %d times, want exactly 1:\n%s", name, len(all), out)
	}
	m := all[0]
	// The message was printed with %q: turn it back into the text the reporter received.
	if i := strings.Index(m[1], " msg="); i >= 0 {
		if msg, err := strconv.Unquote(m[1][i+len(" msg="):]); err == nil {
			return m[1][:i] + " msg=" + msg
		}
	}
	return m[1]
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
	result := func(name string) string { return resultOf(t, out, name) }
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

	// Task-created controller (P1b): the unmet expectation and the unexpected call inside the task
	// both fail the spec and reach SpecResultEvent.Message with method, matcher and declaration site.
	contains("task unmet", result("task unmet"),
		"mock: unmet expectation Save(", `equal to "x"`, "want 1, got 0", "declared at "+decl("DECL_TASK_UNMET"))
	contains("task unexpected", result("task unexpected"),
		`mock: unexpected call Save("y")`, "no expectations declared for this method")

	// Count bounds (P2) through a real spec.
	contains("bounds 3..5 with 2 calls", result("bounds 3..5 with 2 calls"),
		"mock: unmet expectation Put(", "want 3..5, got 2", "declared at "+decl("DECL_B35_2"))
	contains("bounds 3..5 with 6 calls", result("bounds 3..5 with 6 calls"),
		"mock: unexpected call Put(", "matched but at capacity (want 3..5, got 5)", "declared at "+decl("DECL_B35_6"))
	contains("bounds 2..3 with 1 call", result("bounds 2..3 with 1 call"),
		"mock: unmet expectation Put(", "want 2..3, got 1", "declared at "+decl("DECL_B23_1"))
	contains("bounds 2..3 with 4 calls", result("bounds 2..3 with 4 calls"),
		"mock: unexpected call Put(", "matched but at capacity (want 2..3, got 3)", "declared at "+decl("DECL_B23_4"))
	contains("bounds AtMost(0) called", result("bounds AtMost(0) called"),
		"mock: forbidden call Put(", `Put("k", "v"): expectation Put(any value, any value) declared at `+decl("DECL_B0_A")+" says never")
	contains("bounds AtLeast(0).AtMost(0) called", result("bounds AtLeast(0).AtMost(0) called"),
		"mock: forbidden call Put(", `Put("k", "v"): expectation Put(any value, any value) declared at `+decl("DECL_B0_B")+" says never")

	// Prohibitions take precedence over permissive expectations, in either declaration order: the case
	// fails with the forbidden-call diagnostic (method, arguments, declaration site), reported once as
	// "forbidden call" (never "unexpected"), and neither the fallback's Do nor its captor saw the
	// protected call.
	for _, c := range prohibitionCases(true) {
		r := result(c.name)
		contains(c.name, r, `mock: forbidden call Put("protected", "v"): expectation Put(equal to "protected", any value) declared at `+decl("DECL_PROHIB")+" says never")
		if n := strings.Count(r, "forbidden call"); n != 1 {
			t.Errorf("%s: %d forbidden-call reports in the message, want 1:\n%s", c.name, n, r)
		}
		if strings.Contains(r, "unexpected call") {
			t.Errorf("%s: a prohibited call must not be reported as unexpected:\n%s", c.name, r)
		}
		ran := regexp.MustCompile(`(?m)^FALLBACK_RAN `+regexp.QuoteMeta(c.name)+` (.*)$`).FindAllStringSubmatch(out, -1)
		if len(ran) != 1 || ran[0][1] != "ok" {
			t.Errorf("%s: the fallback Do must run once, for \"ok\" only, got %v", c.name, ran)
		}
		if want := "CAPTURED " + c.name + " [ok]"; !strings.Contains(out, want) {
			t.Errorf("%s: the captor must hold only the allowed call, want %q in:\n%s", c.name, want, out)
		}
	}

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

// --- concurrent reports (P1a): a controller created with the spec's ctx, called from many ctx.Go tasks ---

const (
	concTasks = 16
	concCalls = 20
)

// unexpectedCounter forwards to the spec's Context and counts the "unexpected call" reports the
// mock makes, so the contract can compare what the mock reported with what the engine delivered.
type unexpectedCounter struct {
	*specs.Context
	n *atomic.Int64
}

func (u unexpectedCounter) Errorf(format string, args ...any) {
	if strings.Contains(fmt.Sprintf(format, args...), "unexpected call") {
		u.n.Add(1)
	}
	u.Context.Errorf(format, args...)
}

// concurrentCase builds a case whose tasks all call Save, which has no expectation, at the same time
// (a start gate makes them truly concurrent: CI's -race run checks this path). tail runs on the spec
// goroutine after the tasks were released and decides what else the case does wrong.
func concurrentCase(label string, tail func(ctx *specs.Context, ctrl *mock.Controller)) contractCase {
	return contractCase{label, func(ctx *specs.Context) {
		var n atomic.Int64
		ctrl := mock.NewController(unexpectedCounter{ctx, &n})
		ctx.Cleanup(func() { fmt.Printf("COUNT %s %d\n", label, n.Load()) })
		start := make(chan struct{})
		for w := range concTasks {
			ctx.Go(func(task *specs.Context) {
				<-start
				for i := range concCalls {
					ctrl.Method("Save").Call(label, w, i)
				}
				if w == 0 {
					tailTask(label, task)
				}
			})
		}
		close(start)
		tail(ctx, ctrl)
	}}
}

// tailTask lets one task fail an assertion of its own in the case that asks for it.
func tailTask(label string, task *specs.Context) {
	if label == "conc task assertion" {
		task.Expect("task-side").ToEqual("task-expected")
	}
}

var concurrentCases = []contractCase{
	concurrentCase("conc body assertion", func(ctx *specs.Context, _ *mock.Controller) {
		ctx.Expect("body-side").ToEqual("body-expected")
	}),
	concurrentCase("conc body expectation", func(_ *specs.Context, ctrl *mock.Controller) {
		ctrl.Method("Get").Expect(mock.Any(), "never-read") // never called: unmet at cleanup
	}),
	concurrentCase("conc task assertion", func(*specs.Context, *mock.Controller) {}),
}

func TestContractConcurrentReports(t *testing.T) {
	if runInSubprocess(t, concurrentCases) {
		return
	}
	forEachEngine(t, func(t *testing.T, e engine) {
		checkConcurrentOutput(t, e.name, runSubprocess(t, e.name, "TestContractConcurrentReports"))
	})
}

func checkConcurrentOutput(t *testing.T, engineName, out string) {
	t.Helper()
	const total = concTasks * concCalls
	keepsFirst := engineName == "Builder.ItParallel"
	for _, c := range concurrentCases {
		r := resultOf(t, out, c.name)
		if !strings.Contains(r, "failed=true") {
			t.Errorf("%s must fail: %s", c.name, r)
		}
		// The mock reported every call: nothing is lost before the engine.
		if want := fmt.Sprintf("COUNT %s %d\n", c.name, total); !strings.Contains(out, want) {
			t.Errorf("%s: the mock did not report %d unexpected calls (want line %q) in:\n%s", c.name, total, want, out)
		}
		// The engine delivers all of them, or keeps the first (Builder.ItParallel records one failure).
		delivered := len(regexp.MustCompile(`mock: unexpected call Save\("`+regexp.QuoteMeta(c.name)+`", `).FindAllString(out, -1))
		switch {
		case keepsFirst && delivered > 1:
			// The only occurrence is the kept message itself (RESULT line).
			t.Errorf("%s: %d reports reached an engine that keeps only the first failure", c.name, delivered)
		case keepsFirst && c.name == "conc body expectation" && delivered != 1:
			t.Errorf("%s: no unexpected-call report reached the backend (want the kept first failure)", c.name)
		case !keepsFirst && delivered != total:
			t.Errorf("%s: %d unexpected-call reports delivered, want exactly %d", c.name, delivered, total)
		}
		// Message rule (docs/DSL.md, ctx.Errorf): an assertion the spec goroutine recorded, else a
		// task's failed assertion, else a deferred Errorf.
		switch c.name {
		case "conc body assertion":
			if !strings.Contains(r, "body-expected") || strings.Contains(r, "mock:") {
				t.Errorf("%s: the message must be the body's failed assertion: %s", c.name, r)
			}
		case "conc task assertion":
			if !strings.Contains(r, "task-expected") || strings.Contains(r, "mock:") {
				t.Errorf("%s: the message must be the task's failed assertion: %s", c.name, r)
			}
		case "conc body expectation":
			if !strings.Contains(r, `mock: unexpected call Save("conc body expectation", `) &&
				!strings.Contains(r, "mock: unmet expectation Get(") {
				t.Errorf("%s: the message must be a mock diagnostic: %s", c.name, r)
			}
		}
	}
}
