package report

import "time"

// SuiteStartEvent is emitted when a suite (root Describe) begins.
type SuiteStartEvent struct {
	Name string
	Time time.Time
}

// SuiteEndEvent is emitted when a suite finishes executing.
type SuiteEndEvent struct {
	Name          string
	Time          time.Time
	Duration      time.Duration // elapsed time between this suite's SuiteStartEvent and this event
	TotalSpecs    int           // passed + failed + skipped + filtered
	FailedSpecs   int
	SkippedSpecs  int
	FilteredSpecs int // specs excluded by external test selection (e.g. `go test -run`) before their body ran
}

// SpecStartEvent captures the start of an individual spec (It/Then).
type SpecStartEvent struct {
	Name string
	// Path is the declared scope names enclosing this spec, outermost first, with Name as the last
	// element. Each element is exactly one declared Describe/When/It name, verbatim — a name that
	// itself contains "/" stays one element, so len(Path) always equals the number of scopes that
	// were actually declared. Reporters may treat Path[:len(Path)-1] as the spec's scopes.
	//
	// Path is freshly allocated per event and safe to retain or modify.
	Path []string
	Time time.Time
}

// SpecResultEvent captures the result of an individual spec.
//
// SpecStartEvent is always the exact event this spec's SpecStarted call sent (same Time, not
// reconstructed at finish time) — Duration is measured against it, so a producer that rebuilds
// SpecStartEvent here instead of reusing the original would silently corrupt Duration too.
type SpecResultEvent struct {
	SpecStartEvent
	Failed  bool
	Skipped bool // true for a compile-time SkipIt/Skip spec: body never ran, Duration is 0, Failed is always false
	// Filtered is true when a runnable spec was excluded by external test selection — e.g. a `go test
	// -run` pattern that does not match this spec's subtest name — so its body never ran either.
	// Failed is always false and Duration is always 0, exactly as for Skipped, but the cause differs:
	// Skipped is a decision the suite itself made (XIt/Skip), Filtered is a decision made outside it.
	// A spec is never both Skipped and Filtered.
	Filtered bool
	Duration time.Duration // elapsed time between SpecStartEvent.Time and this event; always 0 when Skipped or Filtered
	Message  string        // short failure summary; empty when not Failed, and also empty for a sequential
	// Fatalf-based assertion failure — runtime.Goexit unwinds the whole Run call before a SpecFinished
	// for that spec is ever emitted, so it never reaches this event at all (see specs.runStepRecovered).
	// Populated today for a recovered panic (the panic value) and for an ItParallel/parallelBackend
	// failure (the recorded failure string).
	Output string // full output/stack trace, if any; only a recovered panic produces one today (its
	// stack trace) — left empty everywhere else, including ItParallel, which has no separable output
	// source to draw from.
}

// EventReporter consumes structured events from the spec runner.
type EventReporter interface {
	SuiteStarted(SuiteStartEvent)
	SuiteFinished(SuiteEndEvent)
	SpecStarted(SpecStartEvent)
	SpecFinished(SpecResultEvent)
}
