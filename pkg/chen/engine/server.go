package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

const PathPrefix = "/koko/chen"
const apiPath = PathPrefix + "/api/v1"

func New(provider core.Provider, opts Options) *Server {
	if opts.MaxRows <= 0 {
		opts.MaxRows = 10000
	}
	if opts.TransactionIdleTimeout == 0 {
		opts.TransactionIdleTimeout = 5 * time.Minute
	}
	ctx, cancel := context.WithCancel(context.Background())
	v := &Server{provider: provider, opts: opts, sessions: make(map[[32]byte]*session), creations: make(map[[32]byte]*creation), ctx: ctx, cancel: cancel, shutdownDone: make(chan struct{}), catalogSlots: make(chan struct{}, 16), exportSlots: make(chan struct{}, 4), downloads: make(map[[32]byte]downloadTicket)}
	v.exportDir, v.exportLock, v.exportInitErr = openExportStore(opts.StateDir)
	return v
}
func (v *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PathPrefix+"/ready", v.ready)
	mux.HandleFunc("GET "+PathPrefix+"/healthy", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET "+apiPath+"/capabilities", func(w http.ResponseWriter, r *http.Request) {
		mode := "managed"
		if v.opts.SingleMode {
			mode = "single"
		}
		writeJSON(w, 200, map[string]any{"version": 1, "mode": mode, "protocols": supportedProtocols(), "transport": "http+sse", "features": []string{"query", "cancel", "completion", "audit", "grid_edit", "csv_import", "copy_sql"}})
	})
	mux.HandleFunc("POST "+apiPath+"/sessions", v.createSession)
	mux.HandleFunc("GET "+apiPath+"/sessions/{sid}", v.sessionStatus)
	mux.HandleFunc("DELETE "+apiPath+"/sessions/{sid}", v.deleteSession)
	mux.HandleFunc("GET "+apiPath+"/sessions/{sid}/events", v.events)
	mux.HandleFunc("GET "+apiPath+"/sessions/{sid}/ai/manifest", v.aiManifestHTTP)
	mux.HandleFunc("POST "+apiPath+"/sessions/{sid}/ai/messages", v.aiMessages)
	mux.HandleFunc("GET "+apiPath+"/sessions/{sid}/catalog/databases", v.catalogDatabases)
	mux.HandleFunc("GET "+apiPath+"/sessions/{sid}/catalog/schemas", v.catalogSchemas)
	mux.HandleFunc("GET "+apiPath+"/sessions/{sid}/catalog/objects", v.catalogObjects)
	mux.HandleFunc("GET "+apiPath+"/sessions/{sid}/catalog/columns", v.catalogColumns)
	mux.HandleFunc("GET "+apiPath+"/sessions/{sid}/catalog/inspect", v.catalogInspect)
	mux.HandleFunc("GET "+apiPath+"/sessions/{sid}/catalog/ddl", v.catalogDDL)
	mux.HandleFunc("GET "+apiPath+"/sessions/{sid}/catalog/overview", v.catalogOverview)
	mux.HandleFunc("GET "+apiPath+"/sessions/{sid}/catalog/diagram", v.catalogDiagram)
	mux.HandleFunc("POST "+apiPath+"/sessions/{sid}/consoles", v.createConsole)
	mux.HandleFunc("DELETE "+apiPath+"/consoles/{cid}", v.deleteConsole)
	mux.HandleFunc("GET "+apiPath+"/consoles/{cid}/transaction-state", v.consoleTransactionState)
	mux.HandleFunc("POST "+apiPath+"/consoles/{cid}/executions", v.submit)
	mux.HandleFunc("POST "+apiPath+"/consoles/{cid}/browse", v.browseTable)
	mux.HandleFunc("GET "+apiPath+"/consoles/{cid}/results/{rid}/edit", v.editMetadata)
	mux.HandleFunc("POST "+apiPath+"/consoles/{cid}/results/{rid}/copy-sql", v.copyResultSQL)
	mux.HandleFunc("POST "+apiPath+"/consoles/{cid}/results/{rid}/edits/preview", v.previewEdits)
	mux.HandleFunc("POST "+apiPath+"/consoles/{cid}/edits/{pid}/apply", v.applyEdits)
	mux.HandleFunc("POST "+apiPath+"/consoles/{cid}/exports", v.submitExport)
	mux.HandleFunc("POST "+apiPath+"/results/{rid}/exports", v.createCachedExport)
	mux.HandleFunc("GET "+apiPath+"/exports/{xid}", v.exportStatus)
	mux.HandleFunc("POST "+apiPath+"/exports/{xid}/cancel", v.cancelExport)
	mux.HandleFunc("DELETE "+apiPath+"/exports/{xid}", v.deleteExport)
	mux.HandleFunc("POST "+apiPath+"/exports/{xid}/download-ticket", v.exportDownloadTicket)
	mux.HandleFunc("POST "+apiPath+"/downloads", v.downloadExport)
	mux.HandleFunc("GET "+apiPath+"/executions/{eid}", v.executionStatus)
	mux.HandleFunc("GET "+apiPath+"/executions/{eid}/plan", v.executionPlan)
	mux.HandleFunc("POST "+apiPath+"/executions/{eid}/cancel", v.cancelExecution)
	mux.HandleFunc("POST "+apiPath+"/executions/{eid}/confirm", v.confirmExecution)
	mux.HandleFunc("GET "+apiPath+"/results/{rid}/rows", v.resultRows)
	mux.HandleFunc("DELETE "+apiPath+"/results/{rid}", v.deleteResult)
	mux.HandleFunc("POST "+apiPath+"/consoles/{cid}/query-page", v.queryPage)
	mux.HandleFunc("POST "+apiPath+"/consoles/{cid}/explain", v.prepareExplain)
	mux.HandleFunc("GET "+apiPath+"/consoles/{cid}/contexts", v.consoleContexts)
	mux.HandleFunc("GET "+apiPath+"/consoles/{cid}/relations", v.relations)
	mux.HandleFunc("GET "+apiPath+"/consoles/{cid}/columns", v.columns)
	mux.HandleFunc("POST "+apiPath+"/consoles/{cid}/completion", v.completion)
	mux.HandleFunc("POST "+apiPath+"/consoles/{cid}/analysis", v.analyzeDocument)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !v.allowCORS(w, r) {
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" && r.Header.Get("Origin") == "" {
			fail(w, 403, "origin_denied", "Cross-site request denied")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// Readiness is separate from Docker liveness. Probes never close sessions,
// change control state or stop the process.
func (v *Server) ready(w http.ResponseWriter, r *http.Request) {
	ready := v.ctx.Err() == nil && v.exportInitErr == nil
	if ready && !v.opts.SingleMode {
		probe, ok := v.provider.(interface{ Ready(context.Context) error })
		ready = ok && probe.Ready(r.Context()) == nil
	}
	if !ready || v.ctx.Err() != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// CORS grants browser access only to the same host or an explicitly trusted
// origin. Session tokens still authenticate every private API request.
func (v *Server) allowCORS(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Add("Vary", "Origin")
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if !v.allowedOrigin(origin, r.Host) {
		fail(w, 403, "origin_denied", "Request origin is not trusted")
		return false
	}
	// Same-origin requests need no CORS headers. In particular, do not replace
	// a gateway's public CORS origin with its rewritten upstream Origin.
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if origin != scheme+"://"+r.Host && slices.Contains(v.opts.AllowedOrigins, origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, X-Chen-Result-Truncated")
	}
	if r.Method != http.MethodOptions || r.Header.Get("Access-Control-Request-Method") == "" {
		return true
	}
	w.Header().Add("Vary", "Access-Control-Request-Method")
	w.Header().Add("Vary", "Access-Control-Request-Headers")
	switch r.Header.Get("Access-Control-Request-Method") {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodDelete:
	default:
		fail(w, 403, "cors_method_denied", "Request method is not allowed")
		return false
	}
	for _, header := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
		switch strings.ToLower(strings.TrimSpace(header)) {
		case "", "accept", "authorization", "content-type", "token", "idempotency-key", "last-event-id", "x-csrftoken", "x-jms-org":
		default:
			fail(w, 403, "cors_header_denied", "Request header is not allowed")
			return false
		}
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, Token, Idempotency-Key, Last-Event-ID, X-CSRFToken, X-JMS-ORG")
	w.WriteHeader(http.StatusNoContent)
	return false
}

func (v *Server) allowedOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if u.Host == host {
		return true
	}
	for _, trusted := range v.opts.AllowedOrigins {
		if origin == trusted {
			return true
		}
	}
	return false
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": APIError{code, message}})
}

const maxJSONRequestBytes = 6*sqlservice.MaxDocumentCodeUnits + 8192

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(w, 415, "json_required", "JSON request required")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONRequestBytes)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		fail(w, 400, "invalid_request", "Invalid JSON request")
		return false
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		fail(w, 400, "invalid_request", "Trailing JSON is not allowed")
		return false
	}
	return true
}
func (v *Server) auth(w http.ResponseWriter, r *http.Request, allowLocked bool) *session {
	token := r.Header.Get("token")
	if a := r.Header.Get("Authorization"); a != "" {
		parts := strings.Split(a, " ")
		if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" || (token != "" && token != parts[1]) {
			fail(w, 401, "unauthorized", "Invalid session authorization")
			return nil
		}
		token = parts[1]
	}
	if token == "" || len(token) > 4096 {
		fail(w, 401, "unauthorized", "Session token required")
		return nil
	}
	v.mu.RLock()
	s := v.sessions[tokenKey(token)]
	v.mu.RUnlock()
	if s == nil {
		fail(w, 401, "unauthorized", "Session is unavailable")
		return nil
	}
	if sid := r.PathValue("sid"); sid != "" && sid != s.id {
		fail(w, 404, "not_found", "Resource not found")
		return nil
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || s.ctx.Err() != nil {
		fail(w, 410, "session_closed", "Session is closed")
		return nil
	}
	if err := s.grant.Check(); err != nil && (!allowLocked || !isSessionPaused(err)) {
		fail(w, 403, "session_unavailable", err.Error())
		return nil
	}
	return s
}
func (v *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token     string `json:"connect_token"`
		Selector  string `json:"selector"`
		RequestID string `json:"request_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if v.opts.SingleMode {
		if req.Token != "" || req.Selector == "" || len(req.Selector) > 128 || strings.TrimSpace(req.Selector) != req.Selector || strings.ContainsAny(req.Selector, "\x00\r\n") {
			fail(w, 400, "invalid_selector", "Single mode requires a configured selector and no connection token")
			return
		}
	} else if req.Selector != "" || len(req.Token) == 0 || len(req.Token) > 4096 {
		fail(w, 400, "invalid_token", "Connection token required")
		return
	}
	if header := r.Header.Get("Idempotency-Key"); header != "" {
		if req.RequestID != "" && req.RequestID != header {
			fail(w, 400, "request_id_conflict", "Request body and idempotency header disagree")
			return
		}
		req.RequestID = header
	}
	if !validCreateRequestID(req.RequestID) {
		fail(w, 400, "request_id_required", "A stable random request_id of 16 to 128 characters is required")
		return
	}
	requestKey := sha256.Sum256([]byte(req.RequestID))
	key := sha256.Sum256([]byte(req.Token))
	credential := req.Token
	if v.opts.SingleMode {
		// Selectors are reusable names, not one-use grants. Only retries from the
		// same browser attempt share a session; separate tabs get separate state.
		credential = req.Selector
		key = sha256.Sum256([]byte(req.Selector + "\x00" + req.RequestID))
	}
	v.mu.Lock()
	v.pruneCreationsLocked(time.Now())
	c := v.creations[key]
	if c != nil && c.requestKey != requestKey {
		v.mu.Unlock()
		fail(w, 409, "connection_token_claimed", "This connection token belongs to another connection attempt")
		return
	}
	if c == nil {
		if v.ctx.Err() != nil {
			v.mu.Unlock()
			fail(w, 503, "server_stopping", "Server is stopping")
			return
		}
		// Include every in-flight attempt in the limit, not only published
		// sessions, so concurrent token exchanges cannot oversubscribe capacity.
		pending := 0
		for _, attempt := range v.creations {
			select {
			case <-attempt.done:
			default:
				pending++
			}
		}
		if v.opts.MaxSessions > 0 && len(v.sessions)+pending >= v.opts.MaxSessions {
			v.mu.Unlock()
			fail(w, 429, "session_limit", "Session capacity reached")
			return
		}
		c = &creation{done: make(chan struct{}), requestKey: requestKey}
		v.creations[key] = c
		// Creation owns its deadline: a browser disconnect must not lose a consumed
		// connection token. Retrying this same token observes this same attempt.
		token, remoteAddr := credential, r.RemoteAddr
		if !v.opts.SingleMode {
			// Managed requests use the client address forwarded by JumpServer's web proxy.
			forwarded, _, _ := strings.Cut(r.Header.Get("X-Forwarded-For"), ",")
			if ip := strings.TrimSpace(forwarded); net.ParseIP(ip) != nil {
				remoteAddr = ip
			}
		}
		v.creating.Add(1)
		go func() {
			defer v.creating.Done()
			ctx, cancel := context.WithTimeout(v.ctx, 30*time.Second)
			defer cancel()
			g, err := v.provider.Open(ctx, token, remoteAddr)
			if err != nil {
				v.finishCreation(key, c, "", nil, authorizationError(err))
				if errors.Is(err, core.ErrControlNotReady) {
					// The provider rejects this condition before exchanging the one-use
					// token. A retry can safely attempt authentication after recovery.
					v.mu.Lock()
					if v.creations[key] == c {
						delete(v.creations, key)
					}
					v.mu.Unlock()
				}
				return
			}
			poolSize := 0
			if v.opts.MaxConsoles > 0 {
				poolSize = v.opts.MaxConsoles + 2
			}
			db, err := openDatabase(g.Config, poolSize)
			if err == nil {
				err = db.PingContext(ctx)
			}
			if err == nil {
				err = v.provider.Connected(ctx, g)
			}
			if err == nil {
				err = ctx.Err()
			}
			if err == nil {
				err = g.Check()
			}
			if err != nil {
				if db != nil {
					_ = db.Close()
				}
				cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
				_ = v.provider.Close(cleanup, g)
				stop()
				failure := safeConnectionError(err, "Database connection could not be established")
				if errors.Is(err, core.ErrAuditDisk) {
					failure = &APIError{"audit_persistence_failed", "Audit storage is unavailable; SQL was not sent"}
				}
				v.finishCreation(key, c, "", nil, failure)
				return
			}
			sctx, scancel := context.WithCancel(v.ctx)
			s := &session{id: newID("ses_"), grant: g, db: db, ctx: sctx, cancel: scancel, consoles: map[string]*console{}, executions: map[string]*execution{}, results: map[string]*Result{}, subscribers: map[uint64]chan Event{}}
			if !v.finishCreation(key, c, newID(""), s, nil) {
				v.closeSession(s, "server_shutdown")
				return
			}
			go v.monitor(s)
		}()
	}
	v.mu.Unlock()
	select {
	case <-r.Context().Done():
		return
	case <-c.done:
	}
	v.mu.RLock()
	err, token, s := c.err, c.token, c.session
	v.mu.RUnlock()
	if err != nil {
		api := authorizationError(err)
		status := 403
		switch api.Code {
		case "session_closed":
			status = 410
		case "connection_failed", "database_tls_error", "token_exchange_failed":
			status = 502
		case "server_stopping", "audit_persistence_failed", "session_control_not_ready", "session_control_unavailable":
			status = 503
		}
		fail(w, status, api.Code, api.Message)
		return
	}
	if s == nil {
		fail(w, 410, "session_closed", "Session is closed")
		return
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed || s.ctx.Err() != nil {
		fail(w, 410, "session_closed", "Session is closed")
		return
	}
	if err := s.grant.Check(); err != nil && !isSessionPaused(err) {
		fail(w, 403, "session_unavailable", err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"token": token, "session": s.view()})
}

const creationRetention = 5 * time.Minute

func validCreateRequestID(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for i := range value {
		if value[i] < 0x21 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

// All creation fields are published and read under v.mu. The completion channel
// remains useful for request cancellation without making close/update races.
func (v *Server) finishCreation(key [32]byte, c *creation, token string, s *session, err error) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err == nil && v.ctx.Err() != nil {
		err = &APIError{"server_stopping", "Server is stopping"}
		s = nil
		token = ""
	}
	c.err, c.session, c.token = err, s, token
	if err != nil {
		c.expiresAt = time.Now().Add(creationRetention)
	} else {
		v.sessions[tokenKey(token)] = s
	}
	close(c.done)
	return err == nil
}

func (v *Server) pruneCreationsLocked(now time.Time) {
	for key, c := range v.creations {
		if !c.expiresAt.IsZero() && !now.Before(c.expiresAt) {
			delete(v.creations, key)
		}
	}
}

// Only explicitly public, static policy diagnostics are returned. RPC and
// driver errors may contain credentials and must never cross the HTTP boundary.
func authorizationError(err error) *APIError {
	var api *APIError
	if errors.As(err, &api) {
		return api
	}
	if errors.Is(err, core.ErrClosed) {
		return &APIError{"session_closed", "Session is closed"}
	}
	if errors.Is(err, core.ErrAuditDisk) {
		return &APIError{"audit_persistence_failed", "Audit storage is unavailable"}
	}
	code := err.Error()
	public := map[string]string{
		"selector_not_found":                            "No database is configured for this selector",
		"connection_token_required":                     "A Core connection token is required",
		"token_exchange_failed":                         "Connection authorization service is unavailable",
		"connection_token_rejected":                     "The Core connection token was rejected",
		"connect_permission_denied":                     "Connection permission is denied",
		"permission_expired":                            "Connection permission has expired",
		"face_monitor_join_failed":                      "Core could not attach this session to face monitoring",
		"unsupported_database_credential_type":          "This credential type requires the legacy workspace",
		"unsupported_database_for_go_v1":                "This database is not supported by the Go workspace",
		"unsupported_command_acl_regex_use_legacy_chen": "This command policy requires the legacy workspace",
		"unsupported_command_acl_regex":                 "This command policy contains an unsupported regular expression",
		"unsupported_command_acl_action":                "This command policy requires the legacy workspace",
		"unsupported_empty_command_acl":                 "This command policy cannot be enforced; use the legacy workspace",
		"unsupported_empty_command_acl_pattern":         "This command policy cannot be enforced; use the legacy workspace",
		"unsupported_empty_data_masking_rule":           "This masking policy cannot be enforced; use the legacy workspace",
		"unsupported_empty_data_masking_pattern":        "This masking policy cannot be enforced; use the legacy workspace",
		"incomplete_connection_authorization":           "Core returned incomplete connection authorization",
		"incomplete_connection_identity":                "Core returned incomplete connection identity",
		"ambiguous_connection_protocol":                 "Core authorization must select one database protocol",
		"invalid_authorized_database_port":              "Core returned an invalid database port",
		"invalid_session_time_limit":                    "Core returned invalid session time limits",
		"core_session_create_failed":                    "Core audit session creation failed",
		"core_session_create_rejected":                  "Core rejected audit session creation",
		"gateway_forward_failed":                        "The authorized gateway connection could not be created",
		"session_control_not_ready":                     "Session control is temporarily unavailable; retry this connection attempt after it recovers",
		"session_control_unavailable":                   "Session control was lost during authentication; open a new connection from JumpServer after it recovers",
	}
	if message, ok := public[code]; ok {
		return &APIError{code, message}
	}
	return &APIError{"authentication_failed", "Connection authorization failed"}
}
func (v *Server) monitor(s *session) {
	go v.provider.Watch(s.ctx, s.grant, func(control core.Control) {
		s.emit("session.control", control)
		if control.Type == "terminate" {
			v.closeSession(s, control.Reason)
			return
		}
		if control.Type == "lock" || control.Type == "permission_revoked" {
			s.mu.Lock()
			for _, e := range s.executions {
				e.cancel()
			}
			s.mu.Unlock()
		}
		s.emit("session.updated", s.view())
	})
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := s.grant.Check(); err != nil && !isSessionPaused(err) {
				v.closeSession(s, err.Error())
				return
			}
		}
	}
}

func isSessionPaused(err error) bool {
	return errors.Is(err, core.ErrLocked) || errors.Is(err, core.ErrControlUnavailable)
}
func (v *Server) closeSession(s *session, reason string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	v.cleanup.Add(1)
	s.closed = true
	cs := make([]*console, 0, len(s.consoles))
	for _, c := range s.consoles {
		cs = append(cs, c)
	}
	s.mu.Unlock()
	v.mu.Lock()
	for key, active := range v.sessions {
		if active == s {
			delete(v.sessions, key)
		}
	}
	for key, ticket := range v.downloads {
		if ticket.job.session == s {
			delete(v.downloads, key)
		}
	}
	for _, attempt := range v.creations {
		if attempt.session == s {
			attempt.session = nil
			attempt.token = ""
			attempt.err = &APIError{"session_closed", "Session is closed"}
			attempt.expiresAt = time.Now().Add(creationRetention)
		}
	}
	v.mu.Unlock()
	s.emit("session.closed", map[string]string{"reason": reason})
	s.cancel()
	for _, c := range cs {
		v.closeConsole(s, c)
	}
	go func() {
		defer v.cleanup.Done()
		// Cancel and drain directory reads/audits before closing the grant or
		// removing its gateway. This pool never holds a SQL console connection.
		s.catalogOps.Wait()
		s.mu.Lock()
		catalog := s.catalog
		s.mu.Unlock()
		if catalog != nil {
			_ = catalog.db.Close()
		}
		// Include deleted consoles still draining accepted work before finishing
		// the Core session or stopping audit delivery.
		s.workers.Wait()
		for _, c := range cs {
			c.mu.Lock()
			c.mu.Unlock() //nolint:staticcheck // SA2001: wait for console operations to release the connection before closing its gateway.
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := v.provider.Close(ctx, s.grant)
		cancel()
		if err != nil {
			v.mu.Lock()
			v.cleanupErr = errors.Join(v.cleanupErr, err)
			v.mu.Unlock()
		}
		if s.db != nil {
			_ = s.db.Close()
		}
	}()
}
func (v *Server) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = v.CloseContext(ctx)
}
func (v *Server) CloseContext(ctx context.Context) error {
	v.shutdownOnce.Do(func() {
		// Stop accepting sessions under the same lock that registers each creation.
		v.mu.Lock()
		v.cancel()
		v.mu.Unlock()
		go func() {
			v.creating.Wait()
			v.mu.RLock()
			ss := make([]*session, 0, len(v.sessions))
			for _, s := range v.sessions {
				ss = append(ss, s)
			}
			v.mu.RUnlock()
			for _, s := range ss {
				v.closeSession(s, "server_shutdown")
			}
			v.cleanup.Wait()
			closeExportStore(v.exportDir, v.exportLock)
			close(v.shutdownDone)
		}()
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-v.shutdownDone:
		v.mu.RLock()
		defer v.mu.RUnlock()
		return v.cleanupErr
	}
}
func (v *Server) sessionStatus(w http.ResponseWriter, r *http.Request) {
	if s := v.auth(w, r, true); s != nil {
		writeJSON(w, 200, s.view())
	}
}
func (v *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	if s := v.auth(w, r, true); s != nil {
		v.closeSession(s, "user_closed")
		w.WriteHeader(204)
	}
}
func validIdentifier(s string) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= 128 && !strings.ContainsRune(s, 0)
}
func (v *Server) createConsole(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, false)
	if s == nil {
		return
	}
	var req sqlservice.Context
	if !decode(w, r, &req) {
		return
	}
	if !validIdentifier(req.Database) || !validIdentifier(req.Schema) {
		fail(w, 400, "invalid_context", "Invalid database context")
		return
	}
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()
		fail(w, 410, "session_closed", "Session is closed")
		return
	}
	if v.opts.MaxConsoles > 0 && len(s.consoles) >= v.opts.MaxConsoles {
		s.mu.Unlock()
		fail(w, 429, "console_limit", "Console capacity reached")
		return
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	db := s.db
	var owned *sql.DB
	if s.grant.Config.Dialect == "postgresql" && req.Database != "" && req.Database != s.grant.Config.Database {
		cfg := s.grant.Config
		cfg.Database = req.Database
		var err error
		owned, err = openDatabase(cfg, 1)
		if err != nil {
			fail(w, 400, "invalid_context", "Unable to open database context")
			return
		}
		db = owned
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		if owned != nil {
			_ = owned.Close()
		}
		diagnostic := safeConnectionError(err, "Unable to open console connection")
		fail(w, 502, diagnostic.Code, diagnostic.Message)
		return
	}
	ok := false
	defer func() {
		if !ok {
			closeConsoleConnection(conn)
			if owned != nil {
				_ = owned.Close()
			}
		}
	}()
	cctx, ccancel := context.WithCancel(s.ctx)
	c := &console{id: newID("con_"), conn: conn, queue: make(chan *execution, 8), ctx: cctx, cancel: ccancel, requests: map[string]*execution{}, ownedDB: owned, catalogAuditAt: map[string]time.Time{}}
	c.context, c.backendID, err = initializeConsole(ctx, c, s.grant.Config, req)
	if err != nil {
		ccancel()
		e := safeDatabaseError(err)
		if isServerError(err) {
			e, _ = v.maskDatabaseDiagnostic(s, e)
		}
		fail(w, 400, e.Code, e.Message)
		return
	}
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil || s.grant.Check() != nil || (v.opts.MaxConsoles > 0 && len(s.consoles) >= v.opts.MaxConsoles) {
		s.mu.Unlock()
		ccancel()
		fail(w, 409, "session_unavailable", "Session changed while opening console")
		return
	}
	s.workers.Add(1)
	c.transaction.Store(&TransactionView{Supported: supportsManualTransaction(s.grant.Config.Dialect), State: "idle", Revision: 1, IdleTimeoutSeconds: v.transactionIdleSeconds()})
	s.consoles[c.id] = c
	s.mu.Unlock()
	ok = true
	s.grant.Touch()
	go v.worker(s, c)
	writeJSON(w, 201, c.view())
}
func (v *Server) findConsole(w http.ResponseWriter, r *http.Request, s *session) *console {
	s.mu.Lock()
	closed := s.closed
	c := s.consoles[r.PathValue("cid")]
	s.mu.Unlock()
	if closed || s.ctx.Err() != nil {
		fail(w, 410, "session_closed", "Session is closed")
		return nil
	}
	if c == nil {
		fail(w, 404, "not_found", "Console not found")
		return nil
	}
	if c.closed.Load() || c.lost.Load() {
		fail(w, 409, "connection_lost", "Open a new console; the previous connection state cannot be restored")
		return nil
	}
	return c
}
func (v *Server) closeConsole(s *session, c *console) {
	if c.closed.Swap(true) {
		return
	}
	s.workers.Add(1)
	c.cancel()
	go func() {
		defer s.workers.Done()
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.conn != nil {
			v.closeManualTransaction(s, c)
			closeConsoleConnection(c.conn)
		}
		if c.ownedDB != nil {
			_ = c.ownedDB.Close()
		}
	}()
}
func (v *Server) deleteConsole(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, true)
	if s == nil {
		return
	}
	s.mu.Lock()
	c := s.consoles[r.PathValue("cid")]
	if c != nil {
		delete(s.consoles, c.id)
		// Register cleanup before session shutdown can wait on the workers.
		v.closeConsole(s, c)
	}
	s.mu.Unlock()
	w.WriteHeader(204)
}
func positiveInt(value string, def, max int) int {
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
