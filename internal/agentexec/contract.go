package agentexec

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jumpserver/koko/internal/assetexec"
)

type ToolError struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	HTTPStatus   int    `json:"http_status,omitempty"`
	Retryable    bool   `json:"retryable"`
	RetryMode    string `json:"retry_mode"`
	NextStep     string `json:"next_step"`
	RetryAfterMS int    `json:"retry_after_ms,omitempty"`
}

func toolError(err error) ToolError {
	result := ToolError{Code: "invalid_arguments", Message: err.Error(), RetryMode: "correct_arguments", NextStep: "correct_arguments"}
	var rejected *CoreError
	if errors.As(err, &rejected) {
		result.Code, _ = rejected.guidance()
		result.HTTPStatus = rejected.Status
		result.RetryMode = "do_not_replay"
		switch {
		case rejected.Status == 401:
			result.Code, result.RetryMode, result.NextStep = "authorization_required", "reauthorize", "reauthorize"
		case rejected.Status == 429 || rejected.Status >= 500:
			result.Code, result.RetryMode, result.NextStep = "service_unavailable", "query_existing", "lookup_by_idempotency_key"
			if rejected.Operation == "assets/" || strings.HasSuffix(rejected.Operation, "/get/") || strings.HasSuffix(rejected.Operation, "/list/") || strings.HasSuffix(rejected.Operation, "/lookup/") {
				result.Retryable, result.RetryMode, result.NextStep = true, "retry_read", "retry_read"
			}
			result.RetryAfterMS = max(rejected.RetryAfterMS, 1000)
		case result.Code == "connection_not_ready":
			result.RetryMode, result.NextStep = "poll_existing", "poll_connection"
		case result.Code == "capacity_exceeded":
			result.RetryMode, result.NextStep, result.RetryAfterMS = "poll_existing", "wait_capacity", 1000
		case result.Code == "idempotency_conflict":
			result.NextStep = "lookup_by_idempotency_key"
		case result.Code == "sftp_session_required" || result.Code == "shell_session_required" || result.Code == "shell_context_required":
			result.NextStep = "choose_compatible_session"
		case result.Code == "asset_not_found":
			result.NextStep = "refresh_assets"
		case rejected.Status == 403:
			result.NextStep = "check_permissions"
		}
		return result
	}
	switch {
	case errors.Is(err, assetexec.ErrContextLost):
		result.Code, result.NextStep = "context_lost", "create_session"
	case errors.Is(err, assetexec.ErrPermission):
		result.Code, result.NextStep = "permission_denied", "check_permissions"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		result.Code, result.RetryMode, result.NextStep = "request_interrupted", "query_existing", "lookup_by_idempotency_key"
	}
	return result
}

type Transfer struct {
	URL                    string            `json:"url"`
	Method                 string            `json:"method"`
	Headers                map[string]string `json:"headers"`
	RequiresBearer         bool              `json:"requires_bearer"`
	RequiresClientTransfer bool              `json:"requires_client_transfer"`
	Ready                  bool              `json:"ready"`
	Size                   any               `json:"size,omitempty"`
	SHA256                 string            `json:"sha256,omitempty"`
	ContentLengthRequired  bool              `json:"content_length_required,omitempty"`
	ExpiresAt              *time.Time        `json:"expires_at,omitempty"`
}

func describeAction(action *Action, p Principal) {
	action.Terminal = terminalState(action.Status)
	if action.NextStep == "" {
		switch action.Status {
		case "awaiting_approval":
			action.NextStep = "open_approval"
		case "awaiting_input":
			action.NextStep = "open_terminal"
		case "unknown":
			action.NextStep = "reconcile_asset_state"
		case "succeeded":
			action.NextStep = "done"
		case "failed", "rejected", "cancelled", "expired":
			action.NextStep = "inspect_result"
		default:
			action.NextStep = "poll_action"
		}
	}
	if action.Result["requires_terminal"] == true {
		action.NextStep = "open_terminal"
	}
	if action.EvidencePending || action.NextOffset != nil {
		action.NextStep = "read_output"
	}
	if action.Status == "accepted" || action.Status == "queued" || action.Status == "dispatching" || action.Status == "running" || action.EvidencePending {
		action.PollAfterMS = 1000
	}
	if action.NextStep == "open_approval" || action.NextStep == "open_terminal" {
		action.PollAfterMS = 0
	}
	if (action.Kind != "file_upload" && action.Kind != "file_download") || p.Resource == "" {
		return
	}
	transfer := &Transfer{RequiresBearer: true, RequiresClientTransfer: true,
		Headers: map[string]string{"X-JumpServer-Action-ID": action.ID}}
	if action.Transfer != nil {
		transfer.ExpiresAt = action.Transfer.ExpiresAt
	}
	if action.Kind == "file_upload" {
		transfer.URL, transfer.Method = strings.TrimRight(p.Resource, "/")+"/files/upload", http.MethodPost
		transfer.Ready = action.NextStep == "upload_bytes"
		transfer.ContentLengthRequired = true
		transfer.Size, transfer.SHA256 = action.Request["size"], stringValue(action.Request["sha256"])
		transfer.Headers["X-Content-SHA256"] = transfer.SHA256
	} else {
		transfer.URL, transfer.Method = strings.TrimRight(p.Resource, "/")+"/files/download", http.MethodGet
		transfer.Ready = action.Status == "succeeded"
		transfer.Size, transfer.SHA256 = action.Result["size"], stringValue(action.Result["sha256"])
		if transfer.Ready {
			action.NextStep = "download_bytes"
		}
	}
	action.Transfer = transfer
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}
