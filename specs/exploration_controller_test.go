package specs

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestProposalControllerBoundsAndIndices(t *testing.T) {
	t.Run("accepted budget retains monotonic attempt and accepted indices", func(t *testing.T) {
		candidates := []PathValues{pathValue(1), pathValue(2), pathValue(3)}
		result := newProposalController(proposalControllerConfig{
			Seed:        42,
			MaxAttempts: 3,
			MaxAccepted: 2,
			Propose: func() (PathValues, bool) {
				candidate := candidates[0]
				candidates = candidates[1:]
				return candidate, true
			},
			Accept: func(PathValues) bool { return true },
			Execute: func(candidate proposalCandidate) bool {
				return candidate.AttemptIndex == candidate.AcceptedIndex
			},
		}).Run(context.Background())

		if result.Terminal != proposalTerminalAcceptedBudget || result.Seed != 42 {
			t.Fatalf("terminal=%v seed=%d, want accepted budget and 42", result.Terminal, result.Seed)
		}
		if got := candidateIndices(result.Candidates); !reflect.DeepEqual(got, [][2]int{{1, 1}, {2, 2}}) {
			t.Fatalf("indices=%v, want [[1 1] [2 2]]", got)
		}
	})

	t.Run("restrictive filter stops at rejection cap", func(t *testing.T) {
		result := newProposalController(proposalControllerConfig{
			MaxAttempts:   9,
			MaxRejections: 2,
			Propose:       func() (PathValues, bool) { return pathValue(1), true },
			Accept:        func(PathValues) bool { return false },
		}).Run(context.Background())
		if result.Terminal != proposalTerminalRejectedCap || result.Attempts != 2 || result.Rejections != 2 {
			t.Fatalf("result=%+v, want two capped rejections", result)
		}
	})
}

func TestProposalControllerCancellationAndDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := newProposalController(proposalControllerConfig{Propose: func() (PathValues, bool) {
		t.Fatal("proposal must not run after cancellation")
		return PathValues{}, false
	}}).Run(ctx)
	if result.Terminal != proposalTerminalCanceled {
		t.Fatalf("terminal=%v, want canceled", result.Terminal)
	}

	deadline, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	result = newProposalController(proposalControllerConfig{}).Run(deadline)
	if result.Terminal != proposalTerminalDeadline {
		t.Fatalf("terminal=%v, want deadline", result.Terminal)
	}
}

func TestProposalControllerDeterministicOrderingAndSequentialExecution(t *testing.T) {
	run := func() proposalControllerResult {
		candidates := []PathValues{pathValue(3), pathValue(5), pathValue(7)}
		feedback := make([]int, 0, 3)
		return newProposalController(proposalControllerConfig{
			Seed:        7,
			MaxAttempts: 3,
			Propose: func() (PathValues, bool) {
				candidate := candidates[0]
				candidates = candidates[1:]
				return candidate, true
			},
			Accept:        func(PathValues) bool { return true },
			Execute:       func(candidate proposalCandidate) bool { return candidate.Values.Int("value") != 5 },
			AdmitFeedback: func(fb proposalFeedback) { feedback = append(feedback, fb.Candidate.Values.Int("value")) },
		}).Run(context.Background())
	}

	first, second := run(), run()
	if first.Terminal != proposalTerminalFirstFailure || !reflect.DeepEqual(first, second) {
		t.Fatalf("fixed-seed results differ or did not stop at first failure: first=%+v second=%+v", first, second)
	}
	if got := candidateValues(first.Candidates); !reflect.DeepEqual(got, []int{3, 5}) {
		t.Fatalf("candidate order=%v, want [3 5]", got)
	}
	if first.FirstFailure.AcceptedIndex != 2 || !reflect.DeepEqual(candidateValues(first.Feedback), []int{3}) {
		t.Fatalf("first failure/feedback=%+v/%v, want accepted index 2 and [3]", first.FirstFailure, candidateValues(first.Feedback))
	}
}

// TestProposalControllerAdmitFeedbackFiresForFailureToo would fail against the old contract, where
// AdmitFeedback was only invoked after a successful Execute and a failing candidate's outcome was
// silently dropped on the way to the first-failure terminal.
func TestProposalControllerAdmitFeedbackFiresForFailureToo(t *testing.T) {
	candidates := []PathValues{pathValue(1), pathValue(2)}
	var got []proposalFeedback
	result := newProposalController(proposalControllerConfig{
		MaxAttempts: 2,
		Propose: func() (PathValues, bool) {
			candidate := candidates[0]
			candidates = candidates[1:]
			return candidate, true
		},
		Accept:  func(PathValues) bool { return true },
		Execute: func(candidate proposalCandidate) bool { return candidate.Values.Int("value") != 2 },
		AdmitFeedback: func(fb proposalFeedback) {
			got = append(got, fb)
		},
	}).Run(context.Background())

	if result.Terminal != proposalTerminalFirstFailure {
		t.Fatalf("terminal=%v, want first failure", result.Terminal)
	}
	if len(got) != 2 {
		t.Fatalf("AdmitFeedback calls=%d, want 2 (fired for both the pass and the failure)", len(got))
	}
	if !got[0].Passed || got[0].Candidate.Values.Int("value") != 1 {
		t.Fatalf("first feedback=%+v, want passed=true value=1", got[0])
	}
	if got[1].Passed || got[1].Candidate.Values.Int("value") != 2 {
		t.Fatalf("second feedback=%+v, want passed=false value=2", got[1])
	}
}

func TestProposalControllerHasNoParallelExecutionOption(t *testing.T) {
	active, maximum := 0, 0
	result := newProposalController(proposalControllerConfig{
		MaxAttempts: 2,
		Propose: func() (PathValues, bool) {
			if active == 0 {
				return pathValue(1), true
			}
			return pathValue(2), true
		},
		Accept: func(PathValues) bool { return true },
		Execute: func(proposalCandidate) bool {
			active++
			if active > maximum {
				maximum = active
			}
			active--
			return true
		},
	}).Run(context.Background())
	if result.Terminal != proposalTerminalAttemptsBudget || maximum != 1 {
		t.Fatalf("terminal=%v maximum concurrent executions=%d, want attempts budget and 1", result.Terminal, maximum)
	}
}

func pathValue(value int) PathValues {
	return PathValues{values: []any{value}, present: []bool{true}, index: map[string]int{"value": 0}}
}

func candidateIndices(candidates []proposalCandidate) [][2]int {
	indices := make([][2]int, len(candidates))
	for i, candidate := range candidates {
		indices[i] = [2]int{candidate.AttemptIndex, candidate.AcceptedIndex}
	}
	return indices
}

func candidateValues(candidates []proposalCandidate) []int {
	values := make([]int, len(candidates))
	for i, candidate := range candidates {
		values[i] = candidate.Values.Int("value")
	}
	return values
}
