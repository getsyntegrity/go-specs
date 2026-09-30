// Package property runs properties, invariants that must hold for every generated input, with
// go-specs assertions, shrinking and exact replay.
//
// The generation, shrinking and replay engine is pgregory.net/rapid; this package adapts it. It adds
// four things rapid does not report on its own: assertions that use the go-specs matchers
// (assert.Evaluate), a per-input failure state so one failing input can never mark another failed,
// a Result that keeps the counterexample and tells apart a rejected input, an assertion failure and a
// panic, and options instead of process-wide flags. Generators are rapid's own; build them with
// pgregory.net/rapid and draw from them with Draw.
//
// This is a separate Go module so that the core go-specs module does not depend on the engine. See
// docs/PROPERTY_TESTING.md for the decision, the API and how to run it in CI.
package property

import (
	"flag"
	"fmt"
	"reflect"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/assert"
	"pgregory.net/rapid"
)

// Outcome is how a property run ended.
type Outcome int

const (
	// Passed: every generated input satisfied the property.
	Passed Outcome = iota
	// Failed: an assertion failed. Result.Counterexample holds the shrunk input.
	Failed
	// Panicked: the property panicked. Result.Panic holds the value and stack.
	Panicked
	// Exhausted: too few valid inputs were generated because the property rejected the rest. It is
	// not a failure of the invariant, and not a pass either: the invariant was not exercised enough.
	Exhausted
)

// String returns the lower-case name of o.
func (o Outcome) String() string {
	switch o {
	case Passed:
		return "passed"
	case Failed:
		return "failed"
	case Panicked:
		return "panicked"
	case Exhausted:
		return "exhausted"
	}
	return "outcome(" + strconv.Itoa(int(o)) + ")"
}

// Drawn is one value taken from a generator during a run, rendered with %#v.
type Drawn struct {
	Label string
	Value string
}

// PanicInfo describes a panic raised by the property.
type PanicInfo struct {
	Value string
	Stack string
}

// Result is what Run returns. Seed, FailFile and the draws are only meaningful when Outcome is
// Failed or Panicked.
type Result struct {
	Outcome Outcome
	// Checks is the number of valid inputs that ran before the first failure, and Rejected the number
	// of inputs the property rejected in the same period.
	Checks   int
	Rejected int
	// Seed reproduces the failure: pass it to WithSeed. It is 0 when the failure came from a fail
	// file, which FailFile names.
	Seed     uint64
	FailFile string
	// Original is the first failing input found, Counterexample the smallest one shrinking reached.
	Original       []Drawn
	Counterexample []Drawn
	// Failures holds the assertion messages of the counterexample run only.
	Failures []string
	Panic    *PanicInfo
	// Report is a human-readable summary: the outcome, counts, the counterexample, and how to replay.
	Report string
}

// T is the handle a property receives. Its failure state belongs to one generated input: a fresh T
// state is made for every run of the property, including each step of shrinking.
type T struct {
	rt *rapid.T
	st *runState
}

// runState is the state of one execution of the property.
type runState struct {
	draws    []Drawn
	failures []string
	panic    *PanicInfo
	rejected bool
}

// Draw takes a value from g and records it, under label, for the counterexample. An empty label
// becomes "draw#N".
func Draw[V any](p *T, g *rapid.Generator[V], label string) V {
	if label == "" {
		label = "draw#" + strconv.Itoa(len(p.st.draws)+1)
	}
	v := g.Draw(p.rt, label)
	p.st.draws = append(p.st.draws, Drawn{Label: label, Value: fmt.Sprintf("%#v", v)})
	return v
}

// Reject marks the current input as not applicable to the property. It stops the run, counts it as
// rejected, and is never a failure.
func (p *T) Reject(format string, args ...any) {
	p.st.rejected = true
	p.rt.Skipf(format, args...)
}

// Assume rejects the current input unless ok is true.
func (p *T) Assume(ok bool) {
	if !ok {
		p.Reject("assumption not met")
	}
}

// Logf logs to the engine output, which is shown for the final counterexample run only.
func (p *T) Logf(format string, args ...any) { p.rt.Logf(format, args...) }

// Expectation is an assertion handle created by T.Expect.
type Expectation struct {
	p      *T
	actual any
}

// Expect starts an assertion on actual, evaluated with the go-specs matchers.
func (p *T) Expect(actual any) Expectation { return Expectation{p: p, actual: actual} }

// To fails the current run, and ends it, when m does not match.
func (e Expectation) To(m assert.Matcher) {
	ok, msg := assert.Evaluate(m, e.actual)
	if ok {
		return
	}
	if _, file, line, found := runtime.Caller(1); found {
		msg = fmt.Sprintf("%s:%d: %s", shortFile(file), line, msg)
	}
	e.p.st.failures = append(e.p.st.failures, msg)
	e.p.rt.Fatalf("%s", msg)
}

// ToEqual fails the current run, and ends it, unless actual equals expected.
func (e Expectation) ToEqual(expected any) {
	ok, msg := assert.Evaluate(assert.Equal(expected), e.actual)
	if ok {
		return
	}
	if _, file, line, found := runtime.Caller(1); found {
		msg = fmt.Sprintf("%s:%d: %s", shortFile(file), line, msg)
	}
	e.p.st.failures = append(e.p.st.failures, msg)
	e.p.rt.Fatalf("%s", msg)
}

func shortFile(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

type config struct {
	name       string
	checks     int
	steps      int
	seed       uint64
	shrinkTime time.Duration
	failFile   string
	noFailFile bool
}

// Option configures Run and Check.
type Option func(*config)

// WithChecks sets how many valid inputs to generate. The engine default is 100, or the value of
// -rapid.checks, and is divided by 5 under -short.
func WithChecks(n int) Option { return func(c *config) { c.checks = n } }

// WithSteps sets the average number of Repeat actions.
func WithSteps(n int) Option { return func(c *config) { c.steps = n } }

// WithSeed fixes the generation seed, which makes a run deterministic. 0 keeps the engine's random
// seed. Pass Result.Seed to replay a failure.
func WithSeed(seed uint64) Option { return func(c *config) { c.seed = seed } }

// WithShrinkTime bounds the time spent minimising a failing input.
func WithShrinkTime(d time.Duration) Option { return func(c *config) { c.shrinkTime = d } }

// WithName names the property. The name selects the corpus directory testdata/rapid/<name>, so two
// properties must not share one. Check defaults it to the test name; Run to "property".
func WithName(name string) Option { return func(c *config) { c.name = name } }

// WithFailFile replays one corpus file before generating any input.
func WithFailFile(path string) Option { return func(c *config) { c.failFile = path } }

// WithoutFailFile stops a failure from being written to testdata/rapid.
func WithoutFailFile() Option { return func(c *config) { c.noFailFile = true } }

// TB is the part of *testing.T that Check uses.
type TB interface {
	Helper()
	Name() string
	Errorf(format string, args ...any)
}

// Check runs prop and reports a falsified, panicking or exhausted property on tb with
// tb.Errorf, including the counterexample and how to replay it.
func Check(tb TB, prop func(*T), opts ...Option) {
	tb.Helper()
	all := append([]Option{WithName(tb.Name())}, opts...)
	res := Run(prop, all...)
	if res.Outcome != Passed {
		tb.Errorf("%s", res.Report)
	}
}

// Fuzz adapts prop to a native fuzz target, for coverage-guided campaigns:
//
//	func FuzzX(f *testing.F) { f.Fuzz(property.Fuzz(prop)) }
//
// Under plain `go test` the target replays its f.Add seeds and testdata/fuzz/FuzzX, which is fast and
// deterministic; `go test -fuzz=FuzzX -fuzztime=...` runs the search. Shrinking of the byte input is
// the native engine's, and a failure is reported on the test by the engine, not as a Result.
func Fuzz(prop func(*T)) func(*testing.T, []byte) {
	return rapid.MakeFuzz(func(rt *rapid.T) {
		prop(&T{rt: rt, st: &runState{}})
	})
}

// engineMu serialises runs: the engine reads its configuration from process-wide flags, so two runs
// with different options must not overlap.
var engineMu sync.Mutex

// Run runs prop against generated inputs and returns the outcome. It never fails a test itself.
// Runs are serialised because the engine is configured through process-wide flags, which Run sets
// for its duration and restores afterwards; flags given on the command line (-rapid.checks and the
// like) apply to every option that is not set.
func Run(prop func(*T), opts ...Option) Result {
	cfg := config{name: "property"}
	for _, o := range opts {
		o(&cfg)
	}

	engineMu.Lock()
	defer engineMu.Unlock()
	restore := cfg.apply()
	defer restore()

	r := &runner{prop: prop}
	tb := &engineTB{name: cfg.name}
	func() {
		defer func() {
			if rec := recover(); rec != nil && rec != errStop {
				panic(rec)
			}
		}()
		rapid.Check(tb, r.exec)
	}()
	return r.result(cfg, tb)
}

func (c *config) apply() (restore func()) {
	var undo []func()
	set := func(name, value string) {
		f := flag.Lookup(name)
		if f == nil {
			panic("property: engine flag " + name + " is missing; pgregory.net/rapid changed its flags")
		}
		old := f.Value.String()
		if err := f.Value.Set(value); err != nil {
			panic("property: cannot set " + name + ": " + err.Error())
		}
		undo = append(undo, func() { _ = f.Value.Set(old) })
	}
	if c.checks > 0 {
		set("rapid.checks", strconv.Itoa(c.checks))
	}
	if c.steps > 0 {
		set("rapid.steps", strconv.Itoa(c.steps))
	}
	if c.seed != 0 {
		set("rapid.seed", strconv.FormatUint(c.seed, 10))
	}
	if c.shrinkTime > 0 {
		set("rapid.shrinktime", c.shrinkTime.String())
	}
	if c.failFile != "" {
		set("rapid.failfile", c.failFile)
	}
	if c.noFailFile {
		set("rapid.nofailfile", "true")
	}
	return func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
}

// runner observes every execution of the property: the search, the shrinking and the final replay.
type runner struct {
	prop func(*T)

	valid, rejected int
	sawFailure      bool
	original        *runState
	lastFailing     *runState
}

func (r *runner) exec(rt *rapid.T) {
	st := &runState{}
	defer func() {
		rec := recover()
		r.finish(st, rec)
		if rec != nil {
			panic(rec)
		}
	}()
	r.prop(&T{rt: rt, st: st})
}

// finish classifies one execution. rec is the panic that ended it, if any.
func (r *runner) finish(st *runState, rec any) {
	switch {
	case rec != nil && !fromEngine(rec):
		st.panic = &PanicInfo{Value: fmt.Sprint(rec), Stack: string(debug.Stack())}
	case rec != nil && len(st.failures) == 0:
		// The engine's own stop for a skipped input or an exhausted generator filter.
		st.rejected = true
	}

	failing := st.panic != nil || len(st.failures) > 0
	if failing {
		if r.original == nil {
			r.original = st
		}
		r.lastFailing = st
		r.sawFailure = true
		return
	}
	if r.sawFailure {
		return // a shrinking candidate that no longer fails
	}
	if st.rejected {
		r.rejected++
	} else {
		r.valid++
	}
}

// fromEngine reports whether rec is one of the panics rapid uses to stop a run (a fatal assertion
// or a skipped input), as opposed to a panic raised by the property.
func fromEngine(rec any) bool {
	t := reflect.TypeOf(rec)
	return t != nil && t.PkgPath() == "pgregory.net/rapid"
}

var (
	seedRe     = regexp.MustCompile(`-rapid\.seed=(\d+)`)
	failFileRe = regexp.MustCompile(`-rapid\.failfile=("(?:[^"\\]|\\.)*")`)
)

func (r *runner) result(cfg config, tb *engineTB) Result {
	res := Result{Checks: r.valid, Rejected: r.rejected}
	if !tb.failed {
		res.Outcome = Passed
		res.Report = fmt.Sprintf("property %q passed %d checks (%d rejected)", cfg.name, r.valid, r.rejected)
		return res
	}
	if m := seedRe.FindStringSubmatch(tb.message); m != nil {
		res.Seed, _ = strconv.ParseUint(m[1], 10, 64)
	}
	if m := failFileRe.FindStringSubmatch(tb.message); m != nil {
		res.FailFile, _ = strconv.Unquote(m[1])
	}
	if r.lastFailing == nil {
		res.Outcome = Exhausted
		res.Seed, res.FailFile = 0, ""
	} else {
		res.Original = r.original.draws
		res.Counterexample = r.lastFailing.draws
		res.Failures = r.lastFailing.failures
		res.Panic = r.lastFailing.panic
		res.Outcome = Failed
		if res.Panic != nil {
			res.Outcome = Panicked
		}
	}
	res.Report = report(cfg, res)
	return res
}

// errStop is the panic an engineTB raises to stop rapid.Check once it has failed the test.
var errStop = new(int)

// engineTB is the testing.TB-like value rapid reports to. It records instead of failing a test.
type engineTB struct {
	name    string
	failed  bool
	message string
}

func (e *engineTB) Helper()                   {}
func (e *engineTB) Name() string              { return e.name }
func (e *engineTB) Logf(string, ...any)       {}
func (e *engineTB) Log(...any)                {}
func (e *engineTB) Skipf(string, ...any)      { panic("property: engine called Skipf on its test") }
func (e *engineTB) Skip(...any)               { panic("property: engine called Skip on its test") }
func (e *engineTB) SkipNow()                  { panic("property: engine called SkipNow on its test") }
func (e *engineTB) Fatal(args ...any)         { e.Error(args...); e.FailNow() }
func (e *engineTB) Fatalf(f string, a ...any) { e.Errorf(f, a...); e.FailNow() }
func (e *engineTB) FailNow()                  { e.failed = true; panic(errStop) }
func (e *engineTB) Fail()                     { e.failed = true }
func (e *engineTB) Failed() bool              { return e.failed }
func (e *engineTB) Error(args ...any)         { e.Errorf("%s", fmt.Sprint(args...)) }
func (e *engineTB) Errorf(f string, a ...any) {
	e.failed = true
	if e.message == "" {
		e.message = fmt.Sprintf(f, a...)
	}
}
