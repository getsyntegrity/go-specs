// Package envprobe exercises the real environment readers from inside a test function.
//
// Position is the whole point. The test log's logger is installed by testing.M.before, inside
// m.Run, so a read performed in TestMain is invisible to the test cache and the two access paths
// are indistinguishable there. Inside a test function the log is live, which is the only place
// os.Environ and os.LookupEnv can be told apart by observing `go test`.
package envprobe
