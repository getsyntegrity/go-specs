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
	s.plan = c.TakePlan()
	if tb != nil {
		s.Compile()
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
		s.plan = c.TakePlan()
		s.Compile()
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
		buildExecutionPlanFromArena(s.arena, s.rootID, plan, scratch)
		s.suite = &CompiledSuite{Plan: plan, Arena: s.arena, RootID: s.rootID, Name: s.name, Reporter: s.reporter}
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
		c.EmitIt(name, fn)
		return
	}
	s.requireBuildTarget("It")
	file, line := callerLocation(2)
	_, pop := s.registry.enterNode(ItNode, name, file, line, fn)
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
