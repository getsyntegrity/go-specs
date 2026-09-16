package specs

import "context"

// proposalTerminal identifies why a bounded generated run stopped.
type proposalTerminal uint8

const (
	proposalTerminalExhausted proposalTerminal = iota
	proposalTerminalAttemptsBudget
	proposalTerminalAcceptedBudget
	proposalTerminalRejectedCap
	proposalTerminalCanceled
	proposalTerminalDeadline
	proposalTerminalFirstFailure
)

// proposalCandidate records one proposed path. AcceptedIndex is zero for rejections.
type proposalCandidate struct {
	Values        PathValues
	AttemptIndex  int
	AcceptedIndex int
	Accepted      bool
}

// proposalFeedback carries an executed candidate's outcome to AdmitFeedback. It is delivered for
// both passing and failing candidates, since a failure is feedback too (e.g. for corpus/coverage
// strategies that want to learn from it).
type proposalFeedback struct {
	Candidate proposalCandidate
	Passed    bool
}

// proposalControllerResult is the ordered, internal result of a bounded run.
type proposalControllerResult struct {
	Seed         int64
	Terminal     proposalTerminal
	Attempts     int
	Accepted     int
	Rejections   int
	Candidates   []proposalCandidate
	Feedback     []proposalCandidate
	FirstFailure proposalCandidate
	HasFailure   bool
}

// proposalControllerConfig keeps proposal, execution, and feedback independent.
type proposalControllerConfig struct {
	Seed          int64
	MaxAttempts   int
	MaxAccepted   int
	MaxRejections int
	Propose       func() (PathValues, bool)
	Accept        func(PathValues) bool
	Execute       func(proposalCandidate) bool
	AdmitFeedback func(proposalFeedback)
}

type proposalController struct{ config proposalControllerConfig }

func newProposalController(config proposalControllerConfig) proposalController {
	return proposalController{config: config}
}

// Run evaluates proposals in order. It deliberately has no parallel execution option.
func (c proposalController) Run(ctx context.Context) proposalControllerResult {
	result := proposalControllerResult{Seed: c.config.Seed}
	for {
		if terminal, done := proposalContextTerminal(ctx); done {
			result.Terminal = terminal
			return result
		}
		if c.config.MaxAttempts > 0 && result.Attempts >= c.config.MaxAttempts {
			result.Terminal = proposalTerminalAttemptsBudget
			return result
		}
		if c.config.MaxAccepted > 0 && result.Accepted >= c.config.MaxAccepted {
			result.Terminal = proposalTerminalAcceptedBudget
			return result
		}
		if c.config.MaxRejections > 0 && result.Rejections >= c.config.MaxRejections {
			result.Terminal = proposalTerminalRejectedCap
			return result
		}
		if c.config.Propose == nil {
			result.Terminal = proposalTerminalExhausted
			return result
		}

		values, ok := c.config.Propose()
		if !ok {
			result.Terminal = proposalTerminalExhausted
			return result
		}
		result.Attempts++
		candidate := proposalCandidate{Values: values.clone(), AttemptIndex: result.Attempts}
		candidate.Accepted = c.config.Accept == nil || c.config.Accept(values)
		if !candidate.Accepted {
			result.Rejections++
			result.Candidates = append(result.Candidates, candidate)
			continue
		}

		result.Accepted++
		candidate.AcceptedIndex = result.Accepted
		result.Candidates = append(result.Candidates, candidate)
		passed := c.config.Execute == nil || c.config.Execute(candidate)
		if c.config.AdmitFeedback != nil {
			c.config.AdmitFeedback(proposalFeedback{Candidate: candidate, Passed: passed})
		}
		if !passed {
			result.Terminal = proposalTerminalFirstFailure
			result.FirstFailure = candidate
			result.HasFailure = true
			return result
		}
		result.Feedback = append(result.Feedback, candidate)
	}
}

func proposalContextTerminal(ctx context.Context) (proposalTerminal, bool) {
	if ctx == nil || ctx.Err() == nil {
		return 0, false
	}
	if ctx.Err() == context.DeadlineExceeded {
		return proposalTerminalDeadline, true
	}
	return proposalTerminalCanceled, true
}
