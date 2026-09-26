package specs

import (
	"strings"
	"testing"
)

// incomparableHint is the part of the failure message that explains an incomparable dynamic type.
const incomparableHint = "is not comparable with ==; use ctx.Expect(...).ToEqual"

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
		hint             bool
	}{
		{"equal slices", []int{1}, []int{1}, true},
		{"different slices", []int{1}, []int{2}, true},
		{"equal maps", map[string]int{"x": 1}, map[string]int{"x": 1}, true},
		{"incomparable errors", dslSliceError{"a"}, dslSliceError{"a"}, true},
		{"different dynamic types", []int{1}, map[string]int{"x": 1}, false},
		{"different comparable values", 1, 2, false},
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
				if got := strings.Contains(message, incomparableHint); got != tc.hint {
					t.Errorf("%s message %q: contains hint=%v, want %v", label, message, got, tc.hint)
				}
			}
		})
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
