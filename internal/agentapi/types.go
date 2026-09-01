package agentapi

import "encoding/json"

const (
	MaxRequestBodyBytes   = 1024 * 1024
	MaxIdentifierBytes    = 128
	MaxMessageBytes       = 64 * 1024
	MaxEventPayloadBytes  = 256 * 1024
	MaxToolArgumentsBytes = 128 * 1024
	MaxToolErrorBytes     = 16 * 1024
	MaxToolResultBytes    = 128 * 1024
	MaxHistoryLimit       = 256
)

const (
	EventSessionCreated             = "session.created"
	EventSessionClosed              = "session.closed"
	EventSessionApprovalModeChanged = "session.approval_mode_changed"
	EventMessageCreated             = "message.created"
	EventModelRequested             = "model.requested"
	EventModelCompleted             = "model.completed"
	EventRunQueued                  = "run.queued"
	EventRunStarted                 = "run.started"
	EventRunCompleted               = "run.completed"
	EventRunFailed                  = "run.failed"
	EventRunCancelled               = "run.cancelled"
	EventRunInterrupted             = "run.interrupted"
	EventApprovalNeeded             = "approval.requested"
	EventApproval                   = "approval.resolved"
	EventToolCall                   = "tool.call"
	EventToolCancel                 = "tool.cancel"
	EventToolResult                 = "tool.result"
)

const (
	ApprovalModeAlways = "always"
	ApprovalModeAuto   = "auto"
	ApprovalModeNever  = "never"
)

const (
	RunStateRunning     = "running"
	RunStateQueued      = "queued"
	RunStateCompleted   = "completed"
	RunStateFailed      = "failed"
	RunStateCancelled   = "cancelled"
	RunStateInterrupted = "interrupted"
	RunStateUnavailable = "unavailable"
)

// Error is deliberately safe for a public response. Implementations must not
// place credentials or upstream response bodies in it.
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

type Principal struct {
	UserID         string `json:"user_id"`
	OrganizationID string `json:"org_id"`
}

// ContextSnapshot is the explicit, credential-free part of a resource session
// context that an agent is allowed to retain.
type ContextSnapshot struct {
	SessionKind      string `json:"session_kind,omitempty"`
	InteractionMode  string `json:"interaction_mode,omitempty"`
	CommandLanguage  string `json:"command_language,omitempty"`
	Dialect          string `json:"dialect,omitempty"`
	Protocol         string `json:"protocol"`
	ConnectionMethod string `json:"connection_method,omitempty"`
	AssetID          string `json:"asset_id,omitempty"`
	AssetName        string `json:"asset_name,omitempty"`
	AssetAddress     string `json:"asset_address,omitempty"`
	PlatformID       int    `json:"platform_id,omitempty"`
	PlatformCategory string `json:"platform_category,omitempty"`
	PlatformType     string `json:"platform_type,omitempty"`
	PlatformName     string `json:"platform_name,omitempty"`
	BaseOS           string `json:"base_os,omitempty"`
	Charset          string `json:"charset,omitempty"`
	Database         string `json:"database,omitempty"`
	Schema           string `json:"schema,omitempty"`
}

type ToolDefinition struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	Icons        []ToolIcon      `json:"icons,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	Annotations  ToolAnnotations `json:"annotations,omitempty"`
	Meta         map[string]any  `json:"_meta,omitempty"`
}

type ToolIcon struct {
	Source   string   `json:"src"`
	MIMEType string   `json:"mimeType,omitempty"`
	Sizes    []string `json:"sizes,omitempty"`
	Theme    string   `json:"theme,omitempty"`
}

type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

type Toolset struct {
	Tools []ToolDefinition `json:"tools"`
}

type BootstrapResponse struct {
	CSRFToken         string `json:"csrf_token"`
	ExpiresAt         int64  `json:"expires_at"`
	RefreshAt         int64  `json:"refresh_at"`
	InstanceID        string `json:"instance_id"`
	ProtocolVersion   string `json:"protocol_version"`
	CapabilityVersion string `json:"capability_version"`
	SessionID         string `json:"session_id,omitempty"`
	Cursor            uint64 `json:"cursor,omitempty"`
	ContextDigest     string `json:"context_digest,omitempty"`
	ToolsetDigest     string `json:"toolset_digest,omitempty"`
}

type CreateSessionRequest struct {
	Profile           string           `json:"profile"`
	Context           ContextSnapshot  `json:"context,omitempty"`
	ResourceSessionID string           `json:"resource_session_id"`
	Revision          uint64           `json:"revision"`
	Tools             []ToolDefinition `json:"tools"`
	ApprovalMode      string           `json:"approval_mode,omitempty"`
}

type CreateSessionResponse struct {
	SessionID string `json:"session_id"`
	After     uint64 `json:"after"`
}

type ApprovalModeRequest struct {
	Mode string `json:"mode"`
}

type ApprovalModeResponse struct {
	Mode      string `json:"mode"`
	Previous  string `json:"previous,omitempty"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Cursor    uint64 `json:"cursor"`
}

// ApprovalEvent is the payload for approval.requested and approval.resolved.
// ToolName and Arguments are the canonical inputs selected by the agent runtime; Summary
// is supplemental model text and must not replace those trusted fields.
type ApprovalEvent struct {
	ApprovalID      string          `json:"approval_id"`
	Digest          string          `json:"digest"`
	ToolName        string          `json:"tool_name,omitempty"`
	Arguments       json.RawMessage `json:"arguments,omitempty"`
	Summary         string          `json:"summary,omitempty"`
	ModelDurationMS int64           `json:"model_duration_ms,omitempty"`
	Approved        *bool           `json:"approved,omitempty"`
	Reason          string          `json:"reason,omitempty"`
}

type MessageRequest struct {
	MessageID      string         `json:"message_id"`
	IdempotencyKey string         `json:"idempotency_key"`
	Role           string         `json:"role"`
	Parts          []MessagePart  `json:"parts"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type MessagePart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type MessageResponse struct {
	MessageID string `json:"message_id"`
	RunID     string `json:"run_id"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Cursor    uint64 `json:"cursor"`
}

type Event struct {
	Sequence          uint64          `json:"seq"`
	EventID           string          `json:"event_id"`
	Type              string          `json:"type"`
	SessionID         string          `json:"session_id"`
	ResourceSessionID string          `json:"resource_session_id"`
	RunID             string          `json:"run_id,omitempty"`
	MessageID         string          `json:"message_id,omitempty"`
	ToolCallID        string          `json:"tool_call_id,omitempty"`
	Timestamp         int64           `json:"timestamp"`
	Payload           json.RawMessage `json:"payload,omitempty"`
}

type HistoryResponse struct {
	Events     []Event `json:"events"`
	NextCursor uint64  `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

type ApprovalRequest struct {
	Decision string `json:"decision"`
	RunID    string `json:"run_id"`
	Digest   string `json:"digest"`
}

type ApprovalResponse struct {
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Cursor    uint64 `json:"cursor"`
}

type ToolCall struct {
	RunID           string          `json:"run_id"`
	ToolCallID      string          `json:"tool_call_id"`
	Revision        uint64          `json:"revision"`
	ToolName        string          `json:"tool_name"`
	Arguments       json.RawMessage `json:"arguments"`
	ModelDurationMS int64           `json:"model_duration_ms,omitempty"`
}

type ToolCancel struct {
	RunID      string `json:"run_id"`
	ToolCallID string `json:"tool_call_id"`
	Reason     string `json:"reason,omitempty"`
}

type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type ToolResultRequest struct {
	JSONRPC  string          `json:"jsonrpc"`
	ID       string          `json:"id"`
	RunID    string          `json:"run_id"`
	Sequence uint64          `json:"seq"`
	Done     bool            `json:"done"`
	Status   string          `json:"status"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    *JSONRPCError   `json:"error,omitempty"`
}

type ToolResultResponse struct {
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Cursor    uint64 `json:"cursor"`
}

type CancelRequest struct {
	RunID  string `json:"run_id"`
	Reason string `json:"reason,omitempty"`
}

type CancelResponse struct {
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Cursor    uint64 `json:"cursor"`
}

type HealthResponse struct {
	Status           string `json:"status"`
	InstanceID       string `json:"instance_id,omitempty"`
	DegradedSessions int    `json:"degraded_sessions,omitempty"`
}

type Acknowledgement struct {
	OK bool `json:"ok"`
}
