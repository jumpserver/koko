package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jumpserver/koko/internal/agentapi"
)

type ErrorKind string

const (
	ErrorInvalid     ErrorKind = "invalid"
	ErrorNotFound    ErrorKind = "not_found"
	ErrorConflict    ErrorKind = "conflict"
	ErrorUnavailable ErrorKind = "unavailable"
	ErrorStorage     ErrorKind = "storage"
	ErrorInternal    ErrorKind = "internal"
)

type ServiceError struct {
	Kind    ErrorKind
	Code    string
	Message string
	cause   error
}

func (e *ServiceError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *ServiceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func serviceError(kind ErrorKind, code, message string, cause error) error {
	return &ServiceError{Kind: kind, Code: code, Message: message, cause: cause}
}

type Status struct {
	InstanceID       string
	Closed           bool
	DegradedSessions int
}

type BootstrapState struct {
	SessionID     string
	Cursor        uint64
	ContextDigest string
	ToolsetDigest string
}

type EventSubscription struct {
	session *agentSession
	once    sync.Once
}

func (s *EventSubscription) After(after uint64) ([]agentapi.Event, <-chan struct{}, bool) {
	if s == nil || s.session == nil {
		return nil, nil, true
	}
	return s.session.events.after(after)
}

func (s *EventSubscription) Close() {
	if s == nil || s.session == nil {
		return
	}
	s.once.Do(s.session.releaseEventStream)
}

func (s *Service) SetSessionRemovedHook(hook func(agentapi.Principal, string)) {
	s.mu.Lock()
	s.sessionRemoved = hook
	s.mu.Unlock()
}

func (s *Service) Status() Status {
	s.mu.RLock()
	degraded := s.degraded
	for _, session := range s.sessions {
		if session.isUnavailable() {
			degraded++
		}
	}
	status := Status{
		InstanceID: s.instanceID, Closed: s.closed, DegradedSessions: degraded,
	}
	s.mu.RUnlock()
	return status
}

func (s *Service) Bootstrap(
	principal agentapi.Principal,
	resourceID string,
) (BootstrapState, error) {
	if !agentapi.ValidIdentifier(resourceID) {
		return BootstrapState{}, serviceError(
			ErrorInvalid, "resource_session_required",
			"resource session header is required", nil,
		)
	}
	s.mu.RLock()
	sessionID := s.resources[resourceID]
	session := s.sessions[sessionID]
	s.mu.RUnlock()
	if session == nil || !session.owns(principal, resourceID) {
		return BootstrapState{}, nil
	}
	session.touch()
	return BootstrapState{
		SessionID: session.id, Cursor: session.events.cursor(),
		ContextDigest: session.contextDigest, ToolsetDigest: session.toolsetDigest,
	}, nil
}

func (s *Service) CreateSession(
	principal agentapi.Principal,
	request agentapi.CreateSessionRequest,
) (agentapi.CreateSessionResponse, error) {
	if err := validateCreateRequest(request); err != nil {
		return agentapi.CreateSessionResponse{}, serviceError(
			ErrorInvalid, "invalid_session", err.Error(), err,
		)
	}
	sessionID, err := randomID()
	if err != nil {
		return agentapi.CreateSessionResponse{}, serviceError(
			ErrorInternal, "random_failed", "create agent session failed", err,
		)
	}
	path := filepath.Join(s.dataDir, "events", sessionID+activeLogSuffix)
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return agentapi.CreateSessionResponse{}, serviceError(
				ErrorUnavailable, "not_ready", "Koko agent runtime is stopping", nil,
			)
		}
		if _, exists := s.resources[request.ResourceSessionID]; exists {
			s.mu.Unlock()
			return agentapi.CreateSessionResponse{}, serviceError(
				ErrorConflict, "resource_session_exists",
				"resource session already has an agent session", nil,
			)
		}
		if len(s.sessions) < s.maxSessions {
			break
		}
		s.mu.Unlock()
		if s.reapIdleSessions(time.Now(), 1) == 0 {
			return agentapi.CreateSessionResponse{}, serviceError(
				ErrorUnavailable, "session_limit", "agent session limit reached", nil,
			)
		}
	}
	events, _, err := openEventLog(path, s.eventCapacity)
	if err != nil {
		s.mu.Unlock()
		return agentapi.CreateSessionResponse{}, serviceError(
			ErrorInternal, "event_store_failed", "create agent event store failed", err,
		)
	}
	session, err := newAgentSession(sessionID, request, principal, events, s.modelFactory)
	if err != nil {
		_ = events.close()
		_ = os.Remove(path)
		s.mu.Unlock()
		return agentapi.CreateSessionResponse{}, serviceError(
			ErrorInvalid, "session_create_failed", "create agent runtime failed", err,
		)
	}
	s.sessions[sessionID] = session
	s.resources[request.ResourceSessionID] = sessionID
	s.mu.Unlock()
	return agentapi.CreateSessionResponse{
		SessionID: sessionID, After: session.events.cursor(),
	}, nil
}

func (s *Service) UpdateApprovalMode(
	principal agentapi.Principal,
	resourceID, sessionID string,
	request agentapi.ApprovalModeRequest,
) (agentapi.ApprovalModeResponse, error) {
	session, err := s.session(principal, resourceID, sessionID, false)
	if err != nil {
		return agentapi.ApprovalModeResponse{}, err
	}
	response, err := session.updateApprovalMode(request)
	if err == nil {
		return response, nil
	}
	if errors.Is(err, errInvalidApprovalMode) {
		return agentapi.ApprovalModeResponse{}, serviceError(
			ErrorInvalid, "invalid_approval_mode", "approval mode is invalid", err,
		)
	}
	if session.isUnavailable() || errors.Is(err, errSessionUnavailable) {
		return agentapi.ApprovalModeResponse{}, sessionUnavailableError(err)
	}
	if errors.Is(err, errSessionBusy) || errors.Is(err, errSessionClosed) {
		return agentapi.ApprovalModeResponse{}, serviceError(
			ErrorConflict, "approval_mode_conflict",
			"approval mode cannot be changed now", err,
		)
	}
	return agentapi.ApprovalModeResponse{}, serviceError(
		ErrorInternal, "approval_mode_failed", "update approval mode failed", err,
	)
}

func (s *Service) HandleMessage(
	principal agentapi.Principal,
	resourceID, sessionID string,
	request agentapi.MessageRequest,
) (agentapi.MessageResponse, error) {
	session, err := s.session(principal, resourceID, sessionID, false)
	if err != nil {
		return agentapi.MessageResponse{}, err
	}
	response, err := session.handleMessage(request)
	if err == nil {
		return response, nil
	}
	if session.isUnavailable() || errors.Is(err, errSessionUnavailable) {
		return agentapi.MessageResponse{}, sessionUnavailableError(err)
	}
	return agentapi.MessageResponse{}, serviceError(
		ErrorInvalid, "message_rejected", err.Error(), err,
	)
}

func (s *Service) History(
	principal agentapi.Principal,
	resourceID, sessionID string,
	after uint64,
	limit int,
) (agentapi.HistoryResponse, error) {
	session, err := s.session(principal, resourceID, sessionID, false)
	if err != nil {
		return agentapi.HistoryResponse{}, err
	}
	events, more, err := session.events.history(after, limit)
	if err != nil {
		return agentapi.HistoryResponse{}, serviceError(
			ErrorInternal, "history_failed", "read agent history failed", err,
		)
	}
	next := after
	if len(events) > 0 {
		next = events[len(events)-1].Sequence
	}
	return agentapi.HistoryResponse{
		Events: events, NextCursor: next, HasMore: more,
	}, nil
}

func (s *Service) ResolveApproval(
	principal agentapi.Principal,
	resourceID, sessionID, approvalID string,
	request agentapi.ApprovalRequest,
) (agentapi.ApprovalResponse, error) {
	if !agentapi.ValidIdentifier(approvalID) {
		return agentapi.ApprovalResponse{}, serviceError(
			ErrorInvalid, "invalid_approval_id", "approval ID is invalid", nil,
		)
	}
	session, err := s.session(principal, resourceID, sessionID, false)
	if err != nil {
		return agentapi.ApprovalResponse{}, err
	}
	response, err := session.resolveApproval(approvalID, request)
	if err == nil {
		return response, nil
	}
	if session.isUnavailable() || errors.Is(err, errSessionUnavailable) {
		return agentapi.ApprovalResponse{}, sessionUnavailableError(err)
	}
	return agentapi.ApprovalResponse{}, serviceError(
		ErrorConflict, "approval_rejected", err.Error(), err,
	)
}

func (s *Service) AcceptToolResult(
	principal agentapi.Principal,
	resourceID, sessionID, toolCallID string,
	request agentapi.ToolResultRequest,
) (agentapi.ToolResultResponse, error) {
	if !agentapi.ValidIdentifier(toolCallID) {
		return agentapi.ToolResultResponse{}, serviceError(
			ErrorInvalid, "invalid_tool_call_id", "tool call ID is invalid", nil,
		)
	}
	if err := validateToolResult(toolCallID, request); err != nil {
		return agentapi.ToolResultResponse{}, serviceError(
			ErrorInvalid, "invalid_tool_result", err.Error(), err,
		)
	}
	session, err := s.session(principal, resourceID, sessionID, false)
	if err != nil {
		return agentapi.ToolResultResponse{}, err
	}
	response, err := session.acceptToolResult(request)
	if err == nil {
		return response, nil
	}
	if session.isUnavailable() || errors.Is(err, errSessionUnavailable) {
		return agentapi.ToolResultResponse{}, sessionUnavailableError(err)
	}
	return agentapi.ToolResultResponse{}, serviceError(
		ErrorConflict, "tool_result_rejected", err.Error(), err,
	)
}

func (s *Service) Cancel(
	principal agentapi.Principal,
	resourceID, sessionID string,
	request agentapi.CancelRequest,
) (agentapi.CancelResponse, error) {
	session, err := s.session(principal, resourceID, sessionID, false)
	if err != nil {
		return agentapi.CancelResponse{}, err
	}
	response, err := session.cancel(request)
	if err == nil {
		return response, nil
	}
	if session.isUnavailable() || errors.Is(err, errSessionUnavailable) {
		return agentapi.CancelResponse{}, sessionUnavailableError(err)
	}
	return agentapi.CancelResponse{}, serviceError(
		ErrorConflict, "cancel_rejected", err.Error(), err,
	)
}

func (s *Service) DeleteSession(
	principal agentapi.Principal,
	resourceID, sessionID string,
) error {
	session, err := s.session(principal, resourceID, sessionID, true)
	if err != nil {
		return err
	}
	wasUnavailable := session.isUnavailable()
	err = session.deleteWithArchive(s.archives)
	if err != nil && !wasUnavailable && session.isUnavailable() {
		wasUnavailable = true
		err = session.deleteWithArchive(s.archives)
	}
	if err != nil {
		if wasUnavailable {
			s.mu.Lock()
			s.degraded++
			s.mu.Unlock()
		}
		if errors.Is(err, errArchiveStoreFull) {
			return serviceError(
				ErrorStorage, "archive_quota", "agent archive quota reached", err,
			)
		}
		return serviceError(
			ErrorInternal, "session_delete_failed", "delete agent session failed", err,
		)
	}
	s.mu.Lock()
	delete(s.sessions, session.id)
	delete(s.resources, session.resourceID)
	hook := s.sessionRemoved
	s.mu.Unlock()
	if hook != nil {
		hook(session.principal, session.resourceID)
	}
	return nil
}

func (s *Service) SubscribeEvents(
	principal agentapi.Principal,
	resourceID, sessionID string,
	after uint64,
) (*EventSubscription, error) {
	session, err := s.session(principal, resourceID, sessionID, false)
	if err != nil {
		return nil, err
	}
	if _, _, expired := session.events.after(after); expired {
		return nil, serviceError(
			ErrorConflict, "cursor_expired", "event cursor is no longer available", nil,
		)
	}
	if !session.acquireEventStream() {
		return nil, serviceError(
			ErrorConflict, "event_stream_conflict", "an event stream is already active", nil,
		)
	}
	return &EventSubscription{session: session}, nil
}

func (s *Service) session(
	principal agentapi.Principal,
	resourceID, sessionID string,
	allowUnavailable bool,
) (*agentSession, error) {
	if !agentapi.ValidIdentifier(sessionID) {
		return nil, serviceError(
			ErrorNotFound, "session_not_found", "agent session not found", nil,
		)
	}
	if !agentapi.ValidIdentifier(resourceID) {
		return nil, serviceError(
			ErrorInvalid, "resource_session_required",
			"resource session header is required", nil,
		)
	}
	s.mu.RLock()
	session := s.sessions[sessionID]
	s.mu.RUnlock()
	if session == nil || !session.owns(principal, resourceID) {
		return nil, serviceError(
			ErrorNotFound, "session_not_found", "agent session not found", nil,
		)
	}
	session.touch()
	if !allowUnavailable && session.isUnavailable() {
		return nil, sessionUnavailableError(nil)
	}
	return session, nil
}

func sessionUnavailableError(cause error) error {
	return serviceError(
		ErrorUnavailable, "session_unavailable", "agent session is unavailable", cause,
	)
}

func validateToolResult(
	toolCallID string,
	request agentapi.ToolResultRequest,
) error {
	if request.JSONRPC != "2.0" || request.ID != toolCallID ||
		(len(request.Result) == 0) == (request.Error == nil) {
		return errors.New("tool result envelope is invalid")
	}
	if request.RunID == "" || request.Sequence == 0 || request.Status == "" {
		return errors.New("tool result binding is invalid")
	}
	switch request.Status {
	case "running", "success", "error", "cancelled", "timeout":
	default:
		return errors.New("tool result status is invalid")
	}
	if len(request.Result) > agentapi.MaxToolResultBytes ||
		(request.Error != nil && len(request.Error.Message) > 4096) {
		return errors.New("tool result exceeds the response limit")
	}
	if request.Error != nil {
		encoded, err := json.Marshal(request.Error)
		if err != nil || len(encoded) > agentapi.MaxToolErrorBytes {
			return errors.New("tool error exceeds the response limit")
		}
	}
	return nil
}

func (s *Service) notifySessionRemoved(principal agentapi.Principal, resourceID string) {
	s.mu.RLock()
	hook := s.sessionRemoved
	s.mu.RUnlock()
	if hook != nil {
		hook(principal, resourceID)
	}
}
