package assert

import (
	"cmp"
	"fmt"
	"math"
	"reflect"
)

// The ordering matchers compare numbers with numbers and strings with strings.
//
// A number is any int, uint or float kind, named types included (time.Duration is an int64, so it
// works), and different kinds compare with each other by exact value: a negative int is below every
// uint, a uint64 above math.MaxInt64 is above every int64, and an int64 beyond 2^53 is not rounded
// to a float64 before it is compared with one. NaN is not ordered, so it never matches any of these
// matchers, on either side. A string compares with a string (byte-wise, like Go's < on strings; named
// string types included). Everything else (a bool, nil, a struct, a slice, time.Time, ...) is not
// orderable, and a number never compares with a string; FailureMessage says which of those happened
// instead of reporting a bogus ordering. Neither Match nor FailureMessage ever panics. Numbers
// compare without reflection or allocation for the builtin types.

// BeGreaterThan returns a matcher that expects actual > x.
func BeGreaterThan(x any) Matcher { return &orderMatcher{op: opGreater, target: x} }

// BeGreaterThanOrEqual returns a matcher that expects actual >= x.
func BeGreaterThanOrEqual(x any) Matcher { return &orderMatcher{op: opGreaterOrEqual, target: x} }

// BeLessThan returns a matcher that expects actual < x.
func BeLessThan(x any) Matcher { return &orderMatcher{op: opLess, target: x} }

// BeLessThanOrEqual returns a matcher that expects actual <= x.
func BeLessThanOrEqual(x any) Matcher { return &orderMatcher{op: opLessOrEqual, target: x} }

type orderOp uint8

const (
	opGreater orderOp = iota
	opGreaterOrEqual
	opLess
	opLessOrEqual
)

func (op orderOp) matcherName() string {
	switch op {
	case opGreater:
		return "BeGreaterThan"
	case opGreaterOrEqual:
		return "BeGreaterThanOrEqual"
	case opLess:
		return "BeLessThan"
	default:
		return "BeLessThanOrEqual"
	}
}

func (op orderOp) phrase() string {
	switch op {
	case opGreater:
		return "greater than"
	case opGreaterOrEqual:
		return "greater than or equal to"
	case opLess:
		return "less than"
	default:
		return "less than or equal to"
	}
}

// holds reports whether an actual that compares as order (its sign against the target) satisfies op.
func (op orderOp) holds(order int) bool {
	switch op {
	case opGreater:
		return order > 0
	case opGreaterOrEqual:
		return order >= 0
	case opLess:
		return order < 0
	default:
		return order <= 0
	}
}

type orderMatcher struct {
	op     orderOp
	target any
}

func (m *orderMatcher) Match(actual any) bool {
	order, status := compareOrdered(actual, m.target)
	return status == ordOK && m.op.holds(order)
}

func (m *orderMatcher) FailureMessage(actual any) string {
	_, status := compareOrdered(actual, m.target)
	name := m.op.matcherName()
	switch status {
	case ordOK:
		return fmt.Sprintf("expected %s to be %s %s", userValue(actual), m.op.phrase(), userValue(m.target))
	case ordNaN:
		return fmt.Sprintf("expected %s to be %s %s — NaN is not ordered", userValue(actual), m.op.phrase(), userValue(m.target))
	case ordTargetInvalid:
		return fmt.Sprintf("%s: expected value %T is not a number or string", name, m.target)
	case ordMixed:
		return fmt.Sprintf("%s: cannot compare %T with %T", name, actual, m.target)
	default:
		return fmt.Sprintf("%s: %T is not a number or string", name, actual)
	}
}

// Description implements Describer; see equalMatcher.Description.
func (m *orderMatcher) Description() string {
	return fmt.Sprintf("%s %s", m.op.phrase(), userValue(m.target))
}

// BeBetween returns a matcher that expects lo <= actual <= hi: both bounds are inclusive. The bounds
// and the actual must all be numbers or all be strings, and a range with lo above hi never matches
// (its failure says so). A degenerate range, lo equal to hi, matches exactly that value.
func BeBetween(lo, hi any) Matcher {
	return &betweenMatcher{lo: lo, hi: hi}
}

type betweenMatcher struct {
	lo, hi any
}

func (m *betweenMatcher) Match(actual any) bool {
	order, status := compareOrdered(actual, m.lo)
	if status != ordOK || order < 0 {
		return false
	}
	order, status = compareOrdered(actual, m.hi)
	return status == ordOK && order <= 0
}

func (m *betweenMatcher) FailureMessage(actual any) string {
	bounds, status := compareOrdered(m.lo, m.hi)
	switch status {
	case ordNaN:
		return "BeBetween: NaN bound is not ordered"
	case ordActualInvalid, ordTargetInvalid:
		return fmt.Sprintf("BeBetween: bounds must be numbers or strings, got %T and %T", m.lo, m.hi)
	case ordMixed:
		return fmt.Sprintf("BeBetween: bounds %T and %T are not comparable", m.lo, m.hi)
	}
	if bounds > 0 {
		return fmt.Sprintf("expected %s to be between %s and %s — lower bound is greater than upper bound", userValue(actual), userValue(m.lo), userValue(m.hi))
	}
	switch _, status := compareOrdered(actual, m.lo); status {
	case ordNaN:
		return fmt.Sprintf("expected %s to be between %s and %s — NaN is not ordered", userValue(actual), userValue(m.lo), userValue(m.hi))
	case ordMixed:
		return fmt.Sprintf("BeBetween: cannot compare %T with %T", actual, m.lo)
	case ordActualInvalid, ordTargetInvalid:
		return fmt.Sprintf("BeBetween: %T is not a number or string", actual)
	}
	return fmt.Sprintf("expected %s to be between %s and %s (inclusive)", userValue(actual), userValue(m.lo), userValue(m.hi))
}

// Description implements Describer; see equalMatcher.Description.
func (m *betweenMatcher) Description() string {
	return fmt.Sprintf("between %s and %s (inclusive)", userValue(m.lo), userValue(m.hi))
}

// BeCloseTo returns a matcher that expects |actual - target| <= delta, the delta being inclusive.
// The actual may be any number kind and is converted to a float64 first, so the comparison is only
// as exact as a float64 is (an int64 beyond 2^53 is rounded); use BeBetween or BeGreaterThan where
// exactness matters. Equal infinities are close. A delta that is negative or NaN never matches (the
// failure says so), and neither does a NaN actual or target.
func BeCloseTo(target, delta float64) Matcher {
	return &closeToMatcher{target: target, delta: delta}
}

type closeToMatcher struct {
	target, delta float64
}

func (m *closeToMatcher) Match(actual any) bool {
	if !(m.delta >= 0) {
		return false
	}
	n, ok := toNumber(actual)
	if !ok {
		return false
	}
	got := n.float()
	if math.IsNaN(got) || math.IsNaN(m.target) {
		return false
	}
	return got == m.target || math.Abs(got-m.target) <= m.delta
}

func (m *closeToMatcher) FailureMessage(actual any) string {
	if !(m.delta >= 0) {
		return fmt.Sprintf("BeCloseTo: delta must be a non-negative number, got %v", m.delta)
	}
	n, ok := toNumber(actual)
	if !ok {
		return fmt.Sprintf("BeCloseTo: %T is not a number", actual)
	}
	got := n.float()
	if math.IsNaN(got) || math.IsNaN(m.target) {
		return fmt.Sprintf("expected %s to be within %v of %s — NaN is not ordered", userValue(actual), m.delta, userValue(m.target))
	}
	return fmt.Sprintf("expected %s to be within %v of %s, difference is %v", userValue(actual), m.delta, userValue(m.target), math.Abs(got-m.target))
}

// Description implements Describer; see equalMatcher.Description.
func (m *closeToMatcher) Description() string {
	return fmt.Sprintf("within %v of %s", m.delta, userValue(m.target))
}

type orderStatus uint8

const (
	ordOK orderStatus = iota
	ordNaN
	ordActualInvalid
	ordTargetInvalid
	ordMixed
)

// compareOrdered returns the sign of actual compared with target (-1, 0 or 1) when the two are
// orderable against each other, and otherwise says why they are not. An invalid target is reported
// before an invalid actual: it is a mistake in the assertion itself.
func compareOrdered(actual, target any) (int, orderStatus) {
	an, aIsNumber := toNumber(actual)
	tn, tIsNumber := toNumber(target)
	if aIsNumber && tIsNumber {
		order, ok := compareNumbers(an, tn)
		if !ok {
			return 0, ordNaN
		}
		return order, ordOK
	}
	as, aIsString := stringValue(actual)
	ts, tIsString := stringValue(target)
	if aIsString && tIsString {
		return cmp.Compare(as, ts), ordOK
	}
	switch {
	case !tIsNumber && !tIsString:
		return 0, ordTargetInvalid
	case !aIsNumber && !aIsString:
		return 0, ordActualInvalid
	default:
		return 0, ordMixed
	}
}

type numberKind uint8

const (
	kindInt numberKind = iota
	kindUint
	kindFloat
)

// number is a numeric value normalised to the widest type of its family, so two of them compare
// exactly whatever their original kinds were.
type number struct {
	kind numberKind
	i    int64
	u    uint64
	f    float64
}

func (n number) float() float64 {
	switch n.kind {
	case kindInt:
		return float64(n.i)
	case kindUint:
		return float64(n.u)
	default:
		return n.f
	}
}

func toNumber(v any) (number, bool) {
	switch x := v.(type) {
	case int:
		return number{kind: kindInt, i: int64(x)}, true
	case int8:
		return number{kind: kindInt, i: int64(x)}, true
	case int16:
		return number{kind: kindInt, i: int64(x)}, true
	case int32:
		return number{kind: kindInt, i: int64(x)}, true
	case int64:
		return number{kind: kindInt, i: x}, true
	case uint:
		return number{kind: kindUint, u: uint64(x)}, true
	case uint8:
		return number{kind: kindUint, u: uint64(x)}, true
	case uint16:
		return number{kind: kindUint, u: uint64(x)}, true
	case uint32:
		return number{kind: kindUint, u: uint64(x)}, true
	case uint64:
		return number{kind: kindUint, u: x}, true
	case uintptr:
		return number{kind: kindUint, u: uint64(x)}, true
	case float32:
		return number{kind: kindFloat, f: float64(x)}, true
	case float64:
		return number{kind: kindFloat, f: x}, true
	case nil:
		return number{}, false
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return number{kind: kindInt, i: rv.Int()}, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return number{kind: kindUint, u: rv.Uint()}, true
	case reflect.Float32, reflect.Float64:
		return number{kind: kindFloat, f: rv.Float()}, true
	default:
		return number{}, false
	}
}

func stringValue(v any) (string, bool) {
	if s, ok := v.(string); ok {
		return s, true
	}
	return namedString(v)
}

// compareNumbers returns the sign of a compared with b, and false when either is NaN.
func compareNumbers(a, b number) (int, bool) {
	switch {
	case a.kind == kindFloat && b.kind == kindFloat:
		if math.IsNaN(a.f) || math.IsNaN(b.f) {
			return 0, false
		}
		return cmp.Compare(a.f, b.f), true
	case a.kind == kindFloat:
		if math.IsNaN(a.f) {
			return 0, false
		}
		if b.kind == kindInt {
			return compareFloatInt(a.f, b.i), true
		}
		return compareFloatUint(a.f, b.u), true
	case b.kind == kindFloat:
		order, ok := compareNumbers(b, a)
		return -order, ok
	case a.kind == kindInt && b.kind == kindInt:
		return cmp.Compare(a.i, b.i), true
	case a.kind == kindUint && b.kind == kindUint:
		return cmp.Compare(a.u, b.u), true
	case a.kind == kindInt:
		if a.i < 0 {
			return -1, true
		}
		return cmp.Compare(uint64(a.i), b.u), true
	default:
		if b.i < 0 {
			return 1, true
		}
		return cmp.Compare(a.u, uint64(b.i)), true
	}
}

// compareFloatInt compares a non-NaN float with an int64 exactly: it never rounds the int to a float.
func compareFloatInt(f float64, i int64) int {
	switch {
	case f >= 1<<63:
		return 1
	case f < -(1 << 63):
		return -1
	}
	whole := int64(f) // in range, and truncation toward zero is exact for a float
	if whole != i {
		return cmp.Compare(whole, i)
	}
	return cmp.Compare(f-float64(whole), 0)
}

// compareFloatUint is compareFloatInt for a uint64.
func compareFloatUint(f float64, u uint64) int {
	switch {
	case f < 0:
		return -1
	case f >= 1<<64:
		return 1
	}
	whole := uint64(f)
	if whole != u {
		return cmp.Compare(whole, u)
	}
	return cmp.Compare(f-float64(whole), 0)
}
