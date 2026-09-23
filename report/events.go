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
	TotalSpecs    int           // passed + failed + skipped + filtered + pending
	FailedSpecs   int
	SkippedSpecs  int
	FilteredSpecs int // specs excluded by external test selection (e.g. `go test -run`) before their body ran
	PendingSpecs  int // compile-time PendingIt/Pending specs: declared but not implemented, body never ran
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
	// Hook marks a synthetic group hook case emitted by a BeforeAll/AfterAll failure (issue #207,
	// docs/SUITE_HOOKS_CONTRACT.md H8): "BeforeAll" or "AfterAll", empty for a real spec. It exists
	// so a consumer can tell a hook case apart from a real spec structurally — never by pattern
	// matching Name's bracketed "[BeforeAll]"/"[AfterAll]" text, which is presentation, not
	// identity. Only a failed hook produces an event at all: a passing BeforeAll/AfterAll emits
	// nothing, so a suite that registers no group hooks never sets this field and its report is
	// unaffected.
	Hook string
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
	// Pending is true for a compile-time PendingIt/Pending spec: the specification exists but its
	// implementation does not. Body never ran, Duration is always 0, and Failed is always false —
	// same shape as Skipped — but the cause differs: Skipped is "intentionally not executed",
	// Pending is "not implemented yet". A spec is never more than one of Skipped/Filtered/Pending.
	Pending  bool
	Duration time.Duration // elapsed time between SpecStartEvent.Time and this event; always 0 when Skipped, Filtered or Pending
	Message  string        // short failure summary; empty when not Failed, and also empty for an
	// ordinary Fatalf-based assertion failure even when Failed is true: runtime.Goexit unwinds the
	// goroutine right there, before the message this event would carry is ever built (see
	// specs.runStepRecovered). This event is still emitted for that spec — Failed reflects it — as
	// long as the spec ran in its own subtest, which every spec does by default; only a Fatalf outside
	// any subtest isolation (e.g. a fake backend that doesn't call Goexit at all) would behave
	// differently. Message is populated today for a recovered panic (the panic value) and for an
	// ItParallel/parallelBackend failure (the recorded failure string).
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
