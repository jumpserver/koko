package ports

import (
	"context"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
)

var ErrNotFound = domainError("not found")
var ErrCapacity = domainError("local AI storage capacity exhausted")
var ErrConflict = domainError("conflict")
var ErrUnavailable = domainError("unavailable")

type domainError string

func (e domainError) Error() string { return string(e) }

type Store interface {
	Ready(context.Context) error
	Close() error
	Transaction(context.Context, func(Tx) error) error
	View(context.Context, func(Tx) error) error
}

type Tx interface {
	CreateConversation(*domain.Conversation) error
	Conversation(id string, principal domain.Principal, lock bool) (*domain.Conversation, error)
	SaveConversation(*domain.Conversation) error
	ListConversations(principal domain.Principal, kind string, offset, limit int) ([]domain.Conversation, int64, error)
	ConversationByOrganization(id, organizationID string) (*domain.Conversation, error)
	ActiveRunCount(conversationID string) (int64, error)

	CreateMessage(*domain.Message) error
	Message(id string, principal domain.Principal, lock bool) (*domain.Message, error)
	MessageByIdempotency(key string, principal domain.Principal) (*domain.Message, error)
	SaveMessage(*domain.Message) error
	ListMessages(conversationID string, principal domain.Principal, offset, limit int) ([]domain.Message, int64, error)
	ListMessagesByOrganization(conversationID, organizationID string, offset, limit int) ([]domain.Message, int64, error)
	MessageAuditStats(conversationID, organizationID string) (messageCount, questionCount int64, lastQuestionAt *time.Time, err error)

	CreateArtifact(*domain.Artifact) error
	Artifact(id string, principal domain.Principal, lock bool) (*domain.Artifact, error)
	SaveArtifact(*domain.Artifact) error

	CreatePanel(*domain.PanelSession) error
	Panel(id string, principal domain.Principal, lock bool) (*domain.PanelSession, error)
	PanelInternal(id string, lock bool) (*domain.PanelSession, error)
	SavePanel(*domain.PanelSession) error
	ListConversationPanels(conversationID, organizationID string) ([]domain.PanelSession, error)
	CreateContext(*domain.ContextSnapshot) error
	Context(panelID string, version uint64) (*domain.ContextSnapshot, error)

	SupersedeRegistrations(panelID string, revision uint64, now time.Time) error
	CreateRegistrations([]domain.Registration) error
	Registration(id string, lock bool) (*domain.Registration, error)
	SaveRegistration(*domain.Registration) error
	ListRegistrations(panelID string, revision uint64) ([]domain.Registration, error)

	CreateRun(*domain.Run) error
	Run(id string, principal domain.Principal, lock bool) (*domain.Run, error)
	RunInternal(id string, lock bool) (*domain.Run, error)
	RunByIdempotency(key string, principal domain.Principal) (*domain.Run, error)
	SaveRun(*domain.Run) error
	ListRuns(conversationID string, principal domain.Principal, offset, limit int) ([]domain.Run, int64, error)
	ClaimRun(instanceID string, now, expires time.Time) (*domain.Run, error)
	InterruptExpiredClaims(now time.Time) (int64, error)
	Maintain(now, eventCutoff time.Time) error

	CreateModelCall(*domain.ModelCall) error
	ModelCall(runID string, sequence int, lock bool) (*domain.ModelCall, error)
	SaveModelCall(*domain.ModelCall) error
	CreateToolCall(*domain.ToolCall) error
	ToolCall(id string, lock bool) (*domain.ToolCall, error)
	ActiveToolCall(runID string, lock bool) (*domain.ToolCall, error)
	RunToolCallCount(runID string) (int64, error)
	ListRunToolCalls(runID string) ([]domain.ToolCall, error)
	SaveToolCall(*domain.ToolCall) error
	LatestToolResult(toolCallID string) (*domain.ToolResult, error)
	CreateToolResult(*domain.ToolResult) error

	CreateApproval(*domain.Approval) error
	Approval(id string, principal domain.Principal, lock bool) (*domain.Approval, error)
	ApprovalInternal(id string, lock bool) (*domain.Approval, error)
	ApprovalByToolCall(toolCallID string, lock bool) (*domain.Approval, error)
	PendingApprovalForRun(runID string, lock bool) (*domain.Approval, error)
	RememberedApproval(panelID, registrationID, definitionVersion, argumentsDigest string) (bool, error)
	SaveApproval(*domain.Approval) error
	ListApprovals(conversationID string, principal domain.Principal, offset, limit int) ([]domain.Approval, int64, error)

	CreateEvent(*domain.DomainEvent) error
	CreateDelivery(*domain.PanelDelivery) error
	ListDeliveries(panelID string, after uint64, limit int, cutoff time.Time) ([]domain.PanelDelivery, bool, error)
	CreateAudit(*domain.AuditRecord) error
	ListAudit(organizationID string, offset, limit int) ([]domain.AuditRecord, int64, error)
	Stats(organizationID string, since time.Time) (map[string]int64, error)
	RuntimeMetrics(since time.Time) (map[string]int64, error)
}
