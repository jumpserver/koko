package terminalai

import "context"

// LoopModel isolates the agent loop from a concrete model provider. The loop
// owns planning, command policy and execution; providers only produce and
// review structured decisions.
type LoopModel interface {
	Decide(
		ctx context.Context,
		question, history, profile, snapshot string,
		correction string,
	) (Decision, error)
	Next(ctx context.Context, request ReActRequest) (ReActDecision, error)
	Summarize(
		ctx context.Context,
		question, summary string,
		steps []Step,
		results []StepResult,
		stopReason string,
	) (string, error)
}

type ReActRequest struct {
	Question            string
	PlanSummary         string
	Steps               []Step
	Results             []StepResult
	Profile             string
	Snapshot            string
	Mode                string
	BackgroundAvailable bool
	Round               int
	MaxRounds           int
	Correction          string
}

type ModelProviderInfo interface {
	ProviderInfo() ProviderInfo
}
