package report

import "time"

// SuiteStartEvent is emitted when a suite (root Describe) begins.
type SuiteStartEvent struct {
	Name string
	Time time.Time
}

// SuiteEndEvent is emitted when a suite finishes executing.
type SuiteEndEvent struct {
	Name         string
	Time         time.Time
	Duration     time.Duration // elapsed time between this suite's SuiteStartEvent and this event
	TotalSpecs   int           // passed + failed + skipped
	FailedSpecs  int
	SkippedSpecs int
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
	// Candidate identifies which one of a Paths()-generated spec's executed candidates this event
	// is about (#103); nil for a spec that doesn't use Paths(). When non-nil, its AttemptIndex and
	// Fingerprint match the trailing "generated#<AttemptIndex>..._<Fingerprint>" segment of the Go
	// subtest name that candidate's execution used, so go test -v output, test2json, and this event
	// all identify the same logical candidate by the same numbers.
	Candidate *CandidateIdentity
}

// CandidateIdentity is SpecStartEvent.Candidate's payload (#103): everything needed to name and,
// given the spec's own declared Paths() configuration, reproduce one executed candidate.
//
// Reproducing the candidate this identifies means rerunning the same declared Paths() spec with
// Seed held fixed through AttemptIndex proposals: every strategy this package implements draws all
// of its randomness from one RNG seeded once from Seed (see PathGenerator's, CoverageExplorer's,
// and SmartExplorer's own constructors) — nothing else, including a -run pattern that skips an
// earlier candidate's subtest body, perturbs that sequence (the earlier candidate's Propose/Execute/
// AdmitFeedback Go code still runs regardless of -run; see specs' runExecutionContext for the one
// caveat this leaves for the coverage-guided strategies' corpus growth).
type CandidateIdentity struct {
	// Strategy names which of the five path-generation strategies produced this candidate:
	// "cartesian", "sample", "explore", "explore-coverage", or "explore-smart".
	Strategy string
	// Seed is the RNG seed the generator resolved for this run — set explicitly via PathSpec.Seed,
	// or the package's documented default otherwise. HasSeed is false, and Seed is meaningless (left
	// 0), only for Strategy == "cartesian": pure enumeration, no RNG involved.
	Seed    int64
	HasSeed bool
	// AttemptIndex is this candidate's 1-based position among every proposal made for this spec's
	// run, in generation order — assigned once, never reused, so it alone already makes every
	// candidate's Go subtest name collision-free regardless of Fingerprint. Matches the
	// "generated#<AttemptIndex>" segment of that name.
	AttemptIndex int
	// AcceptedIndex is this candidate's 1-based position among only the accepted proposals. Every
	// candidate that reaches execution (and therefore ever gets a SpecStartEvent) was accepted, so
	// AcceptedIndex always equals AttemptIndex in every build of this package today — no strategy
	// wires a rejecting Accept callback yet — but the two are tracked as distinct fields because
	// Accept is public API a future strategy could use to reject proposals.
	AcceptedIndex int
	// Fingerprint is a content fingerprint of the candidate's path values (PathValues.Hash,
	// truncated to 32 bits): a diagnostic aid for spotting identical inputs across candidates or
	// runs at a glance. It is never an identity guarantee on its own (AttemptIndex already is) and
	// never embeds a raw value. Matches the trailing hex digits of the Go subtest name.
	Fingerprint uint32
}

// SpecResultEvent captures the result of an individual spec.
//
// SpecStartEvent is always the exact event this spec's SpecStarted call sent (same Time, not
// reconstructed at finish time) — Duration is measured against it, so a producer that rebuilds
// SpecStartEvent here instead of reusing the original would silently corrupt Duration too.
type SpecResultEvent struct {
	SpecStartEvent
	Failed   bool
	Skipped  bool          // true for a compile-time SkipIt/Skip spec: body never ran, Duration is 0, Failed is always false
	Duration time.Duration // elapsed time between SpecStartEvent.Time and this event; always 0 when Skipped
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
