package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/identity"
	"github.com/jumpserver/koko/internal/kael/service"
	"go.uber.org/zap"
)

const principalKey = "kael.principal"

type Options struct {
	Service       *service.Service
	Authenticator *identity.CoreAuthenticator
	Origin        *identity.OriginVerifier
	Logger        *zap.Logger
}

type Server struct {
	service       *service.Service
	authenticator *identity.CoreAuthenticator
	origin        *identity.OriginVerifier
	logger        *zap.Logger
	engine        *gin.Engine
}

func New(options Options) (*Server, error) {
	if options.Service == nil || options.Authenticator == nil || options.Origin == nil {
		return nil, fmt.Errorf("api dependencies are incomplete")
	}
	if options.Logger == nil {
		options.Logger = zap.NewNop()
	}
	gin.SetMode(gin.ReleaseMode)
	server := &Server{service: options.Service, authenticator: options.Authenticator, origin: options.Origin, logger: options.Logger}
	engine := gin.New()
	// Handlers that pass Gin's context must still propagate request values,
	// cancellation and deadlines to the service layer.
	engine.ContextWithFallback = true
	engine.RedirectTrailingSlash = false
	engine.RedirectFixedPath = false
	engine.Use(server.requestID(), gin.Recovery())
	server.engine = engine
	server.routes()
	return server, nil
}

func (s *Server) Handler() http.Handler { return s.engine }

func (s *Server) routes() {
	s.engine.GET("/kael/health/live", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	s.engine.GET("/kael/health/startup", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	s.engine.GET("/kael/health/ready", s.ready)
	s.engine.GET("/kael/internal/metrics", s.metrics)
	s.engine.GET("/kael/openapi.json", s.openapi)

	api := s.engine.Group("/kael/api/v1")
	api.Use(s.authorize())
	api.GET("/bootstrap", func(c *gin.Context) { c.JSON(http.StatusOK, s.service.Bootstrap()) })
	api.GET("/assistants", s.assistants)
	api.GET("/runtime-profiles", s.assistants)
	api.GET("/conversations", s.listConversations)
	api.POST("/conversations", s.createConversation)
	api.GET("/conversations/:id", s.getConversation)
	api.PATCH("/conversations/:id", s.updateConversation)
	api.DELETE("/conversations/:id", s.deleteConversation)
	api.GET("/conversations/:id/messages", s.listMessages)
	api.POST("/conversations/:id/messages", s.createMessage)
	api.GET("/conversations/:id/runs", s.listRuns)
	api.GET("/conversations/:id/approvals", s.listApprovals)
	api.POST("/conversations/:id/branches", s.branch)
	api.POST("/messages/:id/regenerations", s.regenerate)
	api.POST("/artifacts", s.createArtifact)
	api.GET("/artifacts/:id", s.getArtifact)
	api.GET("/artifacts/:id/content", s.artifactContent)
	api.DELETE("/artifacts/:id", s.deleteArtifact)
	api.POST("/transcriptions", func(c *gin.Context) { s.writeError(c, s.service.UnsupportedTranscription()) })

	api.POST("/panel-sessions", s.createPanel)
	api.POST("/panel-sessions/:id/heartbeat", s.heartbeatPanel)
	api.POST("/panel-sessions/:id/resume", s.resumePanel)
	api.PATCH("/panel-sessions/:id/approval-mode", s.updateApprovalMode)
	api.DELETE("/panel-sessions/:id", s.closePanel)
	api.PUT("/panel-sessions/:id/context", s.updateContext)
	api.PUT("/panel-sessions/:id/registrations", s.replaceRegistrations)
	api.GET("/panel-sessions/:id/events", s.events)
	api.DELETE("/registrations/:id", s.revokeRegistration)

	api.POST("/runs", s.createRun)
	api.GET("/runs/:id", s.getRun)
	api.POST("/runs/:id/cancel", s.cancelRun)
	api.POST("/runs/:id/resume", s.resumeRun)
	api.POST("/tool-calls/:id/results", s.submitToolResult)
	api.GET("/approvals/:id", s.getApproval)
	api.POST("/approvals/:id/decisions", s.decideApproval)

	api.POST("/admin/platform-registry/refresh", s.refreshPlatformRegistry)
	api.GET("/admin/stats", s.adminStats)
}

func (s *Server) requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if requestID == "" || len(requestID) > 128 {
			requestID = uuid.NewString()
		}
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}

func (s *Server) authorize() gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := s.origin.Verify(c.Request); err != nil {
			s.writePublicError(c, http.StatusForbidden, "origin_forbidden", "request origin is not allowed", false)
			c.Abort()
			return
		}
		if err := identity.VerifyCSRF(c.Request); err != nil {
			s.writePublicError(c, http.StatusForbidden, "csrf_failed", "csrf validation failed", false)
			c.Abort()
			return
		}
		principal, err := s.authenticator.Authenticate(c.Request.Context(), c.Request)
		if err != nil {
			if errors.Is(err, identity.ErrUnauthenticated) {
				s.writePublicError(c, http.StatusUnauthorized, "unauthenticated", "authentication is required", false)
			} else {
				s.logger.Warn("identity provider unavailable", zap.String("request_id", c.Writer.Header().Get("X-Request-ID")), zap.Error(err))
				s.writePublicError(c, http.StatusServiceUnavailable, "identity_unavailable", "identity service is temporarily unavailable", true)
			}
			c.Abort()
			return
		}
		if !principal.IsSuperuser && !hasPermission(principal, "chat_ai.use_chatai") {
			s.writePublicError(c, http.StatusForbidden, "permission_denied", "chat AI permission is required", false)
			c.Abort()
			return
		}
		c.Set(principalKey, principal)
		c.Request = c.Request.WithContext(identity.WithCoreCredentials(c.Request.Context(), identity.CredentialsFromRequest(c.Request)))
		c.Next()
	}
}

func hasPermission(principal domain.Principal, required string) bool {
	for _, permission := range principal.Permissions {
		if permission == required {
			return true
		}
	}
	return false
}

func principal(c *gin.Context) domain.Principal {
	value, _ := c.Get(principalKey)
	principal, _ := value.(domain.Principal)
	return principal
}

func (s *Server) bind(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, domain.MaxContextBytes+1024*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if requestTooLarge(err) {
			s.writePublicError(c, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds the configured size limit", false)
			return false
		}
		s.writePublicError(c, http.StatusBadRequest, "invalid_json", "request body is invalid", false)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if requestTooLarge(err) {
			s.writePublicError(c, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds the configured size limit", false)
			return false
		}
		s.writePublicError(c, http.StatusBadRequest, "invalid_json", "request body must contain one JSON value", false)
		return false
	}
	return true
}

func requestTooLarge(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError) || errors.Is(err, multipart.ErrMessageTooLarge)
}

func pagination(c *gin.Context) (int, int) {
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	return offset, limit
}

func (s *Server) createConversation(c *gin.Context) {
	var request service.CreateConversationRequest
	if !s.bind(c, &request) {
		return
	}
	value, err := s.service.CreateConversation(c, principal(c), request)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.Header("Location", "/kael/api/v1/conversations/"+value.ID)
	c.JSON(http.StatusCreated, value)
}

func (s *Server) listConversations(c *gin.Context) {
	offset, limit := pagination(c)
	value, err := s.service.ListConversations(c, principal(c), c.Query("kind"), offset, limit)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) getConversation(c *gin.Context) {
	value, err := s.service.Conversation(c, principal(c), c.Param("id"))
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) updateConversation(c *gin.Context) {
	var request service.UpdateConversationRequest
	if !s.bind(c, &request) {
		return
	}
	value, err := s.service.UpdateConversation(c, principal(c), c.Param("id"), request)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) deleteConversation(c *gin.Context) {
	if err := s.service.DeleteConversation(c, principal(c), c.Param("id")); err != nil {
		s.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) createMessage(c *gin.Context) {
	var request service.CreateMessageRequest
	if !s.bind(c, &request) {
		return
	}
	value, duplicate, err := s.service.CreateMessage(c, principal(c), c.Param("id"), request)
	if err != nil {
		s.writeError(c, err)
		return
	}
	if duplicate {
		c.Header("Idempotent-Replayed", "true")
		c.JSON(http.StatusOK, value)
		return
	}
	c.Header("Location", "/kael/api/v1/conversations/"+c.Param("id")+"/messages")
	c.JSON(http.StatusCreated, value)
}

func (s *Server) listMessages(c *gin.Context) {
	offset, limit := pagination(c)
	value, err := s.service.ListMessages(c, principal(c), c.Param("id"), offset, limit)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) branch(c *gin.Context) {
	var request service.BranchRequest
	if !s.bind(c, &request) {
		return
	}
	value, err := s.service.Branch(c, principal(c), c.Param("id"), request)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.Header("Location", "/kael/api/v1/conversations/"+value.ID)
	c.JSON(http.StatusCreated, value)
}

func (s *Server) regenerate(c *gin.Context) {
	var request struct {
		PanelSessionID string `json:"panel_session_id"`
	}
	if !s.bind(c, &request) {
		return
	}
	value, err := s.service.Regenerate(c.Request.Context(), principal(c), c.Param("id"), request.PanelSessionID)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, value)
}

func (s *Server) createArtifact(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, s.service.MaxArtifactBytes()+1024*1024)
	file, err := c.FormFile("file")
	if err != nil {
		if requestTooLarge(err) {
			s.writePublicError(c, http.StatusRequestEntityTooLarge, "request_too_large", "multipart request exceeds the configured size limit", false)
			return
		}
		s.writePublicError(c, http.StatusBadRequest, "file_required", "artifact file is required", false)
		return
	}
	value, err := s.service.CreateArtifact(c, principal(c), file, c.PostForm("kind"))
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.Header("Location", "/kael/api/v1/artifacts/"+value.ID+"/content")
	c.JSON(http.StatusCreated, value)
}

func (s *Server) getArtifact(c *gin.Context) {
	artifact, _, err := s.service.Artifact(c, principal(c), c.Param("id"))
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, artifact)
}

func (s *Server) artifactContent(c *gin.Context) {
	artifact, path, err := s.service.Artifact(c, principal(c), c.Param("id"))
	if err != nil {
		s.writeError(c, err)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		s.writePublicError(c, http.StatusNotFound, "artifact_content_missing", "artifact content is unavailable", false)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		s.writePublicError(c, http.StatusNotFound, "artifact_content_missing", "artifact content is unavailable", false)
		return
	}
	disposition := "attachment"
	if artifact.Kind == "image" {
		disposition = "inline"
	}
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": artifact.Name}))
	c.Header("Content-Type", artifact.MediaType)
	c.Header("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Writer, c.Request, artifact.Name, stat.ModTime(), file)
}

func (s *Server) deleteArtifact(c *gin.Context) {
	if err := s.service.DeleteArtifact(c, principal(c), c.Param("id")); err != nil {
		s.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) createPanel(c *gin.Context) {
	var request service.CreatePanelRequest
	if !s.bind(c, &request) {
		return
	}
	value, err := s.service.CreatePanel(c, principal(c), request)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.Header("Location", "/kael/api/v1/panel-sessions/"+value.ID)
	c.JSON(http.StatusCreated, value)
}

func (s *Server) heartbeatPanel(c *gin.Context) {
	value, err := s.service.HeartbeatPanel(c, principal(c), c.Param("id"))
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) resumePanel(c *gin.Context) {
	var request service.ResumePanelRequest
	if !s.bind(c, &request) {
		return
	}
	value, err := s.service.ResumePanel(c, principal(c), c.Param("id"), request)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) updateApprovalMode(c *gin.Context) {
	var request service.UpdateApprovalModeRequest
	if !s.bind(c, &request) {
		return
	}
	value, err := s.service.UpdateApprovalMode(c, principal(c), c.Param("id"), request)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) closePanel(c *gin.Context) {
	if err := s.service.ClosePanel(c, principal(c), c.Param("id")); err != nil {
		s.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) updateContext(c *gin.Context) {
	var request service.UpdateContextRequest
	if !s.bind(c, &request) {
		return
	}
	value, err := s.service.UpdateContext(c, principal(c), c.Param("id"), request)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) replaceRegistrations(c *gin.Context) {
	var request service.ReplaceRegistrationsRequest
	if !s.bind(c, &request) {
		return
	}
	value, err := s.service.ReplaceRegistrations(c, principal(c), c.Param("id"), request)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) revokeRegistration(c *gin.Context) {
	if err := s.service.RevokeRegistration(c, principal(c), c.Param("id")); err != nil {
		s.writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) createRun(c *gin.Context) {
	var request service.CreateRunRequest
	if !s.bind(c, &request) {
		return
	}
	value, err := s.service.CreateRun(c.Request.Context(), principal(c), request)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.Header("Location", "/kael/api/v1/runs/"+value.ID)
	c.JSON(http.StatusAccepted, value)
}

func (s *Server) getRun(c *gin.Context) {
	value, err := s.service.Run(c, principal(c), c.Param("id"))
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) listRuns(c *gin.Context) {
	offset, limit := pagination(c)
	value, err := s.service.ListRuns(c, principal(c), c.Param("id"), offset, limit)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) cancelRun(c *gin.Context) {
	var request struct {
		Reason string `json:"reason"`
	}
	if c.Request.ContentLength != 0 && !s.bind(c, &request) {
		return
	}
	value, err := s.service.CancelRun(c, principal(c), c.Param("id"), request.Reason)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) resumeRun(c *gin.Context) {
	value, err := s.service.ResumeRun(c.Request.Context(), principal(c), c.Param("id"))
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, value)
}

func (s *Server) submitToolResult(c *gin.Context) {
	var request service.ToolResultRequest
	if !s.bind(c, &request) {
		return
	}
	value, duplicate, err := s.service.SubmitToolResult(c, principal(c), c.Param("id"), request)
	if err != nil {
		s.writeError(c, err)
		return
	}
	if duplicate {
		c.Header("Idempotent-Replayed", "true")
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) getApproval(c *gin.Context) {
	value, err := s.service.Approval(c, principal(c), c.Param("id"))
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) listApprovals(c *gin.Context) {
	offset, limit := pagination(c)
	value, err := s.service.ListApprovals(c, principal(c), c.Param("id"), offset, limit)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) decideApproval(c *gin.Context) {
	var request service.ApprovalDecisionRequest
	if !s.bind(c, &request) {
		return
	}
	value, duplicate, err := s.service.DecideApproval(c, principal(c), c.Param("id"), request)
	if err != nil {
		s.writeError(c, err)
		return
	}
	if duplicate {
		c.Header("Idempotent-Replayed", "true")
	}
	c.JSON(http.StatusOK, value)
}

func parseCursor(c *gin.Context) (uint64, error) {
	query, header := strings.TrimSpace(c.Query("after")), strings.TrimSpace(c.GetHeader("Last-Event-ID"))
	if query != "" && header != "" && query != header {
		return 0, fmt.Errorf("event cursors do not match")
	}
	value := query
	if value == "" {
		value = header
	}
	if value == "" {
		return 0, nil
	}
	return strconv.ParseUint(value, 10, 64)
}

func (s *Server) events(c *gin.Context) {
	after, err := parseCursor(c)
	if err != nil {
		s.writePublicError(c, http.StatusBadRequest, "invalid_cursor", "event cursor is invalid", false)
		return
	}
	if c.Query("once") == "true" {
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "256"))
		deliveries, expired, loadErr := s.service.Deliveries(c, principal(c), c.Param("id"), after, limit)
		if loadErr != nil {
			s.writeError(c, loadErr)
			return
		}
		if expired {
			s.writePublicError(c, http.StatusGone, "cursor_expired", "event cursor has expired", false)
			return
		}
		next := after
		if len(deliveries) > 0 {
			next = deliveries[len(deliveries)-1].Sequence
		}
		c.JSON(http.StatusOK, gin.H{"events": deliveries, "next_cursor": next, "has_more": len(deliveries) == limit})
		return
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		s.writePublicError(c, http.StatusInternalServerError, "stream_unsupported", "streaming is unavailable", true)
		return
	}
	notifications, unsubscribe := s.service.PanelEvents(c.Param("id"))
	defer unsubscribe()
	pending, expired, loadErr := s.service.Deliveries(c, principal(c), c.Param("id"), after, domain.MaxPageSize)
	if loadErr != nil {
		s.writeError(c, loadErr)
		return
	}
	if expired {
		s.writePublicError(c, http.StatusGone, "cursor_expired", "event cursor has expired", false)
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		for {
			deliveries := pending
			pending = nil
			if deliveries == nil {
				deliveries, expired, loadErr = s.service.Deliveries(c, principal(c), c.Param("id"), after, domain.MaxPageSize)
				if loadErr != nil || expired {
					return
				}
			}
			for _, delivery := range deliveries {
				encoded, marshalErr := json.Marshal(delivery)
				if marshalErr != nil {
					return
				}
				if _, err = fmt.Fprintf(c.Writer, "id: %d\nevent: %s\ndata: %s\n\n", delivery.Sequence, delivery.Type, encoded); err != nil {
					return
				}
				after = delivery.Sequence
			}
			if len(deliveries) > 0 {
				flusher.Flush()
			}
			if len(deliveries) < domain.MaxPageSize {
				break
			}
		}
		select {
		case <-c.Request.Context().Done():
			return
		case <-notifications:
		case timestamp := <-heartbeat.C:
			if _, err = fmt.Fprintf(c.Writer, ": heartbeat %d\n\n", timestamp.Unix()); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) assistants(c *gin.Context) {
	profiles := s.service.Profiles(principal(c))
	c.JSON(http.StatusOK, gin.H{"results": profiles, "count": len(profiles)})
}

func (s *Server) adminStats(c *gin.Context) {
	days := 30
	if raw := strings.TrimSpace(c.Query("days")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err == nil {
			days = min(365, max(1, parsed))
		}
	}
	value, err := s.service.AdminStats(c, principal(c), days)
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) refreshPlatformRegistry(c *gin.Context) {
	value, err := s.service.RefreshPlatformRegistry(c, principal(c))
	if err != nil {
		s.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

func (s *Server) ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := s.service.Ready(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) metrics(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	values, err := s.service.Metrics(ctx)
	if err != nil {
		c.String(http.StatusServiceUnavailable, "# kael metrics unavailable\n")
		return
	}
	c.Header("Content-Type", "text/plain; version=0.0.4")
	var output strings.Builder
	output.WriteString("# HELP kael_up Whether the Kael HTTP process is running.\n# TYPE kael_up gauge\nkael_up 1\n")
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&output, "kael_%s %d\n", key, values[key])
	}
	c.String(http.StatusOK, output.String())
}

func (s *Server) openapi(c *gin.Context) {
	paths := map[string]any{}
	for _, route := range s.engine.Routes() {
		if strings.HasPrefix(route.Path, "/kael/api/v1") {
			path := strings.TrimPrefix(strings.ReplaceAll(route.Path, ":id", "{id}"), "/kael/api/v1")
			if path == "" {
				path = "/"
			}
			entry, _ := paths[path].(map[string]any)
			if entry == nil {
				entry = map[string]any{}
				paths[path] = entry
			}
			entry[strings.ToLower(route.Method)] = map[string]any{"operationId": strings.ReplaceAll(strings.Trim(route.Path, "/"), "/", "_") + "_" + strings.ToLower(route.Method), "responses": map[string]any{"200": map[string]any{"description": "Success"}}}
		}
	}
	c.JSON(http.StatusOK, gin.H{"openapi": "3.1.0", "info": gin.H{"title": "Kael API", "version": domain.APIVersion}, "servers": []gin.H{{"url": "/kael/api/v1"}}, "paths": paths})
}

func (s *Server) writeError(c *gin.Context, err error) {
	var serviceErr *service.Error
	if !errors.As(err, &serviceErr) {
		s.logger.Error("api request failed", zap.String("request_id", c.Writer.Header().Get("X-Request-ID")), zap.Error(err))
		s.writePublicError(c, http.StatusInternalServerError, "internal_error", "request could not be completed", true)
		return
	}
	status := http.StatusInternalServerError
	switch serviceErr.Kind {
	case service.Invalid:
		status = http.StatusBadRequest
		if serviceErr.Code == "artifact_too_large" {
			status = http.StatusRequestEntityTooLarge
		}
	case service.NotFound:
		status = http.StatusNotFound
	case service.Conflict:
		status = http.StatusConflict
	case service.Forbidden:
		status = http.StatusForbidden
	case service.Unavailable:
		status = http.StatusServiceUnavailable
	case service.RateLimited:
		status = http.StatusTooManyRequests
	}
	s.writePublicError(c, status, serviceErr.Code, serviceErr.Detail, serviceErr.Retryable)
}

func (s *Server) writePublicError(c *gin.Context, status int, code, detail string, retryable bool) {
	c.JSON(status, domain.PublicError{Code: code, Detail: detail, Retryable: retryable, RequestID: c.Writer.Header().Get("X-Request-ID")})
}
