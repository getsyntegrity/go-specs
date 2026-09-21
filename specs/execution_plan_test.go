package specs

import (
	"fmt"
	"testing"
)

type controlledBackend struct {
	failNow bool
	errors  []string
}

func (b *controlledBackend) Helper()               {}
func (b *controlledBackend) FailNow()              { b.failNow = true; panic(isolatedCaseAbort{}) }
func (b *controlledBackend) Fatal(args ...any)     { b.FailNow() }
func (b *controlledBackend) Fatalf(string, ...any) { b.FailNow() }
func (b *controlledBackend) Error(args ...any)     { b.errors = append(b.errors, fmt.Sprint(args...)) }
func (b *controlledBackend) Errorf(format string, args ...any) {
	b.errors = append(b.errors, fmt.Sprintf(format, args...))
}
func (b *controlledBackend) Log(...any)                   {}
func (b *controlledBackend) Logf(string, ...any)          {}
func (b *controlledBackend) Name() string                 { return "controlled" }
func (b *controlledBackend) Cleanup(func())               {}
func (b *controlledBackend) Run(string, func(testing.TB)) {}

func TestOrdinaryItKeepsGoTestSemantics(t *testing.T) {
	var order []string
	Describe(t, "ordinary", func(spec *Spec) {
		spec.BeforeEach(func(*Context) { order = append(order, "before") })
		spec.AfterEach(func(*Context) { order = append(order, "after") })
		spec.It("runs", func(*Context) { order = append(order, "body") })
	})
	if got, want := fmt.Sprint(order), "[before body after]"; got != want {
		t.Fatalf("ordinary It order = %s, want %s", got, want)
	}
}
