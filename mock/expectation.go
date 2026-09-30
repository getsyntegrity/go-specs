package mock

import (
	"fmt"
	"runtime"
	"strings"
)

const mockPkgPrefix = "github.com/getsyntegrity/go-specs/mock."

// Expectation declares how one call shape of a Method is expected to happen. It is created by
// Method.Expect and configured by chaining.
//
// Extension points for later work (all state is guarded by the owning Controller's mutex):
//   - counts: min/max is the accepted call count range (max < 0 means unbounded) and kind records
//     how it was configured: countDefault (no count method ran: exactly 1), countExact (Times, Never
//     or AnyTimes set both bounds) or countRange (AtLeast/AtMost set one bound each). The kind is
//     stored explicitly so AtLeast/AtMost never infer it from the bound values.
//   - stubbing: respond builds the Result of a claimed call; Return/Do add their state here and
//     leave the calling code in Method.Call unchanged.
type Expectation struct {
	m        *Method
	matchers []ArgMatcher
	site     string

	kind     countKind
	min, max int

	responses [][]any           // Return: one entry per response, in call order
	do        func([]any) []any // Do: computes the results, wins over responses

	got      int    // calls that claimed this expectation
	firstSeq uint64 // global sequence of the first claiming call, 0 when none
}

// Times requires exactly n calls. Without a count method an expectation defaults to Times(1).
// It panics when n is negative.
func (e *Expectation) Times(n int) *Expectation {
	if e == nil {
		return nil
	}
	if n < 0 {
		panic(fmt.Sprintf("mock: Times requires n >= 0, got %d", n))
	}
	e.setCount(n, n)
	return e
}

// countKind says how an expectation's bounds were configured.
type countKind uint8

const (
	countDefault countKind = iota // no count method ran: exactly 1
	countExact                    // Times, Never or AnyTimes set both bounds
	countRange                    // AtLeast/AtMost built the range one bound at a time
)

// setCount installs a complete configuration (Times, Never, AnyTimes), replacing any earlier one.
func (e *Expectation) setCount(min, max int) {
	e.m.c.mu.Lock()
	e.kind, e.min, e.max = countExact, min, max
	e.m.c.mu.Unlock()
}

// bounds returns the effective accepted call range; the caller holds the controller lock.
func (e *Expectation) bounds() (min, max int) {
	if e.kind == countDefault {
		return 1, 1
	}
	return e.min, e.max
}

// hasCapacity reports whether one more call may claim e; the caller holds the controller lock.
func (e *Expectation) hasCapacity() bool {
	_, max := e.bounds()
	return max < 0 || e.got < max
}

// mismatch returns why args do not match, or "" when they do. It only reads immutable fields and
// runs user matchers, so it may run without the controller lock.
func (e *Expectation) mismatch(args []any) string {
	if len(args) != len(e.matchers) {
		return fmt.Sprintf("got %d argument(s), want %d", len(args), len(e.matchers))
	}
	for i, m := range e.matchers {
		if m == nil || !m.Match(args[i]) {
			return fmt.Sprintf("argument %d: got %s, want %s", i, formatValue(args[i]), describeMatcher(m))
		}
	}
	return ""
}

// describe renders the expectation as Method(matcher, ...).
func (e *Expectation) describe() string {
	parts := make([]string, len(e.matchers))
	for i, m := range e.matchers {
		parts[i] = describeMatcher(m)
	}
	return e.m.name + "(" + strings.Join(parts, ", ") + ")"
}

// countText renders the wanted count range for diagnostics; the caller holds the controller lock.
func (e *Expectation) countText() string {
	min, max := e.bounds()
	switch {
	case max == 0:
		return "never"
	case max < 0 && min == 0:
		return "any number"
	case max < 0:
		return fmt.Sprintf("at least %d", min)
	case min == max:
		return fmt.Sprintf("%d", min)
	case min == 0:
		return fmt.Sprintf("at most %d", max)
	default:
		return fmt.Sprintf("%d..%d", min, max)
	}
}

// declarationSite returns file:line of the first frame outside package mock (test files of the mock
// package itself count as user code).
func declarationSite() string {
	pcs := make([]uintptr, 16)
	n := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		if !strings.HasPrefix(f.Function, mockPkgPrefix) || strings.HasSuffix(f.File, "_test.go") {
			return fmt.Sprintf("%s:%d", shortPath(f.File), f.Line)
		}
		if !more {
			return "unknown"
		}
	}
}

func shortPath(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// formatValue renders a call argument for diagnostics.
func formatValue(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	return fmt.Sprintf("%v", v)
}

func describeMatcher(m ArgMatcher) string {
	switch d := m.(type) {
	case nil:
		return "nil matcher"
	case fmt.Stringer:
		return d.String()
	}
	return fmt.Sprintf("%T", m)
}
