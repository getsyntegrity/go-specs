package specs

import (
	"strings"
	"testing"
)

// execution_engine_contract_test.go pins the correctness invariants that EVERY supported sequential
// execution engine must implement, in one table instead of one test per engine (#176).
//
// Before this file, each engine carried its own private copy of these tests. That is exactly how
// #62, #63, #64 and #65 came to be four separate issues for one missing invariant: a rule added to
// one engine's test file says nothing about the others, so an engine that never grew the rule looks
// covered. A contract table inverts that — a new engine must be added here, and a new rule is
// asserted against every engine at once.
//
// Only invariants that all these engines genuinely share belong here. Hooks, filtering, subtest
// identity and reporting do NOT: MinimalRunner, BlockRunner and BytecodeRunner execute bare
// func(*Context) lists and implement none of them. See docs/EXECUTION_ENGINES.md for which engine
// owns which invariant.

// sequentialEngine adapts one engine's sequential entry point to a single shape: run these spec
// bodies, in order, against this Context.
type sequentialEngine struct {
	name string
	run  func(ctx *Context, fns []func(*Context))
}

// sequentialEngines are the engines that claim the shared per-spec recovery contract. Adding an
// engine to the codebase means adding it here; every contract below then applies to it.
func sequentialEngines() []sequentialEngine {
	return []sequentialEngine{
		{
			name: "Runner/Program",
			run: func(ctx *Context, fns []func(*Context)) {
				steps := make([]step, len(fns))
				for i, fn := range fns {
					steps[i] = step(fn)
				}
				runGroups(ctx, []group{{specs: steps}})
			},
		},
		{
			name: "ExecutionPlan",
			run: func(ctx *Context, fns []func(*Context)) {
				for _, fn := range fns {
					runProgram([]Instruction{{Code: OpBody, Fn: fn}}, ctx, nil)
				}
			},
		},
		{
			name: "MinimalRunner",
			run: func(ctx *Context, fns []func(*Context)) {
				specs := make([]RunSpec, len(fns))
				for i, fn := range fns {
					specs[i] = RunSpec{Name: "spec", Fn: fn}
				}
				runMinimalSpecs(ctx, specs)
			},
		},
		{
			name: "BlockRunner",
			run: func(ctx *Context, fns []func(*Context)) {
				blocks := make([]specBlock, len(fns))
				for i := range fns {
					blocks[i] = specBlock{start: i, count: 1}
				}
				runBlocks(ctx, fns, blocks)
			},
		},
		{
			name: "BytecodeRunner",
			run: func(ctx *Context, fns []func(*Context)) {
				code := make([]instruction, len(fns))
				starts := make([]int, 0, len(fns)+1)
				for i, fn := range fns {
					code[i] = instruction{fn: fn}
					starts = append(starts, i)
				}
				starts = append(starts, len(fns))
				runBytecodeSequential(ctx, code, starts)
			},
		},
	}
}

// TestEngineContractRecoversPanicAndRunsSiblings pins the invariant every engine's doc comment
// already claims: a panicking spec is recorded as a failure instead of crashing the process, and the
// next spec still runs. This is the invariant #62–#65 had to add to four engines one at a time.
func TestEngineContractRecoversPanicAndRunsSiblings(t *testing.T) {
	for _, engine := range sequentialEngines() {
		t.Run(engine.name, func(t *testing.T) {
			backend := &controlledBackend{}
			ctx := &Context{backend: backend}
			var ranSecond bool
			engine.run(ctx, []func(*Context){
				func(*Context) { panic("boom") },
				func(*Context) { ranSecond = true },
			})

			if !ranSecond {
				t.Fatal("expected the spec after the panicking one to still run")
			}
			if len(backend.errors) != 1 {
				t.Fatalf("expected exactly one recorded failure, got %d: %v", len(backend.errors), backend.errors)
			}
			if !strings.Contains(backend.errors[0], "boom") {
				t.Fatalf("expected the recorded failure to name the panic value, got %q", backend.errors[0])
			}
		})
	}
}

// TestEngineContractFailedFlagScopeIsNotShared pins a divergence rather than an invariant: after a
// run whose first spec panicked and whose second passed, ctx.failed means different things per
// engine. Runner/Program resets it at the top of every spec (runner.go's runSpecsRecovered) because
// FailFast asks "did THIS spec fail"; the other engines never reset it, so it accumulates into "did
// ANY spec fail".
//
// This is deliberate, not a bug — but it is exactly the kind of quiet semantic difference that makes
// a fix in one engine wrong in another, so it is asserted here to keep it visible and intentional.
// Only Runner/Program implements FailFast at all; see docs/EXECUTION_ENGINES.md.
func TestEngineContractFailedFlagScopeIsNotShared(t *testing.T) {
	perSpecScope := map[string]bool{"Runner/Program": true}

	for _, engine := range sequentialEngines() {
		t.Run(engine.name, func(t *testing.T) {
			backend := &controlledBackend{}
			ctx := &Context{backend: backend}
			engine.run(ctx, []func(*Context){
				func(*Context) { panic("boom") },
				func(*Context) {},
			})

			if perSpecScope[engine.name] {
				if ctx.failed {
					t.Fatal("expected a per-spec failed flag to be cleared by the passing spec that followed")
				}
				return
			}
			if !ctx.failed {
				t.Fatal("expected a cumulative failed flag to still record the earlier panic")
			}
		})
	}
}

// TestEngineContractDoesNotDoubleReportExpectedAbort pins the counterpart invariant: the
// isolatedCaseAbort sentinel a controlled backend panics with from FailNow/Fatal/Fatalf is an
// already-recorded stop, not an unexpected panic. Re-reporting it turns one assertion failure into
// two, with the second carrying a meaningless stack trace into the sentinel.
//
// Every engine but one had a private test for this rule. The Runner/Program path never grew it —
// precisely the omission a per-engine test layout makes invisible.
func TestEngineContractDoesNotDoubleReportExpectedAbort(t *testing.T) {
	for _, engine := range sequentialEngines() {
		t.Run(engine.name, func(t *testing.T) {
			backend := &controlledBackend{}
			ctx := &Context{backend: backend}
			var ranSecond bool
			engine.run(ctx, []func(*Context){
				func(ctx *Context) { ctx.backend.FailNow() },
				func(*Context) { ranSecond = true },
			})

			if !ranSecond {
				t.Fatal("expected the spec after the aborted one to still run")
			}
			if !backend.failNow {
				t.Fatal("expected the backend to have recorded the FailNow that produced the sentinel")
			}
			if len(backend.errors) != 0 {
				t.Fatalf("expected the already-recorded abort to produce no extra failure, got %v", backend.errors)
			}
		})
	}
}
