// isolation_state_release_test.go pins #304: once a spec has finished on the sequential *testing.T
// isolation path, the *Context that ran it must not keep that spec's program or its finished
// subtest *testing.T alive (see Context's iso* fields and runSpecProgramIsolated).
//
// Like isolation_state_reuse_test.go, it drives runSpecProgramIsolated directly against a *Context
// it constructs and keeps itself, so the result never depends on sync.Pool reuse or on the pool
// dropping idle objects. weak.Pointer plus runtime.GC observes the retention directly: a weak
// pointer only reads back nil once nothing strongly reachable still refers to its target.
package specs

import (
	"runtime"
	"testing"
	"weak"
)

func TestSequentialIsolationPathReleasesFinishedSpecProgramAndSubtest(t *testing.T) {
	ctx := &Context{}
	var subtest weak.Pointer[testing.T]
	program := make([]Instruction, 1)
	program[0] = Instruction{Code: OpBody, Fn: func(ctx *Context) { subtest = weak.Make(ctx.T) }}
	programRef := weak.Make(&program[0])

	_, _, ran, failed, _ := runSpecProgramIsolated(t, ctx, program, "spec")
	if !ran || failed {
		t.Fatalf("spec did not run cleanly: ran=%t failed=%t", ran, failed)
	}
	if subtest.Value() == nil {
		t.Fatal("spec body did not observe its subtest *testing.T")
	}

	// Drop this test's own reference and do exactly what releaseContext does before contextPool.Put,
	// so the only remaining path to the program or the subtest is through ctx itself.
	program = nil
	ctx.Reset(nil)
	runtime.GC()
	runtime.GC()

	if programRef.Value() != nil {
		t.Error("released Context still retains the finished spec's program (ctx.isoProgram)")
	}
	if subtest.Value() != nil {
		t.Error("released Context still retains the finished subtest's *testing.T (ctx.isoSub)")
	}
	runtime.KeepAlive(ctx)
}
