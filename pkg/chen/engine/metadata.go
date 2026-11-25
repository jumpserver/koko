package engine

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/sqlanalysis"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

type editorStatement struct {
	ByteSpan           sqlanalysis.Span      `json:"byte_span"`
	Features           []sqlanalysis.Feature `json:"features,omitempty"`
	SourceCandidate    bool                  `json:"source_candidate"`
	Diagnostics        []editorDiagnostic    `json:"diagnostics,omitempty"`
	From               int                   `json:"from"`
	To                 int                   `json:"to"`
	Kind               string                `json:"kind"`
	MayWrite           bool                  `json:"may_write"`
	ReturnsRows        bool                  `json:"returns_rows"`
	ImplicitCommit     bool                  `json:"implicit_commit,omitempty"`
	TransactionControl bool                  `json:"transaction_control,omitempty"`
}

type editorAnalysis struct {
	Syntax        sqlanalysis.Status `json:"syntax"`
	Compatibility sqlanalysis.Status `json:"compatibility"`
	Diagnostics   []editorDiagnostic `json:"diagnostics,omitempty"`
	Version       int                `json:"document_version"`
	Statements    []editorStatement  `json:"statements"`
}

// Use the same native parsers and live SQL mode as execution. Document text is
// only parsed, never executed or included in the coalesced metadata audit.
func (v *Server) analyzeDocument(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Document string `json:"document"`
		Version  int    `json:"document_version"`
	}
	if !decode(w, r, &req) {
		return
	}
	s, c, ctx, done := v.catalogConsole(w, r)
	if s == nil {
		return
	}
	defer done()
	if v.auditCatalog(ctx, s, c, "analysis") != nil {
		fail(w, 503, "audit_persistence_failed", "SQL analysis access could not be audited")
		return
	}
	options, err := liveOptions(ctx, c, s.grant.Config.Dialect)
	if err != nil {
		fail(w, 502, "metadata_failed", "Unable to read database SQL settings")
		return
	}
	analysis, err := sqlservice.AnalyzeWithOptions(sqlservice.Dialect(s.grant.Config.Dialect), req.Document, options)
	if err != nil {
		var diagnostic *sqlanalysis.Error
		if errors.As(err, &diagnostic) {
			writeJSON(w, 400, map[string]any{"document_version": req.Version, "error": APIError{"sql_" + string(diagnostic.Code), diagnostic.Message}, "diagnostics": []editorDiagnostic{editorDiagnosticFor(req.Document, diagnostic.Diagnostic)}})
		} else {
			fail(w, 400, "analysis_failed", "SQL analysis failed; check the dialect and syntax before execution")
		}
		return
	}
	response := editorAnalysis{Version: req.Version, Statements: []editorStatement{}, Syntax: analysis.Syntax, Compatibility: analysis.Compatibility}
	for _, d := range analysis.Diagnostics {
		response.Diagnostics = append(response.Diagnostics, editorDiagnosticFor(req.Document, d))
	}
	for _, statement := range analysis.Statements {
		implicit, control := sqlservice.ManualTransactionRisk(sqlservice.Dialect(s.grant.Config.Dialect), statement)
		response.Statements = append(response.Statements, editorStatement{
			ByteSpan:           sqlanalysis.Span{Start: statement.Start, End: statement.End, Known: true},
			From:               len(utf16.Encode([]rune(req.Document[:statement.Start]))),
			To:                 len(utf16.Encode([]rune(req.Document[:statement.End]))),
			Kind:               statement.Kind,
			MayWrite:           statement.MayWrite,
			ReturnsRows:        statement.ReturnsRows,
			ImplicitCommit:     implicit,
			TransactionControl: control,
		})
		if statement.Facts != nil {
			view := &response.Statements[len(response.Statements)-1]
			view.Features = statement.Facts.Features
			view.SourceCandidate = statement.Facts.SourceCandidate
			for _, d := range statement.Facts.Diagnostics {
				view.Diagnostics = append(view.Diagnostics, editorDiagnosticFor(req.Document, d))
			}
		}
	}
	writeJSON(w, 200, response)
}

func (v *Server) catalogConsole(w http.ResponseWriter, r *http.Request) (*session, *console, context.Context, func()) {
	s := v.auth(w, r, false)
	if s == nil {
		return nil, nil, nil, nil
	}
	c := v.findConsole(w, r, s)
	if c == nil {
		return nil, nil, nil, nil
	}
	if !c.mu.TryLock() {
		fail(w, 409, "console_busy", "Metadata is unavailable while this console is executing")
		return nil, nil, nil, nil
	}
	// A stale editor request must not close the console's physical MySQL
	// connection (the driver closes it when QueryContext is canceled). Let an
	// admitted metadata read finish within its small budget; console/session
	// shutdown still cancels it. No user SQL is executed by these handlers.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
	stop := context.AfterFunc(c.ctx, cancel)
	s.grant.SetBusy(true)
	return s, c, ctx, func() { stop(); cancel(); s.grant.SetBusy(false); c.mu.Unlock() }
}

// Metadata access is audited as an internal operation, without copying partially
// typed SQL into audit on every keystroke. Each console and operation has a
// one-minute audit window; a persistence failure prevents new metadata access.
func (v *Server) auditCatalog(ctx context.Context, s *session, c *console, kind string) error {
	if c.catalogAuditAt == nil {
		c.catalogAuditAt = map[string]time.Time{}
	}
	now := time.Now()
	if now.Sub(c.catalogAuditAt[kind]) < time.Minute {
		return nil
	}
	err := v.provider.Record(ctx, s.grant, core.AuditRecord{ID: newID("meta_"), RiskClassified: true, SQL: "/* Chen metadata: " + kind + " */", Output: "Authorized catalog access; repeated requests summarized for one minute", StartedAt: now, FinishedAt: now})
	if err != nil {
		return errors.New("metadata audit could not be persisted")
	}
	c.catalogAuditAt[kind] = now
	return nil
}

// Read live context: USE and SET search_path from user SQL must also affect
// completion. Only the database can confirm the active scope.
func liveContext(ctx context.Context, c *console, dialect string) (sqlservice.Context, error) {
	return connectionContext(ctx, c.conn, dialect, c.context)
}
func (v *Server) relations(w http.ResponseWriter, r *http.Request) {
	s, c, ctx, done := v.catalogConsole(w, r)
	if s == nil {
		return
	}
	defer done()
	if v.auditCatalog(ctx, s, c, "relations") != nil {
		fail(w, 503, "audit_persistence_failed", "Metadata access could not be audited")
		return
	}
	current, err := liveContext(ctx, c, s.grant.Config.Dialect)
	if err != nil {
		fail(w, 502, "metadata_failed", "Unable to read database context")
		return
	}
	if s.grant.Config.Dialect == "postgresql" {
		current.Schema = "" // This endpoint lists unqualified suggestions in the live search_path.
	}
	items, err := sqlservice.Metadata(ctx, c.conn, sqlservice.Dialect(s.grant.Config.Dialect), sqlservice.MetadataRequest{Context: current, Prefix: r.URL.Query().Get("prefix"), Limit: 200})
	if err != nil {
		fail(w, 502, "metadata_failed", "Unable to load authorized database relations")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (v *Server) columns(w http.ResponseWriter, r *http.Request) {
	s, c, ctx, done := v.catalogConsole(w, r)
	if s == nil {
		return
	}
	defer done()
	if v.auditCatalog(ctx, s, c, "columns") != nil {
		fail(w, 503, "audit_persistence_failed", "Metadata access could not be audited")
		return
	}
	current, err := liveContext(ctx, c, s.grant.Config.Dialect)
	if err != nil {
		fail(w, 502, "metadata_failed", "Unable to read database context")
		return
	}
	items, err := sqlservice.Columns(ctx, c.conn, sqlservice.Dialect(s.grant.Config.Dialect), sqlservice.ColumnRequest{Context: current, Schema: r.URL.Query().Get("schema"), Table: r.URL.Query().Get("table")})
	if err != nil {
		fail(w, 502, "metadata_failed", "Unable to load authorized database columns")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (v *Server) completion(w http.ResponseWriter, r *http.Request) {
	var req sqlservice.CompletionRequest
	if !decode(w, r, &req) {
		return
	}
	s, c, ctx, done := v.catalogConsole(w, r)
	if s == nil {
		return
	}
	defer done()
	if v.auditCatalog(ctx, s, c, "completion") != nil {
		fail(w, 503, "audit_persistence_failed", "Completion access could not be audited")
		return
	}
	current, err := liveContext(ctx, c, s.grant.Config.Dialect)
	if err != nil {
		fail(w, 502, "metadata_failed", "Unable to read database context")
		return
	}
	req.Context = current
	options, err := liveOptions(ctx, c, s.grant.Config.Dialect)
	if err != nil {
		fail(w, 502, "metadata_failed", "Unable to read database SQL settings")
		return
	}
	req.Options = options
	response, err := sqlservice.Completion(ctx, c.conn, sqlservice.Dialect(s.grant.Config.Dialect), req)
	if err != nil {
		fail(w, 400, "completion_failed", "Unable to analyze completion context")
		return
	}
	writeJSON(w, 200, response)
}

// UI positions use UTF-16; all internal facts retain original byte ranges.
type editorDiagnostic struct {
	Code     sqlanalysis.Code `json:"code"`
	Message  string           `json:"message"`
	ByteSpan sqlanalysis.Span `json:"byte_span"`
	From     *int             `json:"from,omitempty"`
	To       *int             `json:"to,omitempty"`
	Line     *int             `json:"line,omitempty"`
	Column   *int             `json:"column,omitempty"`
}

func editorDiagnosticFor(document string, d sqlanalysis.Diagnostic) editorDiagnostic {
	out := editorDiagnostic{Code: d.Code, Message: d.Message, ByteSpan: d.Span}
	if d.Span.ValidFor(document) && utf8.ValidString(document[:d.Span.Start]) && utf8.ValidString(document[:d.Span.End]) {
		prefix := document[:d.Span.Start]
		from, to := len(utf16.Encode([]rune(prefix))), len(utf16.Encode([]rune(document[:d.Span.End])))
		line := strings.Count(prefix, "\n") + 1
		column := len(utf16.Encode([]rune(prefix[strings.LastIndex(prefix, "\n")+1:]))) + 1
		out.From, out.To, out.Line, out.Column = &from, &to, &line, &column
	}
	return out
}
