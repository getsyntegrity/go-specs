package mock

import "fmt"

// Result carries the stubbed return values of one call. The zero Result has no values: every
// accessor then reports "not configured" (nil or the zero value of the requested type).
type Result struct {
	values []any
}

// Get returns the i-th value, or nil when it is not configured or i is out of range.
func (r Result) Get(i int) any {
	if i < 0 || i >= len(r.values) {
		return nil
	}
	return r.values[i]
}

// Err returns the i-th value as an error: nil when it is not configured or nil. It panics with a
// clear message when the value is set but is not an error.
func (r Result) Err(i int) error {
	v := r.Get(i)
	if v == nil {
		return nil
	}
	err, ok := v.(error)
	if !ok {
		panic(fmt.Sprintf("mock: result %d is %T, which is not an error", i, v))
	}
	return err
}

// Value returns the i-th value of r as T: the zero T when it is not configured, and a panic with a
// clear message when the configured value is not a T.
func Value[T any](r Result, i int) T {
	var zero T
	v := r.Get(i)
	if v == nil {
		return zero
	}
	t, ok := v.(T)
	if !ok {
		panic(fmt.Sprintf("mock: result %d is %T, want %T", i, v, zero))
	}
	return t
}
