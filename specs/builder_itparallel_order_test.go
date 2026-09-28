// builder_itparallel_order_test.go pins that Builder.ItParallel's compiled group step emits its
// batch's events in declaration order however the bodies complete (#315), mirroring Spec's
// runParallelGroup.
package specs

import (
	"strings"
	"testing"
	"time"
)

// TestParallelStepEmitsInDeclarationOrder (#315): completion is forced to run p3, p2, p1 with
// channels (each body waits for the next-declared one to finish), which also proves the bodies run
// concurrently. Events must still come out p1, p2, p3, each start immediately followed by its finish.
func TestParallelStepEmitsInDeclarationOrder(t *testing.T) {
	want := []string{
		"start:s/p1", "finish:s/p1",
		"start:s/p2", "finish:s/p2",
		"start:s/p3", "finish:s/p3",
	}
	for run := 0; run < 50; run++ {
		rep := &orderedReporter{}
		backend := &collectingBackend{}
		ctx := &Context{backend: backend, execObserver: &reporterObserver{rep: rep}}
		done2, done3 := make(chan struct{}), make(chan struct{})
		wait := func(c chan struct{}) {
			select {
			case <-c:
			case <-time.After(5 * time.Second):
				panic("bodies are not running concurrently")
			}
		}
		parallelStep([]step{
			func(*Context) { wait(done2) },
			func(c *Context) { wait(done3); c.backend.Error("p2 failed"); close(done2) },
			func(*Context) { close(done3) },
		}, []string{"p1", "p2", "p3"}, [][]string{{"s"}, {"s"}, {"s"}})(ctx)

		if got := strings.Join(rep.events, ","); got != strings.Join(want, ",") {
			t.Fatalf("run %d: events = %v, want %v", run, rep.events, want)
		}
		failed := []bool{rep.final[0].Failed, rep.final[1].Failed, rep.final[2].Failed}
		if failed[0] || !failed[1] || failed[2] {
			t.Fatalf("run %d: failed = %v, want [false true false]", run, failed)
		}
		if !ctx.hasFailed() || len(backend.messages) != 1 || !strings.HasPrefix(backend.messages[0], "spec[1]") {
			t.Fatalf("run %d: failure not folded onto ctx / attributed to spec[1]: %v", run, backend.messages)
		}
	}
}
