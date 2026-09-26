package specs

import (
	"errors"
	"fmt"
	"testing"
)

// error_equality_complement_test.go pins the follow-up to #237: every equality-shaped API gives an
// error pair the same verdict, and every negative form (there is no ToNotEqual/NotEqualTo; the
// negatives are the NotEqual and Not(Equal) matchers, on both ctx.Expect and ExpectT) gives exactly
// the opposite one. It also pins how a typed nil pointer behaves once errors.Is is in the picture.

// dslWrapError unwraps through its field, so calling Unwrap on a nil *dslWrapError dereferences nil.
type dslWrapError struct{ inner error }

func (e *dslWrapError) Error() string { return "wrap: " + e.inner.Error() }
func (e *dslWrapError) Unwrap() error { return e.inner }

// dslKindError matches any *dslKindError of the same Kind, so two distinct pointers are errors.Is
// equal while == says no. Its Is reads the receiver's field, so it too dereferences a nil receiver.
type dslKindError struct{ Kind string }

func (e *dslKindError) Error() string { return "kind " + e.Kind }
func (e *dslKindError) Is(target error) bool {
	t, ok := target.(*dslKindError)
	return ok && t.Kind == e.Kind
}

// outcome runs one assertion against a fresh backend and reports whether it passed, turning a panic
// into a test error instead of letting it take the whole table down.
func outcome(t *testing.T, label string, assertion func(*Context)) (passed bool) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", label, r)
			passed = false
		}
	}()
	backend := &capturingBackend{}
	assertion(&Context{backend: backend})
	return !backend.failed
}

func TestErrorEqualityVerdictsAgreeAndNegativesAreTheirComplement(t *testing.T) {
	sentinel := errors.New("not found")
	impostor := errors.New("not found")
	wrapped := fmt.Errorf("repo: %w", sentinel)
	var typedNil *dslWrapError

	cases := []struct {
		name             string
		actual, expected error
		equal            bool
	}{
		{"wrapped sentinel", wrapped, sentinel, true},
		{"same sentinel", sentinel, sentinel, true},
		{"unrelated error sharing the message", impostor, sentinel, false},
		{"bare sentinel against its wrapper", sentinel, wrapped, false},
		{"nil against a sentinel", nil, sentinel, false},
		{"sentinel against nil", sentinel, nil, false},
		{"nil against nil", nil, nil, true},
		{"typed nil against a nil interface", typedNil, nil, false},
		{"nil interface against a typed nil", nil, typedNil, false},
		{"typed nil against a sentinel", typedNil, sentinel, false},
		{"sentinel against a typed nil", sentinel, typedNil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, e := tc.actual, tc.expected
			positives := map[string]func(*Context){
				"EqualTo":           func(c *Context) { EqualTo(c, a, e) },
				"ExpectT.ToEqual":   func(c *Context) { ExpectT(c, a).ToEqual(e) },
				"Expect.ToEqual":    func(c *Context) { c.Expect(a).ToEqual(e) },
				"Expect.To(Equal)":  func(c *Context) { c.Expect(a).To(Equal(e)) },
				"ExpectT.To(Equal)": func(c *Context) { ExpectT(c, a).To(Equal(e)) },
			}
			negatives := map[string]func(*Context){
				"Expect.To(NotEqual)":    func(c *Context) { c.Expect(a).To(NotEqual(e)) },
				"ExpectT.To(NotEqual)":   func(c *Context) { ExpectT(c, a).To(NotEqual(e)) },
				"Expect.To(Not(Equal))":  func(c *Context) { c.Expect(a).To(Not(Equal(e))) },
				"ExpectT.To(Not(Equal))": func(c *Context) { ExpectT(c, a).To(Not(Equal(e))) },
			}
			for label, assertion := range positives {
				if got := outcome(t, label, assertion); got != tc.equal {
					t.Errorf("%s passed=%v, want %v", label, got, tc.equal)
				}
			}
			for label, assertion := range negatives {
				if got := outcome(t, label, assertion); got != !tc.equal {
					t.Errorf("%s passed=%v, want %v (the complement of ToEqual)", label, got, !tc.equal)
				}
			}
		})
	}
}

// With a concrete error type as T, the fallback still runs: two distinct pointers that errors.Is
// relates through the type's Is method are equal, and the negatives say the opposite.
func TestTypedEqualityFallbackWorksForAConcreteErrorType(t *testing.T) {
	a, b := &dslKindError{Kind: "x"}, &dslKindError{Kind: "x"}
	other := &dslKindError{Kind: "y"}

	if !outcome(t, "ExpectT[*dslKindError].ToEqual", func(c *Context) { ExpectT(c, a).ToEqual(b) }) {
		t.Error("expected ExpectT(ctx, a).ToEqual(b) to pass through dslKindError.Is")
	}
	if !outcome(t, "EqualTo[*dslKindError]", func(c *Context) { EqualTo(c, a, b) }) {
		t.Error("expected EqualTo(ctx, a, b) to pass through dslKindError.Is")
	}
	if outcome(t, "ExpectT[*dslKindError].To(NotEqual)", func(c *Context) { ExpectT(c, a).To(NotEqual(b)) }) {
		t.Error("expected ExpectT(ctx, a).To(NotEqual(b)) to fail")
	}
	if outcome(t, "ExpectT[*dslKindError].ToEqual(other)", func(c *Context) { ExpectT(c, a).ToEqual(other) }) {
		t.Error("expected a different Kind not to match")
	}
}

// A typed nil pointer of a concrete error type must neither panic — its Is/Unwrap methods
// dereference the receiver — nor be taken as equal to a non-nil value of the same type.
func TestTypedEqualityTypedNilPointerNeitherPanicsNorMatches(t *testing.T) {
	var nilKind *dslKindError
	var nilWrap *dslWrapError
	sentinel := errors.New("boom")

	if outcome(t, "ExpectT[*dslKindError](nil).ToEqual(x)", func(c *Context) {
		ExpectT(c, nilKind).ToEqual(&dslKindError{Kind: "x"})
	}) {
		t.Error("expected a typed nil *dslKindError not to equal a non-nil one")
	}
	if outcome(t, "EqualTo[*dslWrapError](nil, x)", func(c *Context) {
		EqualTo(c, nilWrap, &dslWrapError{inner: sentinel})
	}) {
		t.Error("expected a typed nil *dslWrapError not to equal a non-nil one")
	}
	if !outcome(t, "ExpectT[*dslKindError](nil).ToEqual(nil)", func(c *Context) {
		ExpectT(c, nilKind).ToEqual(nil)
	}) {
		t.Error("expected two typed nils of the same type to be == equal")
	}
}

// dslSliceError is an error whose dynamic type is not comparable. error satisfies the comparable
// constraint, so EqualTo[error] compiles, but == on two error values holding it panics at runtime.
type dslSliceError []string

func (e dslSliceError) Error() string { return fmt.Sprint([]string(e)) }

// dslCodesError is incomparable too, but its Is method relates two values with the same first code.
type dslCodesError []int

func (e dslCodesError) Error() string { return fmt.Sprint([]int(e)) }
func (e dslCodesError) Is(target error) bool {
	t, ok := target.(dslCodesError)
	return ok && len(t) > 0 && len(e) > 0 && t[0] == e[0]
}

// Codex review on #259: the typed path must not panic on errors whose dynamic type is incomparable.
// It gives the verdict ctx.Expect gives — errors.Is, which skips == for incomparable values.
func TestTypedEqualityHandlesDynamicallyIncomparableErrors(t *testing.T) {
	cases := []struct {
		name             string
		actual, expected error
		equal            bool
	}{
		{"same incomparable type, no Is method", dslSliceError{"a"}, dslSliceError{"a"}, false},
		{"incomparable type with a matching Is method", dslCodesError{7, 1}, dslCodesError{7, 2}, true},
		{"incomparable type with a non-matching Is method", dslCodesError{7}, dslCodesError{8}, false},
		{"wrapped incomparable error with a matching Is", fmt.Errorf("w: %w", dslCodesError{7}), dslCodesError{7}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, e := tc.actual, tc.expected
			want := outcome(t, "Expect.ToEqual", func(c *Context) { c.Expect(a).ToEqual(e) })
			if want != tc.equal {
				t.Fatalf("ctx.Expect(...).ToEqual passed=%v, test expects %v", want, tc.equal)
			}
			if got := outcome(t, "EqualTo", func(c *Context) { EqualTo(c, a, e) }); got != want {
				t.Errorf("EqualTo passed=%v, want %v like ctx.Expect", got, want)
			}
			if got := outcome(t, "ExpectT.ToEqual", func(c *Context) { ExpectT(c, a).ToEqual(e) }); got != want {
				t.Errorf("ExpectT.ToEqual passed=%v, want %v like ctx.Expect", got, want)
			}
		})
	}
}

// dslWideCodeError is a value error type wider than a word whose Is method relates values that ==
// does not. Converting one to an interface has to box it on the heap.
type dslWideCodeError struct{ Code, A, B, C, D, E, F, G int }

func (e dslWideCodeError) Error() string { return fmt.Sprintf("code %d", e.Code) }
func (e dslWideCodeError) Is(target error) bool {
	t, ok := target.(dslWideCodeError)
	return ok && t.Code == e.Code
}

// Codex review on #259: BENCHMARKS.md promises that EqualTo and ExpectT(...).ToEqual cost nothing for
// any comparable T at any width. That covers the errors.Is fallback too: a typed equality assertion
// that PASSES must allocate nothing, whatever T is. (A failing one may allocate; the spec is ending.)
func TestPassingTypedEqualityNeverAllocatesThroughTheErrorsFallback(t *testing.T) {
	sentinel := errors.New("not found")
	wrapped := fmt.Errorf("repo: %w", sentinel)
	kindA, kindB := &dslKindError{Kind: "x"}, &dslKindError{Kind: "x"}
	wideA, wideB := dslWideCodeError{Code: 7, A: 1}, dslWideCodeError{Code: 7, A: 2}

	probes := map[string]func(*Context){
		"EqualTo[error]":                    func(c *Context) { EqualTo(c, wrapped, error(sentinel)) },
		"ExpectT[error].ToEqual":            func(c *Context) { ExpectT(c, wrapped).ToEqual(error(sentinel)) },
		"EqualTo[*dslKindError]":            func(c *Context) { EqualTo(c, kindA, kindB) },
		"ExpectT[*dslKindError].ToEqual":    func(c *Context) { ExpectT(c, kindA).ToEqual(kindB) },
		"EqualTo[dslWideCodeError]":         func(c *Context) { EqualTo(c, wideA, wideB) },
		"ExpectT[dslWideCodeError].ToEqual": func(c *Context) { ExpectT(c, wideA).ToEqual(wideB) },
	}
	for name, probe := range probes {
		backend := &capturingBackend{}
		ctx := &Context{backend: backend}
		allocs := testing.AllocsPerRun(allocContractRuns, func() {
			backend.failed = false
			probe(ctx)
		})
		if !backend.failed && allocs != 0 {
			t.Errorf("%s passed but allocated %.1f times per call, want 0", name, allocs)
		}
	}
}
