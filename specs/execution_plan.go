// execution_plan.go defines the bytecode ExecutionPlan and CompiledSuite used by the compiler and runner.
package specs

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/report"
)

// ExecutionPlan holds the flat instruction stream and per-spec metadata for the runner.
type ExecutionPlan struct {
	Instructions []Instruction
	ProgramStart []int
	ProgramLen   []int
	Names        []string
	FullNames    []string
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

// Run executes all specs in the plan. Uses one context from the pool per spec.
func (s *CompiledSuite) Run(tb testing.TB) {
	if s == nil || s.Plan == nil || tb == nil || len(s.Plan.ProgramStart) == 0 {
		return
	}
	backend := asTestBackend(tb)
	defer putTestBackend(backend)

	if s.Reporter == nil {
		runPlanSpecsInOrder(backend, nil, s.Plan)
		return
	}
	// counter observes every SpecFinished event to total TotalSpecs/FailedSpecs for SuiteEndEvent:
	// -run filtering can still make the reported count differ from len(plan.ProgramStart).
	counter := &specCounter{EventReporter: s.Reporter}
	name := s.Name
	if name == "" {
		name = backend.Name()
	}
	suiteStart := time.Now()
	s.Reporter.SuiteStarted(report.SuiteStartEvent{Name: name, Time: suiteStart})
	runPlanSpecsInOrder(backend, counter, s.Plan)
	s.Reporter.SuiteFinished(report.SuiteEndEvent{
		Name:          name,
		Time:          time.Now(),
		Duration:      time.Since(suiteStart),
		TotalSpecs:    counter.total,
		FailedSpecs:   counter.failed,
		FilteredSpecs: counter.filtered,
	})
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

// runPlanSpecsInOrder runs every spec in the plan once, in declaration order. The plan is already
// flat — its hooks are compiled into each spec's own instruction range — so there is no group
// nesting to walk here. Each spec still gets its own subtest when the backend wraps a real
// *testing.T; that decision belongs to runSpecProgram, not to this loop.
func runPlanSpecsInOrder(backend testBackend, rep report.EventReporter, plan *ExecutionPlan) {
	for i := 0; i < len(plan.ProgramStart); i++ {
		runExecution(backend, rep, plan, i)
	}
}

func runExecution(backend testBackend, rep report.EventReporter, plan *ExecutionPlan, i int) {
	start := plan.ProgramStart[i]
	length := plan.ProgramLen[i]
	if start+length > len(plan.Instructions) {
		return
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
	ctx, release := acquireContext(backend)
	defer release()
	started := reportSpecStarted(rep, name, path)
	message, output, ran := runSpecProgram(backend, ctx, program, specSubtestName(plan, i))
	reportSpecFinished(rep, started, specResult{Failed: ctx.hasFailed(), Message: message, Output: output, Filtered: !ran})
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
		message, output = runProgram(program, ctx)
		return message, output, true
	}
	t, ok := real.tb.(*testing.T)
	if !ok {
		ctx.Reset(backend)
		message, output = runProgram(program, ctx)
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
	var parked bool
	ran, parked = runSubtestGuardingParallel(t, subtestName, func(subT *testing.T) {
		subBackend := asTestBackend(subT)
		defer putTestBackend(subBackend)
		ctx.Reset(subBackend)
		message, output = runProgram(program, ctx)
	})
	if parked {
		// The body called the unsupported ctx.T.Parallel(). ctx is still Reset to that subtest's
		// backend and the parked body needs it that way, so stop the run rather than let the caller
		// release ctx back to the pool underneath it (#172).
		failUnsupportedSpecBodyParallel(t, ctx, subtestName)
	}
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
// "panic: value" summary, output is the raw stack trace — and reused both for reportRecoveredPanic
// (unchanged wire format: "message\noutput") and as the return value runExecutionContext feeds
// into reportSpecFinished's specResult; they are never reconstructed elsewhere. If the body didn't
// panic but an after-hook instruction (scoped to this one spec here, unlike the group-shared after
// hooks in runner.go) did, that after-hook's own message/output (built the same way, once, in
// runAfterInstructionRecovered) is used instead — first recorded panic wins, matching the
// first-write-wins convention parallelStep/parallelBackend already use. Both stay "" when the spec
// didn't panic at all, including when it failed via runtime.Goexit (a real testing.T.Fatalf/FailNow)
// — recover() cannot observe that case; see specResult's doc comment.
func runProgram(program []Instruction, ctx *Context) (message, output string) {
	var after []Instruction
	for _, inst := range program {
		if inst.Code == OpAfterHook && inst.Fn != nil {
			after = append(after, inst)
		}
	}
	defer func() {
		message, output = recoverSpecFailure(ctx, recover(), "panic")
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
	defer func() { message, output = recoverSpecFailure(ctx, recover(), "panic in after hook") }()
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
