package domain

import (
	"encoding/json"
	"time"
)

const (
	APIVersion            = "v1"
	ProtocolVersion       = "1"
	CapabilityVersion     = "1"
	MaxIdentifierBytes    = 128
	MaxMessageBytes       = 64 * 1024
	MaxContextBytes       = 4 * 1024 * 1024
	MaxToolSchemaBytes    = 64 * 1024
	MaxToolArgumentsBytes = 128 * 1024
	MaxToolResultBytes    = 128 * 1024
	MaxEventPayloadBytes  = 256 * 1024
	MaxTools              = 64
	MaxQueuedRuns         = 64
	MaxToolCalls          = 128
	MaxPageSize           = 256
	MaxExtractedTextBytes = 40 * 1024
	MaxImagePixels        = 40 * 1000 * 1000
)

type Principal struct {
	SubjectID      string   `json:"subject_id"`
	Name           string   `json:"-"`
	Username       string   `json:"-"`
	OrganizationID string   `json:"organization_id"`
	AuthSource     string   `json:"auth_source"`
	Fingerprint    string   `json:"fingerprint"`
	IsSuperuser    bool     `json:"is_superuser,omitempty"`
	IsOrgAdmin     bool     `json:"is_org_admin,omitempty"`
	Permissions    []string `json:"permissions,omitempty"`
}

func (p Principal) Owns(subjectID, organizationID string) bool {
	return p.SubjectID != "" && p.SubjectID == subjectID && p.OrganizationID == organizationID
}

type Conversation struct {
	ID              string          `json:"id"`
	SubjectID       string          `json:"-"`
	SubjectName     string          `json:"-"`
	SubjectUsername string          `json:"-"`
	OrganizationID  string          `json:"-"`
	Kind            string          `json:"kind"`
	Assistant       string          `json:"assistant"`
	Profile         string          `json:"profile"`
	Surface         string          `json:"surface,omitempty"`
	Title           string          `json:"title,omitempty"`
	Status          string          `json:"status"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	Version         uint64          `json:"version"`
	CreatedAt       time.Time       `json:"date_created"`
	UpdatedAt       time.Time       `json:"date_updated"`
	ArchivedAt      *time.Time      `json:"archived_at,omitempty"`
	DeletedAt       *time.Time      `json:"-"`
}

type MessagePart struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ArtifactID string          `json:"artifact_id,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
}

type ResultCard struct {
	Type    string          `json:"type,omitempty"`
	Title   string          `json:"title,omitempty"`
	Source  json.RawMessage `json:"source,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
}

type Message struct {
	ID                string          `json:"id"`
	ConversationID    string          `json:"conversation_id"`
	SubjectID         string          `json:"-"`
	OrganizationID    string          `json:"-"`
	Role              string          `json:"role"`
	Status            string          `json:"status"`
	Parts             json.RawMessage `json:"parts"`
	Content           string          `json:"content"`
	ResultCards       json.RawMessage `json:"result_cards,omitempty"`
	ErrorCode         string          `json:"error_code,omitempty"`
	ErrorDetail       string          `json:"error,omitempty"`
	Failure           json.RawMessage `json:"failure,omitempty"`
	IdempotencyKey    string          `json:"-"`
	IdempotencyDigest string          `json:"-"`
	ParentMessageID   string          `json:"parent_message_id,omitempty"`
	RegeneratedFromID string          `json:"regenerated_from_id,omitempty"`
	InputTokens       int64           `json:"input_tokens,omitempty"`
	OutputTokens      int64           `json:"output_tokens,omitempty"`
	CreatedAt         time.Time       `json:"date_created"`
	UpdatedAt         time.Time       `json:"date_updated"`
}

type Artifact struct {
	ID             string     `json:"id"`
	SubjectID      string     `json:"-"`
	OrganizationID string     `json:"-"`
	MessageID      string     `json:"message_id,omitempty"`
	Status         string     `json:"status"`
	Kind           string     `json:"kind"`
	Name           string     `json:"name"`
	MediaType      string     `json:"media_type"`
	Size           int64      `json:"size"`
	Digest         string     `json:"digest"`
	StorageKey     string     `json:"-"`
	ExtractedText  string     `json:"-"`
	CreatedAt      time.Time  `json:"date_created"`
	DeletedAt      *time.Time `json:"-"`
}

type PanelSession struct {
	ID               string     `json:"id"`
	ConversationID   string     `json:"conversation_id"`
	SubjectID        string     `json:"-"`
	OrganizationID   string     `json:"-"`
	Surface          string     `json:"surface"`
	Profile          string     `json:"profile"`
	ApprovalMode     string     `json:"approval_mode"`
	State            string     `json:"state"`
	ContextVersion   uint64     `json:"context_version"`
	ContextDigest    string     `json:"context_digest,omitempty"`
	RegistryRevision uint64     `json:"registry_revision"`
	ResumeTokenHash  string     `json:"-"`
	ClientInstanceID string     `json:"-"`
	ConnectionOwner  string     `json:"-"`
	NextSequence     uint64     `json:"cursor"`
	LeaseExpiresAt   time.Time  `json:"lease_expires_at"`
	LastHeartbeatAt  time.Time  `json:"last_heartbeat_at"`
	CreatedAt        time.Time  `json:"date_created"`
	UpdatedAt        time.Time  `json:"date_updated"`
	ClosedAt         *time.Time `json:"closed_at,omitempty"`
}

type ContextSnapshot struct {
	ID             string          `json:"id"`
	PanelSessionID string          `json:"panel_session_id"`
	Version        uint64          `json:"version"`
	Digest         string          `json:"digest"`
	Domain         string          `json:"domain"`
	Surface        string          `json:"surface"`
	Sensitivity    string          `json:"sensitivity"`
	Data           json.RawMessage `json:"data"`
	CreatedAt      time.Time       `json:"date_created"`
}

type ToolAnnotations struct {
	ReadOnly      bool   `json:"read_only"`
	Destructive   bool   `json:"destructive"`
	Idempotent    bool   `json:"idempotent"`
	OpenWorld     bool   `json:"open_world"`
	FinalResult   bool   `json:"final_result,omitempty"`
	CommandPolicy string `json:"command_policy,omitempty"`
}

type Registration struct {
	ID                   string          `json:"id"`
	PanelSessionID       string          `json:"panel_session_id"`
	BindingKind          string          `json:"binding_kind"`
	ExecutionBindingID   string          `json:"execution_binding_id,omitempty"`
	ClientKey            string          `json:"client_key"`
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	InputSchema          json.RawMessage `json:"input_schema"`
	OutputSchema         json.RawMessage `json:"output_schema,omitempty"`
	DefinitionVersion    string          `json:"definition_version"`
	DefinitionDigest     string          `json:"definition_digest"`
	Namespace            string          `json:"namespace"`
	Risk                 string          `json:"risk"`
	RequiresConfirmation bool            `json:"requires_confirmation"`
	AnnotationsJSON      json.RawMessage `json:"-"`
	RegistryRevision     uint64          `json:"registry_revision"`
	State                string          `json:"state"`
	LeaseExpiresAt       time.Time       `json:"lease_expires_at"`
	CreatedAt            time.Time       `json:"date_created"`
	UpdatedAt            time.Time       `json:"date_updated"`
}

func (r Registration) Annotations() ToolAnnotations {
	var value ToolAnnotations
	_ = json.Unmarshal(r.AnnotationsJSON, &value)
	return value
}

type RunRegistrationSnapshot struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	BindingKind          string          `json:"binding_kind"`
	ExecutionBindingID   string          `json:"execution_binding_id,omitempty"`
	DefinitionVersion    string          `json:"definition_version"`
	DefinitionDigest     string          `json:"definition_digest"`
	Risk                 string          `json:"risk"`
	RequiresConfirmation bool            `json:"requires_confirmation"`
	Annotations          ToolAnnotations `json:"annotations"`
	InputSchema          json.RawMessage `json:"input_schema"`
	OutputSchema         json.RawMessage `json:"output_schema,omitempty"`
}

type Run struct {
	ID                       string          `json:"id"`
	ConversationID           string          `json:"conversation_id"`
	InputMessageID           string          `json:"input_message_id"`
	OutputMessageID          string          `json:"output_message_id,omitempty"`
	RegeneratedFromID        string          `json:"regenerated_from_id,omitempty"`
	PanelSessionID           string          `json:"panel_session_id"`
	SubjectID                string          `json:"-"`
	OrganizationID           string          `json:"-"`
	AuthorizationSuperuser   bool            `json:"-"`
	AuthorizationOrgAdmin    bool            `json:"-"`
	AuthorizationPermissions []string        `json:"-"`
	Profile                  string          `json:"profile"`
	ProfileVersion           string          `json:"profile_version"`
	ExecutionMode            string          `json:"execution_mode"`
	CapabilityMode           string          `json:"capability_mode"`
	ContextVersion           uint64          `json:"context_version"`
	ContextDigest            string          `json:"context_digest"`
	RegistryRevision         uint64          `json:"registry_revision"`
	RegistrationSnapshot     json.RawMessage `json:"registration_snapshot"`
	ModelPolicy              json.RawMessage `json:"model_policy"`
	ApprovalPolicy           json.RawMessage `json:"approval_policy"`
	State                    string          `json:"state"`
	CancelReason             string          `json:"cancel_reason,omitempty"`
	ErrorCode                string          `json:"error_code,omitempty"`
	ErrorDetail              string          `json:"error,omitempty"`
	Failure                  json.RawMessage `json:"failure,omitempty"`
	Partial                  bool            `json:"partial"`
	FinishReason             string          `json:"finish_reason,omitempty"`
	RoundCount               int             `json:"round_count"`
	ModelRequestCount        int             `json:"model_request_count"`
	InputTokens              int64           `json:"input_tokens"`
	OutputTokens             int64           `json:"output_tokens"`
	ClaimOwner               string          `json:"-"`
	ClaimExpiresAt           *time.Time      `json:"-"`
	IdempotencyKey           string          `json:"-"`
	IdempotencyDigest        string          `json:"-"`
	CreatedAt                time.Time       `json:"date_created"`
	UpdatedAt                time.Time       `json:"date_updated"`
	StartedAt                *time.Time      `json:"started_at,omitempty"`
	FinishedAt               *time.Time      `json:"finished_at,omitempty"`
}

func (r Run) Terminal() bool {
	return r.State == "completed" || r.State == "failed" || r.State == "cancelled"
}

type ModelCall struct {
	ID              string     `json:"id"`
	RunID           string     `json:"run_id"`
	Sequence        int        `json:"sequence"`
	Provider        string     `json:"provider"`
	Model           string     `json:"model"`
	State           string     `json:"state"`
	RequestID       string     `json:"request_id,omitempty"`
	InputTokens     int64      `json:"input_tokens"`
	OutputTokens    int64      `json:"output_tokens"`
	ReasoningTokens int64      `json:"reasoning_tokens"`
	DurationMS      int64      `json:"duration_ms"`
	ErrorCode       string     `json:"error_code,omitempty"`
	CreatedAt       time.Time  `json:"date_created"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

type ToolCall struct {
	ID                   string          `json:"id"`
	ConversationID       string          `json:"conversation_id"`
	RunID                string          `json:"run_id"`
	PanelSessionID       string          `json:"panel_session_id"`
	BindingKind          string          `json:"binding_kind"`
	ExecutionBindingID   string          `json:"execution_binding_id,omitempty"`
	SubjectID            string          `json:"-"`
	OrganizationID       string          `json:"-"`
	RegistrationID       string          `json:"registration_id"`
	DefinitionVersion    string          `json:"definition_version"`
	DefinitionDigest     string          `json:"definition_digest"`
	ToolName             string          `json:"tool_name"`
	Arguments            json.RawMessage `json:"arguments"`
	ArgumentsDigest      string          `json:"arguments_digest"`
	Risk                 string          `json:"risk"`
	RequiresConfirmation bool            `json:"requires_confirmation"`
	InvocationSequence   uint64          `json:"invocation_sequence"`
	InvocationID         string          `json:"invocation_id"`
	State                string          `json:"state"`
	CreatedAt            time.Time       `json:"date_created"`
	UpdatedAt            time.Time       `json:"date_updated"`
	FinishedAt           *time.Time      `json:"finished_at,omitempty"`
}

type ToolResult struct {
	ID                     string          `json:"id"`
	ToolCallID             string          `json:"tool_call_id"`
	RunID                  string          `json:"run_id"`
	PanelSessionID         string          `json:"panel_session_id"`
	Sequence               uint64          `json:"sequence"`
	Done                   bool            `json:"done"`
	Status                 string          `json:"status"`
	Result                 json.RawMessage `json:"result,omitempty"`
	ErrorJSON              json.RawMessage `json:"error,omitempty"`
	PayloadDigest          string          `json:"-"`
	ExecutorAuditReference string          `json:"executor_audit_reference,omitempty"`
	CreatedAt              time.Time       `json:"date_created"`
}

type Approval struct {
	ID                string          `json:"id"`
	ConversationID    string          `json:"conversation_id"`
	RunID             string          `json:"run_id"`
	ToolCallID        string          `json:"tool_call_id"`
	RegistrationID    string          `json:"registration_id"`
	PanelSessionID    string          `json:"panel_session_id"`
	Scope             string          `json:"scope"`
	SubjectID         string          `json:"-"`
	OrganizationID    string          `json:"-"`
	DefinitionVersion string          `json:"definition_version"`
	ArgumentsDigest   string          `json:"arguments_digest"`
	Risk              string          `json:"risk"`
	Preview           json.RawMessage `json:"preview"`
	PolicyVersion     string          `json:"policy_version"`
	State             string          `json:"status"`
	DecisionDigest    string          `json:"-"`
	Remembered        bool            `json:"remembered,omitempty"`
	Reason            string          `json:"reason,omitempty"`
	ExpiresAt         time.Time       `json:"expires_at"`
	CreatedAt         time.Time       `json:"date_created"`
	UpdatedAt         time.Time       `json:"date_updated"`
	ResolvedAt        *time.Time      `json:"resolved_at,omitempty"`
}

type DomainEvent struct {
	ID             string          `json:"event_id"`
	Sequence       uint64          `json:"seq"`
	ConversationID string          `json:"conversation_id,omitempty"`
	RunID          string          `json:"run_id,omitempty"`
	MessageID      string          `json:"message_id,omitempty"`
	ToolCallID     string          `json:"tool_call_id,omitempty"`
	ApprovalID     string          `json:"approval_id,omitempty"`
	AggregateType  string          `json:"aggregate_type"`
	AggregateID    string          `json:"aggregate_id"`
	Type           string          `json:"type"`
	SchemaVersion  string          `json:"schema_version"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"timestamp"`
	ProjectedAt    *time.Time      `json:"-"`
}

type PanelDelivery struct {
	ID             string          `json:"-"`
	PanelSessionID string          `json:"panel_session_id"`
	Sequence       uint64          `json:"seq"`
	EventID        string          `json:"event_id"`
	Audience       string          `json:"audience"`
	Type           string          `json:"type"`
	SchemaVersion  string          `json:"schema_version"`
	ConversationID string          `json:"conversation_id,omitempty"`
	RunID          string          `json:"run_id,omitempty"`
	MessageID      string          `json:"message_id,omitempty"`
	ToolCallID     string          `json:"tool_call_id,omitempty"`
	ApprovalID     string          `json:"approval_id,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"timestamp"`
}

type AuditRecord struct {
	ID                     string          `json:"id"`
	SubjectHash            string          `json:"subject_hash"`
	OrganizationID         string          `json:"organization_id"`
	ConversationID         string          `json:"conversation_id,omitempty"`
	PanelSessionID         string          `json:"panel_session_id,omitempty"`
	RunID                  string          `json:"run_id,omitempty"`
	ModelCallID            string          `json:"model_call_id,omitempty"`
	ToolCallID             string          `json:"tool_call_id,omitempty"`
	ApprovalID             string          `json:"approval_id,omitempty"`
	RegistrationID         string          `json:"registration_id,omitempty"`
	ExecutorAuditReference string          `json:"executor_audit_reference,omitempty"`
	Action                 string          `json:"action"`
	Summary                json.RawMessage `json:"summary"`
	CreatedAt              time.Time       `json:"date_created"`
}

type Page[T any] struct {
	Results []T    `json:"results"`
	Next    string `json:"next,omitempty"`
	Count   int64  `json:"count"`
}

type PublicError struct {
	Code      string `json:"code"`
	Detail    string `json:"detail"`
	Retryable bool   `json:"retryable,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}
