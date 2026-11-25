package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/event"
	"github.com/jumpserver/koko/internal/kael/ports"
)

var errClosed = errors.New("store is closed")

type Memory struct {
	mu          sync.RWMutex
	state       *memoryState
	persistence statePersistence
	closed      bool
}

type memoryState struct {
	conversations     map[string]domain.Conversation
	messages          map[string]domain.Message
	artifacts         map[string]domain.Artifact
	panels            map[string]domain.PanelSession
	contexts          map[string]domain.ContextSnapshot
	registrations     map[string]domain.Registration
	runs              map[string]domain.Run
	modelCalls        map[string]domain.ModelCall
	toolCalls         map[string]domain.ToolCall
	toolResults       map[string]domain.ToolResult
	approvals         map[string]domain.Approval
	events            map[string]domain.DomainEvent
	eventHighWater    map[string]uint64
	deliveries        map[string]domain.PanelDelivery
	deliveryHighWater map[string]uint64
	audits            map[string]domain.AuditRecord
}

type memoryTx struct{ state *memoryState }

type readinessPersistence interface {
	Ready(context.Context) error
}

type metricsPersistence interface {
	RuntimeMetrics() map[string]int64
}

func NewMemory() *Memory { return &Memory{state: newMemoryState()} }

func newMemoryState() *memoryState {
	return &memoryState{
		conversations:     map[string]domain.Conversation{},
		messages:          map[string]domain.Message{},
		artifacts:         map[string]domain.Artifact{},
		panels:            map[string]domain.PanelSession{},
		contexts:          map[string]domain.ContextSnapshot{},
		registrations:     map[string]domain.Registration{},
		runs:              map[string]domain.Run{},
		modelCalls:        map[string]domain.ModelCall{},
		toolCalls:         map[string]domain.ToolCall{},
		toolResults:       map[string]domain.ToolResult{},
		approvals:         map[string]domain.Approval{},
		events:            map[string]domain.DomainEvent{},
		eventHighWater:    map[string]uint64{},
		deliveries:        map[string]domain.PanelDelivery{},
		deliveryHighWater: map[string]uint64{},
		audits:            map[string]domain.AuditRecord{},
	}
}

func (s *memoryState) clone() *memoryState {
	return &memoryState{
		conversations:     cloneMap(s.conversations),
		messages:          cloneMap(s.messages),
		artifacts:         cloneMap(s.artifacts),
		panels:            cloneMap(s.panels),
		contexts:          cloneMap(s.contexts),
		registrations:     cloneMap(s.registrations),
		runs:              cloneMap(s.runs),
		modelCalls:        cloneMap(s.modelCalls),
		toolCalls:         cloneMap(s.toolCalls),
		toolResults:       cloneMap(s.toolResults),
		approvals:         cloneMap(s.approvals),
		events:            cloneMap(s.events),
		eventHighWater:    cloneMap(s.eventHighWater),
		deliveries:        cloneMap(s.deliveries),
		deliveryHighWater: cloneMap(s.deliveryHighWater),
		audits:            cloneMap(s.audits),
	}
}

func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	result := make(map[K]V, len(source))
	for key, value := range source {
		result[key] = cloneStoredValue(value)
	}
	return result
}

func cloneValue[T any](value T) *T {
	result := cloneStoredValue(value)
	return &result
}

func cloneStoredValue[T any](value T) T {
	var cloned any = value
	switch item := any(value).(type) {
	case domain.Conversation:
		item.Metadata = cloneRawMessage(item.Metadata)
		item.ArchivedAt = cloneTime(item.ArchivedAt)
		item.DeletedAt = cloneTime(item.DeletedAt)
		cloned = item
	case domain.Message:
		item.Parts = cloneRawMessage(item.Parts)
		item.ResultCards = cloneRawMessage(item.ResultCards)
		item.Failure = cloneRawMessage(item.Failure)
		cloned = item
	case domain.Artifact:
		item.DeletedAt = cloneTime(item.DeletedAt)
		cloned = item
	case domain.PanelSession:
		item.ClosedAt = cloneTime(item.ClosedAt)
		cloned = item
	case domain.ContextSnapshot:
		item.Data = cloneRawMessage(item.Data)
		cloned = item
	case domain.Registration:
		item.InputSchema = cloneRawMessage(item.InputSchema)
		item.OutputSchema = cloneRawMessage(item.OutputSchema)
		item.AnnotationsJSON = cloneRawMessage(item.AnnotationsJSON)
		cloned = item
	case domain.Run:
		item.Failure = cloneRawMessage(item.Failure)
		item.AuthorizationPermissions = cloneStrings(item.AuthorizationPermissions)
		item.RegistrationSnapshot = cloneRawMessage(item.RegistrationSnapshot)
		item.ModelPolicy = cloneRawMessage(item.ModelPolicy)
		item.ApprovalPolicy = cloneRawMessage(item.ApprovalPolicy)
		item.ClaimExpiresAt = cloneTime(item.ClaimExpiresAt)
		item.StartedAt = cloneTime(item.StartedAt)
		item.FinishedAt = cloneTime(item.FinishedAt)
		cloned = item
	case domain.ModelCall:
		item.FinishedAt = cloneTime(item.FinishedAt)
		cloned = item
	case domain.ToolCall:
		item.Arguments = cloneRawMessage(item.Arguments)
		item.FinishedAt = cloneTime(item.FinishedAt)
		cloned = item
	case domain.ToolResult:
		item.Result = cloneRawMessage(item.Result)
		item.ErrorJSON = cloneRawMessage(item.ErrorJSON)
		cloned = item
	case domain.Approval:
		item.Preview = cloneRawMessage(item.Preview)
		item.ResolvedAt = cloneTime(item.ResolvedAt)
		cloned = item
	case domain.DomainEvent:
		item.Payload = cloneRawMessage(item.Payload)
		item.ProjectedAt = cloneTime(item.ProjectedAt)
		cloned = item
	case domain.PanelDelivery:
		item.Payload = cloneRawMessage(item.Payload)
		cloned = item
	case domain.AuditRecord:
		item.Summary = cloneRawMessage(item.Summary)
		cloned = item
	}
	return cloned.(T)
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func cloneStrings(value []string) []string {
	if value == nil {
		return nil
	}
	return append([]string(nil), value...)
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (s *Memory) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return errClosed
	}
	persistence, ok := s.persistence.(readinessPersistence)
	s.mu.RUnlock()
	if ok {
		return persistence.Ready(ctx)
	}
	return ctx.Err()
}

func (s *Memory) PersistenceMetrics(ctx context.Context) (map[string]int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, errClosed
	}
	persistence, ok := s.persistence.(metricsPersistence)
	s.mu.RUnlock()
	result := map[string]int64{
		"runtime_store_snapshot_disabled":      0,
		"runtime_store_revision":               0,
		"runtime_store_records_since_snapshot": 0,
	}
	if ok {
		for key, value := range persistence.RuntimeMetrics() {
			result[key] = value
		}
	}
	return result, nil
}

func (s *Memory) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.persistence != nil {
		return s.persistence.Close()
	}
	return nil
}

func (s *Memory) Transaction(ctx context.Context, fn func(ports.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return errClosed
	}
	next := s.state.clone()
	if err := fn(&memoryTx{state: next}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.persistence != nil {
		if err := s.persistence.Commit(s.state, next); err != nil {
			return err
		}
	}
	s.state = next
	return nil
}

func (s *Memory) View(ctx context.Context, fn func(ports.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return errClosed
	}
	snapshot := s.state.clone()
	s.mu.RUnlock()
	return fn(&memoryTx{state: snapshot})
}

func create[T any](values map[string]T, id string, value T) error {
	if _, exists := values[id]; exists {
		return ports.ErrConflict
	}
	values[id] = cloneStoredValue(value)
	return nil
}

func one[T any](values map[string]T, id string) (*T, error) {
	value, exists := values[id]
	if !exists {
		return nil, ports.ErrNotFound
	}
	return cloneValue(value), nil
}

func page[T any](values []T, offset, limit int) []T {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(values) {
		return []T{}
	}
	end := len(values)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	result := make([]T, end-offset)
	for index, value := range values[offset:end] {
		result[index] = cloneStoredValue(value)
	}
	return result
}

func owns(principal domain.Principal, subjectID, organizationID string) bool {
	return principal.SubjectID == subjectID && principal.OrganizationID == organizationID
}

func activeRun(state string) bool {
	switch state {
	case "queued", "running", "waiting_capability", "waiting_approval", "cancelling":
		return true
	default:
		return false
	}
}

func (t *memoryTx) CreateConversation(value *domain.Conversation) error {
	return create(t.state.conversations, value.ID, *value)
}

func (t *memoryTx) Conversation(id string, principal domain.Principal, _ bool) (*domain.Conversation, error) {
	value, err := one(t.state.conversations, id)
	if err != nil || !owns(principal, value.SubjectID, value.OrganizationID) || value.DeletedAt != nil {
		return nil, ports.ErrNotFound
	}
	return value, nil
}

func (t *memoryTx) SaveConversation(value *domain.Conversation) error {
	if _, exists := t.state.conversations[value.ID]; !exists {
		return ports.ErrNotFound
	}
	t.state.conversations[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) ListConversations(principal domain.Principal, kind string, offset, limit int) ([]domain.Conversation, int64, error) {
	values := make([]domain.Conversation, 0)
	for _, value := range t.state.conversations {
		if owns(principal, value.SubjectID, value.OrganizationID) && value.DeletedAt == nil && (kind == "" || value.Kind == kind) {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].UpdatedAt.Equal(values[j].UpdatedAt) {
			return values[i].ID > values[j].ID
		}
		return values[i].UpdatedAt.After(values[j].UpdatedAt)
	})
	return page(values, offset, limit), int64(len(values)), nil
}

func (t *memoryTx) ConversationByOrganization(id, organizationID string) (*domain.Conversation, error) {
	value, err := one(t.state.conversations, id)
	if err != nil || value.OrganizationID != organizationID {
		return nil, ports.ErrNotFound
	}
	return value, nil
}

func (t *memoryTx) ActiveRunCount(conversationID string) (int64, error) {
	var count int64
	for _, value := range t.state.runs {
		if value.ConversationID == conversationID && activeRun(value.State) {
			count++
		}
	}
	return count, nil
}

func (t *memoryTx) CreateMessage(value *domain.Message) error {
	for _, existing := range t.state.messages {
		if value.IdempotencyKey != "" && existing.IdempotencyKey == value.IdempotencyKey && owns(domain.Principal{SubjectID: value.SubjectID, OrganizationID: value.OrganizationID}, existing.SubjectID, existing.OrganizationID) {
			return ports.ErrConflict
		}
	}
	return create(t.state.messages, value.ID, *value)
}

func (t *memoryTx) Message(id string, principal domain.Principal, _ bool) (*domain.Message, error) {
	value, err := one(t.state.messages, id)
	if err != nil || !owns(principal, value.SubjectID, value.OrganizationID) {
		return nil, ports.ErrNotFound
	}
	return value, nil
}

func (t *memoryTx) MessageByIdempotency(key string, principal domain.Principal) (*domain.Message, error) {
	for _, value := range t.state.messages {
		if value.IdempotencyKey == key && owns(principal, value.SubjectID, value.OrganizationID) {
			return cloneValue(value), nil
		}
	}
	return nil, ports.ErrNotFound
}

func (t *memoryTx) SaveMessage(value *domain.Message) error {
	if _, exists := t.state.messages[value.ID]; !exists {
		return ports.ErrNotFound
	}
	t.state.messages[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) ListMessages(conversationID string, principal domain.Principal, offset, limit int) ([]domain.Message, int64, error) {
	values := make([]domain.Message, 0)
	for _, value := range t.state.messages {
		if value.ConversationID == conversationID && owns(principal, value.SubjectID, value.OrganizationID) {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return page(values, offset, limit), int64(len(values)), nil
}

func (t *memoryTx) ListMessagesByOrganization(conversationID, organizationID string, offset, limit int) ([]domain.Message, int64, error) {
	values := make([]domain.Message, 0)
	for _, value := range t.state.messages {
		if value.ConversationID == conversationID && value.OrganizationID == organizationID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return page(values, offset, limit), int64(len(values)), nil
}

func (t *memoryTx) MessageAuditStats(conversationID, organizationID string) (int64, int64, *time.Time, error) {
	var messageCount, questionCount int64
	var lastQuestionAt *time.Time
	for _, value := range t.state.messages {
		if value.ConversationID != conversationID || value.OrganizationID != organizationID {
			continue
		}
		messageCount++
		if value.Role == "user" {
			questionCount++
			if lastQuestionAt == nil || value.CreatedAt.After(*lastQuestionAt) {
				stamp := value.CreatedAt
				lastQuestionAt = &stamp
			}
		}
	}
	return messageCount, questionCount, lastQuestionAt, nil
}

func (t *memoryTx) CreateArtifact(value *domain.Artifact) error {
	return create(t.state.artifacts, value.ID, *value)
}

func (t *memoryTx) Artifact(id string, principal domain.Principal, _ bool) (*domain.Artifact, error) {
	value, err := one(t.state.artifacts, id)
	if err != nil || !owns(principal, value.SubjectID, value.OrganizationID) || value.DeletedAt != nil {
		return nil, ports.ErrNotFound
	}
	return value, nil
}

func (t *memoryTx) SaveArtifact(value *domain.Artifact) error {
	if _, exists := t.state.artifacts[value.ID]; !exists {
		return ports.ErrNotFound
	}
	t.state.artifacts[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) CreatePanel(value *domain.PanelSession) error {
	return create(t.state.panels, value.ID, *value)
}

func (t *memoryTx) Panel(id string, principal domain.Principal, _ bool) (*domain.PanelSession, error) {
	value, err := one(t.state.panels, id)
	if err != nil || !owns(principal, value.SubjectID, value.OrganizationID) {
		return nil, ports.ErrNotFound
	}
	return value, nil
}

func (t *memoryTx) PanelInternal(id string, _ bool) (*domain.PanelSession, error) {
	return one(t.state.panels, id)
}

func (t *memoryTx) SavePanel(value *domain.PanelSession) error {
	if _, exists := t.state.panels[value.ID]; !exists {
		return ports.ErrNotFound
	}
	t.state.panels[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) ListConversationPanels(conversationID, organizationID string) ([]domain.PanelSession, error) {
	values := make([]domain.PanelSession, 0)
	for _, value := range t.state.panels {
		if value.ConversationID == conversationID && value.OrganizationID == organizationID && (value.State == "active" || value.State == "disconnected") {
			values = append(values, cloneStoredValue(value))
		}
	}
	return values, nil
}

func contextKey(panelID string, version uint64) string {
	return panelID + "\x00" + strconv.FormatUint(version, 10)
}

func (t *memoryTx) CreateContext(value *domain.ContextSnapshot) error {
	return create(t.state.contexts, contextKey(value.PanelSessionID, value.Version), *value)
}

func (t *memoryTx) Context(panelID string, version uint64) (*domain.ContextSnapshot, error) {
	return one(t.state.contexts, contextKey(panelID, version))
}

func (t *memoryTx) SupersedeRegistrations(panelID string, revision uint64, now time.Time) error {
	for id, value := range t.state.registrations {
		if value.PanelSessionID == panelID && value.RegistryRevision <= revision && value.State == "active" {
			value.State, value.UpdatedAt = "superseded", now
			t.state.registrations[id] = cloneStoredValue(value)
		}
	}
	return nil
}

func (t *memoryTx) CreateRegistrations(values []domain.Registration) error {
	for _, value := range values {
		if _, exists := t.state.registrations[value.ID]; exists {
			return ports.ErrConflict
		}
		for _, existing := range t.state.registrations {
			if existing.PanelSessionID == value.PanelSessionID && existing.ClientKey == value.ClientKey && existing.RegistryRevision == value.RegistryRevision {
				return ports.ErrConflict
			}
		}
	}
	for _, value := range values {
		t.state.registrations[value.ID] = cloneStoredValue(value)
	}
	return nil
}

func (t *memoryTx) Registration(id string, _ bool) (*domain.Registration, error) {
	return one(t.state.registrations, id)
}

func (t *memoryTx) SaveRegistration(value *domain.Registration) error {
	if _, exists := t.state.registrations[value.ID]; !exists {
		return ports.ErrNotFound
	}
	t.state.registrations[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) ListRegistrations(panelID string, revision uint64) ([]domain.Registration, error) {
	values := make([]domain.Registration, 0)
	for _, value := range t.state.registrations {
		if value.PanelSessionID == panelID && (revision == 0 || value.RegistryRevision == revision) {
			values = append(values, cloneStoredValue(value))
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Name == values[j].Name {
			return values[i].ID < values[j].ID
		}
		return values[i].Name < values[j].Name
	})
	return values, nil
}

func (t *memoryTx) CreateRun(value *domain.Run) error {
	for _, existing := range t.state.runs {
		if value.IdempotencyKey != "" && existing.IdempotencyKey == value.IdempotencyKey && existing.SubjectID == value.SubjectID && existing.OrganizationID == value.OrganizationID {
			return ports.ErrConflict
		}
	}
	return create(t.state.runs, value.ID, *value)
}

func (t *memoryTx) Run(id string, principal domain.Principal, _ bool) (*domain.Run, error) {
	value, err := one(t.state.runs, id)
	if err != nil || !owns(principal, value.SubjectID, value.OrganizationID) {
		return nil, ports.ErrNotFound
	}
	return value, nil
}

func (t *memoryTx) RunInternal(id string, _ bool) (*domain.Run, error) {
	return one(t.state.runs, id)
}

func (t *memoryTx) RunByIdempotency(key string, principal domain.Principal) (*domain.Run, error) {
	for _, value := range t.state.runs {
		if value.IdempotencyKey == key && owns(principal, value.SubjectID, value.OrganizationID) {
			return cloneValue(value), nil
		}
	}
	return nil, ports.ErrNotFound
}

func (t *memoryTx) SaveRun(value *domain.Run) error {
	if _, exists := t.state.runs[value.ID]; !exists {
		return ports.ErrNotFound
	}
	t.state.runs[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) ListRuns(conversationID string, principal domain.Principal, offset, limit int) ([]domain.Run, int64, error) {
	values := make([]domain.Run, 0)
	for _, value := range t.state.runs {
		if value.ConversationID == conversationID && owns(principal, value.SubjectID, value.OrganizationID) {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID > values[j].ID
		}
		return values[i].CreatedAt.After(values[j].CreatedAt)
	})
	return page(values, offset, limit), int64(len(values)), nil
}

func (t *memoryTx) ClaimRun(instanceID string, now, expires time.Time) (*domain.Run, error) {
	candidates := make([]domain.Run, 0)
	for _, value := range t.state.runs {
		if value.State != "queued" || value.ClaimExpiresAt != nil && !value.ClaimExpiresAt.Before(now) {
			continue
		}
		blocked := false
		for _, active := range t.state.runs {
			if active.ConversationID == value.ConversationID && active.ID != value.ID && activeRun(active.State) && active.State != "queued" {
				blocked = true
				break
			}
		}
		if !blocked {
			candidates = append(candidates, value)
		}
	}
	if len(candidates) == 0 {
		return nil, ports.ErrNotFound
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})
	value := candidates[0]
	value.ClaimOwner, value.ClaimExpiresAt, value.State, value.UpdatedAt = instanceID, &expires, "running", now
	if value.StartedAt == nil {
		started := now
		value.StartedAt = &started
	}
	t.state.runs[value.ID] = cloneStoredValue(value)
	return cloneValue(value), nil
}

func (t *memoryTx) InterruptExpiredClaims(now time.Time) (int64, error) {
	var count int64
	for id, value := range t.state.runs {
		if value.ClaimExpiresAt != nil && value.ClaimExpiresAt.Before(now) && (value.State == "running" || value.State == "waiting_capability" || value.State == "waiting_approval" || value.State == "cancelling") {
			value.Failure = t.interruptedFailure(value, "worker_interrupted")
			value.State, value.ErrorCode, value.ErrorDetail = "interrupted", "worker_interrupted", "run worker stopped before completion"
			value.ClaimOwner, value.ClaimExpiresAt, value.UpdatedAt = "", nil, now
			t.state.runs[id] = cloneStoredValue(value)
			if message, exists := t.state.messages[value.OutputMessageID]; exists && message.Status != "completed" {
				message.Status, message.ErrorCode, message.ErrorDetail, message.UpdatedAt = "cancelled", value.ErrorCode, value.ErrorDetail, now
				message.Failure = cloneRawMessage(value.Failure)
				t.state.messages[message.ID] = cloneStoredValue(message)
			}
			count++
		}
	}
	return count, nil
}

func (t *memoryTx) interruptedFailure(run domain.Run, code string) json.RawMessage {
	calls, _ := t.ListRunToolCalls(run.ID)
	results := make(map[string]domain.ToolResult, len(calls))
	approvals := make(map[string]domain.Approval)
	for _, call := range calls {
		if approval, err := t.ApprovalByToolCall(call.ID, false); err == nil {
			approvals[call.ID] = *approval
		}
		if result, err := t.LatestToolResult(call.ID); err == nil {
			results[call.ID] = *result
		}
	}
	failure := domain.DescribeRunFailure(calls, results, approvals, "interrupted", code, "continue")
	// A previous explicit cancellation may have happened before a process
	// stopped. Retain its pre-cancellation evidence rather than reclassifying
	// a never-dispatched approval as an uncertain write.
	if len(run.Failure) > 0 {
		var previous domain.RunFailure
		if json.Unmarshal(run.Failure, &previous) == nil && previous.Code == "cancelled" {
			failure.CompletedSteps, failure.UncertainSteps, failure.NextAction = previous.CompletedSteps, previous.UncertainSteps, previous.NextAction
		}
	}
	encoded, _ := json.Marshal(failure)
	return encoded
}

func (t *memoryTx) Maintain(now, eventCutoff time.Time) error {
	if _, err := t.InterruptExpiredClaims(now); err != nil {
		return err
	}
	for _, value := range t.state.approvals {
		if value.State == "pending" && value.ExpiresAt.Before(now) {
			if err := event.ExpireApproval(t, &value, now); err != nil {
				return err
			}
		}
	}
	for id, value := range t.state.registrations {
		if value.State == "active" && value.LeaseExpiresAt.Before(now) {
			value.State, value.UpdatedAt = "expired", now
			t.state.registrations[id] = cloneStoredValue(value)
		}
	}
	for id, value := range t.state.panels {
		if (value.State == "active" || value.State == "disconnected") && value.LeaseExpiresAt.Before(now) {
			value.State, value.ConnectionOwner, value.UpdatedAt = "expired", "", now
			t.state.panels[id] = cloneStoredValue(value)
		}
	}
	for id, value := range t.state.deliveries {
		if value.CreatedAt.Before(eventCutoff) {
			delete(t.state.deliveries, id)
		}
	}
	return nil
}

func (t *memoryTx) CreateModelCall(value *domain.ModelCall) error {
	for _, existing := range t.state.modelCalls {
		if existing.ID == value.ID || existing.RunID == value.RunID && existing.Sequence == value.Sequence {
			return ports.ErrConflict
		}
	}
	t.state.modelCalls[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) ModelCall(runID string, sequence int, _ bool) (*domain.ModelCall, error) {
	for _, value := range t.state.modelCalls {
		if value.RunID == runID && value.Sequence == sequence {
			return cloneValue(value), nil
		}
	}
	return nil, ports.ErrNotFound
}

func (t *memoryTx) SaveModelCall(value *domain.ModelCall) error {
	if _, exists := t.state.modelCalls[value.ID]; !exists {
		return ports.ErrNotFound
	}
	t.state.modelCalls[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) CreateToolCall(value *domain.ToolCall) error {
	for _, existing := range t.state.toolCalls {
		if existing.ID == value.ID || existing.InvocationID == value.InvocationID || existing.InvocationSequence == value.InvocationSequence {
			return ports.ErrConflict
		}
	}
	t.state.toolCalls[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) ToolCall(id string, _ bool) (*domain.ToolCall, error) {
	return one(t.state.toolCalls, id)
}

func (t *memoryTx) ActiveToolCall(runID string, _ bool) (*domain.ToolCall, error) {
	var selected *domain.ToolCall
	for _, value := range t.state.toolCalls {
		if value.RunID != runID || value.State != "created" && value.State != "waiting_approval" && value.State != "dispatched" && value.State != "running" {
			continue
		}
		if selected == nil || value.CreatedAt.After(selected.CreatedAt) {
			selected = cloneValue(value)
		}
	}
	if selected == nil {
		return nil, ports.ErrNotFound
	}
	return selected, nil
}

func (t *memoryTx) RunToolCallCount(runID string) (int64, error) {
	var count int64
	for _, value := range t.state.toolCalls {
		if value.RunID == runID {
			count++
		}
	}
	return count, nil
}

func (t *memoryTx) ListRunToolCalls(runID string) ([]domain.ToolCall, error) {
	result := []domain.ToolCall{}
	for _, value := range t.state.toolCalls {
		if value.RunID == runID {
			result = append(result, cloneStoredValue(value))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

func (t *memoryTx) SaveToolCall(value *domain.ToolCall) error {
	if _, exists := t.state.toolCalls[value.ID]; !exists {
		return ports.ErrNotFound
	}
	t.state.toolCalls[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) LatestToolResult(toolCallID string) (*domain.ToolResult, error) {
	var selected *domain.ToolResult
	for _, value := range t.state.toolResults {
		if value.ToolCallID == toolCallID && (selected == nil || value.Sequence > selected.Sequence) {
			selected = cloneValue(value)
		}
	}
	if selected == nil {
		return nil, ports.ErrNotFound
	}
	return selected, nil
}

func (t *memoryTx) CreateToolResult(value *domain.ToolResult) error {
	for _, existing := range t.state.toolResults {
		if existing.ID == value.ID || existing.ToolCallID == value.ToolCallID && existing.Sequence == value.Sequence {
			return ports.ErrConflict
		}
	}
	t.state.toolResults[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) CreateApproval(value *domain.Approval) error {
	for _, existing := range t.state.approvals {
		if existing.ID == value.ID || existing.ToolCallID == value.ToolCallID {
			return ports.ErrConflict
		}
	}
	t.state.approvals[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) Approval(id string, principal domain.Principal, _ bool) (*domain.Approval, error) {
	value, err := one(t.state.approvals, id)
	if err != nil || !owns(principal, value.SubjectID, value.OrganizationID) {
		return nil, ports.ErrNotFound
	}
	return value, nil
}

func (t *memoryTx) ApprovalInternal(id string, _ bool) (*domain.Approval, error) {
	return one(t.state.approvals, id)
}

func (t *memoryTx) ApprovalByToolCall(toolCallID string, _ bool) (*domain.Approval, error) {
	for _, value := range t.state.approvals {
		if value.ToolCallID == toolCallID {
			return cloneValue(value), nil
		}
	}
	return nil, ports.ErrNotFound
}

func (t *memoryTx) PendingApprovalForRun(runID string, _ bool) (*domain.Approval, error) {
	var selected *domain.Approval
	for _, value := range t.state.approvals {
		if value.RunID == runID && value.State == "pending" && (selected == nil || value.CreatedAt.After(selected.CreatedAt)) {
			selected = cloneValue(value)
		}
	}
	if selected == nil {
		return nil, ports.ErrNotFound
	}
	return selected, nil
}

func (t *memoryTx) RememberedApproval(panelID, registrationID, definitionVersion, argumentsDigest string) (bool, error) {
	for _, value := range t.state.approvals {
		if value.PanelSessionID == panelID && value.RegistrationID == registrationID &&
			value.DefinitionVersion == definitionVersion && value.ArgumentsDigest == argumentsDigest &&
			value.State == "approved" && value.Remembered {
			return true, nil
		}
	}
	return false, nil
}

func (t *memoryTx) SaveApproval(value *domain.Approval) error {
	if _, exists := t.state.approvals[value.ID]; !exists {
		return ports.ErrNotFound
	}
	t.state.approvals[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) ListApprovals(conversationID string, principal domain.Principal, offset, limit int) ([]domain.Approval, int64, error) {
	values := make([]domain.Approval, 0)
	for _, value := range t.state.approvals {
		if value.ConversationID == conversationID && owns(principal, value.SubjectID, value.OrganizationID) {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID > values[j].ID
		}
		return values[i].CreatedAt.After(values[j].CreatedAt)
	})
	return page(values, offset, limit), int64(len(values)), nil
}

func (t *memoryTx) CreateEvent(value *domain.DomainEvent) error {
	if _, exists := t.state.events[value.ID]; exists {
		return ports.ErrConflict
	}
	if value.ConversationID != "" {
		highWater := t.state.eventHighWater[value.ConversationID]
		if value.Sequence == 0 {
			value.Sequence = highWater + 1
		} else if value.Sequence <= highWater {
			return ports.ErrConflict
		}
		t.state.eventHighWater[value.ConversationID] = value.Sequence
	}
	t.state.events[value.ID] = cloneStoredValue(*value)
	return nil
}

func (t *memoryTx) CreateDelivery(value *domain.PanelDelivery) error {
	for _, existing := range t.state.deliveries {
		if existing.ID == value.ID || existing.PanelSessionID == value.PanelSessionID && (existing.Sequence == value.Sequence || existing.EventID == value.EventID) {
			return ports.ErrConflict
		}
	}
	t.state.deliveries[value.ID] = cloneStoredValue(*value)
	if value.Sequence > t.state.deliveryHighWater[value.PanelSessionID] {
		t.state.deliveryHighWater[value.PanelSessionID] = value.Sequence
	}
	return nil
}

func (t *memoryTx) ListDeliveries(panelID string, after uint64, limit int, cutoff time.Time) ([]domain.PanelDelivery, bool, error) {
	values := make([]domain.PanelDelivery, 0)
	var oldest uint64
	for _, value := range t.state.deliveries {
		if value.PanelSessionID != panelID || value.CreatedAt.Before(cutoff) {
			continue
		}
		if oldest == 0 || value.Sequence < oldest {
			oldest = value.Sequence
		}
		if value.Sequence > after {
			values = append(values, value)
		}
	}
	if after > 0 && (oldest > 0 && after+1 < oldest || oldest == 0 && t.state.deliveryHighWater[panelID] > after) {
		return nil, true, nil
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Sequence < values[j].Sequence })
	return page(values, 0, limit), false, nil
}

func (t *memoryTx) CreateAudit(value *domain.AuditRecord) error {
	return create(t.state.audits, value.ID, *value)
}

func (t *memoryTx) ListAudit(organizationID string, offset, limit int) ([]domain.AuditRecord, int64, error) {
	values := make([]domain.AuditRecord, 0)
	for _, value := range t.state.audits {
		if value.OrganizationID == organizationID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].CreatedAt.After(values[j].CreatedAt) })
	return page(values, offset, limit), int64(len(values)), nil
}

func (t *memoryTx) Stats(organizationID string, since time.Time) (map[string]int64, error) {
	result := map[string]int64{"conversations": 0, "runs": 0, "runs_queued": 0, "runs_running": 0, "runs_waiting_approval": 0, "runs_completed": 0, "runs_failed": 0, "runs_cancelled": 0, "tool_calls": 0, "core_api_calls": 0, "approvals": 0, "input_tokens": 0, "output_tokens": 0, "model_calls": 0, "model_duration_ms": 0}
	for _, value := range t.state.conversations {
		if value.OrganizationID == organizationID && !value.CreatedAt.Before(since) {
			result["conversations"]++
		}
	}
	for _, value := range t.state.runs {
		if value.OrganizationID == organizationID && !value.CreatedAt.Before(since) {
			result["runs"]++
			switch value.State {
			case "queued":
				result["runs_queued"]++
			case "running", "waiting_capability", "cancelling":
				result["runs_running"]++
			case "waiting_approval":
				result["runs_waiting_approval"]++
			case "completed":
				result["runs_completed"]++
			case "failed", "interrupted":
				result["runs_failed"]++
			case "cancelled":
				result["runs_cancelled"]++
			}
			result["input_tokens"] += value.InputTokens
			result["output_tokens"] += value.OutputTokens
		}
	}
	for _, value := range t.state.toolCalls {
		if value.OrganizationID == organizationID && !value.CreatedAt.Before(since) {
			result["tool_calls"]++
			if value.ToolName == "call_core_api" && (value.State == "succeeded" || value.State == "failed") {
				var arguments struct {
					OperationID string `json:"operation_id"`
				}
				if json.Unmarshal(value.Arguments, &arguments) == nil {
					if operationID := strings.TrimSpace(arguments.OperationID); operationID != "" {
						result["core_api_calls"]++
						result["operation_count:"+operationID]++
					}
				}
			}
		}
	}
	for _, value := range t.state.approvals {
		if value.OrganizationID == organizationID && !value.CreatedAt.Before(since) {
			result["approvals"]++
		}
	}
	for _, value := range t.state.modelCalls {
		run, exists := t.state.runs[value.RunID]
		if exists && run.OrganizationID == organizationID && !value.CreatedAt.Before(since) {
			result["model_calls"]++
			result["model_duration_ms"] += value.DurationMS
		}
	}
	return result, nil
}

func (t *memoryTx) RuntimeMetrics(since time.Time) (map[string]int64, error) {
	result := map[string]int64{}
	for _, state := range []string{"queued", "running", "waiting_capability", "waiting_approval", "completed", "failed", "cancelled", "interrupted"} {
		result["runs_"+state+"_24h"] = 0
	}
	for _, value := range t.state.runs {
		if value.CreatedAt.Before(since) {
			continue
		}
		result["runs_"+value.State+"_24h"]++
		result["input_tokens_24h"] += value.InputTokens
		result["output_tokens_24h"] += value.OutputTokens
	}
	for _, value := range t.state.modelCalls {
		if !value.CreatedAt.Before(since) {
			result["model_duration_ms_24h"] += value.DurationMS
		}
	}
	for _, value := range t.state.toolCalls {
		if !value.CreatedAt.Before(since) && (value.State == "failed" || value.State == "expired" || value.State == "rejected") {
			result["tool_failures_24h"]++
		}
	}
	for _, value := range t.state.approvals {
		if value.State == "pending" {
			result["pending_approvals"]++
		}
	}
	return result, nil
}

var _ ports.Store = (*Memory)(nil)
var _ ports.Tx = (*memoryTx)(nil)
