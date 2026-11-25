package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

type QueryPage struct {
	RequiresReexecution bool   `json:"requires_reexecution_confirmation,omitempty"`
	Supported           bool   `json:"supported"`
	Reason              string `json:"reason,omitempty"`
	Mode                string `json:"mode"` // cache or database
	ScopeID             string `json:"scope_id,omitempty"`
	Ordered             bool   `json:"ordered"` // not a uniqueness or snapshot guarantee
	Offset              int    `json:"offset"`
	PageSize            int    `json:"page_size"`
	MaxPageSize         int    `json:"max_page_size"`
	MaxOffset           int    `json:"max_offset"`
	HasMore             *bool  `json:"has_more"`
	ReadOnlyReason      string `json:"read_only_reason,omitempty"`
}
type QueryCount struct {
	SourceResultID string `json:"source_result_id"`
	ScopeID        string `json:"scope_id"`
	Total          string `json:"total"`
}
type queryPageRequest struct {
	ConfirmReexecution bool   `json:"confirm_reexecution,omitempty"`
	SourceResultID     string `json:"source_result_id"`
	Action             string `json:"action"`
	Offset             int    `json:"offset"`
	PageSize           int    `json:"page_size"`
	Format             string `json:"format,omitempty"`
	RequestID          string `json:"request_id"`
	SubmitReview       bool   `json:"submit_review"`
	ConfirmWarnings    bool   `json:"confirm_warnings"`
}
type queryPagePlan struct {
	reexecutionConfirmed                bool
	view                                QueryPage
	query                               *sqlservice.QueryPageSQL
	original                            string
	options                             sqlservice.AnalyzeOptions
	context                             string
	damengRefreshIdentity               string // plain projection on a connection without NLS catalog access
	columns                             int
	action, sourceResultID, fingerprint string
	generated                           string
}

func (v *Server) queryPage(w http.ResponseWriter, r *http.Request) {
	var req queryPageRequest
	if !decode(w, r, &req) {
		return
	}
	if req.RequestID == "" {
		req.RequestID = r.Header.Get("Idempotency-Key")
	}
	if req.RequestID == "" || len(req.RequestID) > 128 {
		fail(w, 400, "request_id_required", "A stable request_id is required")
		return
	}
	fingerprint, _ := json.Marshal(req)
	s := v.auth(w, r, true)
	if s == nil {
		return
	}
	s.mu.Lock()
	c := s.consoles[r.PathValue("cid")]
	var old *execution
	if c != nil {
		old = c.requests[req.RequestID]
	}
	source := s.results[req.SourceResultID]
	s.mu.Unlock()
	if old != nil {
		if old.queryPage == nil || old.queryPage.fingerprint != string(fingerprint) {
			fail(w, 409, "idempotency_conflict", "Request ID belongs to another operation")
			return
		}
		writeJSON(w, 202, old.snapshot())
		return
	}
	// Recover an accepted identity even after its connection is lost. A new
	// action still needs current permission and a ready original console.
	if v.auth(w, r, false) == nil {
		return
	}
	c = v.findConsole(w, r, s)
	if c == nil {
		return
	}
	if source == nil || source.consoleID != c.id || source.querySource == nil {
		fail(w, 400, "query_page_unavailable", "Choose a supported result from this console; database paging cannot replace its SQL")
		return
	}
	if !source.querySource.view.Supported && !(req.Action == "refresh" && (source.querySource.context != "" || source.querySource.damengRefreshIdentity != "") && source.editSourceBound && source.editSource != nil && source.editSourceError == "") {
		fail(w, 400, "query_page_unavailable", "This result has no verified query source to refresh")
		return
	}
	if source.querySource.view.RequiresReexecution && !req.ConfirmReexecution {
		fail(w, 409, "query_reexecution_confirmation_required", "This operation runs the original query again; confirm its SQL before continuing")
		return
	}
	if req.Action == "" {
		req.Action = "page"
	}
	if req.Action != "page" && req.Action != "refresh" && req.Action != "count" && req.Action != "export" {
		fail(w, 400, "invalid_query_page", "Unknown query paging action")
		return
	}
	if req.Offset < 0 || req.Offset > browseMaxOffset || req.PageSize < 0 || req.PageSize > min(1000, v.opts.MaxRows) {
		fail(w, 400, "invalid_query_page", "Page size or offset exceeds the limit")
		return
	}
	p := *source.querySource
	p.reexecutionConfirmed = req.ConfirmReexecution
	p.action, p.sourceResultID, p.fingerprint = req.Action, source.ID, string(fingerprint)
	p.view.Mode = "database"
	p.view.Offset = req.Offset
	if req.PageSize > 0 {
		p.view.PageSize = req.PageSize
	}
	if p.view.PageSize == 0 {
		p.view.PageSize = min(50, v.opts.MaxRows)
	}
	p.view.HasMore = nil
	p.view.ReadOnlyReason = ""
	p.columns = len(source.Columns)
	switch p.action {
	case "refresh":
		// Refresh cached results through their captured source and context. In
		// particular, never replay the surrounding editor script after a save.
		p.view.Mode = "cache"
		p.view.Offset = 0
		p.generated = p.original
	case "page":
		names := make([]string, len(source.Columns))
		for i, column := range source.Columns {
			names[i] = column.Name
		}
		if err := p.query.ValidateColumns(names); err != nil {
			fail(w, 400, "query_page_unavailable", err.Error())
			return
		}
		if err := p.query.ValidatePageOrder(names); err != nil {
			fail(w, 400, "query_page_unavailable", err.Error())
			return
		}
		p.generated = p.query.Page(p.view.Offset, p.view.PageSize+1, names)
	case "count":
		names := make([]string, len(source.Columns))
		for i, col := range source.Columns {
			names[i] = col.Name
		}
		if err := p.query.ValidateColumns(names); err != nil {
			fail(w, 400, "query_page_unavailable", err.Error())
			return
		}
		p.generated = p.query.Count(p.columns)
	case "export":
		p.generated = p.original
	}
	v.submitExecution(w, r, executionIntent{
		export: p.action == "export", queryPage: &p,
	}, &executionRequest{
		SQL: p.original, RequestID: req.RequestID, Format: req.Format,
		SubmitReview: req.SubmitReview, ConfirmWarnings: req.ConfirmWarnings,
	})
}

// This snapshot is taken on the same physical connection, under the worker lock.
// An old result cannot be moved into a new console after reconnect.
func queryPageContext(ctx context.Context, c *console, d dialect.ID) (string, error) {
	fingerprint, _, err := readQueryPageContext(ctx, c, d)
	return fingerprint, err
}

// The live database and schema used to resolve an open table are already in
// the paging settings. Return both from the same read on this console.
func readQueryPageContext(ctx context.Context, c *console, d dialect.ID) (string, sqlservice.Context, error) {
	scope := c.context
	var values []string
	if d == dialect.PostgreSQL {
		values = make([]string, 7)
		err := c.conn.QueryRowContext(ctx, "SELECT pg_catalog.current_database(), pg_catalog.current_schema(), pg_catalog.current_setting('search_path'), pg_catalog.current_setting('standard_conforming_strings'), current_user, pg_catalog.current_setting('TimeZone'), pg_catalog.current_setting('row_security')").Scan(&values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6])
		if err != nil {
			return "", scope, err
		}
		if values[3] != "on" {
			return "", scope, errors.New("Database paging requires standard_conforming_strings=on")
		}
	} else if !d.UsesMySQLSyntax() {
		var err error
		values, err = vendorQueryPageContext(ctx, c, d)
		if err != nil {
			return "", scope, err
		}
	} else {
		values = make([]string, 6)
		err := c.conn.QueryRowContext(ctx, "SELECT "+sqlservice.MySQLPageSettings).Scan(&values[0], &values[1], &values[2], &values[3], &values[4], &values[5])
		if err != nil {
			return "", scope, err
		}
	}
	switch {
	case d.UsesMySQLSyntax() || d == dialect.ClickHouse:
		scope.Database, scope.Schema = values[0], values[0]
	case d.UsesOracleSyntax():
		// The configured service alias identifies the database in the catalog.
		scope.Schema = values[1]
		if scope.Database == "" {
			scope.Database = scope.Schema
		}
	default: // PostgreSQL, SQL Server and Db2 return database, schema first.
		scope.Database, scope.Schema = values[0], values[1]
		if d == dialect.SQLServer && scope.Schema == "" {
			scope.Schema = "dbo"
		}
	}
	return pageContextJSON(values), scope, nil
}

type resultStarSource struct {
	source  sqlservice.EditableSource
	columns []string
}

// Only the original statement uses this column list; a later query page must
// validate its own result, whose wrapper may explicitly alias old labels.
func (v *Server) captureQuerySQLSource(s *session, c *console, e *execution, opts sqlservice.AnalyzeOptions, statement sqlservice.Statement, mysqlState *mysqlSessionState, hint *resultStarSource) *resultStarSource {
	d := s.grant.Dialect()
	statementSQL := statement.SQL
	editableSource := func() (sqlservice.EditableSource, error) {
		if statement.Facts != nil && statement.Facts.SourceCandidate {
			return sqlservice.EditableSourceFromFacts(*statement.Facts)
		}
		return sqlservice.AnalyzeEditableQuery(d, statementSQL, opts)
	}
	p := &queryPagePlan{original: statementSQL, options: opts, view: QueryPage{Mode: "cache", MaxPageSize: min(1000, v.opts.MaxRows), MaxOffset: browseMaxOffset}}
	e.querySource = p
	var scope sqlservice.Context
	captureContext := func() error {
		if p.context != "" {
			return nil
		}
		if mysqlState != nil {
			p.context, scope = mysqlState.fingerprint, mysqlState.scope
			return nil
		}
		var err error
		p.context, scope, err = readQueryPageContext(e.ctx, c, d)
		return err
	}
	q, err := sqlservice.AnalyzeQueryPage(d, statementSQL, opts)
	if err == nil && (d == dialect.MariaDB || d == dialect.OceanBaseMySQL) {
		_, reason, vendorErr := browseVendorContext(e.ctx, c, d, opts)
		if vendorErr != nil {
			err = vendorErr
		} else if reason != "" {
			err = errors.New(reason)
		}
	}
	var star *resultStarSource
	if err == nil && (d.UsesMySQLSyntax() || d == dialect.PostgreSQL) && e.export == nil && len(q.Relations) == 1 && len(q.CTEs) == 0 {
		source, sourceErr := editableSource()
		if sourceErr == nil && source.Star && source.Schema == q.Relations[0].Schema && source.Table == q.Relations[0].Name {
			star = &resultStarSource{source: source}
		}
	}
	if star != nil && d.UsesMySQLSyntax() {
		source := &star.source
		if source.Schema == "" && captureContext() == nil {
			source.Schema = scope.Database
		}
		source.Database = source.Schema
		if hint != nil && source.Schema == hint.source.Schema && source.Table == hint.source.Table {
			star.columns = hint.columns
		} else if columns, err := sqlservice.MySQLQueryStarColumns(e.ctx, c.conn, source.Schema, source.Table); err == nil {
			star.columns = columns
		}
	}
	if err == nil && (star == nil || len(star.columns) == 0) {
		err = validateQueryPageSources(e.ctx, c, d, q, star)
	}
	if err != nil {
		star = nil
		reason := err.Error()
		q, err = sqlservice.AnalyzeQueryPageForReexecution(d, statementSQL, opts)
		if err != nil {
			p.view.Reason = reason
			// Paging and cached refresh have different requirements. A direct
			// editable SELECT can still be refreshed without a page wrapper.
			// Freeze its context now; the handler also requires the result's
			// later metadata/provenance binding to have succeeded.
			if _, editErr := editableSource(); editErr == nil {
				_ = captureContext()
			}
			return nil
		}
		p.view.RequiresReexecution = true
	}
	if err = captureContext(); err != nil {
		p.view.Reason = err.Error()
		var denied *damengNLSAccessError
		if q.DirectTableProjection && !p.view.RequiresReexecution && errors.As(err, &denied) {
			// Re-reading directly projected cells needs no NLS-dependent comparisons or
			// conversions. The request must also have verified editable provenance.
			p.query, p.damengRefreshIdentity = q, denied.identity
		}
		return nil
	}
	p.query = q
	p.view.Supported = true
	p.view.ScopeID = newID("qpage_")
	p.view.Ordered = q.Ordered
	return star
}
func validateQueryPage(ctx context.Context, c *console, d dialect.ID, p *queryPagePlan, opts sqlservice.AnalyzeOptions, contextVerified bool) error {
	if p.damengRefreshIdentity != "" {
		current, err := readPageContextRow(ctx, c, oraclePageIdentity, 6)
		if err != nil {
			return err
		}
		if p.action != "refresh" || opts != p.options || pageContextJSON(current) != p.damengRefreshIdentity {
			return sourceContextChanged()
		}
		return validateQueryPageSources(ctx, c, d, p.query, nil)
	}
	// The worker already verified (and, when needed, selected) this complete
	// fingerprint. Do not probe it twice on every ordinary page/refresh.
	if !contextVerified || opts != p.options {
		return errors.New("Query context changed; run the original SQL again before enabling database paging")
	}
	if p.action == "refresh" && !p.view.Supported {
		_, err := sqlservice.AnalyzeEditableQuery(d, p.original, opts)
		return err
	}
	if p.view.RequiresReexecution {
		if !p.reexecutionConfirmed {
			return errors.New("Explicit query reexecution has not been confirmed")
		}
		_, err := sqlservice.AnalyzeQueryPageForReexecution(d, p.original, opts)
		return err
	}
	return validateQueryPageSources(ctx, c, d, p.query, nil)
}

var queryCountDecimal = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

func (v *Server) collectQueryCount(s *session, e *execution, rows *sql.Rows) error {
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return errors.New("Count returned no value")
	}
	var total string
	if err := rows.Scan(&total); err != nil {
		return err
	}
	if !queryCountDecimal.MatchString(total) {
		return errors.New("Count did not return an exact nonnegative integer")
	}
	if rows.Next() {
		return errors.New("Count returned multiple rows")
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := e.restoreSourceReadContext(); err != nil {
		return err
	}
	v.update(s, e, func(x *ExecutionView) {
		x.QueryCount = &QueryCount{SourceResultID: e.queryPage.sourceResultID, ScopeID: e.queryPage.view.ScopeID, Total: total}
	})
	return nil
}
func queryPageAudit(p *queryPagePlan) string {
	return fmt.Sprintf(" query_scope=%s source_result=%s action=%s confirmed_reexecution=%t generated_sql=%q", p.view.ScopeID, p.sourceResultID, p.action, p.reexecutionConfirmed, p.generated)
}

// Replay is restricted to ordinary tables with built-in PostgreSQL types and
// no RLS. Views/foreign tables can hide arbitrary function calls from the source
// AST. A CTE name that also resolves to an unsafe table is conservatively denied.
func validateQueryPageSources(ctx context.Context, c *console, d dialect.ID, q *sqlservice.QueryPageSQL, star *resultStarSource) error {
	if len(q.Relations) > 128 || len(q.Functions) > 128 || len(q.Operators) > 128 || len(q.Types) > 128 {
		return errors.New("Query paging source inspection exceeds its limit")
	}
	var tableQuery string
	seen := map[string]bool{}
	for _, r := range q.Relations {
		key := r.Schema + "\x00" + r.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		var safe bool
		var err error
		cteName := r.Name
		if d == dialect.SQLServer {
			cteName = strings.ToLower(cteName)
		}
		if d == dialect.PostgreSQL {
			safe, err = postgresQueryPageRelation(ctx, c, r, star)
		} else if d == dialect.MySQL {
			cteName = strings.ToLower(cteName)
			safe, err = sqlservice.MySQLQueryPageSource(ctx, c.conn, r.Schema, r.Name)
		} else if d != dialect.MariaDB && d != dialect.OceanBaseMySQL {
			err = vendorQueryPageSource(ctx, c, d, r, &safe, &tableQuery)
		} else {
			cteName = strings.ToLower(cteName)
			query := `SELECT TABLE_TYPE='BASE TABLE' AND ENGINE IN ('InnoDB','MyISAM','MEMORY','Aria') FROM information_schema.TABLES WHERE TABLE_SCHEMA=COALESCE(NULLIF(?,''),DATABASE()) AND TABLE_NAME=?`
			if d == dialect.MariaDB {
				query = strings.Replace(query, "TABLE_TYPE='BASE TABLE'", "TABLE_TYPE IN ('BASE TABLE','SYSTEM VERSIONED')", 1)
			}
			err = c.conn.QueryRowContext(ctx, query, r.Schema, r.Name).Scan(&safe)
		}
		if errors.Is(err, sql.ErrNoRows) && (d == dialect.PostgreSQL || d.UsesMySQLSyntax()) && r.Schema == "" && q.CTEs[cteName] {
			continue
		}
		if err != nil || !safe {
			return errors.New("Database paging requires visible ordinary tables; views, inherited/partitioned or foreign tables, row security and unverified types cannot be safely replayed")
		}
	}
	if d != dialect.PostgreSQL && !d.UsesMySQLSyntax() {
		if err := validateVendorQueryFunctions(ctx, c, d, q.Functions); err != nil {
			return err
		}
		if d.UsesOracleSyntax() {
			if err := validateOracleColumnBindings(ctx, c, d, q.Scopes); err != nil {
				return err
			}
		}

	}

	if d == dialect.PostgreSQL && (len(q.Functions) > 0 || len(q.Operators) > 0 || len(q.Types) > 0) {
		// All names are interpreted on this connection's current search_path.
		// Check them in one catalog snapshot, including duplicate references.
		var function, operator, typ bool
		err := c.conn.QueryRowContext(ctx, `SELECT
EXISTS (SELECT 1 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace
  WHERE p.proname=ANY($1::pg_catalog.name[]) AND n.nspname<>'pg_catalog' AND pg_catalog.pg_function_is_visible(p.oid)),
EXISTS (SELECT 1 FROM pg_catalog.pg_operator o JOIN pg_catalog.pg_namespace n ON n.oid=o.oprnamespace
  WHERE o.oprname=ANY($2::pg_catalog.name[]) AND n.nspname<>'pg_catalog' AND pg_catalog.pg_operator_is_visible(o.oid)),
EXISTS (SELECT 1 FROM pg_catalog.pg_type t JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace
  WHERE t.typname=ANY($3::pg_catalog.name[]) AND n.nspname<>'pg_catalog' AND pg_catalog.pg_type_is_visible(t.oid))`,
			q.Functions, q.Operators, q.Types).Scan(&function, &operator, &typ)
		switch {
		case err != nil:
			return errors.New("Database paging could not verify visible PostgreSQL functions, operators and types")
		case function:
			return errors.New("Database paging cannot prove the selected function resolves only to a built-in")
		case operator:
			return errors.New("Database paging cannot prove the selected operator resolves only to a built-in")
		case typ:
			return errors.New("Database paging cannot prove the selected type resolves only to a built-in")
		}
	}
	return nil
}
func sqlQuoteIdentifier(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func postgresQueryPageRelation(ctx context.Context, c *console, r sqlservice.QueryPageRelation, star *resultStarSource) (bool, error) {
	qualified := sqlQuoteIdentifier(r.Name)
	if r.Schema != "" {
		qualified = sqlQuoteIdentifier(r.Schema) + "." + qualified
	}
	const guard = `SELECT c.relkind IN ('r','m') AND NOT c.relrowsecurity
AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_inherits i WHERE i.inhparent=c.oid)
AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
  JOIN pg_catalog.pg_type t ON t.oid=a.atttypid JOIN pg_catalog.pg_namespace n ON n.oid=t.typnamespace
  WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped AND n.nspname<>'pg_catalog')`
	var safe bool
	if star == nil {
		err := c.conn.QueryRowContext(ctx, guard+` FROM pg_catalog.pg_class c WHERE c.oid=pg_catalog.to_regclass($1)`, qualified).Scan(&safe)
		return safe, err
	}
	// Resolve against the actual search_path, including temporary schemas.
	// A materialized view may pass replay admission but cannot bind as an editable table.
	const projection = `, pg_catalog.current_database(), n.nspname, c.relname,
pg_catalog.array_to_json(ARRAY(SELECT a.attname FROM pg_catalog.pg_attribute a
  WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped AND c.relkind='r'
  AND pg_catalog.has_schema_privilege(n.oid,'USAGE')
  AND pg_catalog.has_column_privilege(c.oid,a.attnum,'SELECT,INSERT,UPDATE,REFERENCES')
  ORDER BY a.attnum))::pg_catalog.text
FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
WHERE c.oid=pg_catalog.to_regclass($1)`
	var names []byte
	err := c.conn.QueryRowContext(ctx, guard+projection, qualified).Scan(&safe, &star.source.Database, &star.source.Schema, &star.source.Table, &names)
	if err == nil && safe {
		err = json.Unmarshal(names, &star.columns)
	}
	return safe, err
}
