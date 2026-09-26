package specs

import (
	"strings"
	"testing"
)

// deepHint and errorHint are the parts of the failure message that explain an incomparable dynamic
// type. A non-error value is pointed at ctx.Expect(...).ToEqual, which compares it with
// reflect.DeepEqual. An error is not: ctx.Expect asks errors.Is for errors too, and errors.Is has
// already said no by the time the message is built, so the remedy is an Is method on the type.
const (
	deepHint  = "is not comparable with ==; use ctx.Expect(...).ToEqual for a deep comparison"
	errorHint = "is not comparable with == and errors.Is found no match; give"
)

// failureOf runs assertion against a capturing backend and returns whether it failed and with what
// message. A panic is reported as a test error: the typed path must never panic on a comparison.
func failureOf(t *testing.T, label string, assertion func(*Context)) (failed bool, message string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", label, r)
			failed = true
		}
	}()
	backend := &capturingBackend{}
	assertion(&Context{backend: backend})
	return backend.failed, backend.message
}

// Issue #238: for an interface T, == panics when both values hold the same incomparable dynamic type.
// The typed path reports that comparison as not equal, so the failure must say why — otherwise it
// reads "expected [1] to equal [1]" — and name the assertion that compares such values.
func TestTypedEqualityExplainsIncomparableDynamicTypes(t *testing.T) {
	cases := []struct {
		name             string
		actual, expected any
		hint             string // "" when the message must carry neither hint
	}{
		{"equal slices", []int{1}, []int{1}, deepHint},
		{"different slices", []int{1}, []int{2}, deepHint},
		{"equal maps", map[string]int{"x": 1}, map[string]int{"x": 1}, deepHint},
		{"different dynamic types", []int{1}, map[string]int{"x": 1}, ""},
		{"different comparable values", 1, 2, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, e := tc.actual, tc.expected
			probes := map[string]func(*Context){
				"EqualTo":         func(c *Context) { EqualTo(c, a, e) },
				"ExpectT.ToEqual": func(c *Context) { ExpectT(c, a).ToEqual(e) },
			}
			for label, probe := range probes {
				failed, message := failureOf(t, label, probe)
				if !failed {
					t.Errorf("%s passed, want a failure", label)
					continue
				}
				checkHint(t, label, message, tc.hint)
			}
		})
	}
}

// checkHint fails unless message carries exactly the hint named by want, and never the other one.
func checkHint(t *testing.T, label, message, want string) {
	t.Helper()
	for _, hint := range []string{deepHint, errorHint} {
		if got := strings.Contains(message, hint); got != (hint == want) {
			t.Errorf("%s message %q: contains %q=%v, want %v", label, message, hint, got, hint == want)
		}
	}
}

// Codex review on #260: for errors, ctx.Expect(...).ToEqual asks errors.Is, not reflect.DeepEqual,
// so pointing there would not help — two dslSliceError{"a"} fail through ctx.Expect too. The message
// for an incomparable error must name the remedy that does work instead.
func TestIncomparableErrorFailureDoesNotRecommendCtxExpect(t *testing.T) {
	var a, e error = dslSliceError{"a"}, dslSliceError{"a"}
	if outcome(t, "Expect.ToEqual", func(c *Context) { c.Expect(a).ToEqual(e) }) {
		t.Fatal("ctx.Expect(...).ToEqual passed; the premise of this test no longer holds")
	}
	const want = "expected [a] to equal [a], but dynamic type specs.dslSliceError is not comparable with == " +
		"and errors.Is found no match; give specs.dslSliceError an Is method to define its equality"
	probes := map[string]func(*Context){
		"EqualTo":         func(c *Context) { EqualTo(c, a, e) },
		"ExpectT.ToEqual": func(c *Context) { ExpectT(c, a).ToEqual(e) },
	}
	for label, probe := range probes {
		if _, got := failureOf(t, label, probe); got != want {
			t.Errorf("%s message = %q, want %q", label, got, want)
		}
	}
}

// docs/DSL.md and CHANGELOG.md quote this message verbatim; keep them in step with it.
func TestIncomparableFailureMessageIsTheDocumentedOne(t *testing.T) {
	const want = "expected [1] to equal [1], but dynamic type []int is not comparable with ==; " +
		"use ctx.Expect(...).ToEqual for a deep comparison"
	var a, e any = []int{1}, []int{1}
	if _, got := failureOf(t, "EqualTo", func(c *Context) { EqualTo(c, a, e) }); got != want {
		t.Errorf("EqualTo message = %q, want %q", got, want)
	}
	if _, got := failureOf(t, "ExpectT.ToEqual", func(c *Context) { ExpectT(c, a).ToEqual(e) }); got != want {
		t.Errorf("ExpectT.ToEqual message = %q, want %q", got, want)
	}
}

// The hint must not hide the errors.Is fallback: an incomparable error whose Is method matches still
// passes, exactly as ctx.Expect(...).ToEqual does.
func TestIncomparableErrorWithMatchingIsStillPasses(t *testing.T) {
	var a, e error = dslCodesError{7, 1}, dslCodesError{7, 2}
	if failed, message := failureOf(t, "EqualTo", func(c *Context) { EqualTo(c, a, e) }); failed {
		t.Errorf("EqualTo failed with %q, want a pass through errors.Is", message)
	}
	if failed, message := failureOf(t, "ExpectT.ToEqual", func(c *Context) { ExpectT(c, a).ToEqual(e) }); failed {
		t.Errorf("ExpectT.ToEqual failed with %q, want a pass through errors.Is", message)
	}
}
