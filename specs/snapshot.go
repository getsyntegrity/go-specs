package specs

import "github.com/pablogore/go-specs/snapshots"

// runSnapshot compares value to the stored snapshot for name, or creates/updates it.
// callerFile is the path to the test file (from runtime.Caller(1) in Context.Snapshot).
//
// When the backend is a real testing.TB it is handed to the snapshots package directly rather than
// wrapped: snapshots.RunFromFile reports failures itself, so it has to be able to call the concrete
// testing.T.Helper. Going through runnableBackend would mark the wrapper frame instead and land the
// failure on testing_backend.go.
func runSnapshot(backend testBackend, callerFile string, name string, value any) {
	var sb snapshots.Backend = backend
	if tb := helperTB(backend); tb != nil {
		tb.Helper()
		sb = tb
	}
	snapshots.RunFromFile(sb, callerFile, name, value)
}
