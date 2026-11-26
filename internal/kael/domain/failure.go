package domain

import (
	"encoding/json"
	"regexp"
)

// ExecutionStep contains identifiers and confirmed state, never tool arguments
// or upstream error text. ReadOnly comes from the trusted execution policy.
type ExecutionStep struct {
	ToolName    string `json:"tool_name"`
	OperationID string `json:"operation_id,omitempty"`
	ReadOnly    bool   `json:"read_only"`
	Status      string `json:"status,omitempty"`
}

type RunFailure struct {
	Stage          string          `json:"stage"`
	Code           string          `json:"code"`
	ToolName       string          `json:"tool_name,omitempty"`
	FailedStep     *ExecutionStep  `json:"failed_step,omitempty"`
	CompletedSteps []ExecutionStep `json:"completed_steps"`
	UncertainSteps []ExecutionStep `json:"uncertain_steps"`
	NextAction     string          `json:"next_action"`
}

var operationIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,128}$`)

// DescribeRunFailure uses persisted execution receipts. A request having been
// sent is not proof that it succeeded, failed without effects, or was undone.
func DescribeRunFailure(calls []ToolCall, results map[string]ToolResult, approvals map[string]Approval, stage, code, nextAction string) RunFailure {
	failure := RunFailure{Stage: stage, Code: code, NextAction: nextAction, CompletedSteps: []ExecutionStep{}, UncertainSteps: []ExecutionStep{}}
	for _, call := range calls {
		step := ExecutionStep{ToolName: call.ToolName, ReadOnly: call.Risk == "read"}
		var args struct {
			OperationID string `json:"operation_id"`
		}
		if json.Unmarshal(call.Arguments, &args) == nil && operationIdentifier.MatchString(args.OperationID) {
			step.OperationID = args.OperationID
		}
		result, hasResult := results[call.ID]
		var receipt struct {
			OK         *bool `json:"ok"`
			StatusCode int   `json:"status_code"`
		}
		_ = json.Unmarshal(result.Result, &receipt)
		if hasResult && result.Done && result.Status == "success" && (receipt.OK == nil || *receipt.OK) {
			step.Status = "success"
			failure.CompletedSteps = append(failure.CompletedSteps, step)
			if !step.ReadOnly {
				failure.NextAction = "review_completed"
			}
			continue
		}
		approval, hasApproval := approvals[call.ID]
		if call.State == "created" || call.State == "waiting_approval" || call.State == "rejected" ||
			call.RequiresConfirmation && hasApproval && approval.State != "consumed" {
			if stage == "approval" {
				failure.ToolName = call.ToolName
				failure.FailedStep = &step
			}
			continue
		}
		// A dispatched read may have an unknown result too. For a write, even an
		// HTTP 5xx can follow a committed change and must not invite blind retry.
		uncertain := call.State == "running" || call.State == "dispatched" || call.State == "timeout" || call.State == "cancelled"
		if !step.ReadOnly && (!hasResult || !result.Done || receipt.StatusCode == 0 || receipt.StatusCode >= 500) {
			uncertain = true
		}
		if uncertain {
			failure.UncertainSteps = append(failure.UncertainSteps, step)
		}
		if stage == "tool" {
			failure.ToolName = call.ToolName
			failure.FailedStep = &step
		}
	}
	for _, step := range failure.UncertainSteps {
		if !step.ReadOnly {
			failure.NextAction = "inspect_resource"
			break
		}
	}
	return failure
}
