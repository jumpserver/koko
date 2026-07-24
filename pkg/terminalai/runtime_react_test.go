package terminalai

import (
	"context"
	"fmt"
	"testing"
)

type runtimeReActModel struct {
	nextCalls int
}

func (m *runtimeReActModel) Decide(
	_ context.Context,
	_, _, _, _, _ string,
) (Decision, error) {
	return Decision{
		Kind: "plan", Summary: "检查并修复服务",
		Steps: []Step{{
			Title: "检查服务", Objective: "检查服务是否正常",
		}},
	}, nil
}

func (m *runtimeReActModel) Next(
	_ context.Context, request ReActRequest,
) (ReActDecision, error) {
	m.nextCalls++
	switch m.nextCalls {
	case 1:
		stepID := request.Steps[0].ID
		return ReActDecision{
			Kind: ReActExecute, ThoughtSummary: "先检查服务状态",
			Observation: ObservationReview{Outcome: "none"},
			Steps: []PlannedStep{{
				ID: stepID, Title: "检查服务", Objective: "检查服务是否正常",
			}},
			NextStepID: stepID,
			Proposal: &CommandProposal{
				Command: "check-service", RiskLevel: 1,
				Execution: ExecutionBackground,
			},
		}, nil
	case 2:
		failedStepID := request.Results[len(request.Results)-1].StepID
		return ReActDecision{
			Kind: ReActExecute, ThoughtSummary: "检查失败，改用修复动作",
			Observation: ObservationReview{
				StepID: failedStepID, Outcome: "error",
				Summary: "服务检查失败", ErrorReason: "exit 1",
			},
			Steps: []PlannedStep{{
				ID: "repair-1", ParentStepID: failedStepID,
				Title: "修复服务", Objective: "执行安全修复",
			}},
			NextStepID: "repair-1",
			Proposal: &CommandProposal{
				Command: "repair-service", RiskLevel: 1,
				Execution: ExecutionBackground,
			},
		}, nil
	case 3:
		completedStepID := request.Results[len(request.Results)-1].StepID
		return ReActDecision{
			Kind: ReActFinish, ThoughtSummary: "修复成功，任务完成",
			Observation: ObservationReview{
				StepID: completedStepID, Outcome: StepCompleted,
				Summary: "服务修复成功",
			},
			Summary: "服务已修复。",
		}, nil
	default:
		return ReActDecision{}, fmt.Errorf("unexpected ReAct turn")
	}
}

func (m *runtimeReActModel) Summarize(
	context.Context,
	string,
	string,
	[]Step,
	[]StepResult,
	string,
) (string, error) {
	return "", fmt.Errorf("unexpected forced summary")
}

type runtimeReActAdapter struct{}

func (runtimeReActAdapter) Name() string {
	return "test-background"
}

func (runtimeReActAdapter) Profile() AssetProfile {
	return AssetProfile{
		Adapter: "test-background", CommandLanguage: "test",
	}
}

func (runtimeReActAdapter) SupportsBackground() bool {
	return true
}

func (runtimeReActAdapter) PrepareProposal(proposal *CommandProposal) error {
	proposal.BackgroundEligible = true
	return nil
}

type runtimeReActExecutor struct {
	commands []string
}

func (e *runtimeReActExecutor) Execute(
	_ context.Context,
	command string,
	_ func(string),
) (string, *int, error) {
	e.commands = append(e.commands, command)
	exitCode := 0
	if command == "check-service" {
		exitCode = 1
		return "service failed", &exitCode, nil
	}
	return "service repaired", &exitCode, nil
}

func (e *runtimeReActExecutor) Close() error {
	return nil
}

func TestRuntimeReActContinuesAfterExecutionFailure(t *testing.T) {
	observer, err := NewObserver(80, 24)
	if err != nil {
		t.Fatalf("create observer: %v", err)
	}
	defer observer.Close()
	model := &runtimeReActModel{}
	executor := &runtimeReActExecutor{}
	var messages []ChatMessage
	runtime := NewRuntime(
		1, model, observer, func([]byte) {},
		func(message ChatMessage) {
			messages = append(messages, message)
		},
	)
	runtime.SetAdapter(runtimeReActAdapter{})
	runtime.SetBackgroundExecutor(executor, nil)
	runtime.run(context.Background(), "修复服务")
	runtime.Close()

	if len(executor.commands) != 2 ||
		executor.commands[0] != "check-service" ||
		executor.commands[1] != "repair-service" {
		t.Fatalf("executed commands = %#v", executor.commands)
	}
	if model.nextCalls != 3 {
		t.Fatalf("ReAct calls = %d, want 3", model.nextCalls)
	}
	var finalText string
	var finalSteps []Step
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Type == "text" {
				finalText = part.Text
			}
			if part.Type != "data-plan" {
				continue
			}
			data, ok := part.Data.(map[string]any)
			if !ok {
				continue
			}
			if steps, ok := data["steps"].([]Step); ok {
				finalSteps = steps
			}
		}
	}
	if finalText != "服务已修复。" {
		t.Fatalf("final text = %q", finalText)
	}
	if len(finalSteps) != 2 ||
		finalSteps[0].Status != StepFailed ||
		finalSteps[1].Status != StepCompleted {
		t.Fatalf("final steps = %#v", finalSteps)
	}
}
