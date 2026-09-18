// Package envread holds the two deliberately different ways coordination reads an environment
// variable.
//
// It is a package of its own for one reason: so the REAL implementations can be exercised by a
// test. Keeping them as unexported methods inside coordination made them untestable, and the
// tests that appeared to cover them actually pinned only which method the caller chose — a name,
// not a behaviour. Scan could then be rewritten as os.LookupEnv, reintroducing the exact
// regression the rule exists to prevent, with the whole suite still green.
package envread

import (
	"os"
	"strings"
)

// Lookup reads name through os.LookupEnv.
//
// os.LookupEnv calls testlog.Getenv, so when this runs anywhere `go test`'s test log is active the
// variable is enrolled in that package's test-cache key. Coordination uses this on the
// ACTIVATED path, where that enrollment is the intent.
//
// Be aware of where it does and does not happen: the test log's logger is installed by
// testing.M.before, inside m.Run. A read performed in TestMain BEFORE m.Run — which is where the
// coordination contract puts configuration resolution — is recorded nowhere and reaches no cache
// key. See docs/145-runtime-claims-inventory.md, "The B2 finding".
func Lookup(name string) (string, bool) {
	return os.LookupEnv(name)
}

// Scan reads name by walking os.Environ().
//
// This is a deliberate use of an implementation detail, not a stylistic choice — DO NOT
// "simplify" it to os.Getenv or os.LookupEnv. os.Environ delegates straight to syscall.Environ
// and records nothing in the test log, while os.Getenv and os.LookupEnv both call testlog.Getenv.
//
// Coordination uses this on the DISABLED path, where enrollment is precisely the harm: it would
// put every coordination variable into the inputs ID of every package in the module, so a stale
// or per-invocation GO_SPECS_RUN_ID sitting in a developer's environment would invalidate the
// cache for every package on every run — while producing no reporting at all in exchange, because
// the gate is off. That regression is silent, cumulative, and typically diagnosed months later as
// "our CI got slow".
//
// The difference between this and Lookup is observable, and is pinned by a test that calls both
// from inside a test function, where the test log is live.
func Scan(name string) (string, bool) {
	prefix := name + "="
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):], true
		}
	}
	return "", false
}
