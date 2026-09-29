package assert

import (
	"math"
	"testing"
	"time"
)

// order_matchers_test.go pins BeGreaterThan, BeGreaterThanOrEqual, BeLessThan, BeLessThanOrEqual,
// BeBetween and BeCloseTo: every numeric kind, mixed kinds compared exactly (no overflow, no
// precision loss), NaN never matching, string ordering, exact failure wording, and no panics.

type namedFloat float64

func TestOrderingMatchersAcrossNumericKinds(t *testing.T) {
	// Each value is 5; every other one below is 3 or 7, in each kind and in named kinds.
	fives := []any{int(5), int8(5), int16(5), int32(5), int64(5), uint(5), uint8(5), uint16(5), uint32(5), uint64(5), uintptr(5), float32(5), float64(5), namedFloat(5), time.Duration(5)}
	threes := []any{int(3), int8(3), int16(3), int32(3), int64(3), uint(3), uint8(3), uint16(3), uint32(3), uint64(3), uintptr(3), float32(3), float64(3), namedFloat(3), time.Duration(3)}
	sevens := []any{int(7), int8(7), int16(7), int32(7), int64(7), uint(7), uint8(7), uint16(7), uint32(7), uint64(7), uintptr(7), float32(7), float64(7), namedFloat(7), time.Duration(7)}
	for i, five := range fives {
		for j := range fives {
			three, seven := threes[j], sevens[j]
			cases := []struct {
				name string
				m    Matcher
				want bool
			}{
				{"gt 3", BeGreaterThan(three), true},
				{"gt 5", BeGreaterThan(fives[j]), false},
				{"gt 7", BeGreaterThan(seven), false},
				{"ge 3", BeGreaterThanOrEqual(three), true},
				{"ge 5", BeGreaterThanOrEqual(fives[j]), true},
				{"ge 7", BeGreaterThanOrEqual(seven), false},
				{"lt 3", BeLessThan(three), false},
				{"lt 5", BeLessThan(fives[j]), false},
				{"lt 7", BeLessThan(seven), true},
				{"le 3", BeLessThanOrEqual(three), false},
				{"le 5", BeLessThanOrEqual(fives[j]), true},
				{"le 7", BeLessThanOrEqual(seven), true},
				{"between", BeBetween(three, seven), true},
				{"between inclusive low", BeBetween(fives[j], seven), true},
				{"between inclusive high", BeBetween(three, fives[j]), true},
				{"outside", BeBetween(seven, seven), false},
			}
			for _, tc := range cases {
				if got := tc.m.Match(five); got != tc.want {
					t.Fatalf("%s: actual %v (%T) [%d] vs %T [%d]: got %v, want %v", tc.name, five, five, i, fives[j], j, got, tc.want)
				}
			}
		}
	}
}

func TestOrderingMixedSignednessAndPrecision(t *testing.T) {
	const maxInt64 = int64(math.MaxInt64)
	const maxUint64 = uint64(math.MaxUint64)
	cases := []struct {
		name   string
		actual any
		target any
		cmp    int // sign of actual compared with target
	}{
		{"negative int below any uint", int(-1), uint(0), -1},
		{"any uint above negative int", uint8(0), int64(-1), 1},
		{"min int64 below zero uint", int64(math.MinInt64), uint64(0), -1},
		{"max uint64 above max int64", maxUint64, maxInt64, 1},
		{"max int64 below max uint64", maxInt64, maxUint64, -1},
		{"equal int and uint", int(7), uint(7), 0},
		{"uint64 just above max int64", uint64(math.MaxInt64) + 1, maxInt64, 1},
		{"int64 beyond float precision is greater", int64(1<<53 + 1), float64(1 << 53), 1},
		{"float below int beyond float precision", float64(1 << 53), int64(1<<53 + 1), -1},
		{"uint64 beyond float precision", uint64(1<<63 + 1), float64(1 << 63), 1},
		{"max uint64 below 2^64 as float", maxUint64, math.Ldexp(1, 64), -1},
		{"max int64 below 2^63 as float", maxInt64, math.Ldexp(1, 63), -1},
		{"min int64 equals -2^63 as float", int64(math.MinInt64), -math.Ldexp(1, 63), 0},
		{"float fraction above int", 2.5, int(2), 1},
		{"float fraction below int", 2.5, int(3), -1},
		{"negative float fraction", -2.5, int(-2), -1},
		{"negative float below uint", -0.5, uint(0), -1},
		{"float equals int", 3.0, int(3), 0},
		{"float32 against float64", float32(0.5), float64(0.5), 0},
		{"+Inf above max int64", math.Inf(1), maxInt64, 1},
		{"-Inf below min int64", math.Inf(-1), int64(math.MinInt64), -1},
		{"+Inf above max uint64", math.Inf(1), maxUint64, 1},
		{"-Inf below zero uint", math.Inf(-1), uint(0), -1},
		{"+Inf equals +Inf", math.Inf(1), math.Inf(1), 0},
		{"string order", "b", "a", 1},
		{"string equal", "a", "a", 0},
		{"string prefix below longer", "a", "ab", -1},
		{"named string", namedText("a"), "b", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			want := map[string]bool{
				"gt": tc.cmp > 0, "ge": tc.cmp >= 0, "lt": tc.cmp < 0, "le": tc.cmp <= 0,
			}
			got := map[string]bool{
				"gt": BeGreaterThan(tc.target).Match(tc.actual),
				"ge": BeGreaterThanOrEqual(tc.target).Match(tc.actual),
				"lt": BeLessThan(tc.target).Match(tc.actual),
				"le": BeLessThanOrEqual(tc.target).Match(tc.actual),
			}
			for op, w := range want {
				if got[op] != w {
					t.Fatalf("%s: actual %v (%T) vs %v (%T): got %v, want %v", op, tc.actual, tc.actual, tc.target, tc.target, got[op], w)
				}
			}
		})
	}
}

func TestOrderingNaNNeverMatches(t *testing.T) {
	nan := math.NaN()
	for _, tc := range []struct{ actual, target any }{
		{nan, 1}, {1, nan}, {nan, nan}, {float32(nan), 1.0}, {uint(1), nan}, {nan, "a"},
	} {
		for name, m := range map[string]Matcher{
			"gt": BeGreaterThan(tc.target), "ge": BeGreaterThanOrEqual(tc.target),
			"lt": BeLessThan(tc.target), "le": BeLessThanOrEqual(tc.target),
		} {
			if m.Match(tc.actual) {
				t.Fatalf("%s: NaN must never match (%v vs %v)", name, tc.actual, tc.target)
			}
		}
	}
	if BeBetween(0, 10).Match(nan) || BeBetween(nan, 10).Match(5) || BeBetween(0, nan).Match(5) {
		t.Fatal("BeBetween must not match with a NaN anywhere")
	}
	if BeCloseTo(1, 1).Match(nan) || BeCloseTo(nan, 1).Match(1) || BeCloseTo(1, nan).Match(1) {
		t.Fatal("BeCloseTo must not match with a NaN anywhere")
	}
}

func TestBeBetweenSemantics(t *testing.T) {
	cases := []struct {
		name   string
		m      Matcher
		actual any
		want   bool
	}{
		{"inside", BeBetween(1, 10), 5, true},
		{"low bound is inclusive", BeBetween(1, 10), 1, true},
		{"high bound is inclusive", BeBetween(1, 10), 10, true},
		{"below", BeBetween(1, 10), 0, false},
		{"above", BeBetween(1, 10), 11, false},
		{"degenerate range", BeBetween(5, 5), 5, true},
		{"reversed bounds never match", BeBetween(10, 1), 5, false},
		{"mixed kinds", BeBetween(int8(-3), uint16(3)), 2.5, true},
		{"mixed kinds outside", BeBetween(int8(-3), uint16(3)), int64(-4), false},
		{"floats", BeBetween(0.5, 1.5), 1.0, true},
		{"strings", BeBetween("b", "d"), "c", true},
		{"strings inclusive", BeBetween("b", "d"), "d", true},
		{"strings outside", BeBetween("b", "d"), "e", false},
		{"string actual, number bounds", BeBetween(1, 10), "5", false},
		{"number actual, string bounds", BeBetween("a", "z"), 5, false},
		{"mixed bounds", BeBetween(1, "z"), 5, false},
		{"durations", BeBetween(time.Second, time.Minute), 30 * time.Second, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			if got := tc.m.Match(tc.actual); got != tc.want {
				t.Fatalf("Match(%v) = %v, want %v", tc.actual, got, tc.want)
			}
		})
	}
}

func TestBeCloseToSemantics(t *testing.T) {
	cases := []struct {
		name   string
		m      Matcher
		actual any
		want   bool
	}{
		{"within", BeCloseTo(3, 0.5), 3.25, true},
		{"delta is inclusive", BeCloseTo(3, 0.5), 3.5, true},
		{"below the window", BeCloseTo(3, 0.5), 2.25, false},
		{"above the window", BeCloseTo(3, 0.5), 3.75, false},
		{"int actual", BeCloseTo(10, 2), 11, true},
		{"uint actual", BeCloseTo(10, 2), uint8(13), false},
		{"float32 actual", BeCloseTo(1, 0.5), float32(1.5), true},
		{"zero delta needs equality", BeCloseTo(3, 0), 3, true},
		{"zero delta rejects difference", BeCloseTo(3, 0), 3.5, false},
		{"equal infinities", BeCloseTo(math.Inf(1), 1), math.Inf(1), true},
		{"opposite infinities", BeCloseTo(math.Inf(1), 1e300), math.Inf(-1), false},
		{"duration as int64", BeCloseTo(float64(time.Second), float64(time.Millisecond)), time.Second + 500*time.Microsecond, true},
		{"negative delta never matches", BeCloseTo(3, -1), 3, false},
		{"string actual", BeCloseTo(3, 1), "3", false},
		{"nil actual", BeCloseTo(3, 1), nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			if got := tc.m.Match(tc.actual); got != tc.want {
				t.Fatalf("Match(%v) = %v, want %v", tc.actual, got, tc.want)
			}
		})
	}
}

func TestOrderingMatchersFailureMessages(t *testing.T) {
	nan := math.NaN()
	type point struct{ X int }
	cases := []struct {
		name    string
		matcher Matcher
		actual  any
		want    string
	}{
		{"gt", BeGreaterThan(5), 3, "expected 3 to be greater than 5"},
		{"gt equal", BeGreaterThan(3), 3, "expected 3 to be greater than 3"},
		{"ge", BeGreaterThanOrEqual(5), 3, "expected 3 to be greater than or equal to 5"},
		{"lt", BeLessThan(3), 5, "expected 5 to be less than 3"},
		{"le", BeLessThanOrEqual(3), 5, "expected 5 to be less than or equal to 3"},
		{"mixed kinds", BeGreaterThan(uint(5)), -1, "expected -1 to be greater than 5"},
		{"strings", BeLessThan("a"), "b", "expected b to be less than a"},
		{"gt NaN actual", BeGreaterThan(1), nan, "expected NaN to be greater than 1 — NaN is not ordered"},
		{"lt NaN target", BeLessThan(nan), 1, "expected 1 to be less than NaN — NaN is not ordered"},
		{"bool actual", BeGreaterThan(1), true, "BeGreaterThan: bool is not a number or string"},
		{"nil actual", BeLessThan(1), nil, "BeLessThan: <nil> is not a number or string"},
		{"struct actual", BeGreaterThanOrEqual(1), point{1}, "BeGreaterThanOrEqual: assert.point is not a number or string"},
		{"time.Time is unsupported", BeLessThanOrEqual(1), time.Time{}, "BeLessThanOrEqual: time.Time is not a number or string"},
		{"slice actual", BeGreaterThan(1), []int{1}, "BeGreaterThan: []int is not a number or string"},
		{"number against string", BeGreaterThan("a"), 5, "BeGreaterThan: cannot compare int with string"},
		{"string against number", BeLessThan(5), "a", "BeLessThan: cannot compare string with int"},
		{"expected value not orderable", BeGreaterThan(true), 5, "BeGreaterThan: expected value bool is not a number or string"},
		{"expected value nil", BeLessThan(nil), 5, "BeLessThan: expected value <nil> is not a number or string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			if tc.matcher.Match(tc.actual) {
				t.Fatalf("expected no match for %v (%T)", tc.actual, tc.actual)
			}
			if got := tc.matcher.FailureMessage(tc.actual); got != tc.want {
				t.Fatalf("FailureMessage:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

func TestBeBetweenAndBeCloseToFailureMessages(t *testing.T) {
	nan := math.NaN()
	cases := []struct {
		name    string
		matcher Matcher
		actual  any
		want    string
	}{
		{"between below", BeBetween(1, 10), 0, "expected 0 to be between 1 and 10 (inclusive)"},
		{"between above", BeBetween(1, 10), 11, "expected 11 to be between 1 and 10 (inclusive)"},
		{"between strings", BeBetween("b", "d"), "e", "expected e to be between b and d (inclusive)"},
		{"between reversed", BeBetween(10, 1), 5, "expected 5 to be between 10 and 1 — lower bound is greater than upper bound"},
		{"between NaN bound", BeBetween(nan, 1), 0, "BeBetween: NaN bound is not ordered"},
		{"between NaN actual", BeBetween(0, 1), nan, "expected NaN to be between 0 and 1 — NaN is not ordered"},
		{"between mixed bounds", BeBetween(1, "z"), 5, "BeBetween: bounds int and string are not comparable"},
		{"between bounds not orderable", BeBetween(true, false), 5, "BeBetween: bounds must be numbers or strings, got bool and bool"},
		{"between actual not orderable", BeBetween(1, 10), nil, "BeBetween: <nil> is not a number or string"},
		{"between string actual, number bounds", BeBetween(1, 10), "5", "BeBetween: cannot compare string with int"},
		{"close", BeCloseTo(3, 0.5), 4.0, "expected 4 to be within 0.5 of 3, difference is 1"},
		{"close int", BeCloseTo(10, 2), 15, "expected 15 to be within 2 of 10, difference is 5"},
		{"close NaN actual", BeCloseTo(3, 1), nan, "expected NaN to be within 1 of 3 — NaN is not ordered"},
		{"close NaN target", BeCloseTo(nan, 1), 3, "expected 3 to be within 1 of NaN — NaN is not ordered"},
		{"close negative delta", BeCloseTo(3, -1), 3, "BeCloseTo: delta must be a non-negative number, got -1"},
		{"close NaN delta", BeCloseTo(3, nan), 3, "BeCloseTo: delta must be a non-negative number, got NaN"},
		{"close string actual", BeCloseTo(3, 1), "3", "BeCloseTo: string is not a number"},
		{"close nil actual", BeCloseTo(3, 1), nil, "BeCloseTo: <nil> is not a number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer mustNotPanic(t)
			if tc.matcher.Match(tc.actual) {
				t.Fatalf("expected no match for %v (%T)", tc.actual, tc.actual)
			}
			if got := tc.matcher.FailureMessage(tc.actual); got != tc.want {
				t.Fatalf("FailureMessage:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

func TestOrderingMatchersDoNotAllocateOnSuccess(t *testing.T) {
	gt, lt := BeGreaterThan(1), BeLessThanOrEqual(2.5)
	gtStr := BeGreaterThan("a")
	between, near := BeBetween(1, 10), BeCloseTo(3, 0.5)
	mixed := BeGreaterThan(uint(2))
	var i, i64, f, s any = 5, int64(5), 2.25, "b"
	if allocs := testing.AllocsPerRun(100, func() {
		_ = gt.Match(i)
		_ = gt.Match(i64)
		_ = lt.Match(f)
		_ = gtStr.Match(s)
		_ = between.Match(i)
		_ = near.Match(f)
		_ = mixed.Match(i)
	}); allocs != 0 {
		t.Fatalf("expected no allocations, got %v", allocs)
	}
}

func TestOrderingMatchersDescribe(t *testing.T) {
	cases := []struct {
		m    Matcher
		want string
	}{
		{BeGreaterThan(2), "greater than 2"},
		{BeGreaterThanOrEqual(2), "greater than or equal to 2"},
		{BeLessThan(2), "less than 2"},
		{BeLessThanOrEqual("b"), "less than or equal to b"},
		{BeBetween(1, 3), "between 1 and 3 (inclusive)"},
		{BeCloseTo(3, 0.5), "within 0.5 of 3"},
	}
	for _, tc := range cases {
		if got := tc.m.(Describer).Description(); got != tc.want {
			t.Fatalf("description = %q, want %q", got, tc.want)
		}
	}
}

func TestOrderingMatchersCompose(t *testing.T) {
	if !Not(BeGreaterThan(5)).Match(3) || Not(BeGreaterThan(1)).Match(3) {
		t.Fatal("Not(BeGreaterThan) should negate")
	}
	if !All(BeGreaterThan(1), BeLessThan(10), Not(BeBetween(20, 30))).Match(5) {
		t.Fatal("All should hold")
	}
	if !Any(BeLessThan(1), BeCloseTo(5, 0.1)).Match(5.05) {
		t.Fatal("Any should hold")
	}
	// Not over a NaN is true: the inner matcher does not match it. See docs/DSL.md.
	if !Not(BeGreaterThan(1)).Match(math.NaN()) {
		t.Fatal("Not(BeGreaterThan) over NaN succeeds because the inner matcher fails")
	}
}
