package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestReceiptExpiryRechecksConcurrentProgress(t *testing.T) {
	s, principal, run, call, approval := approvalFixture(t)
	ctx := context.Background()
	if _, _, err := s.DecideApproval(ctx, principal, approval.ID, ApprovalDecisionRequest{Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	if err := s.dispatchToolCall(ctx, run, call); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.SubmitToolResult(ctx, principal, call.ID, ToolResultRequest{
		RunID: run.ID, Sequence: 1, Status: "running",
		Result: json.RawMessage(`{"status":"awaiting_approval"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.expireToolWait(ctx, run, call, 45*time.Second); !errors.Is(err, errToolWaitProgress) {
		t.Fatalf("concurrent review progress was expired: %v", err)
	}
	if _, _, err = s.SubmitToolResult(ctx, principal, call.ID, ToolResultRequest{
		RunID: run.ID, Sequence: 2, Done: true, Status: "success", Result: json.RawMessage(`{"output":"files"}`),
	}); err != nil {
		t.Fatal(err)
	}
	observation, err := s.waitToolResult(ctx, run, call)
	if err != nil || observation.Status != "success" {
		t.Fatalf("lost completion after approval: %+v %v", observation, err)
	}
}

func TestToolReceiptDeadlineReturnsObservationAndRejectsLateResult(t *testing.T) {
	s, principal, run, call, approval := approvalFixture(t)
	s.toolResultTimeout = 10 * time.Millisecond
	ctx := context.Background()
	if _, _, err := s.DecideApproval(ctx, principal, approval.ID, ApprovalDecisionRequest{Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	if err := s.dispatchToolCall(ctx, run, call); err != nil {
		t.Fatal(err)
	}
	observation, err := s.waitToolResult(ctx, run, call)
	if err != nil || observation.Status != "timeout" || len(observation.Error) == 0 {
		t.Fatalf("receipt timeout aborted the model: %#v %v", observation, err)
	}
	if _, _, err = s.SubmitToolResult(ctx, principal, call.ID, ToolResultRequest{RunID: run.ID, Sequence: 2, Done: true, Status: "success", Result: json.RawMessage(`{"ok":true}`)}); err == nil {
		t.Fatal("late result overwrote unknown execution outcome")
	}
	stored, err := s.latestToolResult(ctx, call.ID)
	if err != nil || !stored.Done || stored.Status != "timeout" {
		t.Fatalf("missing terminal receipt: %#v %v", stored, err)
	}
}
