package engine

// Grid editing deliberately uses a separate, server-owned plan. A browser never
// supplies table identifiers, predicates, original primary keys or executable SQL.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

type EditOutcome struct {
	PlanID            string `json:"plan_id"`
	ChangesApplied    bool   `json:"changes_applied"`
	DatabaseCommitted bool   `json:"database_committed"`
	Outcome           string `json:"outcome"`
	AffectedRows      int    `json:"affected_rows"`
	RefreshRequired   bool   `json:"refresh_required"`
}
type editBrowse struct {
	Schema, Table string
	plan          *browsePlan
}
type editColumnView struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	DataType     string `json:"data_type"`
	Editable     bool   `json:"editable"`
	Insertable   bool   `json:"insertable"`
	Nullable     bool   `json:"nullable"`
	PrimaryKey   bool   `json:"primary_key"`
	Generated    bool   `json:"generated"`
	HasDefault   bool   `json:"has_default"`
	DefaultValue string `json:"default_value,omitempty"`
	Reason       string `json:"reason,omitempty"`
}
type editMetadataView struct {
	Supported bool             `json:"supported"`
	Reason    string           `json:"reason,omitempty"`
	Mode      string           `json:"mode"`
	Schema    string           `json:"schema"`
	Table     string           `json:"table"`
	Columns   []editColumnView `json:"columns"`
	RowRefs   []string         `json:"row_refs"`
	Offset    int              `json:"offset"`
	Version   string           `json:"version"`
}
type editResolved struct {
	view    editMetadataView
	table   sqlservice.EditTable
	columns []sqlservice.EditColumn
	pk      int
}
type editChange struct {
	Op     string             `json:"op"`
	RowRef string             `json:"row_ref,omitempty"`
	Values map[string]*string `json:"values,omitempty"`
}
type editPreviewRequest struct {
	Changes []editChange `json:"changes"`
	Source  string       `json:"source,omitempty"`
}
type editCommand struct {
	sql                   string
	args                  []any
	argColumns            []string // source column for each parameter, including the primary-key predicate
	policySQL, previewSQL string
	op                    string
}
type editPlan struct {
	id, consoleID, resultID, source, previewSQL string
	schemaFingerprint                           string
	commands                                    []editCommand
	count, updates, inserts, deletes            int
	expires                                     time.Time
	used                                        bool // protected by session.mu; one accepted dispatch, even after rollback
}

func editFail(w http.ResponseWriter, err error) { fail(w, 409, "edit_unavailable", err.Error()) }

// A succeeded execution may still be finishing its audit under the console
// lock. Wait briefly for that handoff instead of failing an immediate Save or
// Browse click. A genuinely busy console remains bounded and cancelable.
func (v *Server) editConsole(w http.ResponseWriter, r *http.Request) (*session, *console, context.Context, func()) {
	s := v.auth(w, r, false)
	if s == nil {
		return nil, nil, nil, nil
	}
	c := v.findConsole(w, r, s)
	if c == nil {
		return nil, nil, nil, nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	stop := context.AfterFunc(c.ctx, cancel)
	for !c.mu.TryLock() {
		select {
		case <-ctx.Done():
			stop()
			cancel()
			fail(w, 409, "console_busy", "Wait for the current console operation to finish")
			return nil, nil, nil, nil
		case <-time.After(10 * time.Millisecond):
		}
	}
	if ctx.Err() != nil || s.grant.Check() != nil || c.closed.Load() || c.lost.Load() {
		c.mu.Unlock()
		stop()
		cancel()
		fail(w, 409, "session_unavailable", "Session or console is unavailable")
		return nil, nil, nil, nil
	}
	stop()
	cancel()
	// Canceling a panel's HTTP request must not cancel a metadata query on
	// the console's physical connection: several drivers close that socket,
	// losing its transaction and session state. Only the lock wait follows
	// the browser request; acquired metadata work finishes within this bound.
	ctx, cancel = context.WithTimeout(c.ctx, 5*time.Second)
	s.grant.SetBusy(true)
	return s, c, ctx, func() { cancel(); s.grant.SetBusy(false); c.mu.Unlock() }
}

func (v *Server) editResult(s *session, c *console, rid string) (*Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.results[rid]
	if r == nil || r.consoleID != c.id {
		return nil, errors.New("Result does not belong to this console")
	}
	if r.editStale {
		return nil, errors.New("This result changed after saving; refresh it before editing again")
	}
	if len(r.original) != len(r.Rows) || len(r.rowRefs) != len(r.Rows) {
		return nil, errors.New("Refresh this result before editing")
	}
	return r, nil
}

// resolveResultSource is shared by editing and SQL copying. It proves provenance
// and metadata without imposing the write-back path's single-key restriction.
func (v *Server) resolveResultSource(ctx context.Context, s *session, c *console, r *Result) (*editResolved, error) {
	mode := "query"
	if r.browse != nil {
		mode = "table"
	}
	x := &editResolved{view: editMetadataView{Mode: mode, Columns: []editColumnView{}, RowRefs: []string{}, Version: r.ID}, pk: -1}
	for i, col := range r.Columns {
		x.view.Columns = append(x.view.Columns, editColumnView{Index: i, Name: col.Name, DataType: col.DataType, Reason: "This result is read-only"})
	}
	if r.editSourceBound && r.editSourceError != "" {
		return x, errors.New(r.editSourceError)
	}
	d := sqlservice.Dialect(s.grant.Config.Dialect)
	source, err := resultSource(ctx, c, d, r)
	if err != nil {
		return x, err
	}
	// A qualified MySQL table names its own database even after USE. Its
	// live privileges and column/key definition are still checked below.
	scope := sqlservice.Context{Database: source.Schema}
	if !sqlservice.IsMySQLDialect(d) || scope.Database == "" {
		scope, err = liveContext(ctx, c, s.grant.Config.Dialect)
		if err != nil {
			return x, errors.New("Database context is unavailable")
		}
	}
	table, err := loadResultSourceTable(ctx, s, c, r, scope, source)
	if err != nil {
		if c.lost.Load() {
			return x, err
		}
		if errors.Is(err, sqlservice.ErrEditColumnFlagsUnknown) {
			return x, sqlservice.ErrEditColumnFlagsUnknown
		}
		return x, errors.New("This table or its column types do not support safe editing")
	}
	x.table = table
	x.view.Schema = table.Schema
	x.view.Table = table.Table
	if !r.editSourceBound {
		source.Database = table.Database
		source.Schema, source.Table = table.Schema, table.Table
		r.editSource = &source
		r.editDatabase = table.Database
	}
	if source.Star && len(table.Columns) != len(r.Columns) {
		return x, errors.New("Result columns no longer match the table; refresh first")
	}
	if !source.Star && len(source.Columns) != len(r.Columns) {
		return x, errors.New("The source of every result column must be known")
	}
	sourceCounts := map[string]int{}
	for i := range r.Columns {
		var field *sqlservice.EditColumn
		if source.Star {
			field = &table.Columns[i]
		} else {
			name := source.Columns[i].Name
			for k := range table.Columns {
				if table.Columns[k].Name == name {
					field = &table.Columns[k]
					break
				}
			}
			if field == nil && (d == sqlservice.SQLServer || sqlservice.IsMySQLDialect(d)) {
				for k := range table.Columns {
					if strings.EqualFold(table.Columns[k].Name, name) {
						if field != nil {
							return x, errors.New("Result column source is ambiguous")
						}
						field = &table.Columns[k]
					}
				}
			}
			if field == nil {
				return x, errors.New("Result columns are ambiguous or no longer match the table")
			}
		}
		name := field.Name
		// The returned labels must agree with the proven projection. In
		// particular a stale SELECT * must not follow a changed column order.
		label := name
		if !source.Star {
			label = source.Columns[i].Label
		}
		labelMatches := r.Columns[i].Name == label
		if !labelMatches && (d == sqlservice.SQLServer || sqlservice.IsMySQLDialect(d)) {
			labelMatches = strings.EqualFold(r.Columns[i].Name, label)
		}
		if !labelMatches {
			return x, errors.New("Result column labels no longer match the source; refresh before editing")
		}
		sourceCounts[name]++
		x.columns = append(x.columns, *field)
		cv := editColumnView{Index: i, Name: r.Columns[i].Name, DataType: field.DataType, Nullable: field.Nullable, PrimaryKey: field.PrimaryKey, Generated: field.Generated || field.Identity, HasDefault: field.HasDefault, DefaultValue: field.Default}
		cv.Editable = field.Supported && !field.PrimaryKey && !cv.Generated
		cv.Insertable = mode == "table" && field.Supported && !cv.Generated
		switch {
		case field.PrimaryKey:
			cv.Reason = "Primary keys cannot be changed"
		case cv.Generated:
			cv.Reason = "Generated columns are managed by the database"
		case !field.Supported:
			cv.Reason = "This column type is read-only"
		}
		x.view.Columns[i] = cv
		if len(table.PrimaryKey) == 1 && field.Name == table.PrimaryKey[0] {
			x.pk = i
		}
	}
	ambiguousPrimaryKey := false
	for i, field := range x.columns {
		if sourceCounts[field.Name] > 1 {
			x.view.Columns[i].Editable = false
			x.view.Columns[i].Insertable = false
			x.view.Columns[i].Reason = "This source column occurs more than once in the result"
			ambiguousPrimaryKey = ambiguousPrimaryKey || field.PrimaryKey
		}
	}
	if ambiguousPrimaryKey {
		// Keep the complete ordinal mapping available for source masking even
		// when the duplicated key prevents writing or copying this result.
		return x, errors.New("The primary key must occur exactly once in the result")
	}
	return x, nil
}

func resultSource(ctx context.Context, c *console, d sqlservice.Dialect, r *Result) (sqlservice.EditableSource, error) {
	switch {
	case r.editSource != nil:
		return *r.editSource, nil
	case r.browse != nil:
		return sqlservice.EditableSource{Schema: r.browse.Schema, Table: r.browse.Table, Star: true}, nil
	case r.sourceAnalysis != nil && r.sourceAnalysis.SourceCandidate:
		return sqlservice.EditableSourceFromFacts(*r.sourceAnalysis)
	case r.querySource != nil:
		return sqlservice.AnalyzeEditableQuery(d, r.sourceSQL, r.querySource.options)
	default:
		opts, err := liveOptions(ctx, c, string(d))
		if err != nil {
			return sqlservice.EditableSource{}, errors.New("Database settings are unavailable")
		}
		return sqlservice.AnalyzeEditableQuery(d, r.sourceSQL, opts)
	}
}

// Results retain their qualified source when USE changes the console context.
// SQL Server catalogs are database-local, so inspect the source database under
// the console lock and restore the user's context before returning.
func loadResultSourceTable(ctx context.Context, s *session, c *console, r *Result, scope sqlservice.Context, source sqlservice.EditableSource) (sqlservice.EditTable, error) {
	d := sqlservice.Dialect(s.grant.Config.Dialect)
	database := source.Database
	if r.editSourceBound {
		database = r.editDatabase
	}
	if d != sqlservice.SQLServer || database == "" || database == scope.Database {
		return sqlservice.LoadCopyTable(ctx, c.conn, d, scope, source.Schema, source.Table)
	}
	current := scope.Database
	var table sqlservice.EditTable
	_, err := c.conn.ExecContext(ctx, "USE "+quoteDatabaseIdentifier(database, string(d)))
	if err == nil {
		scope.Database = database
		table, err = sqlservice.LoadCopyTable(ctx, c.conn, d, scope, source.Schema, source.Table)
	}
	// Even a failed USE may have reached the server before cancellation or a
	// lost response. Restore after every attempt, or discard the connection.
	restore, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, restoreErr := c.conn.ExecContext(restore, "USE "+quoteDatabaseIdentifier(current, string(d))); restoreErr != nil {
		invalidateEditConnection(s, c)
		return sqlservice.EditTable{}, errors.New("Database context could not be restored; reconnect before editing")
	}
	return table, err
}

func (v *Server) resolveEdit(ctx context.Context, s *session, c *console, r *Result) (*editResolved, error) {
	x, err := v.resolveResultSource(ctx, s, c, r)
	if err != nil {
		return x, err
	}
	if len(x.table.PrimaryKey) != 1 {
		return x, errors.New("Editing requires exactly one primary-key column")
	}
	if x.pk < 0 || !x.columns[x.pk].Supported {
		return x, errors.New("The result must contain the complete supported primary key")
	}
	x.view.Supported = true
	return x, nil
}
func (v *Server) editMetadata(w http.ResponseWriter, r *http.Request) {
	s, c, ctx, done := v.editConsole(w, r)
	if s == nil {
		return
	}
	defer done()
	if v.auditCatalog(ctx, s, c, "edit-metadata") != nil {
		fail(w, 503, "audit_persistence_failed", "Edit metadata could not be audited")
		return
	}
	result, err := v.editResult(s, c, r.PathValue("rid"))
	if err != nil {
		editFail(w, err)
		return
	}
	x, err := v.resolveEdit(ctx, s, c, result)
	if x == nil {
		editFail(w, err)
		return
	}
	if err != nil {
		x.view.Reason = err.Error()
	}
	offset := positiveInt(r.URL.Query().Get("offset"), 0, len(result.Rows))
	limit := positiveInt(r.URL.Query().Get("limit"), 200, 1000)
	if limit == 0 {
		limit = 200
	}
	x.view.Offset = offset
	if x.view.Supported {
		x.view.RowRefs = append([]string{}, result.rowRefs[offset:min(offset+limit, len(result.Rows))]...)
	}
	writeJSON(w, 200, x.view)
}
func (v *Server) previewEdits(w http.ResponseWriter, r *http.Request) {
	var req editPreviewRequest
	if !decode(w, r, &req) {
		return
	}
	s, c, ctx, done := v.editConsole(w, r)
	if s == nil {
		return
	}
	defer done()
	switch req.Source {
	case "":
		req.Source = "grid"
	case "grid", "csv":
	default:
		fail(w, 400, "invalid_source", "Unsupported edit source")
		return
	}
	if req.Source == "csv" && !s.grant.Permissions.Upload {
		fail(w, 403, "upload_permission_denied", "CSV import is not permitted")
		return
	}
	if v.auditCatalog(ctx, s, c, "edit-preview") != nil {
		fail(w, 503, "audit_persistence_failed", "Edit preview could not be audited")
		return
	}
	result, err := v.editResult(s, c, r.PathValue("rid"))
	if err != nil {
		editFail(w, err)
		return
	}
	x, err := v.resolveEdit(ctx, s, c, result)
	if err != nil {
		editFail(w, err)
		return
	}
	p, err := buildEditPlan(s.grant.Config.Dialect, c.id, result, x, req)
	if err != nil {
		fail(w, 400, "invalid_changes", err.Error())
		return
	}
	s.mu.Lock()
	if s.editPlans == nil {
		s.editPlans = map[string]*editPlan{}
	}
	for id, old := range s.editPlans {
		if time.Now().After(old.expires) {
			delete(s.editPlans, id)
		}
	}
	if len(s.editPlans) >= 32 {
		s.mu.Unlock()
		fail(w, 429, "edit_plan_limit", "Too many edit previews; wait for existing previews to expire")
		return
	}
	s.editPlans[p.id] = p
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"plan_id": p.id, "sql": p.previewSQL, "change_count": p.count, "update_count": p.updates, "insert_count": p.inserts, "delete_count": p.deletes, "expires_at": p.expires})
}
func (v *Server) applyEdits(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, false)
	if s == nil {
		return
	}
	c := v.findConsole(w, r, s)
	if c == nil {
		return
	}
	var req struct {
		RequestID       string `json:"request_id"`
		SubmitReview    bool   `json:"submit_review"`
		ConfirmWarnings bool   `json:"confirm_warnings"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.RequestID == "" {
		req.RequestID = r.Header.Get("Idempotency-Key")
	}
	if len(req.RequestID) < 1 || len(req.RequestID) > 128 {
		fail(w, 400, "request_id_required", "A stable request_id is required")
		return
	}
	s.mu.Lock()
	if old := c.requests[req.RequestID]; old != nil {
		s.mu.Unlock()
		if old.edit == nil || old.edit.id != r.PathValue("pid") || old.submitReview != req.SubmitReview || old.confirmWarnings != req.ConfirmWarnings {
			fail(w, 409, "idempotency_conflict", "Request ID belongs to another operation")
			return
		}
		writeJSON(w, 202, old.snapshot())
		return
	}
	p := s.editPlans[r.PathValue("pid")]
	if p == nil || p.consoleID != c.id {
		s.mu.Unlock()
		fail(w, 404, "not_found", "Edit preview not found")
		return
	}
	if p.source == "csv" && !s.grant.Permissions.Upload {
		s.mu.Unlock()
		fail(w, 403, "upload_permission_denied", "CSV import is not permitted")
		return
	}
	if p.used || time.Now().After(p.expires) {
		s.mu.Unlock()
		fail(w, 409, "edit_plan_expired", "Preview was used or expired; refresh and create a new preview")
		return
	}
	if s.closed || s.ctx.Err() != nil || c.closed.Load() || c.lost.Load() || c.ctx.Err() != nil || s.grant.Check() != nil {
		s.mu.Unlock()
		fail(w, 409, "session_unavailable", "Session or console is unavailable")
		return
	}
	if s.activeExecutionCountLocked() >= 256 {
		s.mu.Unlock()
		fail(w, 429, "execution_limit", "Too many active executions; wait for pending work to finish")
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	e := &execution{edit: p, sql: p.previewSQL, ctx: ctx, cancel: cancel, cancelDone: make(chan struct{}), submitReview: req.SubmitReview, confirmWarnings: req.ConfirmWarnings, view: ExecutionView{ID: newID("exe_"), ConsoleID: c.id, Status: "queued", ResultIDs: []string{}, StatementResults: []StatementResult{}, AuditStatus: "pending", Edit: &EditOutcome{PlanID: p.id, Outcome: "pending"}}}
	e.startDeadline(v.opts.QueryTimeout)
	select {
	case c.queue <- e:
		s.executions[e.view.ID] = e
		c.requests[req.RequestID] = e
	default:
		s.mu.Unlock()
		cancel()
		e.stopDeadline()
		fail(w, 429, "queue_full", "Console queue is full")
		return
	}
	s.mu.Unlock()
	s.grant.Touch()
	s.emit("execution.updated", e.snapshot())
	writeJSON(w, 202, e.snapshot())
}
func editQualified(schema, table, dialect string) string {
	if schema == "" {
		return quoteDatabaseIdentifier(table, dialect)
	}
	return quoteDatabaseIdentifier(schema, dialect) + "." + quoteDatabaseIdentifier(table, dialect)
}
func editQualifiedTable(table sqlservice.EditTable, dialect string) string {
	target := editQualified(table.Schema, table.Table, dialect)
	if dialect == "sqlserver" && table.Database != "" {
		return quoteDatabaseIdentifier(table.Database, dialect) + "." + target
	}
	return target
}

func editPlaceholder(d string, n int) string {
	switch d {
	case "postgresql":
		return fmt.Sprintf("$%d", n)
	case "sqlserver":
		return fmt.Sprintf("@p%d", n)
	case "oracle", "dameng":
		return fmt.Sprintf(":%d", n)
	default:
		return "?"
	}
}
func editError(code, message string) *APIError { return &APIError{Code: code, Message: message} }
func editMaskSQL(d string, value any, masked bool) string {
	if masked {
		return "'[MASKED]'"
	}
	if value == nil {
		return "NULL"
	}
	text := fmt.Sprint(value)
	if d == "mysql" || d == "mariadb" || d == "oceanbase-mysql" {
		text = strings.ReplaceAll(text, "\\", "\\\\")
	}
	prefix := ""
	if d == "sqlserver" {
		prefix = "N"
	}
	return prefix + "'" + strings.ReplaceAll(text, "'", "''") + "'"
}

func editSchemaFingerprint(x *editResolved) string {
	data, _ := json.Marshal(struct {
		Table   sqlservice.EditTable
		Columns []sqlservice.EditColumn
	}{x.table, x.columns})
	return string(data)
}

// Freeze qualified provenance before the worker releases its physical
// connection. A later USE/search_path change cannot retarget cached result rows.
func (v *Server) bindResultEditSource(s *session, e *execution, results []*Result, starSource *resultStarSource) {
	s.mu.Lock()
	c := s.consoles[e.view.ConsoleID]
	s.mu.Unlock()
	for _, result := range results {
		if c == nil || len(results) != 1 {
			result.editSourceBound = true
			result.editSourceError = "Multiple-result statements cannot be edited"
			continue
		}
		d := sqlservice.Dialect(s.grant.Config.Dialect)
		if bindStarSource(d, result, starSource) {
			result.editSourceBound = true
			continue
		}
		ctx, cancel := sqlservice.MetadataContext(e.ctx, d)
		resolved, err := v.resolveResultSource(ctx, s, c, result)
		cancel()
		if resolved != nil && len(resolved.columns) == len(result.Columns) {
			v.applyEditSourceMask(s, result, resolved.columns)
		}
		result.editSourceBound = true
		if err != nil {
			result.editSourceError = err.Error()
		}
	}
}

// A single base table's SELECT * already carries the physical column
// names used for masking. Freeze its namespace under the execution lock and
// read the live write/copy metadata only when the caller asks for it.
func bindStarSource(d sqlservice.Dialect, r *Result, star *resultStarSource) bool {
	if len(r.Columns) == 0 {
		return false
	}
	var source sqlservice.EditableSource
	if r.browse != nil {
		p := r.browse.plan
		// Older paging wrappers project a saved list of named columns. Only
		// a fresh star expansion can attest to the current physical order.
		if p == nil || p.table.View || p.view.Supported && browsePageProjectsColumns(string(d)) {
			return false
		}
		if !slices.EqualFunc(r.Columns, p.table.Columns, func(col Column, field sqlservice.EditColumn) bool {
			return col.Name == field.Name
		}) {
			return false
		}
		source = sqlservice.EditableSource{Database: p.table.Database, Schema: p.table.Schema, Table: p.table.Table, Star: true}
	} else {
		if star == nil || !slices.EqualFunc(r.Columns, star.columns, func(col Column, name string) bool {
			return col.Name == name
		}) {
			return false
		}
		source = star.source
	}
	if !source.Star || source.Database == "" || source.Schema == "" || source.Table == "" {
		return false
	}
	r.editSource, r.editDatabase = &source, source.Database
	return true
}

// Initial row reading masks conservatively when an alias has no proven origin.
// A validated single-table mapping permits applying the precise source policy,
// including restoring unprotected values without revealing protected PKs.
func (v *Server) applyEditSourceMask(s *session, result *Result, columns []sqlservice.EditColumn) {
	memory := 0
	for row := range result.Rows {
		for col, field := range columns {
			value, masked := v.provider.Mask(s.grant, field.Name, result.Columns[col].Name, result.original[row][col])
			result.Rows[row][col], result.masked[row][col] = value, masked
			if masked {
				encoded, _ := json.Marshal(result.original[row][col])
				memory += len(encoded)
			}
		}
		encoded, _ := json.Marshal(result.Rows[row])
		memory += len(encoded)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resultBytes -= result.memoryBytes
	if v.opts.MaxResultBytes > 0 && s.resultBytes+memory > v.opts.MaxResultBytes {
		result.memoryBytes = 0
		result.Rows = [][]any{}
		result.original = nil
		result.masked = nil
		result.copyLossy = nil
		result.rowRefs = nil
		result.TotalRows = 0
		result.Truncated = true
		if result.Browse != nil && result.Browse.Supported {
			result.Browse.HasMore = nil
		}
		return
	}
	result.memoryBytes = memory
	s.resultBytes += memory
}
