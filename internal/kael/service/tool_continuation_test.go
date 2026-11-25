package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/ports"
)

// The harness starts one model turn for the entire tool loop. No modelStarted
// callback resets the run between an execution receipt and its progress queries.
func TestToolOutcomeAllowsReadOnlyContinuation(t *testing.T) {
	for _, outcome := range []string{"success", "running_job", "error", "cancelled", "timeout", "receipt_deadline", "rejected"} {
		t.Run(outcome, func(t *testing.T) {
			ctx := context.Background()
			s, principal, run, call, approval := approvalFixture(t)
			var registration domain.Registration
			if err := s.store.Transaction(ctx, func(tx ports.Tx) error {
				panel, err := tx.PanelInternal(run.PanelSessionID, true)
				if err != nil {
					return err
				}
				panel.ApprovalMode = "auto"
				if err = tx.SavePanel(panel); err != nil {
					return err
				}
				stored, err := tx.Registration(call.RegistrationID, false)
				if err == nil {
					registration = *stored
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			decision := "approve"
			if outcome == "rejected" {
				decision = "reject"
			}
			if _, _, err := s.DecideApproval(ctx, principal, approval.ID, ApprovalDecisionRequest{Decision: decision}); err != nil {
				t.Fatal(err)
			}
			if outcome != "rejected" {
				if err := s.dispatchToolCall(ctx, run, call); err != nil {
					t.Fatal(err)
				}
				if _, _, err := s.SubmitToolResult(ctx, principal, call.ID, ToolResultRequest{Sequence: 1, Status: "running", Result: json.RawMessage(`{"output":"partial"}`)}); err != nil {
					t.Fatal(err)
				}
				stored, err := s.Run(ctx, principal, run.ID)
				if err != nil || stored.State != "waiting_capability" {
					t.Fatalf("progress must keep waiting for the receipt: %+v %v", stored, err)
				}
				if outcome == "receipt_deadline" {
					s.toolResultTimeout = time.Millisecond
				} else {
					status, result := outcome, json.RawMessage(`{"ok":true}`)
					if outcome == "running_job" {
						status, result = "success", json.RawMessage(`{"execution_id":"job","status":"running","process_finished":false}`)
					}
					request := ToolResultRequest{Sequence: 2, Done: true, Status: status, Result: result}
					if _, _, err = s.SubmitToolResult(ctx, principal, call.ID, request); err != nil {
						t.Fatal(err)
					}
					if _, duplicate, err := s.SubmitToolResult(ctx, principal, call.ID, request); err != nil || !duplicate {
						t.Fatalf("terminal receipt retry must remain idempotent: %v %v", duplicate, err)
					}
				}
				if _, err = s.waitToolResult(ctx, run, call); err != nil {
					t.Fatal(err)
				}
			}

			// Two yielded waits followed by completion must all dispatch in the same
			// model turn, including recovery after an error or receipt deadline.
			for _, result := range []json.RawMessage{
				json.RawMessage(`{"execution_id":"job","status":"running","process_finished":false}`),
				json.RawMessage(`{"execution_id":"job","status":"running","process_finished":false}`),
				json.RawMessage(`{"execution_id":"job","status":"success","process_finished":true,"stop_confirmed":true}`),
			} {
				followUp, confirmation, err := s.prepareToolCall(ctx, run, registration, json.RawMessage(`{}`), 0)
				if err != nil || confirmation != nil {
					t.Fatalf("prepare read-only follow-up: %v %v", confirmation, err)
				}
				if err = s.dispatchToolCall(ctx, run, followUp); err != nil {
					t.Fatalf("dispatch follow-up after %s: %v", outcome, err)
				}
				if _, _, err = s.SubmitToolResult(ctx, principal, followUp.ID, ToolResultRequest{Sequence: 1, Done: true, Status: "success", Result: result}); err != nil {
					t.Fatal(err)
				}
				observation, err := s.waitToolResult(ctx, run, followUp)
				if err != nil || string(observation.Result) != string(result) {
					t.Fatalf("follow-up receipt: %+v %v", observation, err)
				}
			}
		})
	}
}

func TestCancelledRunCannotResumeFromToolReceipt(t *testing.T) {
	ctx := context.Background()
	s, principal, run, call, approval := approvalFixture(t)
	if _, _, err := s.DecideApproval(ctx, principal, approval.ID, ApprovalDecisionRequest{Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	if err := s.dispatchToolCall(ctx, run, call); err != nil {
		t.Fatal(err)
	}
	s.finishCancelled(run, nil, context.Canceled)
	if _, _, err := s.SubmitToolResult(ctx, principal, call.ID, ToolResultRequest{Sequence: 1, Done: true, Status: "success"}); err == nil {
		t.Fatal("cancelled run accepted a late receipt")
	}
	stored, err := s.Run(ctx, principal, run.ID)
	if err != nil || stored.State != "cancelled" {
		t.Fatalf("late receipt resumed a cancelled run: %+v %v", stored, err)
	}
}
