// focus.go provides focused-spec support. FIt and Focus() are compile-time only; the compiler/
// builder decide what to keep and drop. Since issue #273, an active focus is also a runtime policy
// decision: the enclosing test fails unless GO_SPECS_ALLOW_FOCUS=1 opts out (see reportFocusPolicy
// in runner.go and execution_plan.go, its two engine-specific callers) — this file holds the pieces
// every engine shares: the env var, and the message both build from their own counts/locations.
package specs

import (
	"fmt"
	"os"
	"strings"
)

// SpecFn wraps a spec function with options (Focus, Skip or Pending). Pass to ItWith: b.ItWith("name", specs.Focus(fn)).
type SpecFn struct {
	Fn      func(*Context)
	Skip    bool
	Focus   bool
	Pending bool
}

// Focus returns a SpecFn that marks the spec as focused. Use with ItWith: b.ItWith("name", specs.Focus(fn)).
// If any spec is focused, only focused specs are compiled into the program.
func Focus(fn func(*Context)) SpecFn {
	return SpecFn{Fn: fn, Focus: true}
}

// allowFocusEnvVar is the only opt-out from the fail-on-committed-focus policy (issue #273): set to
// "1", it disables the enclosing test failure a committed FIt otherwise causes, in every engine.
// Focus still filters exactly as before either way — only this one failure is suppressed.
const allowFocusEnvVar = "GO_SPECS_ALLOW_FOCUS"

// focusAllowed reports whether the fail-on-focus policy is opted out for this call. It reads the
// environment fresh every time rather than caching the result in a sync.Once or package variable:
// t.Setenv scopes the variable to one test and restores it afterwards, so a cached answer would
// either leak across tests or go stale within one. Every caller in this package reads it at most
// once per suite compile/run (never per spec — see reportFocusPolicy's callers), so this costs one
// os.Getenv call per suite, not one per spec; the always-on per-spec passing path never calls it at
// all.
func focusAllowed() bool {
	return os.Getenv(allowFocusEnvVar) == "1"
}

// focusPolicyMessage builds the actionable diagnostic reportFocusPolicy reports through the
// enclosing tb.Errorf (issue #273): how many specs are focused, how many were excluded because of
// it, and — when the caller could get them cheaply (see registerFocusedSpec's doc comment) — where
// the focused specs are, so the message points straight at the FIt(s) to remove.
func focusPolicyMessage(focusedCount, excludedCount int, locations []string) string {
	msg := fmt.Sprintf(
		"go-specs: %d focused spec(s) (FIt) are active, %d spec(s) excluded; remove FIt or set %s=1",
		focusedCount, excludedCount, allowFocusEnvVar,
	)
	if len(locations) > 0 {
		msg += "; focused: " + strings.Join(locations, ", ")
	}
	return msg
}
