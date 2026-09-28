package report

import "time"

// SuiteStartEvent is emitted when a suite (root Describe) begins.
type SuiteStartEvent struct {
	Name string
	Time time.Time
}

// SuiteEndEvent is emitted when a suite finishes executing.
type SuiteEndEvent struct {
	Name           string
	Time           time.Time
	Duration       time.Duration // elapsed time between this suite's SuiteStartEvent and this event
	TotalSpecs     int           // passed + failed + skipped + filtered + pending
	FailedSpecs    int
	SkippedSpecs   int
	FilteredSpecs  int // specs excluded by external test selection (e.g. `go test -run`) before their body ran
	PendingSpecs   int // compile-time PendingIt/Pending specs: declared but not implemented, body never ran
	UnstartedSpecs int // specs a fail-fast stop prevented from ever running (issue #274); never counted in TotalSpecs
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

// HookKind marks whether a spec-shaped event is a synthetic group hook case emitted by a
// BeforeAll/AfterAll failure (issue #207, docs/SUITE_HOOKS_CONTRACT.md H8), or a real spec. It is
// a compact uint8 enum rather than a string so that adding it to SpecResultEvent (see below) costs
// nothing: SpecResultEvent is copied by value once per spec on every engine, so any size growth is
// paid by every suite, including one that never registers a BeforeAll/AfterAll at all — exactly
// what H10 ("no cost for suites without BeforeAll/AfterAll") forbids. A string field here would
// grow SpecResultEvent by 16 bytes; HookKind fits in existing struct padding instead (see
// SpecResultEvent.Hook).
type HookKind uint8

const (
	// HookNone is the zero value: an ordinary spec, not a synthetic hook case.
	HookNone HookKind = iota
	HookBeforeAll
	HookAfterAll
)

// String returns "BeforeAll", "AfterAll", or "" for HookNone — the same text callers already read
// off Case.Hook as a plain string.
func (k HookKind) String() string {
	switch k {
	case HookBeforeAll:
		return "BeforeAll"
	case HookAfterAll:
		return "AfterAll"
	default:
		return ""
	}
}

// DeclaredKind marks how an Unstarted spec (issue #274) was originally declared in source before a
// fail-fast stop prevented it from ever running: DeclaredSkip for a compile-time SkipIt/Skip spec,
// DeclaredPending for a compile-time PendingIt/Pending spec, DeclaredNone for an ordinary spec (or
// whenever the field does not apply, i.e. every non-Unstarted SpecResultEvent). It is a compact
// uint8 enum for the same reason HookKind is (see its doc above): it occupies remaining
// SpecResultEvent padding instead of growing the struct, so adding it costs nothing per spec.
type DeclaredKind uint8

const (
	// DeclaredNone is the zero value: no compile-time SkipIt/PendingIt declaration applies.
	DeclaredNone DeclaredKind = iota
	DeclaredSkip
	DeclaredPending
)

// String returns "skip", "pending", or "" for DeclaredNone — the same text report.Case.Declared
// carries as a plain string.
func (k DeclaredKind) String() string {
	switch k {
	case DeclaredSkip:
		return "skip"
	case DeclaredPending:
		return "pending"
	default:
		return ""
	}
}

// SpecResultEvent captures the result of an individual spec.
//
// SpecStartEvent is always the exact event this spec's SpecStarted call sent (same Time, not
// reconstructed at finish time) — Duration is measured against it, so a producer that rebuilds
// SpecStartEvent here instead of reusing the original would silently corrupt Duration too.
type SpecResultEvent struct {
	SpecStartEvent
	Failed bool
	// Skipped is true for a compile-time SkipIt/Skip spec (body never ran, Duration is 0) or for a
	// spec whose own subtest skipped at runtime via ctx.T.Skip/Skipf/SkipNow (issue #254): the body
	// did start running there, so Duration may be non-zero. Failed is always false either way — a
	// body that fails and then calls SkipNow is reported Failed instead, matching go test itself
	// (a failed test that later skips still prints FAIL, not SKIP; see the testing package's
	// tRunner). Both causes share this one field rather than a separate runtime-skip marker: nothing
	// downstream (report.Status, JUnit's <skipped>) can distinguish them anyway, and no consumer
	// depends on Duration being 0 for a skipped case.
	Skipped bool
	// Filtered is true when a runnable spec was excluded by external test selection — e.g. a `go test
	// -run` pattern that does not match this spec's subtest name — so its body never ran either.
	// Failed is always false and Duration is always 0, exactly as for a compile-time Skipped spec,
	// but the cause differs: Skipped is a decision the suite itself made (XIt/Skip, or a runtime
	// ctx.T.Skip), Filtered is a decision made outside it. A spec is never both Skipped and Filtered.
	Filtered bool
	// Pending is true for a compile-time PendingIt/Pending spec: the specification exists but its
	// implementation does not. Body never ran, Duration is always 0, and Failed is always false —
	// same shape as a compile-time Skipped spec — but the cause differs: Skipped is "intentionally
	// not executed", Pending is "not implemented yet". A spec is never more than one of
	// Skipped/Filtered/Pending.
	Pending bool
	// Unstarted is true for a spec that CompiledSuite.SetFailFast(true) or Runner.FailFast prevented
	// from ever being reached, after an earlier spec already failed (issue #274). Failed is always
	// false and Duration is always 0, exactly as for Filtered/compile-time-Skipped/Pending, but the
	// cause differs again: none of those describe the suite simply never getting there. A spec is
	// never more than one of Skipped/Filtered/Pending/Unstarted.
	Unstarted bool
	// Hook marks this result as a synthetic group hook case (issue #207, docs/SUITE_HOOKS_CONTRACT.md
	// H8): HookBeforeAll or HookAfterAll, HookNone for a real spec. It lives only here, on the
	// result event — the matching SpecStartEvent this spec's SpecStarted call carried is never
	// marked, so a consumer that wants to distinguish a hook case structurally (never by pattern
	// matching Name's bracketed "[BeforeAll]"/"[AfterAll]" text, which is presentation, not
	// identity) must attribute at SpecFinished. Only a failed hook produces an event at all: a
	// passing BeforeAll/AfterAll emits nothing, so a suite that registers no group hooks never sets
	// this field and its report is unaffected. Placed next to Failed/Skipped/Filtered/Pending
	// deliberately: it occupies padding those bools already leave before Duration, so
	// SpecResultEvent's size is unchanged from before this field existed (H10) — see
	// report/hook_case_test.go's TestSpecResultEventSizeUnchanged.
	Hook HookKind
	// Declared preserves an Unstarted spec's original compile-time declaration (issue #274):
	// DeclaredSkip or DeclaredPending when the unreached spec was itself a SkipIt/PendingIt,
	// DeclaredNone otherwise (including for every non-Unstarted event). Placed next to Hook for the
	// same reason: it occupies the same existing padding, so this field costs nothing per spec
	// either — see report/hook_case_test.go's
	// TestSpecResultEventCarriesUnstartedAndDeclaredWithNoSizeGrowth.
	Declared DeclaredKind
	Duration time.Duration // elapsed time between SpecStartEvent.Time and this event; always 0 when
	// Filtered or Pending, and for a compile-time Skipped spec, since none of those ever ran a body.
	// A runtime-Skipped spec (ctx.T.Skip/Skipf/SkipNow, issue #254) is the one Skipped case where
	// Duration may be non-zero: its body did start running before it skipped.
	Message string // short failure summary; empty when not Failed. Populated for a recovered panic
	// (the panic value), an ItParallel/parallelBackend failure (the recorded failure string), and,
	// since issue #272, an ordinary built-in ctx.Expect/Context.Snapshot assertion failure too: the
	// engine records the formatted assertion text before calling the backend, so a real *testing.T's
	// Fatalf ending the goroutine with runtime.Goexit right there no longer discards it. Message stays
	// empty only for a spec that failed exclusively through ctx.T directly — Error, Fatal, Fail,
	// FailNow, or a Cleanup (#253) — since none of those go through the built-in assertion path that
	// records it; a plain `go test` run still shows that failure (`--- FAIL`), just with no structured
	// text for a report.EventReporter to carry.
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
