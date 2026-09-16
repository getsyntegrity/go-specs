// execution_plan.go defines the bytecode ExecutionPlan and CompiledSuite used by the compiler and runner.
package specs

import (
	"context"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pablogore/go-specs/report"
)

// ExecutionPlan holds the flat instruction stream and per-spec metadata for the runner.
type ExecutionPlan struct {
	Instructions []Instruction
	ProgramStart []int
	ProgramLen   []int
	Names        []string
	FullNames    []string
	PathGens     []*PathGenerator
	// PathScopes is the shared backing array holding the declared scope names that enclose each
	// spec, laid out the same way Instructions is: PathScopeStart[i] and PathScopeLen[i] delimit
	// spec i's window. The spec's own name is not repeated here — Names[i] already holds it, and
	// SpecStartEvent.Path is the window followed by Names[i].
	//
	// The scopes are stored rather than recovered from FullNames because joining with "/" is not
	// injective — a declared name that itself contains "/" is indistinguishable from a scope
	// boundary once joined, so splitting the breadcrumb back apart invents scopes that were never
	// declared. FullNames stays as-is: it is the subtest identity, a different derivation.
	//
	// Consecutive specs sharing the same scopes share one window instead of each storing a copy.
	// Storing a private copy per spec is O(specs × depth) string headers, and Go grows a large slice
	// by ~1.25x, so the discarded intermediate arrays cost several times the final size again. On
	// 50k sibling specs that shape measured +91% build memory; sharing the window removes it.
	//
	// The sharing is by adjacency, not by set: appendSpecPath reuses only the window it handed the
	// previous spec (see there). Every spec in one block shares a window, which is the shape suites
	// have, but an interleaved tree — When("a"){It}, When("b"){It}, When("a"){It} — reuses nothing
	// and degrades back to a copy per spec. That is a size trade, never a correctness one: the
	// reported Path is identical either way. Deduplicating across the whole plan would need a lookup
	// keyed on the chain, which costs more than it saves for the tree shapes seen in practice.
	PathScopes     []string
	PathScopeStart []int
	PathScopeLen   []int
}

func newExecutionPlan(estimatedSpecs int) *ExecutionPlan {
	if estimatedSpecs <= 0 {
		estimatedSpecs = 64
	}
	return &ExecutionPlan{
		Instructions: make([]Instruction, 0, estimatedSpecs*8),
		ProgramStart: make([]int, 0, estimatedSpecs),
		ProgramLen:   make([]int, 0, estimatedSpecs),
		Names:        make([]string, 0, estimatedSpecs),
		FullNames:    make([]string, 0, estimatedSpecs),
		PathGens:     make([]*PathGenerator, 0, estimatedSpecs),
		// PathScopes is sized for distinct scope chains, not for one copy per spec: sibling specs
		// share a window, so this grows with the shape of the tree rather than with the spec count.
		PathScopes:     make([]string, 0, 16),
		PathScopeStart: make([]int, 0, estimatedSpecs),
		PathScopeLen:   make([]int, 0, estimatedSpecs),
	}
}

type planScratch struct {
	beforeFlat []func(*Context)
	afterFlat  []func(*Context)
	program    []Instruction
	path       []string
}

var planScratchPool = sync.Pool{
	New: func() any {
		return &planScratch{
			beforeFlat: make([]func(*Context), 0, 32),
			afterFlat:  make([]func(*Context), 0, 32),
			program:    make([]Instruction, 0, 32),
			path:       make([]string, 0, 8),
		}
	},
}

func countSpecsArena(arena *NodeArena, rootID int) int {
	if arena == nil || rootID < 0 || rootID >= len(arena.Nodes) {
		return 0
	}
	n := 0
	if arena.Nodes[rootID].Type == ItNode {
		n = 1
	}
	for _, cid := range arena.Children[rootID] {
		n += countSpecsArena(arena, cid)
	}
	return n
}

func buildExecutionPlanFromArena(arena *NodeArena, rootID int, plan *ExecutionPlan, scratch *planScratch) {
	if arena == nil || plan == nil || scratch == nil {
		return
	}
	scratch.path = scratch.path[:0]
	buildExecutionPlanFromArenaRec(arena, rootID, plan, scratch)
}

func buildExecutionPlanFromArenaRec(arena *NodeArena, nodeID int, plan *ExecutionPlan, scratch *planScratch) {
	if arena == nil || nodeID < 0 || nodeID >= len(arena.Nodes) {
		return
	}
	node := &arena.Nodes[nodeID]
	name := node.Name
	if name != "" && node.Type != SuiteNode {
		scratch.path = append(scratch.path, name)
	}
	if node.Type == ItNode {
		scratch.beforeFlat = scratch.beforeFlat[:0]
		scratch.afterFlat = scratch.afterFlat[:0]
		ancestorIDs := collectAncestorIDs(arena, node.Parent)
		for _, id := range ancestorIDs {
			scratch.beforeFlat = append(scratch.beforeFlat, arena.BeforeHooks[id]...)
			scratch.afterFlat = append(scratch.afterFlat, arena.AfterHooks[id]...)
		}
		scratch.beforeFlat = append(scratch.beforeFlat, arena.BeforeHooks[nodeID]...)
		scratch.afterFlat = append(scratch.afterFlat, arena.AfterHooks[nodeID]...)
		scratch.program = scratch.program[:0]
		if node.PathGen != nil {
			scratch.program = append(scratch.program, Instruction{Code: OpSetPath, Fn: nil})
		}
		for _, h := range scratch.beforeFlat {
			if h != nil {
				scratch.program = append(scratch.program, Instruction{Code: OpBeforeHook, Fn: h})
			}
		}
		if node.Fn != nil {
			scratch.program = append(scratch.program, Instruction{Code: OpBody, Fn: node.Fn})
		}
		for i := len(scratch.afterFlat) - 1; i >= 0; i-- {
			if h := scratch.afterFlat[i]; h != nil {
				scratch.program = append(scratch.program, Instruction{Code: OpAfterHook, Fn: h})
			}
		}
		start := len(plan.Instructions)
		plan.Instructions = append(plan.Instructions, scratch.program...)
		plan.ProgramStart = append(plan.ProgramStart, start)
		plan.ProgramLen = append(plan.ProgramLen, len(scratch.program))
		plan.Names = append(plan.Names, name)
		plan.FullNames = append(plan.FullNames, strings.Join(scratch.path, "/"))
		plan.PathGens = append(plan.PathGens, node.PathGen)
		// scratch.path already has this spec's own name pushed as its last element (an ItNode is
		// not a SuiteNode, so the push above applies to it too); the scopes are everything before
		// it. An unnamed spec was never pushed, so for it the whole of scratch.path is scopes.
		scopes := scratch.path
		if name != "" {
			scopes = scopes[:len(scopes)-1]
		}
		appendSpecPath(plan, scopes)
	}
	for _, cid := range arena.Children[nodeID] {
		buildExecutionPlanFromArenaRec(arena, cid, plan, scratch)
	}
	if name != "" && node.Type != SuiteNode && len(scratch.path) > 0 {
		scratch.path = scratch.path[:len(scratch.path)-1]
	}
}

// collectAncestorIDs returns ancestor IDs from root to the given node (inclusive), so that hooks are in declaration order.
func collectAncestorIDs(arena *NodeArena, nodeID int) []int {
	if arena == nil || nodeID < 0 {
		return nil
	}
	var ids []int
	for id := nodeID; id >= 0 && id < len(arena.Nodes); id = arena.Nodes[id].Parent {
		ids = append(ids, id)
	}
	for i, j := 0, len(ids)-1; i < j; i, j = i+1, j-1 {
		ids[i], ids[j] = ids[j], ids[i]
	}
	return ids
}

// CompiledSuite holds the compiled plan and optional arena reference. Run executes the plan.
type CompiledSuite struct {
	Plan     *ExecutionPlan
	Arena    *NodeArena
	RootID   int
	Name     string // suite name for SuiteStartEvent/SuiteEndEvent; falls back to the backend's name if empty
	Reporter report.EventReporter
}

// Run executes all specs in the plan. Uses one context from the pool per spec (or per path iteration).
func (s *CompiledSuite) Run(tb testing.TB) {
	s.run(tb, nil)
}

func (s *CompiledSuite) run(tb testing.TB, runCtx context.Context) []proposalControllerResult {
	if s == nil || s.Plan == nil || tb == nil || len(s.Plan.ProgramStart) == 0 {
		return nil
	}
	if runCtx == nil {
		var cancel context.CancelFunc
		runCtx, cancel = executionContext(tb)
		defer cancel()
	}
	backend := asTestBackend(tb)
	defer putTestBackend(backend)

	if s.Reporter == nil {
		return runPlanSpecsInOrder(runCtx, backend, nil, s.Plan)
	}
	// counter observes every SpecFinished event to total TotalSpecs/FailedSpecs for SuiteEndEvent:
	// Paths() can execute a variable number of candidates per plan index, so the plan alone can't
	// tell us the count up front.
	counter := &specCounter{EventReporter: s.Reporter}
	name := s.Name
	if name == "" {
		name = backend.Name()
	}
	suiteStart := time.Now()
	s.Reporter.SuiteStarted(report.SuiteStartEvent{Name: name, Time: suiteStart})
	results := runPlanSpecsInOrder(runCtx, backend, counter, s.Plan)
	s.Reporter.SuiteFinished(report.SuiteEndEvent{
		Name:          name,
		Time:          time.Now(),
		Duration:      time.Since(suiteStart),
		TotalSpecs:    counter.total,
		FailedSpecs:   counter.failed,
		FilteredSpecs: counter.filtered,
	})
	return results
}

// specCounter decorates an EventReporter to tally executed/failed/filtered specs for the enclosing
// suite's SuiteEndEvent, then forwards every event unchanged to the underlying reporter.
type specCounter struct {
	report.EventReporter
	total    int
	failed   int
	filtered int
}

func (c *specCounter) SpecFinished(e report.SpecResultEvent) {
	c.total++
	if e.Failed {
		c.failed++
	}
	if e.Filtered {
		c.filtered++
	}
	c.EventReporter.SpecFinished(e)
}

func executionContext(tb testing.TB) (context.Context, context.CancelFunc) {
	ctx := context.Background()
	if contextual, ok := tb.(interface{ Context() context.Context }); ok && contextual.Context() != nil {
		ctx = contextual.Context()
	}
	if timed, ok := tb.(interface{ Deadline() (time.Time, bool) }); ok {
		if deadline, ok := timed.Deadline(); ok {
			return context.WithDeadline(ctx, deadline)
		}
	}
	return context.WithCancel(ctx)
}

// runPlanSpecsInOrder runs every spec in the plan once, in declaration order. The plan is already
// flat — its hooks are compiled into each spec's own instruction range — so there is no group
// nesting to walk here. Each spec still gets its own subtest when the backend wraps a real
// *testing.T; that decision belongs to runSpecProgram, not to this loop.
func runPlanSpecsInOrder(runCtx context.Context, backend testBackend, rep report.EventReporter, plan *ExecutionPlan) []proposalControllerResult {
	results := make([]proposalControllerResult, 0, len(plan.ProgramStart))
	for i := 0; i < len(plan.ProgramStart); i++ {
		results = append(results, runExecutionContext(runCtx, backend, rep, plan, i))
	}
	return results
}

func runExecution(backend testBackend, rep report.EventReporter, plan *ExecutionPlan, i int) proposalControllerResult {
	return runExecutionContext(context.Background(), backend, rep, plan, i)
}

func runExecutionContext(runCtx context.Context, backend testBackend, rep report.EventReporter, plan *ExecutionPlan, i int) proposalControllerResult {
	start := plan.ProgramStart[i]
	length := plan.ProgramLen[i]
	if start+length > len(plan.Instructions) {
		return proposalControllerResult{}
	}
	program := plan.Instructions[start : start+length]
	name := specEventName(plan, i)
	// path feeds reportSpecStarted and nothing else, and that returns a zero event when rep is nil.
	// Building it unconditionally would cost one allocation per spec per run on the reporter-less
	// path — the one this package advertises as allocation-free.
	var path []string
	if rep != nil {
		path = specEventPath(plan, i)
	}
	if i < len(plan.PathGens) && plan.PathGens[i] != nil {
		gen := plan.PathGens[i]
		seq := gen.sequence()
		maxAttempts, maxAccepted, maxRejections := gen.bounds()
		// wantsCoverage is true only for the two strategies that actually consume Coverage
		// (CoverageExplorer/SmartExplorer's Feedback) — allocating and hitting a 64KB bitmap per
		// candidate for Cartesian/Sample/plain-Explore, which never look at it, would be pure waste.
		wantsCoverage := gen.mode == ExplorationGuided && (gen.strategy == strategyCoverage || gen.strategy == strategySmart)
		// lastCoverage hands the Coverage collected by Execute to AdmitFeedback for the same
		// candidate. proposalController.Run calls them back-to-back with no concurrency in between
		// (see its doc comment: "no parallel execution option"), so a closure variable is safe —
		// keeping Coverage out of proposalCandidate/proposalFeedback keeps the controller itself
		// generic instead of coupling it to path-generation concerns.
		var lastCoverage *Coverage
		// Both per-candidate names are computed only when someone will actually read them, and the
		// two predicates are hoisted out of the closure so the decision costs nothing per candidate.
		// namesSubtests is false for a *testing.B or a fake backend, whose Run never opens a real
		// subtest — there the name would be built and thrown away on the hot path (see
		// runIsolatedCase). reports is false when the plan runs without a reporter.
		namesSubtests := backendNamesSubtests(backend)
		reports := rep != nil
		specIdentity := ""
		if namesSubtests {
			specIdentity = specIdentityName(plan, i)
		}
		return newProposalController(proposalControllerConfig{
			MaxAttempts:   maxAttempts,
			MaxAccepted:   maxAccepted,
			MaxRejections: maxRejections,
			Propose:       seq.next,
			Execute: func(candidate proposalCandidate) bool {
				// Every executed candidate is its own spec execution and reports its own
				// SpecStarted/SpecFinished — not just the last accepted one. Hiding rejected-
				// then-retried or intermediate Explore candidates would misrepresent how many
				// executions actually happened, how long the suite really took, and where a
				// failure occurred.
				//
				// Each execution is identified by this candidate, not by a shared literal: the
				// reporter gets the framework's own formatted values plus the executed ordinal,
				// and the subtest gets the -run-safe form. See candidate_identity.go for the
				// contract both names satisfy.
				reportName := name
				if reports {
					reportName = generatedCaseReportName(gen, name, candidate)
				}
				started := reportSpecStarted(rep, reportName, path)
				var cov *Coverage
				if wantsCoverage {
					cov = &Coverage{}
				}
				caseName := ""
				if namesSubtests {
					caseName = generatedCaseName(gen, specIdentity, candidate)
				}
				result := runIsolatedCase(backend, caseName, program, candidate.Values, cov)
				reportSpecFinished(rep, started, specResult{Failed: result.Failed})
				lastCoverage = cov
				return !result.Failed
			},
			AdmitFeedback: func(feedback proposalFeedback) {
				seq.admitFeedback(feedback.Candidate.Values, feedback.Passed, lastCoverage)
			},
		}).Run(runCtx)
	}
	ctx, release := acquireContext(backend)
	defer release()
	started := reportSpecStarted(rep, name, path)
	message, output, ran := runSpecProgram(backend, ctx, program, specSubtestName(plan, i))
	reportSpecFinished(rep, started, specResult{Failed: ctx.failed, Message: message, Output: output, Filtered: !ran})
	return proposalControllerResult{}
}

// runSpecProgram runs program for one ExecutionPlan spec against ctx, isolated in its own subtest
// when backend wraps a real *testing.T (#74) — same gate runIsolatedCase already uses (see this
// function's mirror, runSpecRecovered, in runner.go) — so a real Fatalf/FailNow (runtime.Goexit)
// inside program unwinds only that subtest's goroutine, letting the remaining specs in this plan
// still run and be reported. A fake backend (e.g. controlledBackend, whose Run is a no-op) or a
// *testing.B falls back to running program directly against ctx/backend, same as before this
// isolation existed.
//
// subtestName is the spec's full Describe/When/It breadcrumb (specSubtestName), passed to
// testing.T.Run purely for -v/-run/IDE/test2json identity. It is never read back from t.Name():
// SpecStartEvent/SpecResultEvent keep taking Name from plan.Names and Path from plan.FullNames, so
// testing's sanitization (spaces to "_") and "#01" suffixing never leak into reported identity.
//
// ran is false exactly when external test selection (e.g. `go test -run`) discarded the subtest,
// threaded back so the caller can report the spec as Filtered instead of passed (#111). It cannot
// be read from t.Run's own bool return: testing.T.Run returns true for a filtered-out subtest too
// (a subtest that never ran vacuously "succeeded"), so runSpecProgramIsolated instead sets ran from
// inside the closure itself — which only runs at all when the filter accepted the subtest. The two
// fast paths above never go through t.Run at all, so they always ran.
func runSpecProgram(backend testBackend, ctx *Context, program []Instruction, subtestName string) (message, output string, ran bool) {
	real, ok := backend.(*runnableBackend)
	if !ok {
		ctx.Reset(backend)
		ctx.SetPathValues(PathValues{})
		message, output = runProgram(program, ctx, nil)
		return message, output, true
	}
	t, ok := real.tb.(*testing.T)
	if !ok {
		ctx.Reset(backend)
		ctx.SetPathValues(PathValues{})
		message, output = runProgram(program, ctx, nil)
		return message, output, true
	}
	return runSpecProgramIsolated(t, ctx, program, subtestName)
}

// runSpecProgramIsolated creates the real subtest and runs program inside it. Split out from
// runSpecProgram because the closure below captures named returns by reference: if it lived directly
// in runSpecProgram, Go's escape analysis would heap-allocate that function's message/output for
// every call — including the non-*testing.T fast paths above that never reach this line — since
// escape analysis decides a variable's storage class for the whole function, not per branch. Keeping
// the capture inside its own function scopes that heap allocation to the isolation path only (see the
// identical split for runner.go's runSpecRecovered/runSpecIsolated).
func runSpecProgramIsolated(t *testing.T, ctx *Context, program []Instruction, subtestName string) (message, output string, ran bool) {
	t.Run(subtestName, func(subT *testing.T) {
		ran = true
		subBackend := asTestBackend(subT)
		defer putTestBackend(subBackend)
		ctx.Reset(subBackend)
		ctx.SetPathValues(PathValues{})
		message, output = runProgram(program, ctx, nil)
	})
	return
}

// specEventName returns plan.Names[i], or "" for a plan built without per-spec metadata (e.g. a
// hand-built ExecutionPlan in a test that only populates Instructions/ProgramStart/ProgramLen).
func specEventName(plan *ExecutionPlan, i int) string {
	if i < 0 || i >= len(plan.Names) {
		return ""
	}
	return plan.Names[i]
}

// specSubtestName returns the Go subtest identity for plan spec i: its full Describe/When/It
// breadcrumb (plan.FullNames[i]), not the leaf It name, so two specs sharing a leaf name under
// different scopes stay independently selectable with `go test -run` instead of being told apart
// only by testing's incidental "#01" suffix (#102). That holds whenever the two breadcrumbs differ
// once testing has normalized them; see joinSubtestPath for the mapping's contract and for the
// ambiguity it accepts and inherits from testing.T.Run.
//
// It falls back to plan.Names[i] for a plan built without breadcrumbs — a hand-built ExecutionPlan,
// or a compiler with an empty name stack, where the leaf name already is the whole breadcrumb.
//
// The empty string doubles as the "no breadcrumb" sentinel, which It("") declared outside any scope
// also produces. That case is deliberately not given a separate representation: both branches return
// "" for it, so the sentinel is indistinguishable from the value it stands for only where the two
// agree. Carrying a presence flag would cost a field on the exported ExecutionPlan — and a slice per
// plan — to encode a distinction nothing can observe.
func specSubtestName(plan *ExecutionPlan, i int) string {
	if i >= 0 && i < len(plan.FullNames) && plan.FullNames[i] != "" {
		return plan.FullNames[i]
	}
	return specEventName(plan, i)
}

// appendSpecPath records the scopes enclosing one spec and stores the window they occupy. Called
// once per spec, in the same order as Names/FullNames. The spec's own name is not passed: Names
// already holds it and specEventPath appends it.
//
// When scopes matches the window the previous spec was given — the common case, since every spec
// declared in the same block sees the same enclosing scopes — that window is reused instead of
// appending a second copy.
//
// Only the previous window is considered, deliberately: specs arrive in declaration order, so one
// comparison against the tail catches every run of siblings without a per-chain lookup. It does not
// catch a chain that recurs after an interruption, which then stores a second copy. See the
// PathScopes field comment for what that trade is and is not.
func appendSpecPath(plan *ExecutionPlan, scopes []string) {
	start := len(plan.PathScopes) - len(scopes)
	if start < 0 || !slices.Equal(plan.PathScopes[start:], scopes) {
		start = len(plan.PathScopes)
		plan.PathScopes = append(plan.PathScopes, scopes...)
	}
	plan.PathScopeStart = append(plan.PathScopeStart, start)
	plan.PathScopeLen = append(plan.PathScopeLen, len(scopes))
}

// specEventPath returns spec i's enclosing scopes followed by its own name, for
// SpecStartEvent.Path, or nil for a plan without per-spec metadata (see specEventName).
//
// The result is a fresh copy, never a window into plan.PathScopes: Path is handed to arbitrary
// report.EventReporter implementations, and one that sorts or truncates it in place would
// otherwise corrupt the plan for every later spec and every later run of the same CompiledSuite —
// and now that scope windows are shared, for every sibling spec as well.
func specEventPath(plan *ExecutionPlan, i int) []string {
	if i < 0 || i >= len(plan.PathScopeStart) || i >= len(plan.PathScopeLen) || i >= len(plan.Names) {
		return nil
	}
	start, length := plan.PathScopeStart[i], plan.PathScopeLen[i]
	if start < 0 || length < 0 || start+length > len(plan.PathScopes) {
		return nil
	}
	path := make([]string, 0, length+1)
	path = append(path, plan.PathScopes[start:start+length]...)
	return append(path, plan.Names[i])
}

// reportSpecStarted emits SpecStarted and returns the event it sent, so reportSpecFinished can
// reuse its Time — SpecResultEvent embeds SpecStartEvent, and that Time means when the spec
// started, not when it finished.
func reportSpecStarted(rep report.EventReporter, name string, path []string) report.SpecStartEvent {
	if rep == nil {
		return report.SpecStartEvent{}
	}
	e := report.SpecStartEvent{Name: name, Path: path, Time: time.Now()}
	rep.SpecStarted(e)
	return e
}

func reportSpecFinished(rep report.EventReporter, start report.SpecStartEvent, result specResult) {
	if rep == nil {
		return
	}
	duration := time.Since(start.Time)
	if result.Filtered {
		duration = 0
	}
	rep.SpecFinished(report.SpecResultEvent{
		SpecStartEvent: start,
		Failed:         result.Failed,
		Filtered:       result.Filtered,
		Duration:       duration,
		Message:        result.Message,
		Output:         result.Output,
	})
}

// runProgram executes one spec's instructions directly in the caller's goroutine (the default,
// non-path, non-isolated path — see runIsolatedCaseDirect for the path-generated equivalent).
// A panic anywhere in the before/body instructions is recovered so it fails this one spec instead
// of crashing the process; after-hook instructions still run regardless, each isolated from the
// others so one panicking after-hook doesn't stop the rest.
//
// On a recovered panic, message and output are built exactly once — message is the short
// "panic: value" summary, output is the raw stack trace — and reused both for ctx.backend.Errorf
// (unchanged wire format: "message\noutput") and as the return value runExecutionContext feeds
// into reportSpecFinished's specResult; they are never reconstructed elsewhere. If the body didn't
// panic but an after-hook instruction (scoped to this one spec here, unlike the group-shared after
// hooks in runner.go) did, that after-hook's own message/output (built the same way, once, in
// runAfterInstructionRecovered) is used instead — first recorded panic wins, matching the
// first-write-wins convention parallelStep/parallelBackend already use. Both stay "" when the spec
// didn't panic at all, including when it failed via runtime.Goexit (a real testing.T.Fatalf/FailNow)
// — recover() cannot observe that case; see specResult's doc comment.
func runProgram(program []Instruction, ctx *Context, path *PathValues) (message, output string) {
	if path != nil {
		ctx.SetPathValues(*path)
	}
	var after []Instruction
	for _, inst := range program {
		if inst.Code == OpAfterHook && inst.Fn != nil {
			after = append(after, inst)
		}
	}
	defer func() {
		if recovered := recover(); recovered != nil && !isExpectedAbort(recovered) {
			ctx.recordFailure()
			message = fmt.Sprintf("panic: %v", recovered)
			output = string(debug.Stack())
			ctx.backend.Errorf("%s\n%s", message, output)
		}
		for _, inst := range after {
			m, o := runAfterInstructionRecovered(ctx, inst)
			if message == "" {
				message, output = m, o
			}
		}
	}()
	for _, inst := range program {
		if inst.Code == OpAfterHook {
			continue
		}
		if inst.Fn != nil {
			inst.Fn(ctx)
		}
	}
	return
}

// runAfterInstructionRecovered runs a single after-hook instruction, recovering any panic so it
// can't stop the remaining after-hooks for this spec. message/output follow the same build-once
// contract as runProgram's own panic recovery — see its doc comment.
func runAfterInstructionRecovered(ctx *Context, inst Instruction) (message, output string) {
	defer func() {
		if recovered := recover(); recovered != nil && !isExpectedAbort(recovered) {
			ctx.recordFailure()
			message = fmt.Sprintf("panic in after hook: %v", recovered)
			output = string(debug.Stack())
			ctx.backend.Errorf("%s\n%s", message, output)
		}
	}()
	inst.Fn(ctx)
	return
}

// isExpectedAbort reports whether a recovered value is the isolatedCaseAbort sentinel a
// controlled testBackend uses to stop one case via FailNow/Fatal/Fatalf — an already-recorded
// stop, not an unexpected panic to report.
func isExpectedAbort(recovered any) bool {
	_, ok := recovered.(isolatedCaseAbort)
	return ok
}

// isolatedCaseAbort lets a controlled backend stop one isolated case without
// terminating the parent test.
type isolatedCaseAbort struct{}

type isolatedCaseResult struct {
	Failed       bool
	Panic        any
	Path         PathValues
	ContextReset bool
}

// backendNamesSubtests reports whether backend will actually open a named Go subtest, i.e. whether
// a generated candidate's subtest name is going to be read by anyone. Only a runnableBackend over a
// real *testing.T does; a *testing.B or a fake backend runs the case inline and discards the name
// (see runnableBackend.Run and runIsolatedCase). Callers use this to skip building the name at all
// on those paths, which is what keeps the benchmark backends' allocation profile unchanged.
func backendNamesSubtests(backend testBackend) bool {
	real, ok := backend.(*runnableBackend)
	if !ok {
		return false
	}
	_, isT := real.tb.(*testing.T)
	return isT
}

// runIsolatedCase executes real generated cases in a subtest so Fatal and FailNow
// terminate only that case while preserving the parent test's failure semantics. cov, when
// non-nil, is wired into the Context so assertions executed by program record real coverage
// into it (see Context.RecordCoverage) — the caller owns the pointer and reads it back directly,
// nothing needs to be copied out before the Context is returned to the pool.
//
// name identifies this one candidate (generatedCaseName; "" when backendNamesSubtests said nobody
// would read it). It used to be the literal "generated" for every candidate of every spec, which
// left `go test -v` showing an undifferentiated "generated#01" run and made a generated case
// unselectable with -run (#103). As with the sequential path, the name is presentation only: it is
// never read back from t.Name(), and reported identity comes from the plan and the generator.
func runIsolatedCase(backend testBackend, name string, program []Instruction, path PathValues, cov *Coverage) (result isolatedCaseResult) {
	if real, ok := backend.(*runnableBackend); ok {
		real.Run(name, func(tb testing.TB) {
			caseBackend := asTestBackend(tb)
			defer putTestBackend(caseBackend)
			defer func() { result.Failed = result.Failed || tb.Failed() }()
			result = runIsolatedCaseDirect(caseBackend, program, path, cov)
		})
		return result
	}
	return runIsolatedCaseDirect(backend, program, path, cov)
}

func runIsolatedCaseDirect(backend testBackend, program []Instruction, path PathValues, cov *Coverage) (result isolatedCaseResult) {
	ctx, release := acquireContext(backend)
	ctx.coverage = cov
	ctx.SetPathValues(path)
	result.Path = ctx.Path().clone()

	var after []Instruction
	for _, inst := range program {
		if inst.Code == OpAfterHook && inst.Fn != nil {
			after = append(after, inst)
		}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if _, aborted := recovered.(isolatedCaseAbort); !aborted {
				result.Panic = recovered
			}
			result.Failed = true
		}
		for _, inst := range after {
			func() {
				defer func() {
					if recovered := recover(); recovered != nil && result.Panic == nil {
						result.Panic = recovered
						result.Failed = true
					}
				}()
				inst.Fn(ctx)
			}()
		}
		result.Failed = result.Failed || ctx.failed
		release()
		result.ContextReset = true
	}()

	for _, inst := range program {
		if inst.Code == OpAfterHook {
			continue
		}
		if inst.Fn != nil {
			inst.Fn(ctx)
		}
	}
	return result
}
