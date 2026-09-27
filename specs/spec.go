package specs

import (
	"sync"
	"testing"

	"github.com/getsyntegrity/go-specs/report"
)

// Spec is the DSL handle for building describe/when/it trees.
// The node tree is compiled into an ExecutionPlan once (Compile), then Run reuses it.
//
// A Spec is only usable when it carries a build target: either a bytecode compiler or a registry,
// set by the entry point that created it (Describe, DescribeFlat, DescribeWithReporter,
// DescribeFlatWithReporter, BuildSuite) and threaded into every nested Spec from there. Spec is
// exported with unexported fields, so external code can still write &specs.Spec{}; such a Spec has
// no build target, and its registration methods panic rather than accept an It, a hook or a nested
// block and drop it, which would let a suite that registered nothing report green (issue #151).
// Obtain a Spec from an entry point; never construct one.
type Spec struct {
	tb       testing.TB
	backend  testBackend
	reporter report.EventReporter
	name     string // top-level Describe/DescribeFlat name, used as SuiteStartEvent/SuiteEndEvent.Name

	// compiler/registry are the exact build target this Spec (and any Spec it constructs for a
	// nested Describe/When) writes into. Set once by the top-level entry point (Describe,
	// BuildSuite, ...) and threaded explicitly through every nested call from then on, instead of
	// resolved via package-level global state — two goroutines building unrelated suites concurrently
	// (e.g. two t.Parallel() tests each calling Describe) must never observe each other's compiler or
	// registry. At most one of the two is non-nil for a given Spec.
	compiler *bytecodeCompiler
	registry *registry

	// Either (arena, rootID) when built via Analyze/registry, or plan when built via bytecode compiler.
	arena       *NodeArena
	rootID      int
	plan        *ExecutionPlan // set by top-level Describe when using bytecode compiler (no arena)
	compileOnce sync.Once
	suite       *CompiledSuite
}

// Describe starts a top-level describe block. May be called inside Analyze(fn) or directly.
// After the callback returns, the spec tree is executed automatically (if tb is non-nil).
// When top-level (no active registry), uses bytecode compiler: no NodeArena, plan built directly.
// tb may be *testing.T or *testing.B (e.g. for scaling benchmarks).
func Describe(tb testing.TB, name string, fn func(*Spec)) {
	if currentRegistry() == nil {
		describeWithCompiler(tb, name, nil, fn)
		return
	}
	defer ensureRegistry()()
	file, line := callerLocation(2)
	rootID, pop := enterAnalyzeNode(DescribeNode, name, file, line, nil)
	if rootID < 0 {
		return
	}
	defer pop()
	var backend testBackend
	if tb != nil {
		backend = asTestBackend(tb)
	}
	s := &Spec{tb: tb, backend: backend, name: name, arena: CurrentArena(), rootID: rootID, registry: currentRegistry()}
	if fn != nil {
		fn(s)
	}
	if tb != nil {
		s.Compile()
		s.Run()
	}
}

// describeWithCompiler runs Describe using the bytecode compiler (no arena).
func describeWithCompiler(tb testing.TB, name string, rep report.EventReporter, fn func(*Spec)) {
	c := newBytecodeCompiler()
	c.PushScope(name)
	var backend testBackend
	if tb != nil {
		backend = asTestBackend(tb)
	}
	s := &Spec{tb: tb, backend: backend, reporter: rep, name: name, compiler: c}
	if fn != nil {
		fn(s)
	}
	var groups *planGroups
	s.plan, groups = c.takePlanAndGroups()
	validateHookGroups(s.plan, groups)
	if tb != nil {
		s.Compile()
		s.suite.groups = groups
		s.suite.Run(tb)
	}
}

// BuildSuite builds the spec tree and compiles it once; returns the CompiledSuite without running.
// Use for benchmarks: build once, then call suite.Run(tb) in a loop to measure execution only.
// When top-level, uses bytecode compiler (no arena).
func BuildSuite(tb testing.TB, name string, fn func(*Spec)) *CompiledSuite {
	if currentRegistry() == nil {
		c := newBytecodeCompiler()
		c.PushScope(name)
		s := &Spec{tb: tb, backend: nil, compiler: c}
		if fn != nil {
			fn(s)
		}
		var groups *planGroups
		s.plan, groups = c.takePlanAndGroups()
		validateHookGroups(s.plan, groups)
		s.Compile()
		s.suite.groups = groups
		return s.suite
	}
	defer ensureRegistry()()
	file, line := callerLocation(2)
	rootID, pop := enterAnalyzeNode(DescribeNode, name, file, line, nil)
	if rootID < 0 {
		return nil
	}
	defer pop()
	s := &Spec{tb: tb, backend: nil, arena: CurrentArena(), rootID: rootID, registry: currentRegistry()}
	if fn != nil {
		fn(s)
	}
	s.Compile()
	return s.suite
}

// DescribeWithReporter starts a top-level describe block with a reporter.
func DescribeWithReporter(tb testing.TB, name string, rep report.EventReporter, fn func(*Spec)) {
	if currentRegistry() == nil {
		describeWithCompiler(tb, name, rep, fn)
		return
	}
	defer ensureRegistry()()
	file, line := callerLocation(2)
	rootID, pop := enterAnalyzeNode(DescribeNode, name, file, line, nil)
	if rootID < 0 {
		return
	}
	defer pop()
	var backend testBackend
	if tb != nil {
		backend = asTestBackend(tb)
	}
	s := &Spec{tb: tb, backend: backend, reporter: rep, name: name, arena: CurrentArena(), rootID: rootID, registry: currentRegistry()}
	if fn != nil {
		fn(s)
	}
	if tb != nil {
		s.Compile()
		s.Run()
	}
}

// DescribeFlat is an alias for Describe, kept for compatibility (#110). Its name refers to the
// compiled plan, not to subtests: "flat" meant hooks flattened into each spec's own instruction
// range instead of resolved by walking a tree. Against a *testing.T every spec still runs in its
// own subtest, named by its full Describe/When/It breadcrumb, exactly as Describe does (#102). Only
// a *testing.B backend runs without subtests.
//
// Prefer Describe.
func DescribeFlat(tb testing.TB, name string, fn func(*Spec)) {
	if currentRegistry() == nil {
		describeWithCompiler(tb, name, nil, fn)
		return
	}
	defer ensureRegistry()()
	file, line := callerLocation(2)
	rootID, pop := enterAnalyzeNode(DescribeNode, name, file, line, nil)
	if rootID < 0 {
		return
	}
	defer pop()
	var backend testBackend
	if tb != nil {
		backend = asTestBackend(tb)
	}
	s := &Spec{tb: tb, backend: backend, name: name, arena: CurrentArena(), rootID: rootID, registry: currentRegistry()}
	if fn != nil {
		fn(s)
	}
	if tb != nil {
		s.Compile()
		s.Run()
	}
}

// DescribeFlatWithReporter is like DescribeFlat with a reporter: rep receives
// SuiteStarted/SuiteFinished and SpecStarted/SpecFinished events for the run.
func DescribeFlatWithReporter(tb testing.TB, name string, rep report.EventReporter, fn func(*Spec)) {
	if currentRegistry() == nil {
		describeWithCompiler(tb, name, rep, fn)
		return
	}
	defer ensureRegistry()()
	file, line := callerLocation(2)
	rootID, pop := enterAnalyzeNode(DescribeNode, name, file, line, nil)
	if rootID < 0 {
		return
	}
	defer pop()
	var backend testBackend
	if tb != nil {
		backend = asTestBackend(tb)
	}
	s := &Spec{tb: tb, backend: backend, reporter: rep, name: name, arena: CurrentArena(), rootID: rootID, registry: currentRegistry()}
	if fn != nil {
		fn(s)
	}
	if tb != nil {
		s.Compile()
		s.Run()
	}
}

// DescribeFast is an alias for DescribeFlat, and therefore for Describe (#110). It does not skip
// the per-spec testing.T.Run, and it does not avoid closure, subtest or name allocations.
//
// BenchmarkDescribeVariant_Describe/_DescribeFlat/_DescribeFast in the benchmarks package keeps that
// claim checkable instead of asserted: all three declare and run the same suite and report the same
// allocs/op.
//
// Kept for compatibility. Prefer Describe.
func DescribeFast(tb testing.TB, name string, fn func(*Spec)) {
	DescribeFlat(tb, name, fn)
}

// DescribeFastWithReporter is like DescribeFast with a reporter: rep receives SuiteStarted/SuiteFinished
// and SpecStarted/SpecFinished events for the run. Kept for compatibility; prefer
// DescribeWithReporter.
func DescribeFastWithReporter(tb testing.TB, name string, rep report.EventReporter, fn func(*Spec)) {
	DescribeFlatWithReporter(tb, name, rep, fn)
}

// Run runs the compiled suite. Call after Compile(); no-op if suite or tb is nil.
func (s *Spec) Run() {
	if s != nil && s.suite != nil && s.tb != nil {
		s.suite.Run(s.tb)
	}
}

// Compile builds the ExecutionPlan once. Safe to call multiple times (sync.Once).
// When s.plan is set (bytecode compiler path), uses it directly. Otherwise builds from s.arena.
func (s *Spec) Compile() {
	if s == nil {
		return
	}
	s.compileOnce.Do(func() {
		if s.plan != nil {
			s.suite = &CompiledSuite{Plan: s.plan, Arena: nil, RootID: 0, Name: s.name, Reporter: s.reporter}
			return
		}
		if s.arena == nil {
			return
		}
		scratch := planScratchPool.Get().(*planScratch)
		defer planScratchPool.Put(scratch)
		plan := newExecutionPlan(countSpecsArena(s.arena, s.rootID))
		groups := buildExecutionPlanFromArenaGroups(s.arena, s.rootID, plan, scratch, s.registry.groupHooksOf())
		validateHookGroups(plan, groups)
		s.suite = &CompiledSuite{Plan: plan, Arena: s.arena, RootID: s.rootID, Name: s.name, Reporter: s.reporter, groups: groups}
	})
}

// requireBuildTarget panics when s has neither a compiler nor a registry to write into. Every Spec
// handed out by an entry point carries exactly one of the two; a Spec with neither was constructed
// directly by external code, so there is no destination for the registration being made and no
// outcome other than discarding it silently.
func (s *Spec) requireBuildTarget(method string) {
	if s.compiler == nil && s.registry == nil {
		panic("specs: Spec." + method + " called on a Spec with no build target; obtain a *Spec from Describe/BuildSuite instead of constructing one")
	}
}

// Describe starts a nested describe block.
func (s *Spec) Describe(name string, fn func(*Spec)) {
	if s == nil || fn == nil {
		return
	}
	if c := s.compiler; c != nil {
		c.PushScope(name)
		defer c.PopScope()
		fn(&Spec{tb: s.tb, backend: s.backend, reporter: s.reporter, compiler: c})
		return
	}
	s.requireBuildTarget("Describe")
	child := &Spec{tb: s.tb, backend: s.backend, reporter: s.reporter, registry: s.registry}
	file, line := callerLocation(2)
	_, pop := s.registry.enterNode(DescribeNode, name, file, line, nil)
	defer pop()
	fn(child)
}

// When starts a when block. fn receives the nested *Spec to register hooks and specs on.
func (s *Spec) When(name string, fn func(*Spec)) {
	if s == nil || fn == nil {
		return
	}
	if c := s.compiler; c != nil {
		c.PushScope(name)
		defer c.PopScope()
		fn(&Spec{tb: s.tb, backend: s.backend, reporter: s.reporter, compiler: c})
		return
	}
	s.requireBuildTarget("When")
	child := &Spec{tb: s.tb, backend: s.backend, reporter: s.reporter, registry: s.registry}
	file, line := callerLocation(2)
	_, pop := s.registry.enterNode(WhenNode, name, file, line, nil)
	defer pop()
	fn(child)
}

// It registers a spec.
func (s *Spec) It(name string, fn func(*Context)) {
	if s == nil {
		return
	}
	if c := s.compiler; c != nil {
		c.closeOpenParallelRun()
		c.EmitIt(name, fn)
		return
	}
	s.requireBuildTarget("It")
	file, line := callerLocation(2)
	_, pop := s.registry.enterNode(ItNode, name, file, line, fn)
	pop()
}

// SkipIt registers a spec that is skipped at compile time (issue #245): fn is never compiled or
// run — it may be nil — but name is kept so the suite still reports it, as StatusSkipped
// (report/model.go), the same way Builder.SkipIt already does (specs/builder.go). If this
// Describe/BuildSuite call also registers an FIt anywhere in its tree, this SkipIt is dropped
// entirely instead, exactly like an unfocused It (see FIt).
func (s *Spec) SkipIt(name string, fn func(*Context)) {
	if s == nil {
		return
	}
	if c := s.compiler; c != nil {
		c.closeOpenParallelRun()
		c.EmitSkip(name)
		return
	}
	s.requireBuildTarget("SkipIt")
	file, line := callerLocation(2)
	id, pop := s.registry.enterNode(ItNode, name, file, line, fn)
	s.registry.setItKind(id, itSkip)
	pop()
}

// PendingIt registers a spec that is pending at compile time (issue #245): the specification
// exists but fn — which may be nil, and is never compiled or run either way — does not, or is not
// wired up yet. name is kept so the suite still reports it, as StatusPending (report/model.go),
// distinct from Skipped, the same way Builder.PendingIt already does. Dropped entirely by a
// suite-wide FIt, exactly like SkipIt.
func (s *Spec) PendingIt(name string, fn func(*Context)) {
	if s == nil {
		return
	}
	if c := s.compiler; c != nil {
		c.closeOpenParallelRun()
		c.EmitPending(name)
		return
	}
	s.requireBuildTarget("PendingIt")
	file, line := callerLocation(2)
	id, pop := s.registry.enterNode(ItNode, name, file, line, fn)
	s.registry.setItKind(id, itPending)
	pop()
}

// FIt registers a focused spec (issue #245): a nil fn is a no-op, exactly like Builder.FIt. If this
// Describe/BuildSuite call registers at least one FIt anywhere in its tree, only focused specs
// compile — every other It, SkipIt and PendingIt in the same call is dropped, never reported, the
// same way Builder.finalize's focus filter already works. Hooks (BeforeEach/AfterEach,
// BeforeAll/AfterAll) around a focused spec still run; a BeforeAll/AfterAll group left with zero
// runnable specs after focus filtering is never entered, the same H3 rule that already applies to
// a group declaring no It at all (docs/SUITE_HOOKS_CONTRACT.md).
func (s *Spec) FIt(name string, fn func(*Context)) {
	if s == nil || fn == nil {
		return
	}
	if c := s.compiler; c != nil {
		c.closeOpenParallelRun()
		c.EmitFocusedIt(name, fn)
		return
	}
	s.requireBuildTarget("FIt")
	file, line := callerLocation(2)
	id, pop := s.registry.enterNode(ItNode, name, file, line, fn)
	s.registry.setItKind(id, itFocus)
	pop()
}

// ItParallel registers a spec that runs concurrently with its adjacent ItParallel siblings (issue
// #245's second spec, the *Spec equivalent of Builder.ItParallel). A nil fn follows It's existing
// nil handling on *Spec (the spec is still registered, with no body instruction) rather than
// Builder.ItParallel's early return — see docs/DSL.md.
//
// Consecutive ItParallel specs form one parallel group, launched from separate goroutines via their
// own real Go subtest (testing.T.Run, never t.Parallel() — see spec_body_parallel.go) once the
// suite reaches them: every spec gets its own pooled *Context and a real ctx.T, so -run selects or
// excludes it like any other spec, BeforeEach/AfterEach still run around its body, and a fatal
// ctx.T.Parallel() call inside it is still detected and stops the run. The group ends at any other
// spec kind (It, FIt, SkipIt, PendingIt) or at a Describe/When boundary — including one that
// registers no BeforeAll/AfterAll — never partway through one BeforeAll/AfterAll group. Under focus
// (any FIt in this Describe/BuildSuite call's tree), every ItParallel is dropped, exactly like an
// unfocused It; there is no FItParallel.
func (s *Spec) ItParallel(name string, fn func(*Context)) {
	if s == nil {
		return
	}
	if c := s.compiler; c != nil {
		c.EmitItParallel(name, fn)
		return
	}
	s.requireBuildTarget("ItParallel")
	file, line := callerLocation(2)
	id, pop := s.registry.enterNode(ItNode, name, file, line, fn)
	s.registry.setItKind(id, itParallel)
	pop()
}

// BeforeEach appends a before-each hook to the current node.
func (s *Spec) BeforeEach(fn func(*Context)) {
	if s == nil || fn == nil {
		return
	}
	if c := s.compiler; c != nil {
		c.AppendBefore(fn)
		return
	}
	s.requireBuildTarget("BeforeEach")
	s.registry.appendBeforeHook(fn)
}

// AfterEach appends an after-each hook to the current node.
func (s *Spec) AfterEach(fn func(*Context)) {
	if s == nil || fn == nil {
		return
	}
	if c := s.compiler; c != nil {
		c.AppendAfter(fn)
		return
	}
	s.requireBuildTarget("AfterEach")
	s.registry.appendAfterHook(fn)
}

// BeforeAll registers a once-per-group setup hook (issue #207): it runs exactly once for this
// Describe/When group, right before the group's first runnable spec — including a spec that
// belongs to a nested group — never once per spec the way BeforeEach does. A group with no It
// anywhere in its subtree never runs its BeforeAll at all. Multiple registrations in the same
// group run in registration order. See docs/SUITE_HOOKS_CONTRACT.md for the full contract.
func (s *Spec) BeforeAll(fn func(*Context)) {
	if s == nil || fn == nil {
		return
	}
	if c := s.compiler; c != nil {
		c.AppendBeforeAll(fn)
		return
	}
	s.requireBuildTarget("BeforeAll")
	s.registry.appendBeforeAllHook(fn)
}

// AfterAll registers a once-per-group teardown hook (issue #207): it runs exactly once for this
// Describe/When group, right after the group's last spec or subgroup finishes — guaranteed to run
// once the group was entered, even after a BeforeAll or spec failure/panic. See
// docs/SUITE_HOOKS_CONTRACT.md for the full contract.
func (s *Spec) AfterAll(fn func(*Context)) {
	if s == nil || fn == nil {
		return
	}
	if c := s.compiler; c != nil {
		c.AppendAfterAll(fn)
		return
	}
	s.requireBuildTarget("AfterAll")
	s.registry.appendAfterAllHook(fn)
}
