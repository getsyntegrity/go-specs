// spec_itparallel_reporting_test.go pins issue #245's T2: reporting and -run for Spec.ItParallel.
// Reporter events for a parallel group are serialized (never called concurrently from a worker
// goroutine — the user's concurrency condition 1) and emitted in declaration order once the whole
// group has finished (condition 2), even when the specs themselves complete in a different order;
// -run selects or excludes an individual parallel spec exactly like any other *Spec spec.
package specs

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// itParallelRunSelectionHelperSuite declares three ItParallel siblings under one Describe, so a
// -run pattern can target exactly one of them by its full subtest name.
func itParallelRunSelectionHelperSuite(s *Spec) {
	s.ItParallel("alpha", func(*Context) { fmt.Println("RAN alpha") })
	s.ItParallel("bravo", func(*Context) { fmt.Println("RAN bravo") })
	s.ItParallel("charlie", func(*Context) { fmt.Println("RAN charlie") })
}

// TestSpecItParallel_RunSelectsOneParallelSpec_RealProcess proves a -run pattern reaches into an
// ItParallel group and selects exactly one of its specs, exactly as it would for ordinary sibling
// It specs: the go test flag applies per real subtest regardless of which goroutine launched it.
func TestSpecItParallel_RunSelectsOneParallelSpec_RealProcess(t *testing.T) {
	const helperEnv = "GO_SPECS_ITPARALLEL_RUN_SELECTION_HELPER"
	if os.Getenv(helperEnv) == "1" {
		Describe(t, "suite", itParallelRunSelectionHelperSuite)
		return
	}
	cmd := exec.Command(os.Args[0],
		"-test.v",
		"-test.run=^TestSpecItParallel_RunSelectsOneParallelSpec_RealProcess$/^suite$/^bravo$",
	)
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	transcript := string(output)

	if strings.Count(transcript, "RAN ") != 1 || !strings.Contains(transcript, "RAN bravo") {
		t.Fatalf("expected exactly one spec body to run (bravo), got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "--- PASS: TestSpecItParallel_RunSelectsOneParallelSpec_RealProcess/suite/bravo") {
		t.Fatalf("expected suite/bravo to run and pass, got:\n%s", transcript)
	}
}

// TestSpecItParallel_RunExcludesAllReportsFiltered_RealProcess proves that a -run pattern matching
// none of an ItParallel group's specs still reports every one of them Filtered (never a bare pass)
// — the same #111 contract every other *Spec spec already has.
func TestSpecItParallel_RunExcludesAllReportsFiltered_RealProcess(t *testing.T) {
	const helperEnv = "GO_SPECS_ITPARALLEL_RUN_FILTER_HELPER"
	if os.Getenv(helperEnv) == "1" {
		var rep recordingReporter
		DescribeWithReporter(t, "suite", &rep, itParallelRunSelectionHelperSuite)
		for i, started := range rep.specStarted {
			finished := rep.specFinished[i]
			fmt.Printf("REPORTED name=%q failed=%v filtered=%v\n", started.Name, finished.Failed, finished.Filtered)
		}
		return
	}
	cmd := exec.Command(os.Args[0],
		"-test.v",
		"-test.run=^TestSpecItParallel_RunExcludesAllReportsFiltered_RealProcess$/^suite$/^nomatch$",
	)
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper run failed: %v\n%s", err, output)
	}
	transcript := string(output)

	if strings.Count(transcript, "RAN ") != 0 {
		t.Fatalf("expected no spec body to run under a non-matching -run pattern, got:\n%s", transcript)
	}
	for _, want := range []string{
		`REPORTED name="alpha" failed=false filtered=true`,
		`REPORTED name="bravo" failed=false filtered=true`,
		`REPORTED name="charlie" failed=false filtered=true`,
	} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("expected the reporter to emit %s, got:\n%s", want, transcript)
		}
	}
}

// TestSpecItParallel_ReporterEventOrderIsDeclarationOrder pins the user's concurrency condition 2:
// SpecResultEvents for a parallel group are emitted in declaration order regardless of completion
// order. The three specs finish in the exact reverse of declaration order (c fastest, a slowest, via
// a channel each waits on before returning), which would show up as a reordering immediately if the
// runner emitted events as each goroutine completed instead of buffering and replaying them.
func TestSpecItParallel_ReporterEventOrderIsDeclarationOrder(t *testing.T) {
	release := map[string]chan struct{}{
		"a": make(chan struct{}),
		"b": make(chan struct{}),
		"c": make(chan struct{}),
	}
	rep := &recordingReporter{}
	suite := BuildSuite(nil, "suite", func(s *Spec) {
		s.ItParallel("a", func(*Context) { <-release["a"] })
		s.ItParallel("b", func(*Context) { <-release["b"] })
		s.ItParallel("c", func(*Context) { <-release["c"] })
	})
	suite.Reporter = rep

	// Release in reverse of declaration order, staggered, so c really does finish before b, which
	// really does finish before a, on the goroutines actually running the bodies.
	go func() {
		close(release["c"])
		close(release["b"])
		close(release["a"])
	}()
	suite.Run(t)

	if len(rep.specFinished) != 3 {
		t.Fatalf("got %d SpecFinished events, want 3: %+v", len(rep.specFinished), rep.specFinished)
	}
	var gotOrder []string
	for _, e := range rep.specFinished {
		gotOrder = append(gotOrder, e.Name)
	}
	wantOrder := []string{"a", "b", "c"}
	for i, want := range wantOrder {
		if gotOrder[i] != want {
			t.Fatalf("SpecFinished order = %v, want %v (declaration order, not completion order)", gotOrder, wantOrder)
		}
	}
	// Every SpecStarted must also precede its own SpecFinished in the same declaration-ordered
	// stream (the user's choice: Started is buffered and replayed the same way Finished is).
	var gotStarted []string
	for _, e := range rep.specStarted {
		gotStarted = append(gotStarted, e.Name)
	}
	for i, want := range wantOrder {
		if gotStarted[i] != want {
			t.Fatalf("SpecStarted order = %v, want %v", gotStarted, wantOrder)
		}
	}
}

// TestSpecItParallel_ReporterOrderStability is TestSpecItParallel_ReporterEventOrderIsDeclarationOrder
// run under -race -count=20 (see the feature doc's T2 verification): the same completion-order
// reversal must reproduce declaration-ordered events every time, and the race detector must find no
// data race in the buffering (each goroutine writes only its own outcome slot; emission happens
// after wg.Wait() on a single goroutine).
func TestSpecItParallel_ReporterOrderStability(t *testing.T) {
	TestSpecItParallel_ReporterEventOrderIsDeclarationOrder(t)
}
