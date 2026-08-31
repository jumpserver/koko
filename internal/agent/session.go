package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jumpserver/koko/internal/agentapi"
	"github.com/jumpserver/koko/internal/agentauth"
	"github.com/jumpserver/koko/internal/agentruntime"
	"github.com/jumpserver/koko/internal/agentruntime/provider"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	maxMessagesPerSession  = 1024
	maxSessionHistoryBytes = 4 * 1024 * 1024
)

var (
	errSessionClosed       = errors.New("agent session is closed")
	errSessionUnavailable  = errors.New("agent session is unavailable")
	errSessionBusy         = errors.New("agent session has queued or active runs")
	errInvalidApprovalMode = errors.New("agent session approval mode is invalid")
	errToolUnavailable     = errors.New("agent tool is unavailable")
)

type sessionCreatedPayload struct {
	SessionID         string                    `json:"session_id"`
	OrganizationID    string                    `json:"org_id"`
	UserID            string                    `json:"user_id"`
	Profile           string                    `json:"profile"`
	Context           agentapi.ContextSnapshot  `json:"context"`
	ResourceSessionID string                    `json:"resource_session_id"`
	Revision          uint64                    `json:"revision"`
	Tools             []agentapi.ToolDefinition `json:"tools"`
	ApprovalMode      string                    `json:"approval_mode"`
	CreatedAt         int64                     `json:"created_at"`
	ContextDigest     string                    `json:"context_digest"`
	ToolsetDigest     string                    `json:"toolset_digest"`
}

type messageCreatedPayload struct {
	Role           string         `json:"role"`
	Text           string         `json:"text"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type runPayload struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type approvalModeChangedPayload struct {
	Previous string `json:"previous"`
	Current  string `json:"current"`
}

type approvalPayload = agentapi.ApprovalEvent

type messageRecord struct {
	messageID string
	runID     string
}

type sessionHistoryItem struct {
	runOrder int
	message  agentruntime.Message
}

type pendingApproval struct {
	runID           string
	toolCallID      string
	digest          string
	toolName        string
	arguments       json.RawMessage
	summary         string
	modelDurationMS int64
	resolved        bool
	approved        bool
	cancelled       bool
	result          chan bool
}

type agentSession struct {
	id            string
	resourceID    string
	principal     agentapi.Principal
	profile       string
	context       agentapi.ContextSnapshot
	revision      uint64
	tools         []agentapi.ToolDefinition
	toolIndex     map[string]agentapi.ToolDefinition
	toolCalls     map[string]agentapi.ToolCall
	toolStartedAt map[string]int64
	approvalMode  string
	createdAt     int64
	contextDigest string
	toolsetDigest string
	events        *eventLog
	results       *toolResultRegistry
	runtime       *modelRuntime

	mu                sync.Mutex
	activeRunID       string
	runState          string
	messages          map[string]messageRecord
	messageOrder      []string
	history           []sessionHistoryItem
	historyBytes      int
	runOrder          map[string]int
	nextRunOrder      int
	pendingRuns       int
	pendingRunIDs     []string
	approvals         map[string]*pendingApproval
	pendingTool       *agentapi.ToolCall
	eventStreamActive bool
	lastActivity      time.Time
	deletePersisted   bool
	closing           bool
	unavailable       bool
	closed            bool
	closeOnce         sync.Once
}

func newAgentSession(
	sessionID string,
	request agentapi.CreateSessionRequest,
	principal agentapi.Principal,
	events *eventLog,
	factory ModelFactory,
) (*agentSession, error) {
	createdAt := time.Now().UnixMilli()
	contextDigest, err := agentauth.HashValue(request.Context)
	if err != nil {
		return nil, err
	}
	toolsetDigest, err := agentauth.HashValue(agentapi.Toolset{Tools: request.Tools})
	if err != nil {
		return nil, err
	}
	session := &agentSession{
		id: sessionID, resourceID: request.ResourceSessionID,
		principal: principal, profile: request.Profile, context: request.Context,
		revision: request.Revision, tools: cloneTools(request.Tools),
		toolIndex: indexTools(request.Tools), toolCalls: make(map[string]agentapi.ToolCall),
		toolStartedAt: make(map[string]int64),
		approvalMode:  normalizeApprovalMode(request.ApprovalMode),
		createdAt:     createdAt, contextDigest: contextDigest, toolsetDigest: toolsetDigest,
		lastActivity: time.Now(),
		events:       events, results: newToolResultRegistry(),
		messages:  make(map[string]messageRecord),
		approvals: make(map[string]*pendingApproval), runOrder: make(map[string]int),
	}
	runtime, err := newModelRuntime(session, factory)
	if err != nil {
		return nil, err
	}
	session.runtime = runtime
	_, err = events.appendJSON(session.event(agentapi.EventSessionCreated, "", "", ""), sessionCreatedPayload{
		SessionID: session.id, OrganizationID: principal.OrganizationID,
		UserID: principal.UserID, Profile: session.profile, Context: session.context,
		ResourceSessionID: session.resourceID, Revision: session.revision,
		Tools: cloneTools(session.tools), ApprovalMode: session.approvalMode,
		CreatedAt: createdAt, ContextDigest: contextDigest, ToolsetDigest: toolsetDigest,
	})
	if err != nil {
		runtime.close()
		return nil, err
	}
	return session, nil
}

func restoreAgentSession(
	events *eventLog,
	loaded []agentapi.Event,
	factory ModelFactory,
) (*agentSession, bool, error) {
	session, deleted, err := restoreAgentSessionState(events, loaded, factory)
	if err == nil {
		return session, deleted, nil
	}
	if errors.Is(err, errRuntimeUnavailable) ||
		errors.Is(err, errEventLogPoisoned) ||
		errors.Is(err, errEventStoreFull) ||
		errors.Is(err, errEventLogClosed) ||
		errors.Is(err, errEventPayloadTooLarge) {
		return nil, false, err
	}
	return nil, false, fmt.Errorf("%w: invalid persisted session state", errEventLogCorrupt)
}

func restoreAgentSessionState(
	events *eventLog,
	loaded []agentapi.Event,
	factory ModelFactory,
) (*agentSession, bool, error) {
	if len(loaded) == 0 || loaded[0].Type != agentapi.EventSessionCreated {
		return nil, false, fmt.Errorf("agent session creation event is missing")
	}
	var created sessionCreatedPayload
	for _, event := range loaded {
		if event.Type == agentapi.EventSessionCreated {
			if err := json.Unmarshal(event.Payload, &created); err != nil {
				return nil, false, err
			}
			break
		}
	}
	if created.SessionID == "" || created.ResourceSessionID == "" {
		return nil, false, fmt.Errorf("agent session creation event is missing")
	}
	if !agentapi.ValidIdentifier(created.SessionID) || created.OrganizationID == "" ||
		created.UserID == "" || !validApprovalMode(created.ApprovalMode) ||
		validatePersistedSessionRequest(agentapi.CreateSessionRequest{
			Profile: created.Profile, Context: created.Context,
			ResourceSessionID: created.ResourceSessionID, Revision: created.Revision,
			Tools: created.Tools, ApprovalMode: created.ApprovalMode,
		}) != nil {
		return nil, false, fmt.Errorf("agent session creation event is invalid")
	}
	contextDigest, digestErr := agentauth.HashValue(created.Context)
	if digestErr != nil || contextDigest != created.ContextDigest {
		return nil, false, fmt.Errorf("agent session context digest is invalid")
	}
	toolsetDigest, digestErr := agentauth.HashValue(agentapi.Toolset{Tools: created.Tools})
	if digestErr != nil || toolsetDigest != created.ToolsetDigest {
		return nil, false, fmt.Errorf("agent session toolset digest is invalid")
	}
	session := &agentSession{
		id: created.SessionID, resourceID: created.ResourceSessionID,
		principal: agentapi.Principal{UserID: created.UserID, OrganizationID: created.OrganizationID},
		profile:   created.Profile, context: created.Context, revision: created.Revision,
		tools: cloneTools(created.Tools), toolIndex: indexTools(created.Tools),
		toolCalls:     make(map[string]agentapi.ToolCall),
		toolStartedAt: make(map[string]int64),
		approvalMode:  normalizeApprovalMode(created.ApprovalMode), createdAt: created.CreatedAt,
		contextDigest: created.ContextDigest, toolsetDigest: created.ToolsetDigest,
		lastActivity: time.Now(),
		events:       events, results: newToolResultRegistry(),
		messages: make(map[string]messageRecord), approvals: make(map[string]*pendingApproval),
		runOrder: make(map[string]int),
	}
	deleted := false
	outstanding := make(map[string]struct{})
	messageRuns := make(map[string]struct{})
	terminalRuns := make(map[string]struct{})
	for index, event := range loaded {
		if event.SessionID != created.SessionID ||
			event.ResourceSessionID != created.ResourceSessionID {
			return nil, false, fmt.Errorf("agent event session binding is invalid")
		}
		switch event.Type {
		case agentapi.EventSessionCreated:
			if index != 0 {
				return nil, false, fmt.Errorf("agent session creation event is duplicated")
			}
		case agentapi.EventSessionClosed:
			deleted = true
		case agentapi.EventSessionApprovalModeChanged:
			var payload approvalModeChangedPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil ||
				!validApprovalMode(payload.Previous) || !validApprovalMode(payload.Current) ||
				session.approvalMode != payload.Previous {
				return nil, false, fmt.Errorf("agent session approval mode event is invalid")
			}
			session.approvalMode = payload.Current
		case agentapi.EventMessageCreated:
			var payload messageCreatedPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil ||
				(payload.Role != "user" && payload.Role != "assistant") ||
				!agentapi.ValidIdentifier(event.RunID) || !agentapi.ValidIdentifier(event.MessageID) ||
				payload.Text == "" || len(payload.Text) > agentapi.MaxMessageBytes ||
				(payload.Role == "user" && !agentapi.ValidIdentifier(payload.IdempotencyKey)) {
				return nil, false, fmt.Errorf("agent message event is invalid")
			}
			metadata, metadataErr := sanitizeMessageMetadata(payload.Metadata)
			if metadataErr != nil {
				return nil, false, metadataErr
			}
			order := session.runOrder[event.RunID]
			if payload.Role == "user" && order == 0 {
				messageRuns[event.RunID] = struct{}{}
				session.nextRunOrder++
				order = session.nextRunOrder
				session.runOrder[event.RunID] = order
			}
			if order > 0 {
				session.appendHistory(order, payload.Role, payload.Text, metadata)
			}
			if payload.IdempotencyKey != "" {
				session.messages[payload.IdempotencyKey] = messageRecord{
					messageID: event.MessageID, runID: event.RunID,
				}
				session.messageOrder = append(session.messageOrder, payload.IdempotencyKey)
			}
		case agentapi.EventRunQueued:
			if err := validateStoredRunEvent(event, agentapi.RunStateQueued); err != nil {
				return nil, false, err
			}
			outstanding[event.RunID] = struct{}{}
		case agentapi.EventRunStarted:
			if err := validateStoredRunEvent(event, agentapi.RunStateRunning); err != nil {
				return nil, false, err
			}
			outstanding[event.RunID] = struct{}{}
			session.activeRunID = event.RunID
			session.runState = agentapi.RunStateRunning
		case agentapi.EventRunCompleted, agentapi.EventRunFailed,
			agentapi.EventRunCancelled, agentapi.EventRunInterrupted:
			if err := validateStoredRunEvent(event, storedRunState(event.Type)); err != nil {
				return nil, false, err
			}
			delete(outstanding, event.RunID)
			terminalRuns[event.RunID] = struct{}{}
			if session.activeRunID == event.RunID {
				session.activeRunID = ""
			}
		case agentapi.EventApprovalNeeded:
			var payload approvalPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil ||
				!agentapi.ValidIdentifier(payload.ApprovalID) || !agentapi.ValidIdentifier(event.RunID) ||
				!agentapi.ValidIdentifier(event.ToolCallID) || payload.Digest == "" ||
				payload.ToolName == "" || len(payload.Arguments) == 0 ||
				len(payload.Summary) > 2048 || !utf8.ValidString(payload.Summary) {
				return nil, false, fmt.Errorf("agent approval request event is invalid")
			}
			if _, ok := session.toolIndex[payload.ToolName]; !ok {
				return nil, false, fmt.Errorf("agent approval request tool is invalid")
			}
			canonical, canonicalErr := agentauth.CanonicalJSON(payload.Arguments)
			if canonicalErr != nil || string(canonical) != string(payload.Arguments) {
				return nil, false, fmt.Errorf("agent approval request arguments are invalid")
			}
			var arguments map[string]any
			if json.Unmarshal(canonical, &arguments) != nil || arguments == nil {
				return nil, false, fmt.Errorf("agent approval request arguments are invalid")
			}
			argsHash, hashErr := agentauth.HashRawJSON(canonical)
			if hashErr != nil {
				return nil, false, fmt.Errorf("agent approval request arguments are invalid")
			}
			expectedDigest, hashErr := agentauth.HashValue(map[string]any{
				"resource_session_id": session.resourceID,
				"run_id":              event.RunID, "tool_call_id": event.ToolCallID,
				"tool_name": payload.ToolName, "args_hash": argsHash,
				"revision": session.revision,
			})
			if hashErr != nil || expectedDigest != payload.Digest {
				return nil, false, fmt.Errorf("agent approval request digest is invalid")
			}
			if _, exists := session.approvals[payload.ApprovalID]; exists {
				return nil, false, fmt.Errorf("agent approval request is duplicated")
			}
			session.approvals[payload.ApprovalID] = &pendingApproval{
				runID: event.RunID, toolCallID: event.ToolCallID,
				digest: payload.Digest, toolName: payload.ToolName,
				arguments: append(json.RawMessage(nil), payload.Arguments...),
				summary:   payload.Summary, modelDurationMS: payload.ModelDurationMS,
				result: make(chan bool, 1),
			}
		case agentapi.EventApproval:
			var payload approvalPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil ||
				payload.Approved == nil || !agentapi.ValidIdentifier(payload.ApprovalID) ||
				!agentapi.ValidIdentifier(event.RunID) || payload.Digest == "" {
				return nil, false, fmt.Errorf("agent approval event is invalid")
			}
			pending := session.approvals[payload.ApprovalID]
			if pending == nil || pending.resolved || pending.digest != payload.Digest ||
				pending.runID != event.RunID || pending.toolCallID != event.ToolCallID ||
				pending.toolName != payload.ToolName ||
				string(pending.arguments) != string(payload.Arguments) {
				return nil, false, fmt.Errorf("agent approval event binding is invalid")
			}
			pending.resolved = true
			pending.approved = *payload.Approved
			pending.cancelled = payload.Reason != ""
		case agentapi.EventToolCall:
			var call agentapi.ToolCall
			if err := json.Unmarshal(event.Payload, &call); err != nil ||
				call.RunID != event.RunID || call.ToolCallID != event.ToolCallID ||
				call.Revision != session.revision || !agentapi.ValidIdentifier(call.ToolCallID) ||
				!agentapi.ValidIdentifier(call.ToolName) || len(call.Arguments) == 0 {
				return nil, false, fmt.Errorf("agent tool call event is invalid")
			}
			if _, ok := session.toolIndex[call.ToolName]; !ok {
				return nil, false, fmt.Errorf("agent tool call event tool is invalid")
			}
			var arguments map[string]json.RawMessage
			if json.Unmarshal(call.Arguments, &arguments) != nil || arguments == nil {
				return nil, false, fmt.Errorf("agent tool call arguments are invalid")
			}
			if _, exists := session.toolCalls[call.ToolCallID]; exists {
				return nil, false, fmt.Errorf("agent tool call event is duplicated")
			}
			session.toolCalls[call.ToolCallID] = call
			session.toolStartedAt[call.ToolCallID] = event.Timestamp
			session.appendToolCallHistory(session.runOrder[event.RunID], call)
		case agentapi.EventToolResult:
			var result safeToolResult
			if err := json.Unmarshal(event.Payload, &result); err != nil {
				return nil, false, err
			}
			call, ok := session.toolCalls[result.ToolCallID]
			if !ok || result.RunID != event.RunID ||
				result.ToolCallID != event.ToolCallID || call.RunID != result.RunID {
				return nil, false, fmt.Errorf("agent tool result event binding is invalid")
			}
			if err := session.results.restore(result); err != nil {
				return nil, false, err
			}
			if result.Done {
				session.appendToolResultHistory(session.runOrder[event.RunID], call, result)
				delete(session.toolStartedAt, result.ToolCallID)
			}
		case agentapi.EventModelRequested, agentapi.EventModelCompleted,
			agentapi.EventToolCancel:
		default:
			return nil, false, fmt.Errorf("agent event type is invalid")
		}
	}
	// A crash can occur after the user message is durable but before run.queued.
	// Preserve its idempotency identity and record an honest terminal state.
	for runID := range messageRuns {
		if _, terminal := terminalRuns[runID]; !terminal {
			outstanding[runID] = struct{}{}
		}
	}
	if deleted {
		return session, true, nil
	}
	runtime, err := newModelRuntime(session, factory)
	if err != nil {
		return nil, false, err
	}
	session.runtime = runtime
	for approvalID, pending := range session.approvals {
		if pending.resolved {
			continue
		}
		rejected := false
		_, err = session.appendEventLocked(
			session.event(
				agentapi.EventApproval, pending.runID, "", pending.toolCallID,
			),
			approvalPayload{
				ApprovalID: approvalID, Digest: pending.digest,
				ToolName:  pending.toolName,
				Arguments: append(json.RawMessage(nil), pending.arguments...),
				Summary:   pending.summary, Approved: &rejected,
				Reason: "Koko agent runtime restarted",
			},
		)
		if err != nil {
			runtime.close()
			return nil, false, err
		}
		pending.resolved = true
		pending.approved = false
		pending.cancelled = true
	}
	for runID := range outstanding {
		_, err = session.appendRunEvent(
			agentapi.EventRunInterrupted, runID, "", "",
			runPayload{State: agentapi.RunStateInterrupted, Reason: "Koko agent runtime restarted"},
		)
		if err != nil {
			runtime.close()
			return nil, false, err
		}
	}
	session.activeRunID = ""
	session.pendingRuns = 0
	if len(outstanding) > 0 {
		session.runState = agentapi.RunStateInterrupted
	}
	return session, false, nil
}

func storedRunState(eventType string) string {
	switch eventType {
	case agentapi.EventRunCompleted:
		return agentapi.RunStateCompleted
	case agentapi.EventRunFailed:
		return agentapi.RunStateFailed
	case agentapi.EventRunCancelled:
		return agentapi.RunStateCancelled
	default:
		return agentapi.RunStateInterrupted
	}
}

func validateStoredRunEvent(event agentapi.Event, expected string) error {
	if !agentapi.ValidIdentifier(event.RunID) {
		return fmt.Errorf("agent run event is invalid")
	}
	var payload runPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.State != expected {
		return fmt.Errorf("agent run event is invalid")
	}
	return nil
}

func (s *agentSession) appendEventLocked(
	event agentapi.Event,
	payload any,
) (agentapi.Event, error) {
	appended, err := s.events.appendJSON(event, payload)
	if err != nil {
		s.markUnavailableLocked(err)
	}
	return appended, err
}

func (s *agentSession) markUnavailableLocked(cause error) {
	if s.unavailable {
		return
	}
	s.events.invalidate(cause)
	s.unavailable = true
	s.activeRunID = ""
	s.runState = agentapi.RunStateUnavailable
	s.pendingRuns = 0
	s.pendingRunIDs = nil
	s.pendingTool = nil
	for _, pending := range s.approvals {
		if pending.resolved {
			continue
		}
		pending.resolved = true
		pending.cancelled = true
		select {
		case pending.result <- false:
		default:
		}
	}
	if s.runtime != nil {
		// Runtime never holds its mutex while invoking Session callbacks. Keeping
		// the established session -> runtime lock order makes fail-stop
		// synchronous without waiting for callbacks or spawning cleanup work.
		s.runtime.close()
	}
}

func (s *agentSession) failStop(err error) {
	s.mu.Lock()
	s.markUnavailableLocked(err)
	s.mu.Unlock()
}

func (s *agentSession) isUnavailable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.events.isPoisoned() {
		s.markUnavailableLocked(errEventLogPoisoned)
	}
	return s.unavailable
}

func (s *agentSession) updateApprovalMode(
	request agentapi.ApprovalModeRequest,
) (agentapi.ApprovalModeResponse, error) {
	if !validApprovalMode(request.Mode) {
		return agentapi.ApprovalModeResponse{}, errInvalidApprovalMode
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return agentapi.ApprovalModeResponse{}, errSessionClosed
	}
	if s.unavailable {
		return agentapi.ApprovalModeResponse{}, errSessionUnavailable
	}
	previous := s.approvalMode
	if request.Mode == previous {
		return agentapi.ApprovalModeResponse{
			Mode: request.Mode, Previous: previous, Duplicate: true,
			Cursor: s.events.cursor(),
		}, nil
	}
	if s.activeRunID != "" || s.pendingRuns != 0 {
		return agentapi.ApprovalModeResponse{}, errSessionBusy
	}
	event, err := s.appendEventLocked(
		s.event(agentapi.EventSessionApprovalModeChanged, "", "", ""),
		approvalModeChangedPayload{Previous: previous, Current: request.Mode},
	)
	if err != nil {
		return agentapi.ApprovalModeResponse{}, err
	}
	s.approvalMode = request.Mode
	return agentapi.ApprovalModeResponse{
		Mode: request.Mode, Previous: previous, Cursor: event.Sequence,
	}, nil
}

func (s *agentSession) runHistory(runID string) []agentruntime.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.runOrder[runID]
	result := make([]agentruntime.Message, 0, len(s.history))
	for _, item := range s.history {
		if order == 0 || item.runOrder < order {
			result = append(result, item.message)
		}
	}
	return result
}

func (s *agentSession) appendHistory(
	order int,
	role, text string,
	metadata map[string]any,
) {
	itemBytes := len(text) + messageMetadataSize(metadata)
	if order <= 0 || text == "" {
		return
	}
	if s.historyBytes+itemBytes > maxSessionHistoryBytes {
		// Keep the durable event log complete, but fail closed for later model
		// turns instead of silently presenting a partial conversation history.
		s.historyBytes = maxSessionHistoryBytes
		return
	}
	s.history = append(s.history, sessionHistoryItem{
		runOrder: order,
		message:  agentruntime.Message{Role: role, Text: text, Metadata: metadata},
	})
	s.historyBytes += itemBytes
}

func (s *agentSession) appendToolCallHistory(order int, call agentapi.ToolCall) {
	encoded, err := json.Marshal(struct {
		ToolCallID string          `json:"tool_call_id"`
		ToolName   string          `json:"tool_name"`
		Arguments  json.RawMessage `json:"arguments"`
	}{
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		Arguments:  call.Arguments,
	})
	if err == nil {
		s.appendHistory(order, "tool_call", string(encoded), nil)
	}
}

func (s *agentSession) appendToolResultHistory(
	order int,
	call agentapi.ToolCall,
	result safeToolResult,
) {
	cleanResult := append(json.RawMessage(nil), result.Result...)
	if len(cleanResult) > 0 {
		var object map[string]json.RawMessage
		if json.Unmarshal(cleanResult, &object) == nil && object != nil {
			delete(object, "_meta")
			cleanResult, _ = json.Marshal(object)
		}
	}
	encoded, err := json.Marshal(struct {
		ToolCallID string                 `json:"tool_call_id"`
		ToolName   string                 `json:"tool_name"`
		Status     string                 `json:"status"`
		Result     json.RawMessage        `json:"result,omitempty"`
		Error      *agentapi.JSONRPCError `json:"error,omitempty"`
	}{
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		Status:     result.Status,
		Result:     cleanResult,
		Error:      result.Error,
	})
	if err == nil {
		s.appendHistory(order, "tool_result", string(encoded), nil)
	}
}

func (s *agentSession) handleMessage(request agentapi.MessageRequest) (agentapi.MessageResponse, error) {
	question, err := messageText(request)
	if err != nil {
		return agentapi.MessageResponse{}, err
	}
	metadata, err := sanitizeMessageMetadata(request.Metadata)
	if err != nil {
		return agentapi.MessageResponse{}, err
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	if key == "" {
		key = strings.TrimSpace(request.MessageID)
	}
	if !agentapi.ValidIdentifier(key) {
		return agentapi.MessageResponse{}, fmt.Errorf("message idempotency key is invalid")
	}
	messageID := strings.TrimSpace(request.MessageID)
	if messageID == "" {
		messageID, err = randomID()
		if err != nil {
			return agentapi.MessageResponse{}, err
		}
	}
	if !agentapi.ValidIdentifier(messageID) {
		return agentapi.MessageResponse{}, fmt.Errorf("message ID is invalid")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return agentapi.MessageResponse{}, errSessionClosed
	}
	if s.unavailable {
		s.mu.Unlock()
		return agentapi.MessageResponse{}, errSessionUnavailable
	}
	if record, ok := s.messages[key]; ok {
		cursor := s.events.cursor()
		s.mu.Unlock()
		return agentapi.MessageResponse{
			MessageID: record.messageID, RunID: record.runID,
			Duplicate: true, Cursor: cursor,
		}, nil
	}
	if len(s.messages) >= maxMessagesPerSession || s.pendingRuns >= agentruntime.MaxQueuedRuns {
		s.mu.Unlock()
		return agentapi.MessageResponse{}, fmt.Errorf("session message queue limit reached")
	}
	if s.historyBytes+len(question)+messageMetadataSize(metadata) > maxSessionHistoryBytes {
		s.mu.Unlock()
		return agentapi.MessageResponse{}, fmt.Errorf("session conversation history limit reached")
	}
	runID, err := randomID()
	if err != nil {
		s.mu.Unlock()
		return agentapi.MessageResponse{}, err
	}
	if _, err = s.appendEventLocked(
		s.event(agentapi.EventMessageCreated, runID, messageID, ""),
		messageCreatedPayload{
			Role: "user", Text: question, IdempotencyKey: key, Metadata: metadata,
		},
	); err != nil {
		s.mu.Unlock()
		return agentapi.MessageResponse{}, err
	}
	queued, err := s.appendEventLocked(
		s.event(agentapi.EventRunQueued, runID, messageID, ""),
		runPayload{State: agentapi.RunStateQueued},
	)
	if err != nil {
		s.mu.Unlock()
		return agentapi.MessageResponse{}, err
	}
	s.messages[key] = messageRecord{messageID: messageID, runID: runID}
	s.messageOrder = append(s.messageOrder, key)
	s.nextRunOrder++
	s.runOrder[runID] = s.nextRunOrder
	s.appendHistory(s.nextRunOrder, "user", question, metadata)
	s.pendingRuns++
	s.pendingRunIDs = append(s.pendingRunIDs, runID)
	if err = s.runtime.start(runID, messageID, question, metadata); err != nil {
		_, appendErr := s.appendEventLocked(
			s.event(agentapi.EventRunFailed, runID, messageID, ""),
			runPayload{State: agentapi.RunStateFailed, Reason: "agent run queue failed"},
		)
		if appendErr == nil {
			s.removePendingRunLocked(runID)
			s.runState = agentapi.RunStateFailed
		}
		s.mu.Unlock()
		return agentapi.MessageResponse{}, err
	}
	if s.activeRunID == "" {
		s.runState = agentapi.RunStateQueued
	}
	s.mu.Unlock()
	return agentapi.MessageResponse{
		MessageID: messageID, RunID: runID, Cursor: queued.Sequence,
	}, nil
}

func (s *agentSession) startRun(runID, messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errSessionClosed
	}
	if s.unavailable {
		return errSessionUnavailable
	}
	if !s.hasPendingRunLocked(runID) {
		return context.Canceled
	}
	_, err := s.appendEventLocked(
		s.event(agentapi.EventRunStarted, runID, messageID, ""),
		runPayload{State: agentapi.RunStateRunning},
	)
	if err != nil {
		return err
	}
	s.activeRunID = runID
	s.runState = agentapi.RunStateRunning
	return nil
}

func (s *agentSession) completeRun(runID, messageID, answer string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errSessionClosed
	}
	if s.unavailable {
		return errSessionUnavailable
	}
	if s.activeRunID != runID {
		return nil
	}
	if len(answer) > agentapi.MaxMessageBytes ||
		s.historyBytes+len(answer) > maxSessionHistoryBytes {
		_, err := s.appendEventLocked(
			s.event(agentapi.EventRunFailed, runID, messageID, ""),
			runPayload{State: agentapi.RunStateFailed, Reason: "agent answer exceeded the history limit"},
		)
		if err != nil {
			return err
		}
		s.activeRunID = ""
		s.removePendingRunLocked(runID)
		s.runState = agentapi.RunStateFailed
		return nil
	}
	if _, err := s.appendEventLocked(
		s.event(agentapi.EventMessageCreated, runID, messageID, ""),
		messageCreatedPayload{Role: "assistant", Text: answer},
	); err != nil {
		return err
	}
	if _, err := s.appendEventLocked(
		s.event(agentapi.EventRunCompleted, runID, messageID, ""),
		runPayload{State: agentapi.RunStateCompleted},
	); err != nil {
		return err
	}
	s.appendHistory(s.runOrder[runID], "assistant", answer, nil)
	s.activeRunID = ""
	s.removePendingRunLocked(runID)
	s.runState = agentapi.RunStateCompleted
	return nil
}

func (s *agentSession) failRun(runID, messageID string, runErr error) error {
	logger.Errorf("Agent session %s run %s failed: %v", s.id, runID, runErr)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errSessionClosed
	}
	if s.unavailable {
		return errSessionUnavailable
	}
	if s.activeRunID != runID {
		return nil
	}
	reason := "agent run failed"
	switch {
	case errors.Is(runErr, agentruntime.ErrRunTimeout):
		reason = "agent run timed out"
	case errors.Is(runErr, provider.ErrRequestBudget):
		reason = "agent model request budget exhausted"
	}
	if s.pendingTool != nil && s.pendingTool.RunID == runID {
		call := *s.pendingTool
		if _, err := s.appendEventLocked(
			s.event(agentapi.EventToolCancel, runID, messageID, call.ToolCallID),
			agentapi.ToolCancel{
				RunID: runID, ToolCallID: call.ToolCallID,
				Reason: reason,
			},
		); err != nil {
			return err
		}
		s.results.cancel(call.ToolCallID)
		delete(s.toolStartedAt, call.ToolCallID)
		s.pendingTool = nil
	}
	if err := s.cancelApprovalsLocked(runID, reason); err != nil {
		return err
	}
	if _, err := s.appendEventLocked(
		s.event(agentapi.EventRunFailed, runID, messageID, ""),
		runPayload{State: agentapi.RunStateFailed, Reason: reason},
	); err != nil {
		return err
	}
	s.activeRunID = ""
	s.removePendingRunLocked(runID)
	s.runState = agentapi.RunStateFailed
	return nil
}

func (s *agentSession) cancel(request agentapi.CancelRequest) (agentapi.CancelResponse, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return agentapi.CancelResponse{}, errSessionClosed
	}
	if s.unavailable {
		s.mu.Unlock()
		return agentapi.CancelResponse{}, errSessionUnavailable
	}
	runID := s.activeRunID
	if request.RunID != "" {
		if request.RunID != s.activeRunID && !s.hasPendingRunLocked(request.RunID) {
			if s.runOrder[request.RunID] != 0 {
				cursor := s.events.cursor()
				s.mu.Unlock()
				return agentapi.CancelResponse{
					Accepted: true, Duplicate: true, Cursor: cursor,
				}, nil
			}
			s.mu.Unlock()
			return agentapi.CancelResponse{}, fmt.Errorf("run ID does not match a pending or active run")
		}
		runID = request.RunID
	} else if runID == "" && len(s.pendingRunIDs) > 0 {
		runID = s.pendingRunIDs[0]
	}
	if runID == "" {
		cursor := s.events.cursor()
		s.mu.Unlock()
		return agentapi.CancelResponse{Accepted: true, Duplicate: true, Cursor: cursor}, nil
	}
	if s.pendingTool != nil && s.pendingTool.RunID == runID {
		call := *s.pendingTool
		_, err := s.appendEventLocked(
			s.event(agentapi.EventToolCancel, runID, "", call.ToolCallID),
			agentapi.ToolCancel{
				RunID: runID, ToolCallID: call.ToolCallID,
				Reason: boundedText(request.Reason, 512),
			},
		)
		if err != nil {
			s.mu.Unlock()
			return agentapi.CancelResponse{}, err
		}
		s.results.cancel(call.ToolCallID)
		delete(s.toolStartedAt, call.ToolCallID)
		s.pendingTool = nil
	}
	if err := s.cancelApprovalsLocked(runID, "run cancelled"); err != nil {
		s.mu.Unlock()
		return agentapi.CancelResponse{}, err
	}
	s.runtime.cancelRun(runID)
	event, err := s.appendEventLocked(
		s.event(agentapi.EventRunCancelled, runID, "", ""),
		runPayload{State: agentapi.RunStateCancelled, Reason: boundedText(request.Reason, 512)},
	)
	if err == nil {
		if s.activeRunID == runID {
			s.activeRunID = ""
		}
		s.removePendingRunLocked(runID)
		s.runState = agentapi.RunStateCancelled
	}
	s.mu.Unlock()
	if err != nil {
		return agentapi.CancelResponse{}, err
	}
	return agentapi.CancelResponse{Accepted: true, Cursor: event.Sequence}, nil
}

func (s *agentSession) awaitApproval(
	ctx context.Context,
	runID, toolCallID, digest, toolName string,
	arguments json.RawMessage,
	summary string,
	modelDurationMS int64,
) (bool, error) {
	canonicalArguments, err := agentauth.CanonicalJSON(arguments)
	if err != nil {
		return false, fmt.Errorf("approval arguments are invalid")
	}
	var argumentObject map[string]any
	if json.Unmarshal(canonicalArguments, &argumentObject) != nil || argumentObject == nil {
		return false, fmt.Errorf("approval arguments must be an object")
	}
	argsHash, err := agentauth.HashRawJSON(canonicalArguments)
	if err != nil {
		return false, fmt.Errorf("approval arguments are invalid")
	}
	expectedDigest, err := agentauth.HashValue(map[string]any{
		"resource_session_id": s.resourceID,
		"run_id":              runID, "tool_call_id": toolCallID,
		"tool_name": toolName, "args_hash": argsHash,
		"revision": s.revision,
	})
	if err != nil || expectedDigest != digest {
		return false, fmt.Errorf("approval digest is invalid")
	}
	approvalID, err := randomID()
	if err != nil {
		return false, err
	}
	pending := &pendingApproval{
		runID: runID, toolCallID: toolCallID, digest: digest,
		toolName: toolName, arguments: append(json.RawMessage(nil), canonicalArguments...),
		summary: boundedText(summary, 2048), modelDurationMS: modelDurationMS,
		result: make(chan bool, 1),
	}
	s.mu.Lock()
	if s.closed || s.unavailable || s.activeRunID != runID {
		s.mu.Unlock()
		if s.unavailable {
			return false, errSessionUnavailable
		}
		return false, errSessionClosed
	}
	s.approvals[approvalID] = pending
	_, err = s.appendEventLocked(
		s.event(agentapi.EventApprovalNeeded, runID, "", toolCallID),
		approvalPayload{
			ApprovalID: approvalID, Digest: digest,
			ToolName: toolName, Arguments: canonicalArguments,
			Summary: pending.summary, ModelDurationMS: pending.modelDurationMS,
		},
	)
	s.mu.Unlock()
	if err != nil {
		return false, err
	}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case approved := <-pending.result:
		return approved, nil
	}
}

func (s *agentSession) resolveApproval(
	approvalID string,
	request agentapi.ApprovalRequest,
) (agentapi.ApprovalResponse, error) {
	if request.Decision != "approve" && request.Decision != "reject" {
		return agentapi.ApprovalResponse{}, fmt.Errorf("approval decision is invalid")
	}
	if request.RunID == "" || request.Digest == "" {
		return agentapi.ApprovalResponse{}, fmt.Errorf("approval run ID and digest are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unavailable {
		return agentapi.ApprovalResponse{}, errSessionUnavailable
	}
	pending := s.approvals[approvalID]
	if pending == nil || pending.runID != request.RunID || pending.digest != request.Digest {
		return agentapi.ApprovalResponse{}, fmt.Errorf("approval is not pending or is not bound to this run")
	}
	if pending.resolved {
		if pending.cancelled || pending.approved != (request.Decision == "approve") {
			return agentapi.ApprovalResponse{}, fmt.Errorf("approval is no longer pending")
		}
		return agentapi.ApprovalResponse{
			Accepted: true, Duplicate: true, Cursor: s.events.cursor(),
		}, nil
	}
	approved := request.Decision == "approve"
	event, err := s.appendEventLocked(
		s.event(agentapi.EventApproval, pending.runID, "", pending.toolCallID),
		approvalPayload{
			ApprovalID: approvalID, Digest: pending.digest,
			ToolName:  pending.toolName,
			Arguments: append(json.RawMessage(nil), pending.arguments...),
			Summary:   pending.summary, ModelDurationMS: pending.modelDurationMS,
			Approved: &approved,
		},
	)
	if err != nil {
		return agentapi.ApprovalResponse{}, err
	}
	pending.resolved = true
	pending.approved = approved
	select {
	case pending.result <- approved:
	default:
	}
	return agentapi.ApprovalResponse{Accepted: true, Cursor: event.Sequence}, nil
}

func (s *agentSession) cancelApprovalsLocked(runID, reason string) error {
	rejected := false
	for approvalID, pending := range s.approvals {
		if pending.resolved || pending.runID != runID {
			continue
		}
		_, err := s.appendEventLocked(
			s.event(agentapi.EventApproval, runID, "", pending.toolCallID),
			approvalPayload{
				ApprovalID: approvalID, Digest: pending.digest,
				ToolName:  pending.toolName,
				Arguments: append(json.RawMessage(nil), pending.arguments...),
				Summary:   pending.summary, ModelDurationMS: pending.modelDurationMS,
				Approved: &rejected,
				Reason:   boundedText(reason, 512),
			},
		)
		if err != nil {
			return err
		}
		pending.resolved = true
		pending.approved = false
		pending.cancelled = true
		select {
		case pending.result <- false:
		default:
		}
	}
	return nil
}

func (s *agentSession) publishToolCall(
	runID, messageID, toolCallID, toolName string,
	arguments json.RawMessage,
	modelDurationMS int64,
) error {
	if err := s.results.begin(runID, toolCallID); err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			s.results.abandon(runID, toolCallID)
		}
	}()
	call := agentapi.ToolCall{
		RunID: runID, ToolCallID: toolCallID, Revision: s.revision,
		ToolName: toolName, Arguments: append(json.RawMessage(nil), arguments...),
		ModelDurationMS: modelDurationMS,
	}
	s.mu.Lock()
	if s.closed || s.unavailable || s.activeRunID != runID {
		s.mu.Unlock()
		s.results.cancel(toolCallID)
		if s.unavailable {
			return errSessionUnavailable
		}
		return errSessionClosed
	}
	event, err := s.appendEventLocked(
		s.event(agentapi.EventToolCall, runID, messageID, toolCallID),
		call,
	)
	if err == nil {
		s.toolCalls[toolCallID] = call
		s.toolStartedAt[toolCallID] = event.Timestamp
		s.appendToolCallHistory(s.runOrder[runID], call)
		s.pendingTool = &call
		published = true
	}
	s.mu.Unlock()
	return err
}

func (s *agentSession) acceptToolResult(
	request agentapi.ToolResultRequest,
) (agentapi.ToolResultResponse, error) {
	var appendErr error
	s.mu.Lock()
	startedAt := s.toolStartedAt[request.ID]
	call := s.toolCalls[request.ID]
	s.mu.Unlock()
	durationMS := int64(0)
	if startedAt > 0 {
		durationMS = time.Now().UnixMilli() - startedAt
		if durationMS < 0 {
			durationMS = 0
		}
	}
	duplicate, cursor, err := s.results.accept(request, func(result safeToolResult) (agentapi.Event, error) {
		var event agentapi.Event
		event, appendErr = s.events.appendJSON(
			s.event(agentapi.EventToolResult, result.RunID, "", result.ToolCallID), struct {
				safeToolResult
				DurationMS      int64 `json:"duration_ms"`
				ModelDurationMS int64 `json:"model_duration_ms,omitempty"`
			}{result, durationMS, call.ModelDurationMS},
		)
		return event, appendErr
	})
	if err != nil {
		if appendErr != nil {
			s.failStop(appendErr)
		}
		return agentapi.ToolResultResponse{}, err
	}
	if request.Done {
		s.mu.Lock()
		if !duplicate {
			if call, ok := s.toolCalls[request.ID]; ok {
				s.appendToolResultHistory(
					s.runOrder[request.RunID], call, toSafeToolResult(request),
				)
			}
		}
		if s.pendingTool != nil && s.pendingTool.ToolCallID == request.ID {
			s.pendingTool = nil
		}
		delete(s.toolStartedAt, request.ID)
		s.mu.Unlock()
	}
	if duplicate && cursor == 0 {
		cursor = s.events.cursor()
	}
	return agentapi.ToolResultResponse{
		Accepted: true, Duplicate: duplicate, Cursor: cursor,
	}, nil
}

func (s *agentSession) emitModelEvent(
	eventType, runID, messageID string,
	payload any,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errSessionClosed
	}
	if s.unavailable {
		return errSessionUnavailable
	}
	_, err := s.appendEventLocked(s.event(eventType, runID, messageID, ""), payload)
	return err
}

func (s *agentSession) appendRunEvent(
	eventType, runID, messageID, toolCallID string,
	payload any,
) (agentapi.Event, error) {
	return s.events.appendJSON(s.event(eventType, runID, messageID, toolCallID), payload)
}

func (s *agentSession) event(
	eventType, runID, messageID, toolCallID string,
) agentapi.Event {
	return agentapi.Event{
		Type: eventType, SessionID: s.id, ResourceSessionID: s.resourceID,
		RunID: runID, MessageID: messageID, ToolCallID: toolCallID,
	}
}

func (s *agentSession) tool(name string) (agentapi.ToolDefinition, bool) {
	value, ok := s.toolIndex[name]
	return value, ok
}

func (s *agentSession) needsApproval(
	tool agentapi.ToolDefinition,
	modelRequested bool,
) bool {
	readOnly := tool.Annotations.ReadOnlyHint != nil && *tool.Annotations.ReadOnlyHint
	switch s.approvalMode {
	case "always":
		return true
	case "never":
		return false
	default:
		destructive := tool.Annotations.DestructiveHint != nil &&
			*tool.Annotations.DestructiveHint
		openWorld := tool.Annotations.OpenWorldHint != nil &&
			*tool.Annotations.OpenWorldHint
		if destructive || openWorld || modelRequested {
			return true
		}
		return !readOnly
	}
}

func (s *agentSession) owns(principal agentapi.Principal, resourceID string) bool {
	return s.principal == principal && s.resourceID == resourceID
}

func (s *agentSession) acquireEventStream() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastActivity = time.Now()
	if s.closing || s.closed || s.unavailable || s.eventStreamActive {
		return false
	}
	s.eventStreamActive = true
	return true
}

func (s *agentSession) hasPendingRunLocked(runID string) bool {
	for _, pending := range s.pendingRunIDs {
		if pending == runID {
			return true
		}
	}
	return false
}

func (s *agentSession) removePendingRunLocked(runID string) {
	for index, pending := range s.pendingRunIDs {
		if pending != runID {
			continue
		}
		s.pendingRunIDs = append(s.pendingRunIDs[:index], s.pendingRunIDs[index+1:]...)
		if s.pendingRuns > 0 {
			s.pendingRuns--
		}
		return
	}
}

func (s *agentSession) releaseEventStream() {
	s.mu.Lock()
	s.eventStreamActive = false
	s.lastActivity = time.Now()
	s.mu.Unlock()
}

func (s *agentSession) touch() {
	s.mu.Lock()
	s.lastActivity = time.Now()
	s.mu.Unlock()
}

func (s *agentSession) beginShutdown(reason string) error {
	reason = boundedText(reason, 512)
	if reason == "" {
		reason = "Koko agent runtime shutting down"
	}
	s.mu.Lock()
	if s.closing || s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closing = true
	s.closed = true
	if s.unavailable {
		s.mu.Unlock()
		if s.runtime != nil {
			s.runtime.close()
		}
		return nil
	}

	runIDs := make([]string, 0, len(s.pendingRunIDs)+1)
	seen := make(map[string]struct{}, len(s.pendingRunIDs)+1)
	for _, runID := range s.pendingRunIDs {
		if runID == "" {
			continue
		}
		if _, exists := seen[runID]; exists {
			continue
		}
		seen[runID] = struct{}{}
		runIDs = append(runIDs, runID)
	}
	if s.activeRunID != "" {
		if _, exists := seen[s.activeRunID]; !exists {
			runIDs = append(runIDs, s.activeRunID)
		}
	}

	var shutdownErrors []error
	if s.pendingTool != nil {
		call := *s.pendingTool
		_, err := s.appendEventLocked(
			s.event(agentapi.EventToolCancel, call.RunID, "", call.ToolCallID),
			agentapi.ToolCancel{
				RunID: call.RunID, ToolCallID: call.ToolCallID,
				Reason: reason,
			},
		)
		if err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
		s.results.cancel(call.ToolCallID)
		delete(s.toolStartedAt, call.ToolCallID)
		s.pendingTool = nil
	}
	for _, runID := range runIDs {
		if err := s.cancelApprovalsLocked(runID, reason); err != nil {
			shutdownErrors = append(shutdownErrors, err)
			break
		}
		if _, err := s.appendEventLocked(
			s.event(agentapi.EventRunInterrupted, runID, "", ""),
			runPayload{State: agentapi.RunStateInterrupted, Reason: reason},
		); err != nil {
			shutdownErrors = append(shutdownErrors, err)
			break
		}
	}
	s.activeRunID = ""
	s.pendingRuns = 0
	s.pendingRunIDs = nil
	if len(runIDs) > 0 {
		s.runState = agentapi.RunStateInterrupted
	}
	s.mu.Unlock()
	if s.runtime != nil {
		s.runtime.close()
	}
	return errors.Join(shutdownErrors...)
}

func (s *agentSession) stop() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		s.closed = true
		s.mu.Unlock()
		if s.runtime != nil {
			s.runtime.close()
		}
		_ = s.events.close()
	})
}

func (s *agentSession) delete() error {
	return s.deleteWithArchive(nil)
}

func (s *agentSession) deleteWithArchive(archives *archiveStore) error {
	s.mu.Lock()
	if s.deletePersisted {
		s.mu.Unlock()
		s.stop()
		return archiveEventLog(archives, s.events)
	}
	if s.unavailable {
		var reservation *archiveReservation
		var err error
		if archives != nil {
			reservation, err = archives.reserveClose()
			if err != nil {
				s.mu.Unlock()
				return err
			}
		}
		s.closing = true
		s.mu.Unlock()
		s.stop()
		if reservation != nil {
			return reservation.quarantine(s.events)
		}
		return s.events.quarantine()
	}
	if s.closed {
		s.mu.Unlock()
		return errSessionClosed
	}
	closedEvent := s.event(agentapi.EventSessionClosed, "", "", "")
	closedPayload := map[string]any{"reason": "session deleted"}
	var (
		reservation *archiveReservation
		err         error
	)
	if archives != nil {
		reservation, err = archives.reserveClose()
		if err != nil {
			s.mu.Unlock()
			return err
		}
	}
	s.closing = true
	_, err = s.appendEventLocked(closedEvent, closedPayload)
	if err == nil {
		s.deletePersisted = true
		s.closed = true
	}
	s.mu.Unlock()
	if err != nil {
		reservation.release()
		return err
	}
	s.stop()
	if reservation != nil {
		return reservation.archive(s.events)
	}
	return s.events.archiveClosed()
}

func (s *agentSession) deleteIfIdle(cutoff time.Time) (bool, error) {
	return s.deleteIfIdleWithArchive(cutoff, nil)
}

func (s *agentSession) deleteIfIdleWithArchive(
	cutoff time.Time,
	archives *archiveStore,
) (bool, error) {
	s.mu.Lock()
	if s.deletePersisted {
		s.mu.Unlock()
		s.stop()
		if err := archiveEventLog(archives, s.events); err != nil {
			return false, err
		}
		return true, nil
	}
	if s.unavailable && (s.closing || s.closed) {
		s.mu.Unlock()
		if err := quarantineEventLog(archives, s.events); err != nil {
			return false, err
		}
		return true, nil
	}
	if s.eventStreamActive || s.closing || s.closed || s.lastActivity.After(cutoff) {
		s.mu.Unlock()
		return false, nil
	}
	if s.unavailable {
		var reservation *archiveReservation
		var err error
		if archives != nil {
			reservation, err = archives.reserveClose()
			if err != nil {
				s.mu.Unlock()
				return false, err
			}
		}
		s.closing = true
		s.mu.Unlock()
		s.stop()
		if reservation != nil {
			err = reservation.quarantine(s.events)
		} else {
			err = s.events.quarantine()
		}
		if err != nil {
			return false, err
		}
		return true, nil
	}
	closedEvent := s.event(agentapi.EventSessionClosed, "", "", "")
	closedPayload := map[string]any{"reason": "idle session lease expired"}
	var (
		reservation *archiveReservation
		err         error
	)
	if archives != nil {
		reservation, err = archives.reserveClose()
		if err != nil {
			s.mu.Unlock()
			return false, err
		}
	}
	s.closing = true
	_, err = s.appendEventLocked(closedEvent, closedPayload)
	if err == nil {
		s.deletePersisted = true
		s.closed = true
	}
	s.mu.Unlock()
	if err != nil {
		s.stop()
		var quarantineErr error
		if reservation != nil {
			quarantineErr = reservation.quarantine(s.events)
		} else {
			quarantineErr = s.events.quarantine()
		}
		if quarantineErr != nil {
			return false, quarantineErr
		}
		return true, err
	}
	s.stop()
	if reservation != nil {
		if err = reservation.archive(s.events); err != nil {
			return false, err
		}
	} else if err = s.events.archiveClosed(); err != nil {
		return false, err
	}
	return true, nil
}

func archiveEventLog(archives *archiveStore, events *eventLog) error {
	if archives != nil {
		return archives.archive(events)
	}
	return events.archiveClosed()
}

func quarantineEventLog(archives *archiveStore, events *eventLog) error {
	if archives != nil {
		return archives.quarantine(events)
	}
	return events.quarantine()
}

func messageText(request agentapi.MessageRequest) (string, error) {
	if request.Role != "user" {
		return "", fmt.Errorf("only user messages are accepted")
	}
	var builder strings.Builder
	for _, part := range request.Parts {
		if part.Type != "text" {
			return "", fmt.Errorf("unsupported message part")
		}
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(part.Text)
	}
	question := strings.TrimSpace(builder.String())
	if question == "" || len(question) > agentapi.MaxMessageBytes || !utf8.ValidString(question) {
		return "", fmt.Errorf("message text is empty, invalid, or too large")
	}
	return question, nil
}

func normalizeApprovalMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case agentapi.ApprovalModeAlways, agentapi.ApprovalModeNever:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return agentapi.ApprovalModeAuto
	}
}

func validApprovalMode(value string) bool {
	return value == agentapi.ApprovalModeAlways || value == agentapi.ApprovalModeAuto ||
		value == agentapi.ApprovalModeNever
}
