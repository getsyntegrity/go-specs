// failure.go holds the one authoritative representation of "this spec failed", and the one path a
// built-in assertion takes to record it.
//
// Before #175 that truth was carried independently in three places: Context's own bool, the
// parallel path's recorded message (where a non-empty string *was* the failure bit), and whatever
// each assertion call site remembered to do before reporting to its backend. Each assertion had to
// mutate state and report, in that order, by hand — #115 and #149 were two call sites that got the
// combination wrong, and fixing them individually left the next one free to get it wrong again.
//
// The invariants this file exists to make unrepresentable:
//
//   - failure is a bit, never inferred from text. A backend.Fatal() with no arguments, a FailNow,
//     or a Matcher whose FailureMessage returns "" is a real failure with an empty message; a
//     representation that derives the bit from the message silently reports those as green.
//   - recording and reporting happen together, in that order. On a real *testing.T, backend.Fatalf
//     ends in runtime.Goexit and never returns, so anything a call site meant to do afterwards is
//     dead code (#115).
package specs

import "fmt"

// failureRecord is the authoritative record of one spec's failure. Both execution paths use this
// one type: Context.failure carries it for the sequential runners, and the parallel worker pool and
// ItParallel keep one per spec index (see scheduler.go and program.go's parallelStep).
//
// Everything that reports failure derives from Failed and nothing else: report.SpecResultEvent.Failed,
// report.SuiteEndEvent.FailedSpecs (via specCounter) and Runner FailFast.
type failureRecord struct {
	// Failed is the authoritative bit. It is never inferred from Message being non-empty — see the
	// file comment for why that inference loses real failures.
	Failed bool
	// Message is the failure text as the backend received it. "" is a valid value for a failed
	// record. The sequential path leaves it empty: its text goes straight to the backend, and the
	// reporter payload it would feed (SpecResultEvent.Message) is documented as empty for a
	// Fatalf-based assertion failure in report/events.go — changing that is a reporter-payload
	// change, not this one.
	Message string
	// File and Line are the user's assertion call site. Only the parallel path captures them (see
	// parallelCallerLocation): there is no live testing.TB on a worker goroutine to mark as a helper,
	// so the location has to be carried as data instead. Both stay zero on the sequential path,
	// where testing.T.Helper() already attributes the failure.
	File string
	Line int
}

// text renders r for a plain-string consumer: "file:line: message" when a location was captured, or
// the bare message otherwise (e.g. FailNow, which records "fail now" with no assertion to locate).
// reportFailures is the only caller — it is the sole place a failureRecord becomes the text
// `go test` actually prints, keeping the format in one place. Go's own decoration on that Fatalf
// call still names the internal frame that made it (see reportFailures); this embeds the real
// location in the message text itself, since that is the only place a plain `go test` run (no
// custom report.EventReporter) can show it at all.
func (r failureRecord) text() string {
	if r.File == "" {
		return r.Message
	}
	return fmt.Sprintf("%s:%d: %s", r.File, r.Line, r.Message)
}

// recordFailure marks this Context's spec as failed without reporting anything. It is for the
// execution paths that report through the backend themselves — the panic-recovery handlers in
// runner.go, execution_plan.go, block_runner.go, minimal_runner.go and runner_bytecode.go, which
// build a message and a stack trace and hand both to backend.Errorf — and for parallelStep folding
// its children's results back onto the parent. Built-in assertions must not use it: they use failf,
// which cannot record without reporting.
func (c *Context) recordFailure() {
	if c != nil {
		c.failure.Failed = true
	}
}

// hasFailed reports whether the spec currently running on c has failed. This is the single read
// every consumer goes through — Runner/ExecutionPlan FailFast and the specResult they hand to a
// report.EventReporter — so there is exactly one answer to "did this spec fail".
func (c *Context) hasFailed() bool {
	return c != nil && c.failure.Failed
}

// resetFailure clears the record for the next spec. Called by Context.Reset and, per spec, by the
// runners that reuse one Context across a group (see runner.go's runSpecIsolated).
func (c *Context) resetFailure() {
	if c != nil {
		c.failure = failureRecord{}
	}
}

// failf is the single failure path every built-in assertion entry point funnels through — EqualTo,
// ExpectT.ToEqual, ExpectT.To, Expectation.To, Expectation.ToEqual and Context.Snapshot. It records
// the authoritative failure, marks its own frame as a test helper, and reports the formatted
// message to the backend, in that order.
//
// The order is the whole point: on a real *testing.T, backend.Fatalf ends in runtime.Goexit and
// never returns, so a call site that recorded after reporting would record nothing at all (#115).
// Because no assertion can reach the backend except through here, that ordering is no longer
// something each call site has to remember.
//
// Callers must mark their own frame with tb.Helper() before calling: one Helper() call marks only
// the function that made it, so both this frame and the assertion's have to opt out before Go
// attributes the failure to the user's assertion line. backend.Fatalf marks the backend's own frame
// from inside it (see runnableBackend.Fatalf), which is the last frame between here and testing.
//
// It is marked noinline for the same reason reportMatcherFailure was: the assertion fast paths
// carry only the failure branch, never this tail and its variadic Fatalf setup. The message is
// formatted once here and passed through as "%s", so a backend sees exactly the same text it used
// to; the boxing that costs is on the failure path, where the spec is ending anyway.
//
// c and c.backend are guaranteed non-nil by the caller: every assertion entry point returns early
// without a backend, because a failure nobody can report must not be recorded either (a FailFast
// run would stop with no message to show).
//
//go:noinline
func (c *Context) failf(format string, args ...any) {
	c.failure.Failed = true
	if c.tb != nil {
		c.tb.Helper()
	}
	c.backend.Fatalf("%s", fmt.Sprintf(format, args...))
}
