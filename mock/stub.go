package mock

// Return adds one response with the given values. Calling it repeatedly builds a sequence: the
// n-th matched call gets the n-th response, and once the sequence is exhausted the remaining calls
// (within the allowed count) repeat the last response. Return() with no values is an explicitly
// empty response.
//
// Result does not know the method signature: an adapter reads the indexes it needs with Get, Err
// or Value, and an index beyond the configured values reads as not configured (nil / zero).
// When Do is also set, Do wins and Return is ignored.
func (e *Expectation) Return(values ...any) *Expectation {
	if e == nil {
		return nil
	}
	resp := append([]any(nil), values...)
	e.m.c.mu.Lock()
	e.responses = append(e.responses, resp)
	e.m.c.mu.Unlock()
	return e
}

// Do sets a callback that computes the results of each matched call from a copy of its arguments.
// It wins over Return when both are set. The callback runs outside the controller lock, so it may
// call other mocked methods of the same controller. A panic inside fn propagates to the caller of
// Method.Call, because it is the code under test's own call that triggered it.
func (e *Expectation) Do(fn func(args []any) []any) *Expectation {
	if e == nil {
		return nil
	}
	e.m.c.mu.Lock()
	e.do = fn
	e.m.c.mu.Unlock()
	return e
}

// respond builds the Result for the n-th claimed call (1-based). The claim (n) was taken under the
// controller lock, so concurrent calls always get distinct sequence positions; the callback runs
// afterwards, outside the lock.
func (e *Expectation) respond(n int, args []any) Result {
	e.m.c.mu.Lock()
	do := e.do
	var values []any
	if do == nil && len(e.responses) > 0 {
		i := n - 1
		if i >= len(e.responses) {
			i = len(e.responses) - 1
		}
		values = append([]any(nil), e.responses[i]...)
	}
	e.m.c.mu.Unlock()
	if do != nil {
		values = do(append([]any(nil), args...))
	}
	return Result{values: values, method: e.m.name}
}
