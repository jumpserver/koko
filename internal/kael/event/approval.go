package event

import (
	"errors"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/ports"
)

func ExpireApproval(tx ports.Tx, approval *domain.Approval, now time.Time) error {
	if approval.State == "expired" {
		return nil
	}
	approval.State, approval.Reason, approval.UpdatedAt, approval.ResolvedAt = "expired", "approval expired", now, &now
	if err := tx.SaveApproval(approval); err != nil {
		return err
	}
	call, err := tx.ToolCall(approval.ToolCallID, true)
	if errors.Is(err, ports.ErrNotFound) {
		return ResolveApproval(tx, approval, now)
	}
	if err != nil {
		return err
	}
	switch call.State {
	case "created", "waiting_approval", "dispatched", "running":
		call.State, call.UpdatedAt, call.FinishedAt = "timeout", now, &now
		if err = tx.SaveToolCall(call); err != nil {
			return err
		}
	}
	return ResolveApproval(tx, approval, now)
}

// ResolveApproval projects a persisted decision for live delivery and history replay.
func ResolveApproval(tx ports.Tx, approval *domain.Approval, now time.Time) error {
	panel, err := tx.PanelInternal(approval.PanelSessionID, true)
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	payload := map[string]any{"approval_id": approval.ID, "tool_call_id": approval.ToolCallID, "status": approval.State, "state": approval.State, "approved": approval.State == "approved", "reason": approval.Reason}
	_, _, err = Project(tx, "approval.resolved", "approval", approval.ID, "approval", References{ConversationID: approval.ConversationID, RunID: approval.RunID, ToolCallID: approval.ToolCallID, ApprovalID: approval.ID}, payload, []domain.PanelSession{*panel}, now)
	return err
}
