package terminalai

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestCompactResultsUsesReviewedSummary(t *testing.T) {
	results := []StepResult{
		{
			ID: "execution-1", Status: StepCompleted,
			Output: strings.Repeat("old output", 512), Summary: "reviewed result",
		},
		{
			ID: "execution-2", Status: StepReviewing, Output: "current output",
		},
	}

	compacted := compactResults(results)
	if compacted[0].Output != "" || compacted[0].Summary != "reviewed result" {
		t.Fatalf("reviewed result was not compacted: %#v", compacted[0])
	}
	if compacted[1].Output != "current output" {
		t.Fatalf("current result output = %q", compacted[1].Output)
	}
	if results[0].Output == "" {
		t.Fatal("source results were mutated")
	}
}

func TestCompactResultsBoundsArchivedSummaries(t *testing.T) {
	results := make([]StepResult, maxReActRounds)
	for index := range results {
		results[index] = StepResult{
			Status: StepCompleted, Output: "raw output",
			Summary: strings.Repeat("summary", maxModelArchivedResultOutput),
		}
	}

	total := 0
	for index, result := range compactResults(results) {
		total += len(result.Output) + len(result.Summary)
		if result.Summary == "" {
			t.Fatalf("archived summary %d was discarded", index)
		}
		if index < len(results)-1 && result.Output != "" {
			t.Fatalf("archived output was retained: %q", result.Output)
		}
		if index == len(results)-1 && result.Output != "raw output" {
			t.Fatalf("latest output was compacted: %q", result.Output)
		}
	}
	if total > maxModelResultsOutput {
		t.Fatalf("compacted results use %d bytes, limit %d", total, maxModelResultsOutput)
	}
}

func TestReactPlanKeepsStableTasksAcrossCommandAttempts(t *testing.T) {
	steps := []Step{
		{
			ID: "task-1", Title: "Inspect", Objective: "Inspect the state",
			Status: StepPending,
		},
		{
			ID: "task-2", Title: "Verify", Objective: "Verify the outcome",
			Status: StepPending,
		},
	}
	plan := newReActPlan("plan-1", "Inspect and verify", steps)
	first := ReActDecision{
		Kind: ReActExecute, ThoughtSummary: "Inspect first",
		Observation: ObservationReview{Outcome: "none"},
		NextStepID:  "task-1",
		Proposal: &CommandProposal{
			Command: "first", Execution: ExecutionPTY,
		},
	}
	transition, err := plan.preview(first)
	if err != nil {
		t.Fatalf("preview first command: %v", err)
	}
	if err = plan.beginExecution(transition); err != nil {
		t.Fatalf("begin first command: %v", err)
	}
	if err = plan.recordExecution(
		"execution-1", "task-1", *first.Proposal, "partial", nil, nil,
	); err != nil {
		t.Fatalf("record first command: %v", err)
	}

	second := ReActDecision{
		Kind: ReActExecute, ThoughtSummary: "Inspect further",
		Observation: ObservationReview{
			StepID: "task-1", Outcome: ReActContinue,
			Summary: "More evidence is required",
		},
		NextStepID: "task-1",
		Proposal: &CommandProposal{
			Command: "second", Execution: ExecutionPTY,
		},
	}
	transition, err = plan.preview(second)
	if err != nil {
		t.Fatalf("preview second command: %v", err)
	}
	if len(transition.steps) != len(steps) {
		t.Fatalf("task count changed from %d to %d", len(steps), len(transition.steps))
	}
	for index := range steps {
		if transition.steps[index].ID != steps[index].ID {
			t.Fatalf("task %d changed from %q to %q", index, steps[index].ID, transition.steps[index].ID)
		}
	}
	if err = plan.beginExecution(transition); err != nil {
		t.Fatalf("begin second command: %v", err)
	}
	if err = plan.recordExecution(
		"execution-2", "task-1", *second.Proposal, "complete", nil, nil,
	); err != nil {
		t.Fatalf("record second command: %v", err)
	}
	if len(plan.results) != 2 || plan.results[0].ID != "execution-1" ||
		plan.results[1].ID != "execution-2" {
		t.Fatalf("command executions were not retained: %#v", plan.results)
	}
}

func TestReactContinuationMustStayOnSameTask(t *testing.T) {
	plan := newReActPlan("plan-1", "Stable plan", []Step{
		{ID: "task-1", Title: "Inspect", Objective: "Inspect", Status: StepReviewing},
		{ID: "task-2", Title: "Verify", Objective: "Verify", Status: StepPending},
	})
	plan.results = []StepResult{{
		ID: "execution-1", StepID: "task-1", Command: "first",
		Status: StepReviewing, Execution: ExecutionPTY,
	}}
	_, err := plan.preview(ReActDecision{
		Kind: ReActExecute, ThoughtSummary: "Continue elsewhere",
		Observation: ObservationReview{
			StepID: "task-1", Outcome: ReActContinue, Summary: "Not done",
		},
		NextStepID: "task-2",
		Proposal: &CommandProposal{
			Command: "second", Execution: ExecutionPTY,
		},
	})
	if err == nil {
		t.Fatal("continuation unexpectedly changed logical tasks")
	}
}

func TestNormalizeReactContinuationKeepsReviewedTask(t *testing.T) {
	decision := ReActDecision{
		Kind: ReActExecute,
		Observation: ObservationReview{
			StepID: "task-1", Outcome: ReActContinue,
		},
		NextStepID: "task-2",
	}
	if !normalizeReActContinuation(&decision) || decision.NextStepID != "task-1" {
		t.Fatalf("normalized next step = %q", decision.NextStepID)
	}
}

func TestReactPlanInterruptsActiveWork(t *testing.T) {
	plan := newReActPlan("plan-1", "Inspect", []Step{
		{ID: "task-1", Status: StepReviewing},
		{ID: "task-2", Status: StepPending},
	})
	plan.results = []StepResult{{ID: "execution-1", StepID: "task-1", Status: StepReviewing}}

	plan.interrupt("interrupted by user")

	if plan.steps[0].Status != StepInterrupted || plan.steps[1].Status != StepSkipped {
		t.Fatalf("unexpected interrupted steps: %#v", plan.steps)
	}
	if plan.results[0].Status != StepInterrupted || plan.results[0].Summary != "interrupted by user" {
		t.Fatalf("unexpected interrupted result: %#v", plan.results[0])
	}
}

func TestInitialDecisionExecutesBeforeNextModelTurn(t *testing.T) {
	events := make([]string, 0, 3)
	messages := make([]ChatMessage, 0, 8)
	model := &orderedLoopModel{events: &events}
	observer, err := NewObserver(80, 24)
	if err != nil {
		t.Fatalf("create observer: %v", err)
	}
	t.Cleanup(func() { _ = observer.Close() })
	runtime := NewRuntime(
		1, model, observer, func([]byte) {}, func(message ChatMessage) {
			messages = append(messages, message)
		},
	)
	runtime.SetAdapter(backgroundTestAdapter{})
	runtime.SetBackgroundExecutor(&orderedExecutor{events: &events}, nil)
	t.Cleanup(runtime.Close)

	runtime.run(context.Background(), "inspect the terminal")
	if want := []string{"decide", "execute", "next"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("event order = %v, want %v", events, want)
	}
	if model.nextCalls != 1 {
		t.Fatalf("next model calls = %d, want 1", model.nextCalls)
	}
	durationFound := false
	decisionDurationFound := false
	for _, message := range messages {
		if len(message.Parts) == 1 && message.Parts[0].Type == "data-command" {
			data, _ := message.Parts[0].Data.(map[string]any)
			decisionDurationFound = data["decisionDurationMs"] != nil
		}
		if len(message.Parts) == 1 && message.Parts[0].Type == "data-execution" {
			data, _ := message.Parts[0].Data.(map[string]any)
			if data["outcome"] == "reviewing" {
				durationFound = data["durationMs"] != nil
			}
		}
	}
	if !decisionDurationFound || !durationFound {
		t.Fatal("command decision or execution has no duration")
	}
}

type orderedLoopModel struct {
	events    *[]string
	nextCalls int
}

func (m *orderedLoopModel) Decide(
	_ context.Context, _ InitialRequest,
) (Decision, error) {
	*m.events = append(*m.events, "decide")
	return Decision{
		Kind: ReActExecute, Summary: "Inspect",
		ThoughtSummary: "Start inspection",
		Steps:          []Step{{Title: "Inspect", Objective: "Inspect the terminal"}},
		Proposal: &CommandProposal{
			Command: "inspect", RiskLevel: 1,
			Execution: ExecutionBackground,
		},
	}, nil
}

func (m *orderedLoopModel) Next(
	_ context.Context, request ReActRequest,
) (ReActDecision, error) {
	*m.events = append(*m.events, "next")
	m.nextCalls++
	return ReActDecision{
		Kind: ReActFinish, ThoughtSummary: "Inspection complete",
		Observation: ObservationReview{
			StepID: request.Steps[0].ID, Outcome: StepCompleted,
			Summary: "The inspection completed",
		},
		Summary: "Done",
	}, nil
}

func (m *orderedLoopModel) Summarize(
	context.Context, string, string, []Step, []StepResult, string,
) (string, error) {
	return "Done", nil
}

type backgroundTestAdapter struct{}

func (backgroundTestAdapter) Name() string { return "test" }

func (backgroundTestAdapter) Profile() AssetProfile {
	return AssetProfile{Adapter: "test", CommandLanguage: "test"}
}

func (backgroundTestAdapter) SupportsBackground() bool { return true }

func (backgroundTestAdapter) PrepareProposal(proposal *CommandProposal) error {
	proposal.BackgroundEligible = true
	return nil
}

type orderedExecutor struct {
	events *[]string
}

func (e *orderedExecutor) Execute(
	_ context.Context, _ string, _ func(string),
) (string, *int, error) {
	*e.events = append(*e.events, "execute")
	exitCode := 0
	return "ok", &exitCode, nil
}

func (*orderedExecutor) Close() error { return nil }
