package service

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/model"
	"github.com/jumpserver/koko/internal/kael/ports"
)

func publicFailure(err error) (code, detail, stage, next string) {
	if errors.Is(err, ports.ErrCapacity) {
		return "storage_capacity_exceeded", "Local AI history storage is full or disk space is low. Free space before retrying.", "preparing", "contact_admin"
	}
	code, detail, next = "run_failed", "The request stopped before completion. Review the completed steps before continuing.", "contact_admin"
	var providerErr *model.ProviderError
	if errors.As(err, &providerErr) {
		stage = "model"
		switch providerErr.Kind {
		case model.ErrorAuthentication:
			return "model_authentication", "The model service rejected its configured credentials.", stage, "check_model_config"
		case model.ErrorRateLimit:
			return "model_rate_limit", "The model service is currently rate limited.", stage, "retry_later"
		case model.ErrorTimeout:
			return "model_timeout", "The model service did not respond in time.", stage, "retry_later"
		case model.ErrorInvalidRequest:
			return "model_invalid_request", "The model service could not accept this request.", stage, "check_model_config"
		case model.ErrorInvalidOutput:
			return "model_invalid_output", "The model service returned an incomplete or unusable response.", stage, "retry_later"
		default:
			return "model_unavailable", "The model service is temporarily unavailable.", stage, "retry_later"
		}
	}
	var serviceErr *Error
	if errors.As(err, &serviceErr) {
		if serviceErr.Code == "approval_rejected" {
			return "approval_rejected", "You declined the confirmation. That operation was not executed.", "approval", "continue"
		}
		if strings.HasPrefix(serviceErr.Code, "approval_") {
			return "approval_expired", "The operation requires a new valid approval.", "approval", "approve_again"
		}
		if serviceErr.Kind == Forbidden {
			return "permission_denied", "The requested operation is not available with the current permissions.", "tool", "check_permissions"
		}
		if strings.HasPrefix(serviceErr.Code, "service_capability_") || strings.HasPrefix(serviceErr.Code, "capability_") {
			return "capability_unavailable", "The connection needed to execute this operation is unavailable.", "tool", "contact_admin"
		}
		if strings.HasPrefix(serviceErr.Code, "storage_") {
			return "storage_unavailable", "The execution state could not be saved reliably.", "preparing", "contact_admin"
		}
	}
	return
}

func describeFailure(tx ports.Tx, run *domain.Run, code, stage, next string) (json.RawMessage, error) {
	calls, err := tx.ListRunToolCalls(run.ID)
	if err != nil {
		return nil, err
	}
	results := make(map[string]domain.ToolResult, len(calls))
	approvals := make(map[string]domain.Approval)
	for _, call := range calls {
		if call.RequiresConfirmation {
			approval, approvalErr := tx.ApprovalByToolCall(call.ID, false)
			if approvalErr == nil {
				approvals[call.ID] = *approval
			} else if !errors.Is(approvalErr, ports.ErrNotFound) {
				return nil, approvalErr
			}
		}
		result, resultErr := tx.LatestToolResult(call.ID)
		if resultErr == nil {
			results[call.ID] = *result
		} else if !errors.Is(resultErr, ports.ErrNotFound) {
			return nil, resultErr
		}
	}
	if stage == "" {
		stage = "preparing"
		if run.ModelRequestCount > 0 {
			stage = "model"
		}
		for _, call := range calls {
			if call.State == "waiting_approval" {
				stage = "approval"
			}
			if call.State == "running" || call.State == "dispatched" {
				stage = "tool"
			}
		}
	}
	return json.Marshal(domain.DescribeRunFailure(calls, results, approvals, stage, code, next))
}
