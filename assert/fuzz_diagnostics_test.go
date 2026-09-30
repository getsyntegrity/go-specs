package assert

import (
	"fmt"
	"reflect"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

// Fuzzing of the assertion diagnostics.
//
// Each target decodes its []byte input with fzBuild (fuzz_recipe_test.go) into a small value graph
// and builds diagnostics for it: EqualFailureMessage, the FailureMessage and Description of
// representative matchers and compositions, and PollResult.Message of Eventually and Consistently on
// a ManualClock. It checks five invariants:
//
//  1. Building a diagnostic terminates without a panic, within fzDeadline. A stack overflow is not a
//     panic and no recover catches it: the fixes of #364-#371 are what make it impossible, so the
//     target runs the diagnostic in this process, and the seeds, the committed corpus and every
//     regression also run in a child process (fuzz_subprocess_test.go) so one cannot kill the suite.
//  2. Output is bounded, and where an operand is certainly beyond every renderer's budget (a cycle, or
//     more than fzHugeNodes values) the text says so with a cycle, truncation or ellipsis marker.
//  3. The same graph gives the same text when its diagnostic is built again.
//  4. A graph rebuilt from the same recipe gives the same text, after hexadecimal addresses are
//     masked. EXCLUDED: a map keyed by a pointer (or channel or unsafe pointer) is ordered by address
//     by design (docs/DSL.md), so its text may legitimately differ between two builds; such a graph
//     (fzGraph.addrKeys) still gets invariants 1 to 3 and 5.
//  5. Verdicts and matcher evaluation counts are unaffected: a counting matcher sees Match once and
//     FailureMessage once on a failure, as Evaluate promises; the verdict agrees with
//     reflect.DeepEqual, errors identity and other references; the graph is not modified; and
//     Eventually/Consistently on a ManualClock make exactly the expected number of attempts.

// fzDeadline stops an iteration that does not return, the symptom of excessive traversal. It is far
// above any legitimate iteration, which takes milliseconds.
const fzDeadline = 10 * time.Second

// fzOperand is a value a message is expected to print, with its size class computed once.
type fzOperand struct {
	v     any
	huge  bool
	maybe bool // the message may or may not print it: it counts toward the length bound only
}

func fzOp(v any) fzOperand { return fzOperand{v: v, huge: fzInfinite(v)} }

// fzMaybe marks operands a message may print, for a composition whose text depends on which child failed.
func fzMaybe(ops ...fzOperand) []fzOperand {
	for i := range ops {
		ops[i].maybe = true
	}
	return ops
}

func fzOps(vs ...any) []fzOperand {
	out := make([]fzOperand, len(vs))
	for i, v := range vs {
		out[i] = fzOp(v)
	}
	return out
}

// fzDiag collects what one run of a diagnostic produced: its texts, in a fixed order, and the
// invariants it broke.
type fzDiag struct {
	texts    []string
	problems []string
}

func (d *fzDiag) bad(format string, args ...any) {
	d.problems = append(d.problems, fmt.Sprintf(format, args...))
}

// fzMarkers are the ways a diagnostic says it left something out: the cycle and truncation markers,
// the ambiguity marker that stands for a tied map value, and the ellipses of the depth and element limits.
var fzMarkers = []string{cycleMarker, truncationMarker, ambiguousValueMarker, "...", "…"}

func fzHasMarker(s string) bool {
	for _, m := range fzMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// msg records one diagnostic text and checks invariant 2 on it. rendered lists the operands the
// message prints; a huge one must leave a marker, and is what caps the length.
func (d *fzDiag) msg(label, text string, rendered ...fzOperand) {
	d.texts = append(d.texts, label+"\x00"+text)
	limit, huge := 64<<10, false
	for _, op := range rendered {
		if op.huge {
			huge = huge || !op.maybe
			limit += 4 << 20 // 10,000 nodes at 256 bytes and a little punctuation each
		} else {
			limit += 16 << 20 // an ordinary value is printed in full: 10,000 nodes of up to 1,024 bytes
		}
	}
	if len(text) > limit {
		d.bad("%s: %d bytes exceeds the bound of %d", label, len(text), limit)
	}
	if huge && !fzHasMarker(text) {
		d.bad("%s: an operand beyond the render budget was printed without a cycle or truncation marker: %.300q", label, text)
	}
}

// fzCounting wraps a matcher and counts the calls it receives.
type fzCounting struct {
	inner    Matcher
	matches  int
	failures int
}

func (c *fzCounting) Match(actual any) bool { c.matches++; return c.inner.Match(actual) }
func (c *fzCounting) FailureMessage(actual any) string {
	c.failures++
	return c.inner.FailureMessage(actual)
}
func (c *fzCounting) Description() string { return describeMatcher(c.inner) }

var fzAddr = regexp.MustCompile(`0x[0-9a-fA-F]{4,}`)

func fzNormalize(s string) string { return fzAddr.ReplaceAllString(s, "0xADDR") }

// fzGuard runs fn with a deadline and turns a panic or a hang into a recorded problem.
func fzGuard(fn func() *fzDiag) *fzDiag {
	done := make(chan *fzDiag, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- &fzDiag{problems: []string{fmt.Sprintf("panic while building a diagnostic: %v\n%s", r, debug.Stack())}}
			}
		}()
		done <- fn()
	}()
	select {
	case d := <-done:
		return d
	case <-time.After(fzDeadline):
		return &fzDiag{problems: []string{fmt.Sprintf("excessive traversal: the diagnostic did not return within %v", fzDeadline)}}
	}
}

// fzSame compares two runs of the same diagnostic and reports the first text that differs.
func fzSame(what string, a, b *fzDiag) []string {
	if len(a.texts) != len(b.texts) {
		return []string{fmt.Sprintf("%s: %d texts against %d", what, len(a.texts), len(b.texts))}
	}
	for i := range a.texts {
		x, y := fzNormalize(a.texts[i]), fzNormalize(b.texts[i])
		if x == y {
			continue
		}
		at := 0
		for at < len(x) && at < len(y) && x[at] == y[at] {
			at++
		}
		from := max(0, at-60)
		return []string{fmt.Sprintf("%s: text %q differs at byte %d\n first:  %q\n second: %q",
			what, strings.SplitN(x, "\x00", 2)[0], at, x[from:min(len(x), at+120)], y[from:min(len(y), at+120)])}
	}
	return nil
}

// fzCheck runs one diagnostic over a recipe and checks all five invariants. It returns the problems.
func fzCheck(data []byte, run func(*fzGraph) *fzDiag) []string {
	g := fzBuild(data)
	first := fzGuard(func() *fzDiag { return run(g) })
	problems := append([]string(nil), first.problems...)
	if len(problems) > 0 {
		return problems
	}
	again := fzGuard(func() *fzDiag { return run(g) }) // invariant 3: the same graph
	problems = append(problems, again.problems...)
	problems = append(problems, fzSame("same graph, built again", first, again)...)
	if !g.addrKeys { // invariant 4, see the header for the pointer-key exclusion
		rebuilt := fzBuild(data)
		third := fzGuard(func() *fzDiag { return run(rebuilt) })
		problems = append(problems, third.problems...)
		problems = append(problems, fzSame("graph rebuilt from the same recipe", first, third)...)
	}
	return problems
}

func fzFuzz(t *testing.T, data []byte, run func(*fzGraph) *fzDiag) {
	t.Helper()
	if len(data) > fzMaxData {
		t.Skipf("recipe of %d bytes exceeds %d", len(data), fzMaxData)
	}
	for _, p := range fzCheck(data, run) {
		t.Error(p)
	}
}

// --- targets ------------------------------------------------------------------------------------

func FuzzEqualFailureMessage(f *testing.F) {
	for _, s := range fzSeeds() {
		f.Add(s.data)
	}
	f.Fuzz(func(t *testing.T, data []byte) { fzFuzz(t, data, fzDiagEqual) })
}

func FuzzMatcherDiagnostics(f *testing.F) {
	for _, s := range fzSeeds() {
		f.Add(s.data)
	}
	f.Fuzz(func(t *testing.T, data []byte) { fzFuzz(t, data, fzDiagMatchers) })
}

func FuzzPollMessage(f *testing.F) {
	for _, s := range fzSeeds() {
		f.Add(s.data)
	}
	f.Fuzz(func(t *testing.T, data []byte) { fzFuzz(t, data, fzDiagPoll) })
}

// fzTargets maps a fuzz target name to its diagnostic, for the subprocess runner.
var fzTargets = map[string]func(*fzGraph) *fzDiag{
	"FuzzEqualFailureMessage": fzDiagEqual,
	"FuzzMatcherDiagnostics":  fzDiagMatchers,
	"FuzzPollMessage":         fzDiagPoll,
}

// --- references ---------------------------------------------------------------------------------

// fzRefEqual is the reference verdict of Equal(expected).Match(actual): errors.Is identity for two
// errors (the fixed test error type is a pointer with no Unwrap or Is, so identity is ==), and
// reflect.DeepEqual for everything else.
func fzRefEqual(expected, actual any) bool {
	ee, eok := expected.(error)
	ae, aok := actual.(error)
	if eok && aok {
		return ee == ae
	}
	return reflect.DeepEqual(expected, actual)
}

func fzHasLen(v any) bool {
	switch reflect.ValueOf(v).Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.String, reflect.Chan:
		return true
	}
	return false
}

func fzIsNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// --- EqualFailureMessage --------------------------------------------------------------------------

func fzDiagEqual(g *fzGraph) *fzDiag {
	d := &fzDiag{}
	actual := g.actual()
	expected, twin := g.operandB(g.tail())
	ops := fzOps(actual, expected)
	shape, twinShape := g.shape(), twin.shape()
	want := fzRefEqual(expected, actual)
	if got := ValuesEqual(expected, actual); got != want {
		d.bad("ValuesEqual = %v, the reference says %v", got, want)
	}

	msg := EqualFailureMessage(expected, actual)
	d.msg("EqualFailureMessage", msg, ops...)
	if ops[0].huge || ops[1].huge {
		// Either side beyond the budget: both are printed by the diff renderer, at most 80 runes each,
		// followed by at most 10 difference lines.
		if len(msg) > 48<<10 {
			d.bad("EqualFailureMessage of an oversized operand is %d bytes, want under 48 KiB", len(msg))
		}
	}

	cm := &fzCounting{inner: Equal(expected)}
	matched, failure := Evaluate(cm, actual)
	if matched != want {
		d.bad("Evaluate(Equal) = %v, the reference says %v", matched, want)
	}
	wantFailures := 1
	if matched {
		wantFailures = 0
	}
	if cm.matches != 1 || cm.failures != wantFailures {
		d.bad("Equal was evaluated with Match x%d, FailureMessage x%d; want x1 and x%d", cm.matches, cm.failures, wantFailures)
	}
	if !matched {
		d.msg("Equal/failure", failure, ops...)
		if failure != msg {
			d.bad("the Equal matcher's failure differs from EqualFailureMessage")
		}
	}
	d.texts = append(d.texts, "diff\x00"+structuralDiff(expected, actual))

	if got := ValuesEqual(expected, actual); got != want {
		d.bad("building the diagnostic changed the verdict: ValuesEqual is now %v, was %v", got, want)
	}
	if after := g.shape(); !reflect.DeepEqual(after, shape) {
		d.bad("building the diagnostic modified the graph: %v -> %v", shape, after)
	}
	if after := twin.shape(); !reflect.DeepEqual(after, twinShape) {
		d.bad("building the diagnostic modified the twin graph: %v -> %v", twinShape, after)
	}
	return d
}

// --- matchers and compositions ----------------------------------------------------------------------

// fzAtom is one representative matcher: how to make a fresh instance, the reference verdict when one
// exists apart from the matcher itself, and the operands its messages print.
type fzAtom struct {
	name      string
	make      func() Matcher
	ref       func() (verdict, known bool)
	renders   func() []fzOperand // the operands FailureMessage prints
	descRends func() []fzOperand // the operands Description prints
}

// fzComposition is a Not, All or Any over atoms, with the evaluation counts Evaluate must produce.
type fzComposition struct {
	name    string
	build   func(children ...Matcher) Matcher
	atoms   []int
	verdict func(v []bool) bool
	// counts returns, for each child, how many times Match and FailureMessage must have been called.
	counts  func(v []bool) (matches, failures []int)
	renders func() []fzOperand // operands the failure message prints
	// descRenders are the operands the composition's Description prints: only its children's.
	descRenders func() []fzOperand
}

func fzDiagMatchers(g *fzGraph) *fzDiag {
	d := &fzDiag{}
	actual := g.actual()
	tl := g.tail()
	other, twin := g.operandB(tl)
	lenN := int(tl.next()) % 40
	flag := tl.next()%2 == 0
	shape, twinShape := g.shape(), twin.shape()
	eqBefore := ValuesEqual(other, actual)

	var target error
	if e, ok := other.(error); ok {
		target = e
	} else {
		target = &fzErr{v: other}
	}
	opA, opB, opT := fzOp(actual), fzOp(other), fzOp(target)

	elems := []any{other, actual}
	atoms := []fzAtom{
		{name: "Equal", make: func() Matcher { return Equal(other) },
			ref:       func() (bool, bool) { return fzRefEqual(other, actual), true },
			renders:   func() []fzOperand { return []fzOperand{opA, opB} },
			descRends: func() []fzOperand { return []fzOperand{opB} }},
		{name: "HaveLen", make: func() Matcher { return HaveLen(lenN) },
			ref: func() (bool, bool) {
				rv := reflect.ValueOf(actual)
				if !fzHasLen(actual) || rv.Kind() == reflect.Array || rv.Kind() == reflect.Chan {
					return false, false
				}
				return rv.Len() == lenN, true
			},
			renders: func() []fzOperand {
				if fzHasLen(actual) {
					return []fzOperand{opA}
				}
				return nil
			}},
		{name: "HaveKey(operand)", make: func() Matcher { return HaveKey(other) },
			ref: func() (bool, bool) { return false, false },
			renders: func() []fzOperand {
				if reflect.ValueOf(actual).Kind() == reflect.Map {
					return []fzOperand{opA, opB}
				}
				return nil
			},
			descRends: func() []fzOperand { return []fzOperand{opB} }},
		{name: "HaveKey(k1)", make: func() Matcher { return HaveKey("k1") },
			ref: func() (bool, bool) {
				if m, ok := actual.(map[string]any); ok {
					_, present := m["k1"]
					return present, true
				}
				return false, false
			},
			renders: func() []fzOperand {
				if reflect.ValueOf(actual).Kind() == reflect.Map {
					return []fzOperand{opA}
				}
				return nil
			}},
		{name: "ContainAllOf", make: func() Matcher { return ContainAllOf(elems...) },
			ref: func() (bool, bool) {
				s, ok := actual.([]any)
				if !ok {
					return false, false
				}
				for _, e := range elems {
					found := false
					for _, x := range s {
						if ValuesEqual(e, x) {
							found = true
							break
						}
					}
					if !found {
						return false, true
					}
				}
				return true, true
			},
			renders: func() []fzOperand {
				if isCollectionOrString(actual) {
					return []fzOperand{opA, opB}
				}
				return nil
			},
			descRends: func() []fzOperand { return []fzOperand{opA, opB} }},
		{name: "BeOneOf", make: func() Matcher { return BeOneOf(elems...) },
			ref: func() (bool, bool) {
				for _, e := range elems {
					if fzRefEqual(e, actual) {
						return true, true
					}
				}
				return false, true
			},
			renders:   func() []fzOperand { return []fzOperand{opA, opA, opB} },
			descRends: func() []fzOperand { return []fzOperand{opA, opB} }},
		{name: "Satisfy", make: func() Matcher { return Satisfy("the flag", func(any) bool { return flag }) },
			ref:     func() (bool, bool) { return flag, true },
			renders: func() []fzOperand { return []fzOperand{opA} }},
		{name: "BeNil", make: func() Matcher { return BeNil() },
			ref:     func() (bool, bool) { return fzIsNil(actual), true },
			renders: func() []fzOperand { return []fzOperand{opA} }},
		{name: "MatchError", make: func() Matcher { return MatchError(target) },
			ref: func() (bool, bool) {
				e, ok := actual.(error)
				return ok && e != nil && e == target, true
			},
			renders: func() []fzOperand {
				if actual == nil {
					return []fzOperand{opT}
				}
				return []fzOperand{opT, opA}
			},
			descRends: func() []fzOperand { return []fzOperand{opT} }},
	}

	verdicts := make([]bool, len(atoms))
	for i, a := range atoms {
		verdicts[i] = d.checkAtom(a, actual, opA, opB)
	}

	byName := map[string]int{}
	for i, a := range atoms {
		byName[a.name] = i
	}
	i := func(name string) int { return byName[name] }
	notRenders := func() []fzOperand { return []fzOperand{opA, opB} }
	onlyB := func() []fzOperand { return []fzOperand{opB} }
	compositions := []fzComposition{
		{name: "Not(Equal)", build: func(c ...Matcher) Matcher { return Not(c[0]) }, atoms: []int{i("Equal")},
			verdict: func(v []bool) bool { return !v[0] },
			counts:  func(v []bool) ([]int, []int) { return []int{1}, []int{0} },
			renders: notRenders, descRenders: onlyB},
		{name: "All(HaveLen, Equal, Satisfy)", build: func(c ...Matcher) Matcher { return All(c...) },
			renders: func() []fzOperand { return fzMaybe(opA, opA, opA, opB) },
			atoms:   []int{i("HaveLen"), i("Equal"), i("Satisfy")},
			verdict: func(v []bool) bool { return v[0] && v[1] && v[2] },
			counts: func(v []bool) ([]int, []int) {
				f := make([]int, len(v))
				for k := range v {
					if !v[k] {
						f[k] = 1
					}
				}
				return []int{1, 1, 1}, f
			},
			descRenders: onlyB},
		{name: "Any(BeNil, Equal, BeOneOf)", build: func(c ...Matcher) Matcher { return Any(c...) },
			renders: func() []fzOperand { return fzMaybe(opA, opA, opA, opA, opB, opB) },
			atoms:   []int{i("BeNil"), i("Equal"), i("BeOneOf")},
			verdict: func(v []bool) bool { return v[0] || v[1] || v[2] },
			counts: func(v []bool) ([]int, []int) {
				m, f := make([]int, len(v)), make([]int, len(v))
				for k := range v {
					m[k] = 1
					if !v[k] {
						f[k] = 1
					}
					if v[k] {
						break // Any stops at the first match
					}
				}
				return m, f
			},
			descRenders: func() []fzOperand { return []fzOperand{opA, opB} }},
		{name: "Not(All(Equal, BeNil))", build: func(c ...Matcher) Matcher { return Not(All(c...)) },
			atoms:   []int{i("Equal"), i("BeNil")},
			verdict: func(v []bool) bool { return !v[0] || !v[1] },
			counts: func(v []bool) ([]int, []int) { // Not calls All.Match, which stops at the first failure
				if v[0] {
					return []int{1, 1}, []int{0, 0}
				}
				return []int{1, 0}, []int{0, 0}
			},
			renders: notRenders, descRenders: onlyB},
	}
	for _, c := range compositions {
		d.checkComposition(c, atoms, verdicts, actual)
	}

	if got := ValuesEqual(other, actual); got != eqBefore {
		d.bad("building diagnostics changed ValuesEqual from %v to %v", eqBefore, got)
	}
	if after := g.shape(); !reflect.DeepEqual(after, shape) {
		d.bad("building diagnostics modified the graph: %v -> %v", shape, after)
	}
	if after := twin.shape(); !reflect.DeepEqual(after, twinShape) {
		d.bad("building diagnostics modified the twin graph: %v -> %v", twinShape, after)
	}
	return d
}

// checkAtom exercises one matcher and returns its verdict.
func (d *fzDiag) checkAtom(a fzAtom, actual any, opA, opB fzOperand) bool {
	raw := a.make()
	verdict := raw.Match(actual)
	if ref, known := a.ref(); known && ref != verdict {
		d.bad("%s: Match = %v, the reference says %v", a.name, verdict, ref)
	}
	var renders []fzOperand
	if a.renders != nil {
		renders = a.renders()
	}
	d.msg(a.name+"/FailureMessage", raw.FailureMessage(actual), renders...)
	var descRenders []fzOperand
	if a.descRends != nil {
		descRenders = a.descRends()
	}
	d.msg(a.name+"/Description", describeMatcher(raw), descRenders...)

	cm := &fzCounting{inner: a.make()}
	matched, failure := Evaluate(cm, actual)
	wantFailures := 1
	if matched {
		wantFailures = 0
	}
	if matched != verdict {
		d.bad("%s: Evaluate = %v, Match = %v", a.name, matched, verdict)
	}
	if cm.matches != 1 || cm.failures != wantFailures {
		d.bad("%s: Match x%d and FailureMessage x%d, want x1 and x%d", a.name, cm.matches, cm.failures, wantFailures)
	}
	if !matched {
		d.msg(a.name+"/Evaluate", failure, renders...)
	}
	if again := a.make().Match(actual); again != verdict {
		d.bad("%s: the verdict changed from %v to %v after the messages were built", a.name, verdict, again)
	}
	return verdict
}

func (d *fzDiag) checkComposition(c fzComposition, atoms []fzAtom, verdicts []bool, actual any) {
	children := make([]*fzCounting, len(c.atoms))
	inner := make([]Matcher, len(c.atoms))
	v := make([]bool, len(c.atoms))
	for k, ai := range c.atoms {
		children[k] = &fzCounting{inner: atoms[ai].make()}
		inner[k] = children[k]
		v[k] = verdicts[ai]
	}
	comp := c.build(inner...)
	matched, failure := Evaluate(comp, actual)
	if want := c.verdict(v); matched != want {
		d.bad("%s: Evaluate = %v, the children say %v", c.name, matched, want)
	}
	wantMatches, wantFailures := c.counts(v)
	for k, ch := range children {
		if ch.matches != wantMatches[k] || ch.failures != wantFailures[k] {
			d.bad("%s: child %d saw Match x%d and FailureMessage x%d, want x%d and x%d",
				c.name, k+1, ch.matches, ch.failures, wantMatches[k], wantFailures[k])
		}
	}
	var renders []fzOperand
	if c.renders != nil {
		renders = c.renders()
	}
	if !matched {
		d.msg(c.name+"/Evaluate", failure, renders...)
		raw := make([]Matcher, len(c.atoms))
		for k, ai := range c.atoms {
			raw[k] = atoms[ai].make()
		}
		d.msg(c.name+"/FailureMessage", c.build(raw...).FailureMessage(actual), renders...)
	}
	rawForDesc := make([]Matcher, len(c.atoms))
	for k, ai := range c.atoms {
		rawForDesc[k] = atoms[ai].make()
	}
	var descRenders []fzOperand
	if c.descRenders != nil {
		descRenders = c.descRenders()
	}
	d.msg(c.name+"/Description", describeMatcher(c.build(rawForDesc...)), descRenders...)
}

// --- Eventually and Consistently ----------------------------------------------------------------------

func fzDiagPoll(g *fzGraph) *fzDiag {
	d := &fzDiag{}
	rd := g.tail()
	consistently := rd.next()%2 == 1
	interval := time.Duration(1+int(rd.next())%3) * time.Millisecond
	// The callback advances the clock by at least one interval, so the attempt timer always fires and
	// the poll never waits for a time that only the callback could bring.
	advance := interval + time.Duration(int(rd.next())%3)*time.Millisecond
	timeout := time.Duration(1+int(rd.next())%12) * time.Millisecond
	at := int(rd.next()) % 6      // Eventually: first attempt that matches; Consistently: first that fails; 0 never
	panicAt := int(rd.next()) % 8 // the attempt on which the callback panics; 0 never
	actual, panicValue := g.actual(), g.expected()
	shape := g.shape()

	clock := NewManualClock()
	attempts := 0
	fn := func() any {
		attempts++
		clock.Advance(advance)
		if panicAt > 0 && attempts == panicAt {
			panic(panicValue)
		}
		return actual
	}
	evals := 0
	pred := func(any) bool {
		evals++
		if consistently {
			return at == 0 || evals < at
		}
		return at > 0 && evals >= at
	}
	cm := &fzCounting{inner: Satisfy("the poll predicate", pred)}
	opts := []PollOption{WithClock(clock), WithTimeout(timeout), WithInterval(interval)}
	var res PollResult
	if consistently {
		res = Consistently(fn, cm, opts...)
	} else {
		res = Eventually(fn, cm, opts...)
	}

	// The expected attempt count, from the clock alone: the deadline is due once attempts*advance has
	// reached the timeout; the earliest of a panic, a matcher outcome and that deadline ends the poll.
	deadline := int((timeout + advance - 1) / advance)
	wantAttempts, want := deadline, TerminatedTimeout
	if consistently {
		want = TerminatedHeld
		if at > 0 && at <= wantAttempts {
			wantAttempts, want = at, TerminatedMismatch
		}
	} else if at > 0 && at <= wantAttempts {
		wantAttempts, want = at, TerminatedMatched
	}
	if panicAt > 0 && panicAt <= wantAttempts {
		wantAttempts, want = panicAt, TerminatedPanic
	}
	wantEvals := wantAttempts
	if want == TerminatedPanic {
		wantEvals-- // the callback panicked before the matcher was asked
	}
	wantFailures := wantEvals
	switch {
	case consistently && want == TerminatedMismatch:
		wantFailures = 1
	case consistently:
		wantFailures = 0
	case want == TerminatedMatched:
		wantFailures--
	}
	if res.Termination != want || res.Attempts != wantAttempts || attempts != wantAttempts {
		d.bad("poll ended %v after %d attempts (callback ran %d), want %v after %d", res.Termination, res.Attempts, attempts, want, wantAttempts)
	}
	if evals != wantEvals || cm.matches != wantEvals || cm.failures != wantFailures {
		d.bad("matcher saw Match x%d (predicate x%d) and FailureMessage x%d, want x%d and x%d", cm.matches, evals, cm.failures, wantEvals, wantFailures)
	}
	if wantElapsed := time.Duration(wantAttempts) * advance; res.Elapsed != wantElapsed {
		d.bad("elapsed %v, want %v", res.Elapsed, wantElapsed)
	}
	if res.Passed != (want == TerminatedMatched || want == TerminatedHeld) {
		d.bad("Passed = %v for %v", res.Passed, want)
	}

	// A recovered panic carries a stack that names goroutines and addresses; the text under test is the rest.
	res.Stack = ""
	text := res.Message()
	if again := res.Message(); again != text {
		d.bad("Message differs between two calls on one result")
	}
	if !res.Passed {
		var rendered []fzOperand
		if want == TerminatedPanic {
			rendered = append(rendered, fzOp(panicValue))
			if panicAt > 1 {
				rendered = append(rendered, fzOp(actual))
			}
		} else {
			rendered = fzOps(actual)
		}
		d.msg("PollResult.Message", text, rendered...)
	} else if text != "" {
		d.bad("a passing result has a message: %.200q", text)
	}
	if after := g.shape(); !reflect.DeepEqual(after, shape) {
		d.bad("the poll modified the graph: %v -> %v", shape, after)
	}
	return d
}
