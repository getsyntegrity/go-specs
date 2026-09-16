package specs

import "github.com/pablogore/go-specs/snapshots"

// runSnapshot compares value to the stored snapshot for name, or creates/updates it, and returns
// the verdict without reporting it: Context.Snapshot must fold a mismatch into c.failed before
// triggering Fatalf, since Fatalf ends in runtime.Goexit on a real testing.T and never returns
// (issue #115), so this calls snapshots.Evaluate directly rather than the Fatalf-reporting
// RunFromFile. callerFile is the path to the test file (from runtime.Caller(1) in Context.Snapshot).
//
// When the backend is a real testing.TB it is handed to snapshots.Evaluate directly rather than
// wrapped, so Evaluate's Helper() call marks the concrete testing.T rather than a runnableBackend
// wrapper frame.
func runSnapshot(backend testBackend, callerFile string, name string, value any) snapshots.Result {
	var helper snapshots.HelperBackend = backend
	if tb := helperTB(backend); tb != nil {
		helper = tb
	}
	return snapshots.Evaluate(helper, callerFile, name, value)
}
