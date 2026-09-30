package mock

import "testing"

// wrappedTB stands for *specs.Context: it satisfies TB and exposes the case's real testing.TB through
// Testing(), which is what lets a mock function mark its own frame on a real *testing.T.
type wrappedTB struct {
	TB
	inner testing.TB
}

func (w wrappedTB) Testing() testing.TB { return w.inner }

func TestTestingTBPrefersTheWrappedTestingTB(t *testing.T) {
	if got := testingTB(wrappedTB{TB: t, inner: t}); got != interface{ Helper() }(t) {
		t.Errorf("testingTB(wrapper) = %v, want the wrapped *testing.T", got)
	}
	nilInner := wrappedTB{TB: t}
	if got := testingTB(nilInner); got != interface{ Helper() }(nilInner) {
		t.Errorf("testingTB(wrapper without a testing.TB) = %v, want the wrapper itself", got)
	}
	if got := testingTB(t); got != interface{ Helper() }(t) {
		t.Errorf("testingTB(*testing.T) = %v, want it unchanged", got)
	}
}
