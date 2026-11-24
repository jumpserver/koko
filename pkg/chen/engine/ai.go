package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/sqlanalysis"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

type aiExecutionError struct {
	ExecutionID      string `json:"executionId"`
	Code             string `json:"code,omitempty"`
	Message          string `json:"message,omitempty"`
	SQL              string `json:"sql,omitempty"`
	MessageTruncated bool   `json:"messageTruncated,omitempty"`
}

type aiTask struct{ cancel context.CancelFunc }
type aiRPC struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  struct {
		Name      string                     `json:"name"`
		Arguments json.RawMessage            `json:"arguments"`
		Meta      map[string]json.RawMessage `json:"_meta"`
		RequestID json.RawMessage            `json:"requestId"`
		Reason    string                     `json:"reason"`
	} `json:"params"`
}
type aiEditorContext struct {
	Dialect          string `json:"dialect"`
	Database         string `json:"database"`
	Schema           string `json:"schema"`
	Table            string `json:"table,omitempty"`
	NodeKey          string `json:"nodeKey"`
	ConsoleID        string `json:"consoleId"`
	PaneID           string `json:"paneId"`
	TabID            string `json:"tabId"`
	WorkspaceTabID   string `json:"workspaceTabId"`
	WorkspaceTabKind string `json:"workspaceTabKind"`
	CurrentContext   string `json:"currentContext"`
	Revision         int    `json:"revision"`
	SelectionFrom    int    `json:"selectionFrom"`
	SelectionTo      int    `json:"selectionTo"`
	SelectedSQL      string `json:"selectedSql"`
	DocumentSQL      string `json:"documentSql"`
	ProposalTarget   string `json:"proposalTarget"`
	LastError        any    `json:"lastError,omitempty"`
}

func aiRequestID(raw json.RawMessage) (string, error) {
	var id string
	if json.Unmarshal(raw, &id) != nil || id == "" || len(id) > 128 {
		return "", errors.New("A string request id of at most 128 bytes is required")
	}
	return id, nil
}
func (v *Server) aiMessages(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, false)
	if s == nil {
		return
	}
	if v.opts.SingleMode {
		fail(w, 404, "ai_unavailable", "AI requires a managed session")
		return
	}
	var frame struct {
		Type              string          `json:"type"`
		Version           int             `json:"version"`
		ResourceSessionID string          `json:"resource_session_id"`
		Data              json.RawMessage `json:"data"`
	}
	// Java's document/context limits count UTF-16 characters, not UTF-8 bytes.
	// Leave room for the JSON envelopes and escaped non-ASCII editor text.
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if !decode(w, r, &frame) {
		return
	}
	if frame.Version != 1 || frame.ResourceSessionID != s.id || (frame.Type != "mcp.request" && frame.Type != "mcp.cancel") {
		fail(w, 400, "invalid_ai_frame", "Invalid AI session binding")
		return
	}
	raw := frame.Data
	var encoded string
	if len(raw) > 0 && raw[0] == '"' {
		if json.Unmarshal(raw, &encoded) != nil {
			fail(w, 400, "invalid_ai_frame", "Invalid AI frame")
			return
		}
		raw = []byte(encoded)
	}
	var rpc aiRPC
	if json.Unmarshal(raw, &rpc) != nil || rpc.JSONRPC != "2.0" {
		fail(w, 400, "invalid_ai_frame", "Invalid JSON-RPC request")
		return
	}
	if frame.Type == "mcp.cancel" {
		if rpc.Method != "notifications/cancelled" {
			fail(w, 400, "invalid_ai_frame", "Invalid AI cancellation")
			return
		}
		id, err := aiRequestID(rpc.Params.RequestID)

		if err != nil {
			fail(w, 400, "invalid_ai_frame", err.Error())
			return
		}
		s.mu.Lock()
		task := s.aiTasks[id]
		s.mu.Unlock()
		if task != nil {
			task.cancel()
		}
		s.emit("ai.message", aiFrame{"mcp.cancel_result", 1, s.id, map[string]any{"jsonrpc": "2.0", "id": id, "version": 1, "resource_session_id": s.id, "result": map[string]bool{"cancelled": task != nil}}})
		w.WriteHeader(http.StatusNoContent)
		return
	}
	id, err := aiRequestID(rpc.ID)
	if err != nil || rpc.Method != "tools/call" {
		fail(w, 400, "invalid_ai_frame", "Only tools/call with a string request id is supported")
		return
	}
	var binding struct {
		ResourceSessionID string `json:"resource_session_id"`
		Revision          int    `json:"revision"`
		ToolCallID        string `json:"tool_call_id"`
	}
	if json.Unmarshal(rpc.Params.Meta["com.jumpserver/agent"], &binding) != nil || binding.ResourceSessionID != s.id || binding.Revision != aiRevision || binding.ToolCallID == "" || len(binding.ToolCallID) > 256 {
		fail(w, 400, "invalid_ai_binding", "Invalid AI resource binding")
		return
	}
	editor, err := v.resolveAIEditor(s, rpc.Params.Meta["com.jumpserver/sqlContext"])
	if err != nil {
		fail(w, 400, "invalid_ai_context", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()
		cancel()
		fail(w, 410, "session_closed", "Session is closed")
		return
	}
	if s.aiTasks == nil {
		s.aiTasks = map[string]*aiTask{}
	}
	if _, exists := s.aiTasks[id]; exists {
		s.mu.Unlock()
		cancel()
		fail(w, 409, "duplicate_ai_request", "AI request id is already active")
		return
	}
	if len(s.aiTasks) >= 2 {
		s.mu.Unlock()
		cancel()
		fail(w, 429, "ai_busy", "At most two AI tools can run per session")
		return
	}
	s.aiTasks[id] = &aiTask{cancel: cancel}
	s.workers.Add(1)
	s.mu.Unlock()
	request := r.Clone(ctx)
	go func() {
		defer s.workers.Done()
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.aiTasks, id); s.mu.Unlock() }()
		now := time.Now()
		auditErr := v.provider.Record(ctx, s.grant, core.AuditRecord{ID: newID("ai_"), RiskClassified: true, SQL: "/* Chen AI draft tool */", Output: "tool=" + rpc.Params.Name, StartedAt: now, FinishedAt: now})
		var result any
		var err error
		if auditErr != nil {
			err = errors.New("AI tool access could not be audited")
		} else {
			result, err = v.runAITool(ctx, request, s, editor, rpc.Params.Name, rpc.Params.Arguments)
		}
		response := map[string]any{"jsonrpc": "2.0", "id": id, "version": 1, "resource_session_id": s.id}
		if timeout := errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded); timeout || ctx.Err() != nil {
			status, code := "cancelled", "tool_cancelled"
			if timeout {
				status, code = "timeout", "tool_timeout"
			}
			response["result"] = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "AI tool " + status}}, "_meta": map[string]any{"com.jumpserver/agent": map[string]any{"status": status, "code": code}}}
		} else if err != nil {
			response["error"] = map[string]any{"code": -32602, "message": err.Error()}
		} else {
			b, e := json.Marshal(result)
			if e != nil {
				response["error"] = map[string]any{"code": -32603, "message": "Tool result could not be encoded"}
			} else {
				response["result"] = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}, "structuredContent": result}
			}
		}
		// A locked/revoked session must not disclose metadata, SQL or proposals after
		// the tool began. The frontend separately enforces ready state when applying.
		if s.ctx.Err() == nil && s.grant.Check() == nil {
			s.emit("ai.message", aiFrame{"mcp.response", 1, s.id, response})
		}
	}()
	w.WriteHeader(http.StatusNoContent)
}
func (v *Server) resolveAIEditor(s *session, raw json.RawMessage) (aiEditorContext, error) {
	var e aiEditorContext
	if len(raw) == 0 || aiTextLength(string(raw)) > 256<<10 || json.Unmarshal(raw, &e) != nil {
		return e, errors.New("Invalid SQL editor context")
	}
	for _, value := range []string{e.NodeKey, e.ConsoleID, e.PaneID, e.TabID, e.WorkspaceTabID, e.Database, e.Schema, e.Table, e.CurrentContext} {
		if aiTextLength(value) > 1024 || strings.ContainsRune(value, 0) {
			return e, errors.New("Invalid SQL editor identifier")
		}
	}
	if e.WorkspaceTabKind == "" {
		e.WorkspaceTabKind = "none"
	}
	requiresConsole := false
	switch e.WorkspaceTabKind {
	case "query", "console":
		requiresConsole = true
	case "data-view":
		if e.ConsoleID == "" && (e.Table == "" || e.Database == "" || e.Schema == "") {
			return e, errors.New("An active catalog table is required")
		}
	case "database", "none":
	default:
		return e, errors.New("Invalid database workspace context")
	}
	if e.NodeKey == "" || e.Dialect != s.grant.Config.Dialect {
		return e, errors.New("Invalid database resource context")
	}
	if e.WorkspaceTabKind != "data-view" {
		e.Table = ""
	}
	if requiresConsole || e.ConsoleID != "" {
		s.mu.Lock()
		c := s.consoles[e.ConsoleID]
		s.mu.Unlock()
		if c == nil || c.closed.Load() || c.lost.Load() || e.NodeKey != c.id {
			return e, errors.New("An active database console is required")
		}
		scope := c.currentContext()
		if e.Database != scope.Database || e.Schema != scope.Schema {
			return e, errors.New("SQL editor scope changed; refresh the editor context")
		}
		if e.WorkspaceTabKind == "data-view" {
			// Older table panes have a dedicated console. Recover their relation from
			// admitted browse results instead of trusting a client table label.
			e.Table = ""
			s.mu.Lock()
			for _, result := range s.results {
				if result.consoleID == c.id && result.browse != nil && result.browse.Schema == e.Schema {
					e.Table = result.browse.Table
					break
				}
			}
			s.mu.Unlock()
		}
	} else if !validAIResourceKey(e.NodeKey, s.id, e.Database, e.Schema) {
		return e, errors.New("SQL editor scope changed; refresh the editor context")
	} else if e.Database == "" && e.Schema != "" {
		return e, errors.New("Invalid database resource context")
	}
	if e.Revision < 0 || aiTextLength(e.DocumentSQL) > aiMaxSQL || e.SelectionFrom < 0 || e.SelectionTo < e.SelectionFrom {
		return e, errors.New("Invalid SQL document or selection")
	}
	doc := utf16.Encode([]rune(e.DocumentSQL))
	if e.SelectionTo > len(doc) {
		return e, errors.New("Invalid SQL selection")
	}
	// Do not split a surrogate pair when converting Monaco's UTF-16 offsets.
	for _, p := range []int{e.SelectionFrom, e.SelectionTo} {
		if p > 0 && p < len(doc) && doc[p] >= 0xdc00 && doc[p] <= 0xdfff && doc[p-1] >= 0xd800 && doc[p-1] <= 0xdbff {
			return e, errors.New("Invalid SQL selection boundary")
		}
	}
	selected := string(utf16.Decode(doc[e.SelectionFrom:e.SelectionTo]))
	if e.SelectedSQL != selected {
		return e, errors.New("SQL selection no longer matches the document")
	}
	if e.ProposalTarget == "" {
		e.ProposalTarget = "new_query"
		if e.WorkspaceTabKind == "query" || e.WorkspaceTabKind == "console" {
			e.ProposalTarget = "document"
		}
		if e.ProposalTarget == "document" && selected != "" {
			e.ProposalTarget = "selection"
		}
	}
	if e.ProposalTarget != "selection" && e.ProposalTarget != "document" && e.ProposalTarget != "new_query" || e.ProposalTarget == "selection" && selected == "" || e.ProposalTarget != "new_query" && e.TabID == "" {
		return e, errors.New("Invalid SQL proposal target")
	}
	lastError, err := v.resolveAIExecutionError(s, e)
	if err != nil {
		return e, err
	}
	e.LastError = nil
	if lastError != nil {
		e.LastError = lastError
	}
	return e, nil
}

func aiTextLength(value string) int { return len(utf16.Encode([]rune(value))) }

func validAIResourceKey(key, sessionID, database, schema string) bool {
	var parts []string
	return json.Unmarshal([]byte(key), &parts) == nil && len(parts) == 3 && parts[0] == sessionID && parts[1] == database && parts[2] == schema
}

// Only an execution reference is accepted from the browser. Its diagnostic and
// SQL are recovered from this session's completed execution, never client text.
func (v *Server) resolveAIExecutionError(s *session, editor aiEditorContext) (*aiExecutionError, error) {
	raw, _ := json.Marshal(editor.LastError)
	var reference struct {
		ExecutionID string `json:"executionId"`
	}
	if json.Unmarshal(raw, &reference) != nil || reference.ExecutionID == "" {
		return nil, nil // Older clients have no trusted execution reference.
	}
	if len(reference.ExecutionID) > 128 {
		return nil, errors.New("Invalid SQL execution reference")
	}
	s.mu.Lock()
	execution := s.executions[reference.ExecutionID]
	s.mu.Unlock()
	if execution == nil {
		return nil, errors.New("SQL execution error is unavailable in this session")
	}
	view := execution.snapshot()
	target := editor.DocumentSQL
	if editor.ProposalTarget == "selection" {
		target = editor.SelectedSQL
	}
	if view.ConsoleID != editor.ConsoleID || view.Status != "failed" || view.Error == nil || execution.edit != nil || execution.export != nil || execution.browse != nil ||
		execution.documentVersion != editor.Revision || strings.TrimSpace(execution.sql) == "" ||
		(editor.ProposalTarget == "selection" && target != execution.sql) || !strings.Contains(target, execution.sql) {
		return nil, errors.New("SQL execution error no longer matches this editor; refresh its execution context before requesting a repair")
	}
	message := view.Error.Message
	if !view.maskedDiagnostic {
		if _, masked := v.provider.Mask(s.grant, "", "", message); masked {
			message = hiddenDatabaseDiagnostic
		}
	}
	return &aiExecutionError{ExecutionID: view.ID, Code: view.Error.Code, Message: message, SQL: execution.sql}, nil
}

func (v *Server) executeAITool(ctx context.Context, r *http.Request, s *session, e aiEditorContext, name string, raw json.RawMessage) (any, error) {
	var args struct {
		SQL         string   `json:"sql"`
		Explanation string   `json:"explanation"`
		Query       string   `json:"query"`
		Schema      string   `json:"schema"`
		Tables      []string `json:"tables"`
		Limit       int      `json:"limit"`
	}
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&args) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("Invalid tool arguments")
	}
	if err := validateAIToolArguments(name, raw); err != nil {
		return nil, err
	}
	if err := s.grant.Check(); err != nil {
		return nil, errors.New("Session is unavailable")
	}
	// Unlike an editor's live console, a database/root tab has no pinned
	// connection. Resolve its requested resource with the account's catalog
	// privileges before disclosing it as verified AI context.
	if e.ConsoleID == "" && e.Database != "" && name != "search_schema" && name != "inspect_schema" {
		req, err := v.beginAICatalogAccess(ctx, r, e)
		if err != nil {
			return nil, err
		}
		if e.Table != "" {
			// A selected object from the browser is not verified until the account can see it.
			err = sqlservice.CatalogObjectAccess(req.ctx, req.conn, sqlservice.Dialect(e.Dialect), e.Schema, e.Table)
		} else {
			_, err = sqlservice.ObjectRows(req.ctx, req.conn, sqlservice.Dialect(e.Dialect), sqlservice.CatalogObjectRequest{Database: req.database, Schema: e.Schema, Limit: 1})
		}
		req.done()
		if err != nil {
			return nil, errors.New("Database context is unavailable to this account")
		}
	}
	switch name {
	case "read_sql_context":
		if string(raw) != "{}" {
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			if len(m) != 0 {
				return nil, errors.New("read_sql_context takes no arguments")
			}
		}
		lastError, err := v.resolveAIExecutionError(s, e)
		if err != nil {
			return nil, err
		}
		e.LastError = nil
		if lastError != nil {
			if len(lastError.Message) > 4096 {
				lastError.Message = strings.ToValidUTF8(lastError.Message[:4096], "") + "…"
				lastError.MessageTruncated = true
			}
			e.LastError = lastError
		}
		if e.SelectedSQL != "" {
			e.DocumentSQL = ""
		}
		return e, nil
	case "validate_sql", "propose_sql":
		if strings.TrimSpace(args.SQL) == "" || aiTextLength(args.SQL) > aiMaxSQL || aiTextLength(args.Explanation) > 4096 {
			return nil, errors.New("Invalid SQL draft")
		}
		analysis, err := sqlservice.Analyze(sqlservice.Dialect(e.Dialect), args.SQL)
		if err == nil && len(analysis.Statements) == 0 {
			err = errors.New("Invalid statement count")
		}
		validationStatus := "unverified"
		if err != nil {
			validationStatus = "invalid"
		} else if analysis.Syntax == sqlanalysis.Checked {
			validationStatus = "valid"
		}
		checked := map[string]any{
			"syntax":           analysis.Syntax,
			"compatibility":    analysis.Compatibility,
			"diagnostics":      analysis.Diagnostics,
			"validationStatus": validationStatus,
			"valid":            validationStatus == "valid",
			"statementCount":   len(analysis.Statements),
			"statements":       analysis.Statements,
			"dialect":          e.Dialect,
			"execution":        "never",
		}
		var messages []string
		for _, d := range analysis.Diagnostics {
			messages = append(messages, d.Message)
		}
		if err != nil {
			checked["message"] = err.Error()
			messages = append(messages, err.Error())
			var diagnostic *sqlanalysis.Error
			if errors.As(err, &diagnostic) {
				checked["diagnostics"] = []sqlanalysis.Diagnostic{diagnostic.Diagnostic}
			}
		} else if analysis.Syntax != sqlanalysis.Checked {
			checked["message"] = "Syntax is unknown; only statement boundaries were checked"
		}
		checked["riskReason"] = strings.Join(messages, "; ")
		if err != nil || analysis.Syntax != sqlanalysis.Checked {
			checked["errors"] = messages
		}
		if len(analysis.Statements) == 1 {
			checked["statementType"] = analysis.Statements[0].Kind
		}
		if name == "validate_sql" {
			// Keep the flat fields for existing API clients; Luna consumes the
			// same result through the established kind/analysis envelope.
			response := make(map[string]any, len(checked)+2)
			for k, value := range checked {
				response[k] = value
			}
			response["kind"], response["analysis"] = "validation", checked
			return response, nil
		}
		if err != nil && e.WorkspaceTabKind != "console" {
			return nil, errors.New("SQL draft cannot be parsed")
		}
		if err != nil {
			const notice = "Chen cannot validate this SQL with its dialect parser.\n\nIt may use database-native syntax.\n\nPlease review it before execution."
			args.Explanation = strings.TrimSpace(args.Explanation)
			if args.Explanation != "" {
				args.Explanation += "\n\n"
			}
			args.Explanation += notice
		}
		original := e.DocumentSQL
		if e.ProposalTarget == "selection" {
			original = e.SelectedSQL
		} else if e.ProposalTarget == "new_query" {
			original = ""
		}
		if e.ProposalTarget != "new_query" && strings.TrimSpace(original) == strings.TrimSpace(args.SQL) {
			return nil, errors.New("SQL proposal does not change the editor")
		}
		return map[string]any{"kind": "proposal", "analysis": checked, "proposal": map[string]any{"sql": args.SQL, "originalSql": original, "explanation": args.Explanation, "analysis": checked, "base": map[string]any{"paneId": e.PaneID, "tabId": e.TabID, "workspaceTabId": e.WorkspaceTabID, "workspaceTabKind": e.WorkspaceTabKind, "currentContext": e.CurrentContext, "revision": e.Revision, "selectionFrom": e.SelectionFrom, "selectionTo": e.SelectionTo, "nodeKey": e.NodeKey, "database": e.Database, "schema": e.Schema, "target": e.ProposalTarget}}}, nil
	case "search_schema", "inspect_schema":
		if err := validateAISchemaScope(e); err != nil {
			return nil, err
		}
		if args.Schema != "" && !strings.EqualFold(args.Schema, e.Schema) {
			return nil, errors.New("Schema is outside the verified editor scope")
		}
		if aiTextLength(args.Query) > 1024 || len(args.Tables) > 8 || args.Limit < 0 || args.Limit > 10 {
			return nil, errors.New("Invalid schema lookup limits")
		}
		if name == "search_schema" && (strings.TrimSpace(args.Query) == "" || args.Query == "*") {
			return nil, errors.New("Schema search requires a business term")
		}
		if name == "inspect_schema" && strings.TrimSpace(args.Query) == "" && len(args.Tables) == 0 {
			if e.Table == "" {
				return nil, errors.New("Select a table or specify a schema query or exact tables")
			}
			args.Tables = []string{e.Table}
		}
		if args.Query == "*" && len(args.Tables) != 0 {
			return nil, errors.New("Table discovery cannot include explicit tables")
		}
		for index, table := range args.Tables {
			if table == e.Table && table != "" {
				continue // A catalog name is literal, including any dots or quotes.
			}
			if table == "" || aiTextLength(table) > 1024 || strings.ContainsRune(table, 0) {
				return nil, errors.New("Invalid table identifier")
			}
			name, err := aiTableName(e, table)
			if err != nil {
				return nil, err
			}
			args.Tables[index] = name
		}
		req, err := v.beginAICatalogAccess(ctx, r, e)
		if err != nil {
			return nil, err
		}
		defer req.done()
		var objects []sqlservice.CatalogObject
		if name == "inspect_schema" && strings.TrimSpace(args.Query) == "" && len(args.Tables) == 1 && args.Tables[0] == e.Table {
			// InspectTableSummary checks exact visibility; no need to enumerate the schema.
			objects = []sqlservice.CatalogObject{{Schema: e.Schema, Name: e.Table}}
		} else {
			lookup := sqlservice.CatalogObjectRequest{Database: req.database, Schema: e.Schema, Limit: 100}
			if name == "inspect_schema" && args.Query != "*" {
				lookup.Query = args.Query
			}
			objects, err = sqlservice.ObjectRows(req.ctx, req.conn, sqlservice.Dialect(e.Dialect), lookup)
			if err != nil {
				return nil, errors.New("Unable to inspect authorized schema")
			}
		}
		if name == "search_schema" {
			for len(objects) > 100 && len(objects) <= 1000 {
				// ObjectRows returns one extra row only when another page exists.
				last := objects[len(objects)-2]
				objects = objects[:len(objects)-1]
				page, pageErr := sqlservice.ObjectRows(req.ctx, req.conn, sqlservice.Dialect(e.Dialect), sqlservice.CatalogObjectRequest{Database: req.database, Schema: e.Schema, Limit: 100, AfterSchema: last.Schema, AfterName: last.Name})
				if pageErr != nil {
					return nil, errors.New("Unable to search authorized schema")
				}
				objects = append(objects, page...)
				if len(page) <= 100 {
					break
				}
			}
		}
		result, err := aiSchemaResult(req, e, name, args.Query, args.Tables, args.Limit, objects)
		if err != nil {
			return nil, err
		}
		if err := req.ctx.Err(); err != nil {
			return nil, err
		}
		if v.catalogAudit(req, "succeeded", len(objects)) != nil {
			return nil, errors.New("Metadata audit could not be persisted")
		}
		return result, nil
	default:
		return nil, fmt.Errorf("Unknown SQL tool %q", name)
	}
}

func (v *Server) beginAICatalogAccess(ctx context.Context, r *http.Request, e aiEditorContext) (*catalogRequest, error) {
	rr := r.Clone(ctx)
	rr.URL = &url.URL{Path: r.URL.Path, RawQuery: url.Values{"schema": {e.Schema}, "limit": {"100"}}.Encode()}
	w := &aiCatalogWriter{header: make(http.Header)}
	req := v.beginCatalogAccess(w, rr, "ai", &e)
	if req == nil {
		var failure struct {
			Error APIError `json:"error"`
		}
		if json.Unmarshal(w.body.Bytes(), &failure) == nil && failure.Error.Message != "" {
			return nil, errors.New(failure.Error.Message)
		}
		return nil, errors.New("Authorized catalog is unavailable")
	}
	return req, nil
}

// The catalog lifecycle shares authentication, audit and concurrency limits with
// the tree API; internal failures are mapped to a stable tool error.
type aiCatalogWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *aiCatalogWriter) Header() http.Header         { return w.header }
func (w *aiCatalogWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *aiCatalogWriter) WriteHeader(status int)      { w.status = status }

func validateAIToolArguments(name string, raw json.RawMessage) error {
	allowed := map[string]bool{}
	switch name {
	case "read_sql_context":
	case "validate_sql":
		allowed["sql"] = true
	case "propose_sql":
		allowed["sql"], allowed["explanation"] = true, true
	case "search_schema":
		allowed["query"], allowed["schema"], allowed["limit"] = true, true, true
	case "inspect_schema":
		allowed["query"], allowed["schema"], allowed["tables"] = true, true, true
	default:
		return errors.New("Unknown SQL tool")
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(raw, &args) != nil || args == nil {
		return errors.New("Tool arguments must be an object")
	}
	for key := range args {
		if !allowed[key] {
			return errors.New("Unexpected tool argument")
		}
	}
	if name == "propose_sql" {
		if _, ok := args["explanation"]; !ok {
			return errors.New("Proposal explanation is required")
		}
	}
	return nil
}
