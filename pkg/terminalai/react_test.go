package terminalai

import (
	"strings"
	"testing"
)

func TestReActFailureCanRevisePlanAndContinue(t *testing.T) {
	plan := newReActPlan("plan-1", "repair service", []Step{
		{
			ID: "step-1", Title: "inspect", Objective: "inspect service",
			Status: StepReviewing, rootStepID: "step-1",
		},
		{
			ID: "step-2", Title: "repair", Objective: "repair service",
			Status: StepPending, rootStepID: "step-2",
		},
		{
			ID: "step-3", Title: "obsolete", Objective: "old approach",
			Status: StepPending, rootStepID: "step-3",
		},
	})
	plan.results = []StepResult{{
		StepID: "step-1", Command: "systemctl status app",
		Output: "unit not found", Status: StepReviewing,
		Execution: ExecutionPTY,
	}}
	decision := ReActDecision{
		Kind: ReActExecute, ThoughtSummary: "改用实际服务名继续检查",
		Observation: ObservationReview{
			StepID: "step-1", Outcome: "error",
			Summary: "目标服务不存在", ErrorReason: "unit not found",
		},
		Steps: []PlannedStep{
			{
				ID: "step-2", Title: "修复服务",
				Objective: "根据实际服务状态修复",
			},
			{
				ID: "retry-1", ParentStepID: "step-1",
				Title: "查找服务", Objective: "定位实际服务名称",
			},
		},
		NextStepID: "retry-1",
		Proposal: &CommandProposal{
			Command:   "systemctl list-units --type=service",
			RiskLevel: 1, Execution: ExecutionPTY,
		},
	}
	transition, err := plan.preview(decision)
	if err != nil {
		t.Fatalf("preview ReAct decision: %v", err)
	}
	if len(transition.steps) != 3 {
		t.Fatalf("step count = %d, want immutable history plus two pending", len(transition.steps))
	}
	if transition.steps[0].Status != StepFailed ||
		transition.results[0].Status != StepFailed {
		t.Fatalf("failed observation was not applied: %#v", transition)
	}
	if _, index := findStep(transition.steps, "step-3"); index >= 0 {
		t.Fatal("deleted pending step remained in revised plan")
	}
	retry, index := findStep(transition.steps, transition.nextStepID)
	if index < 0 || retry.ParentStepID != "step-1" ||
		retry.rootStepID != "step-1" {
		t.Fatalf("retry step relationship = %#v", retry)
	}
	if err = plan.beginExecution(transition); err != nil {
		t.Fatalf("begin revised step: %v", err)
	}
	retry, _ = findStep(plan.steps, transition.nextStepID)
	if retry.Status != StepInProgress {
		t.Fatalf("retry status = %q", retry.Status)
	}
}

func TestReActFinishMarksRemainingStepsSkipped(t *testing.T) {
	plan := newReActPlan("plan-1", "inspect", []Step{{
		ID: "step-1", Title: "inspect", Objective: "inspect",
		Status: StepPending,
	}})
	transition, err := plan.preview(ReActDecision{
		Kind: ReActFinish, ThoughtSummary: "现有证据已足够",
		Observation: ObservationReview{Outcome: "none"},
		Steps: []PlannedStep{{
			ID: "step-1", Title: "inspect", Objective: "inspect",
		}},
		Summary: "任务结束，剩余检查不再需要。",
	})
	if err != nil {
		t.Fatalf("preview finish: %v", err)
	}
	if transition.steps[0].Status != StepSkipped {
		t.Fatalf("pending status = %q", transition.steps[0].Status)
	}
}

func TestReActCannotModifyExecutedHistory(t *testing.T) {
	plan := newReActPlan("plan-1", "inspect", []Step{{
		ID: "step-1", Title: "done", Objective: "done",
		Status: StepCompleted,
	}})
	_, err := plan.preview(ReActDecision{
		Kind: ReActFinish, ThoughtSummary: "结束",
		Observation: ObservationReview{Outcome: "none"},
		Steps: []PlannedStep{{
			ID: "step-1", Title: "changed", Objective: "changed",
		}},
		Summary: "结束。",
	})
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("immutable history error = %v", err)
	}
}

func TestReActRejectsCyclicPendingSteps(t *testing.T) {
	plan := newReActPlan("plan-1", "inspect", []Step{
		{
			ID: "step-1", Title: "one", Objective: "one",
			Status: StepPending,
		},
		{
			ID: "step-2", Title: "two", Objective: "two",
			Status: StepPending,
		},
	})
	_, err := plan.preview(ReActDecision{
		Kind: ReActFinish, ThoughtSummary: "结束",
		Observation: ObservationReview{Outcome: "none"},
		Steps: []PlannedStep{
			{
				ID: "new-1", ParentStepID: "new-2",
				Title: "one", Objective: "one",
			},
			{
				ID: "new-2", ParentStepID: "new-1",
				Title: "two", Objective: "two",
			},
		},
		Summary: "结束。",
	})
	if err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("cyclic plan error = %v", err)
	}
}

func TestReActStepAttemptLimit(t *testing.T) {
	steps := []Step{
		{
			ID: "root", Title: "root", Objective: "root",
			Status: StepFailed, rootStepID: "root",
		},
		{
			ID: "retry-1", ParentStepID: "root",
			Title: "retry", Objective: "retry",
			Status: StepFailed, rootStepID: "root",
		},
		{
			ID: "retry-2", ParentStepID: "retry-1",
			Title: "retry", Objective: "retry",
			Status: StepFailed, rootStepID: "root",
		},
		{
			ID: "retry-3", ParentStepID: "retry-2",
			Title: "retry", Objective: "retry",
			Status: StepPending, rootStepID: "root",
		},
	}
	plan := newReActPlan("plan-1", "retry", steps)
	for _, step := range steps[:3] {
		plan.results = append(plan.results, StepResult{
			StepID: step.ID, Command: "false", Status: StepFailed,
		})
	}
	_, err := plan.preview(ReActDecision{
		Kind: ReActExecute, ThoughtSummary: "再次重试",
		Observation: ObservationReview{Outcome: "none"},
		Steps: []PlannedStep{{
			ID: "retry-3", ParentStepID: "retry-2",
			Title: "retry", Objective: "retry",
		}},
		NextStepID: "retry-3",
		Proposal: &CommandProposal{
			Command: "false", RiskLevel: 1, Execution: ExecutionPTY,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "3-attempt") {
		t.Fatalf("attempt limit error = %v", err)
	}
}

func TestReActSameCommandContextLimit(t *testing.T) {
	plan := newReActPlan("plan-1", "inspect", []Step{{
		ID: "step-1", Title: "inspect", Objective: "inspect",
		Status: StepPending,
	}})
	key := commandContextKey("step-1", "pwd", "")
	plan.commandRepeats[key] = maxSameCommandContext
	_, err := plan.preview(ReActDecision{
		Kind: ReActExecute, ThoughtSummary: "继续检查",
		Observation: ObservationReview{Outcome: "none"},
		Steps: []PlannedStep{{
			ID: "step-1", Title: "inspect", Objective: "inspect",
		}},
		NextStepID: "step-1",
		Proposal: &CommandProposal{
			Command: "pwd", RiskLevel: 1, Execution: ExecutionPTY,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "without material context change") {
		t.Fatalf("command loop error = %v", err)
	}
}

func TestRecordedExecutionCarriesTruncationState(t *testing.T) {
	plan := newReActPlan("plan-1", "inspect", []Step{{
		ID: "step-1", Title: "inspect", Objective: "inspect",
		Status: StepInProgress,
	}})
	exitCode := 0
	err := plan.recordExecution(
		"step-1",
		CommandProposal{Command: "inspect", Execution: ExecutionPTY},
		outputTruncatedFirstMarker+"\nvisible output",
		&exitCode,
		nil,
	)
	if err != nil {
		t.Fatalf("record execution: %v", err)
	}
	if len(plan.results) != 1 || !plan.results[0].OutputTruncated {
		t.Fatalf("recorded result = %#v", plan.results)
	}
}
