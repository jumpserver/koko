package terminalai

import "context"

// LoopModel isolates the agent loop from a concrete model provider. The loop
// owns planning, command policy and execution; providers only produce and
// review structured decisions.
type LoopModel interface {
	Decide(
		ctx context.Context,
		question, history, profile, snapshot string,
	) (Decision, error)
	Propose(
		ctx context.Context,
		question, summary string,
		steps []Step,
		index int,
		profile, snapshot string,
		results []StepResult,
		mode string,
		backgroundAvailable bool,
	) (CommandProposal, error)
	Review(
		ctx context.Context,
		step Step,
		proposal CommandProposal,
		output string,
		exitCode *int,
	) (StepReview, error)
	Summarize(
		ctx context.Context,
		question, summary string,
		steps []Step,
		results []StepResult,
	) (string, error)
}
