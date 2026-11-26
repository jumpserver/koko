package ports

import (
	"context"
	"encoding/json"

	"github.com/jumpserver/koko/internal/kael/domain"
)

type CapabilityRequest struct {
	Principal      domain.Principal
	ConversationID string
	RunID          string
	ToolCallID     string
	ApprovalID     string
	Profile        string
	Registration   domain.Registration
	Arguments      json.RawMessage
	// AccountPassword is supplied by the approval UI only. It must never be
	// included in Arguments, previews, tool results, or the runtime store.
	AccountPassword string
	// SecretInputs are supplied by the approval UI for write-only Core fields.
	// They are never part of model arguments, previews, or persisted tool calls.
	SecretInputs map[string]string
}

type CapabilityPolicy struct {
	Risk                 string
	RequiresConfirmation bool
	Preview              json.RawMessage
}

type InvalidCapabilityArgumentsError struct {
	Detail string
}

func (e *InvalidCapabilityArgumentsError) Error() string {
	return "capability arguments are invalid: " + e.Detail
}

type CapabilityResult struct {
	Status                 string
	Result                 json.RawMessage
	Error                  json.RawMessage
	ResultCards            json.RawMessage
	ExecutorAuditReference string
}

type CapabilityProvider interface {
	Registrations(context.Context, domain.Principal, string) ([]domain.Registration, error)
	Prepare(context.Context, CapabilityRequest) (CapabilityPolicy, error)
	Execute(context.Context, CapabilityRequest) (CapabilityResult, error)
	Refresh(context.Context) (map[string]any, error)
}
