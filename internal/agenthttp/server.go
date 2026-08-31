package agenthttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jumpserver/koko/internal/agent"
	"github.com/jumpserver/koko/internal/agentapi"
	"github.com/jumpserver/koko/internal/agentauth"
)

type Options struct {
	Addr              string
	Service           *agent.Service
	Authenticator     agentauth.RequestAuthenticator
	OriginVerifier    agentauth.OriginVerifier
	CSRF              *agentauth.CSRFManager
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
}

type Server struct {
	http           *http.Server
	service        *agent.Service
	authenticator  agentauth.RequestAuthenticator
	originVerifier agentauth.OriginVerifier
	csrf           *agentauth.CSRFManager
	instanceID     string
}

func New(options Options) (*Server, error) {
	if strings.TrimSpace(options.Addr) == "" || options.Service == nil ||
		options.Authenticator == nil || options.OriginVerifier == nil {
		return nil, fmt.Errorf("Koko agent HTTP server dependencies are required")
	}
	if options.CSRF == nil {
		options.CSRF = agentauth.NewCSRFManager()
	}
	server := &Server{
		service: options.Service, authenticator: options.Authenticator,
		originVerifier: options.OriginVerifier, csrf: options.CSRF,
		instanceID: options.Service.Status().InstanceID,
	}
	server.http = &http.Server{
		Addr: options.Addr, Handler: server,
		ReadHeaderTimeout: options.ReadHeaderTimeout, IdleTimeout: options.IdleTimeout,
	}
	server.service.SetSessionRemovedHook(func(principal agentapi.Principal, resourceID string) {
		server.csrf.Delete(server.csrfSubject(principal, resourceID))
	})
	return server, nil
}

func (s *Server) Addr() string {
	if s == nil || s.http == nil {
		return ""
	}
	return s.http.Addr
}

func (s *Server) ListenAndServe() error {
	return s.http.ListenAndServe()
}

func (s *Server) BeginShutdown() error {
	return s.service.BeginShutdown()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

func (s *Server) Close() {
	s.service.Close()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if request.URL.Path == agentapi.HealthPath || request.URL.Path == agentapi.ReadyPath {
		s.handleStatus(w, request)
		return
	}
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set(
		"Vary",
		"Cookie, Authorization, X-JMS-ORG, "+agentapi.HeaderResourceSessionID,
	)
	s.setProtocolHeaders(w)
	if !s.verifyProtocol(w, request) {
		return
	}
	principal, ok := s.authorizeRequest(w, request)
	if !ok {
		return
	}
	if request.URL.Path == agentapi.BootstrapPath {
		if request.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		s.bootstrap(w, request, principal)
		return
	}
	if request.URL.Path == agentapi.SessionsPath {
		if request.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		if !s.verifyCSRF(w, request, principal) {
			return
		}
		s.createSession(w, request, principal)
		return
	}
	segments, ok := sessionRoute(request.URL.Path)
	if !ok || len(segments) == 0 {
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	if request.Method == http.MethodPost || request.Method == http.MethodDelete {
		if !s.verifyCSRF(w, request, principal) {
			return
		}
	}
	s.routeSession(w, request, principal, segments[0], segments[1:])
}

func (s *Server) bootstrap(
	w http.ResponseWriter,
	request *http.Request,
	principal agentapi.Principal,
) {
	resourceID := strings.TrimSpace(request.Header.Get(agentapi.HeaderResourceSessionID))
	if !agentapi.ValidIdentifier(resourceID) {
		writeError(w, http.StatusBadRequest, "resource_session_required", "resource session header is required")
		return
	}
	subject := s.csrfSubject(principal, resourceID)
	current := strings.TrimSpace(request.Header.Get(agentapi.HeaderCSRFToken))
	var (
		token agentauth.CSRFToken
		err   error
	)
	if current == "" {
		token, err = s.csrf.Issue(subject)
	} else {
		token, err = s.csrf.Refresh(subject, current)
	}
	if err != nil {
		if errors.Is(err, agentauth.ErrCSRFCapacity) {
			writeError(w, http.StatusServiceUnavailable, "csrf_capacity", "CSRF subject capacity is exhausted")
			return
		}
		writeError(w, http.StatusForbidden, "csrf_rejected", "CSRF token is invalid or expired")
		return
	}
	state, err := s.service.Bootstrap(principal, resourceID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agentapi.BootstrapResponse{
		CSRFToken: token.Value, ExpiresAt: token.ExpiresAt.Unix(),
		RefreshAt: token.RefreshAt.Unix(), InstanceID: s.instanceID,
		ProtocolVersion: agentapi.ProtocolVersion, CapabilityVersion: agentapi.CapabilityVersion,
		SessionID: state.SessionID, Cursor: state.Cursor,
		ContextDigest: state.ContextDigest, ToolsetDigest: state.ToolsetDigest,
	})
}

func (s *Server) createSession(
	w http.ResponseWriter,
	request *http.Request,
	principal agentapi.Principal,
) {
	var body agentapi.CreateSessionRequest
	if !decodeJSON(w, request, &body) {
		return
	}
	resourceID := strings.TrimSpace(request.Header.Get(agentapi.HeaderResourceSessionID))
	if resourceID != body.ResourceSessionID {
		writeError(w, http.StatusBadRequest, "resource_session_mismatch", "resource session header does not match body")
		return
	}
	response, err := s.service.CreateSession(principal, body)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) routeSession(
	w http.ResponseWriter,
	request *http.Request,
	principal agentapi.Principal,
	sessionID string,
	action []string,
) {
	resourceID := strings.TrimSpace(request.Header.Get(agentapi.HeaderResourceSessionID))
	switch {
	case len(action) == 0 && request.Method == http.MethodDelete:
		err := s.service.DeleteSession(principal, resourceID, sessionID)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, agentapi.Acknowledgement{OK: true})
	case len(action) == 1 && action[0] == "messages" && request.Method == http.MethodPost:
		s.handleMessage(w, request, principal, resourceID, sessionID)
	case len(action) == 1 && action[0] == "approval-mode" && request.Method == http.MethodPost:
		s.handleApprovalMode(w, request, principal, resourceID, sessionID)
	case len(action) == 1 && action[0] == "events" && request.Method == http.MethodGet:
		s.streamEvents(w, request, principal, resourceID, sessionID)
	case len(action) == 1 && action[0] == "history" && request.Method == http.MethodGet:
		s.history(w, request, principal, resourceID, sessionID)
	case len(action) == 1 && action[0] == "cancel" && request.Method == http.MethodPost:
		s.cancel(w, request, principal, resourceID, sessionID)
	case len(action) == 2 && action[0] == "approvals" && request.Method == http.MethodPost:
		s.approval(w, request, principal, resourceID, sessionID, action[1])
	case len(action) == 2 && action[0] == "tool-results" && request.Method == http.MethodPost:
		s.toolResult(w, request, principal, resourceID, sessionID, action[1])
	default:
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
	}
}

func (s *Server) handleApprovalMode(
	w http.ResponseWriter,
	request *http.Request,
	principal agentapi.Principal,
	resourceID, sessionID string,
) {
	var body agentapi.ApprovalModeRequest
	if !decodeJSON(w, request, &body) {
		return
	}
	response, err := s.service.UpdateApprovalMode(principal, resourceID, sessionID, body)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleMessage(
	w http.ResponseWriter,
	request *http.Request,
	principal agentapi.Principal,
	resourceID, sessionID string,
) {
	var body agentapi.MessageRequest
	if !decodeJSON(w, request, &body) {
		return
	}
	response, err := s.service.HandleMessage(principal, resourceID, sessionID, body)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (s *Server) history(
	w http.ResponseWriter,
	request *http.Request,
	principal agentapi.Principal,
	resourceID, sessionID string,
) {
	after, err := parseAfter(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_after", "history cursor is invalid")
		return
	}
	limit := agentapi.MaxHistoryLimit
	if value := strings.TrimSpace(request.URL.Query().Get("limit")); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit <= 0 || limit > agentapi.MaxHistoryLimit {
			writeError(w, http.StatusBadRequest, "invalid_limit", "history limit is invalid")
			return
		}
	}
	response, err := s.service.History(principal, resourceID, sessionID, after, limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) approval(
	w http.ResponseWriter,
	request *http.Request,
	principal agentapi.Principal,
	resourceID, sessionID, approvalID string,
) {
	var body agentapi.ApprovalRequest
	if !decodeJSON(w, request, &body) {
		return
	}
	response, err := s.service.ResolveApproval(principal, resourceID, sessionID, approvalID, body)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) toolResult(
	w http.ResponseWriter,
	request *http.Request,
	principal agentapi.Principal,
	resourceID, sessionID, toolCallID string,
) {
	var body agentapi.ToolResultRequest
	if !decodeJSON(w, request, &body) {
		return
	}
	response, err := s.service.AcceptToolResult(principal, resourceID, sessionID, toolCallID, body)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) cancel(
	w http.ResponseWriter,
	request *http.Request,
	principal agentapi.Principal,
	resourceID, sessionID string,
) {
	var body agentapi.CancelRequest
	if !decodeJSON(w, request, &body) {
		return
	}
	response, err := s.service.Cancel(principal, resourceID, sessionID, body)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) streamEvents(
	w http.ResponseWriter,
	request *http.Request,
	principal agentapi.Principal,
	resourceID, sessionID string,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream_unsupported", "event streaming is unavailable")
		return
	}
	after, err := parseAfter(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_after", "event cursor is invalid")
		return
	}
	subscription, err := s.service.SubscribeEvents(principal, resourceID, sessionID, after)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	defer subscription.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		events, notify, expired := subscription.After(after)
		if expired {
			return
		}
		for _, event := range events {
			if err = writeSSE(w, event); err != nil {
				return
			}
			after = event.Sequence
		}
		if len(events) > 0 {
			flusher.Flush()
			continue
		}
		select {
		case <-request.Context().Done():
			return
		case <-notify:
		case <-heartbeat.C:
			if _, err = io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}
	state := s.service.Status()
	status := "ok"
	code := http.StatusOK
	if request.URL.Path == agentapi.ReadyPath {
		if state.Closed {
			status = "not_ready"
			code = http.StatusServiceUnavailable
		} else {
			status = "ready"
		}
	} else if state.DegradedSessions > 0 {
		status = "degraded"
	}
	if !state.Closed && state.DegradedSessions > 0 {
		status = "degraded"
	}
	writeJSON(w, code, agentapi.HealthResponse{
		Status: status, InstanceID: state.InstanceID,
		DegradedSessions: state.DegradedSessions,
	})
}

func (s *Server) authorizeRequest(
	w http.ResponseWriter,
	request *http.Request,
) (agentapi.Principal, bool) {
	if err := s.originVerifier.VerifyOrigin(request); err != nil {
		writeError(w, http.StatusForbidden, "origin_rejected", "request origin is not allowed")
		return agentapi.Principal{}, false
	}
	principal, err := s.authenticator.Authenticate(request.Context(), request)
	if err != nil || principal.UserID == "" || principal.OrganizationID == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication failed")
		return agentapi.Principal{}, false
	}
	return principal, true
}

func (s *Server) verifyCSRF(
	w http.ResponseWriter,
	request *http.Request,
	principal agentapi.Principal,
) bool {
	resourceID := strings.TrimSpace(request.Header.Get(agentapi.HeaderResourceSessionID))
	if !agentapi.ValidIdentifier(resourceID) {
		writeError(w, http.StatusBadRequest, "resource_session_required", "resource session header is required")
		return false
	}
	token := strings.TrimSpace(request.Header.Get(agentapi.HeaderCSRFToken))
	if token == "" || s.csrf.Validate(s.csrfSubject(principal, resourceID), token) != nil {
		writeError(w, http.StatusForbidden, "csrf_rejected", "CSRF token is invalid or expired")
		return false
	}
	return true
}

func (s *Server) verifyProtocol(w http.ResponseWriter, request *http.Request) bool {
	if request.Header.Get(agentapi.HeaderProtocolVersion) != agentapi.ProtocolVersion ||
		request.Header.Get(agentapi.HeaderCapabilityVersion) != agentapi.CapabilityVersion {
		writeError(w, http.StatusUpgradeRequired, "protocol_mismatch", "agent protocol or capability version is unsupported")
		return false
	}
	return true
}

func (s *Server) setProtocolHeaders(w http.ResponseWriter) {
	w.Header().Set(agentapi.HeaderProtocolVersion, agentapi.ProtocolVersion)
	w.Header().Set(agentapi.HeaderCapabilityVersion, agentapi.CapabilityVersion)
}

func (s *Server) csrfSubject(principal agentapi.Principal, resourceID string) string {
	return strings.Join([]string{
		s.instanceID, principal.OrganizationID, principal.UserID, resourceID,
	}, "\x00")
}

func sessionRoute(path string) ([]string, bool) {
	if !strings.HasPrefix(path, agentapi.SessionsPath) {
		return nil, false
	}
	value := strings.TrimPrefix(path, agentapi.SessionsPath)
	if value == "" || strings.HasSuffix(value, "/") {
		return nil, false
	}
	parts := strings.Split(value, "/")
	for index := range parts {
		decoded, err := url.PathUnescape(parts[index])
		if err != nil || decoded == "" || strings.Contains(decoded, "/") {
			return nil, false
		}
		parts[index] = decoded
	}
	return parts, true
}

func decodeJSON(w http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(w, request.Body, agentapi.MaxRequestBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_json", "request body is invalid")
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body has trailing data")
		return false
	}
	return true
}

func parseAfter(request *http.Request) (uint64, error) {
	value := strings.TrimSpace(request.URL.Query().Get("after"))
	if value == "" {
		value = strings.TrimSpace(request.Header.Get("Last-Event-ID"))
	}
	if value == "" {
		return 0, nil
	}
	return strconv.ParseUint(value, 10, 64)
}

func writeSSE(w io.Writer, event agentapi.Event) error {
	value, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Type, value)
	return err
}

func writeServiceError(w http.ResponseWriter, err error) {
	var serviceErr *agent.ServiceError
	if !errors.As(err, &serviceErr) {
		writeError(w, http.StatusInternalServerError, "internal_error", "agent service failed")
		return
	}
	status := http.StatusInternalServerError
	switch serviceErr.Kind {
	case agent.ErrorInvalid:
		status = http.StatusBadRequest
	case agent.ErrorNotFound:
		status = http.StatusNotFound
	case agent.ErrorConflict:
		status = http.StatusConflict
	case agent.ErrorUnavailable:
		status = http.StatusServiceUnavailable
	case agent.ErrorStorage:
		status = http.StatusInsufficientStorage
	case agent.ErrorInternal:
	}
	writeError(w, status, serviceErr.Code, serviceErr.Message)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, agentapi.Error{Code: code, Message: message})
}

func writeMethodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}
