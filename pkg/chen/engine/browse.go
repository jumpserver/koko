package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

// Java stores the row count/page in signed 32-bit integers. Leave room for
// fetching the extra row used to detect the next page.
const browseMaxOffset = (1 << 31) - 1001

type browseOrder struct {
	Column string `json:"column"`
	Desc   bool   `json:"desc"`
}
type browseFilter struct {
	Op       string         `json:"op"`
	Column   string         `json:"column,omitempty"`
	Values   []string       `json:"values,omitempty"`
	Children []browseFilter `json:"children,omitempty"`
}
type browseRequest struct {
	ConfirmReexecution bool          `json:"confirm_reexecution,omitempty"`
	Schema             string        `json:"schema"`
	Table              string        `json:"table"`
	SourceResultID     string        `json:"source_result_id,omitempty"`
	Action             string        `json:"action,omitempty"` // page (default), refresh, append, count, export
	Where              string        `json:"where,omitempty"`
	Filter             *browseFilter `json:"filter,omitempty"`
	Sort               []browseOrder `json:"sort,omitempty"`
	Offset             int           `json:"offset"`
	PageSize           int           `json:"page_size"`
	Format             string        `json:"format,omitempty"`
	RequestID          string        `json:"request_id"`
	SubmitReview       bool          `json:"submit_review"`
	ConfirmWarnings    bool          `json:"confirm_warnings"`
}
type BrowsePage struct {
	RequiresReexecution bool          `json:"requires_reexecution_confirmation,omitempty"`
	ServerVersion       string        `json:"server_version,omitempty"`
	MaxLoadedRows       int           `json:"max_loaded_rows"`
	LimitReached        string        `json:"limit_reached,omitempty"` // rows or bytes; not end of table
	Supported           bool          `json:"supported"`
	Reason              string        `json:"reason,omitempty"`
	Database            string        `json:"database"`
	Schema              string        `json:"schema"`
	Table               string        `json:"table"`
	Where               string        `json:"where"`
	Filter              *browseFilter `json:"filter,omitempty"`
	Sort                []browseOrder `json:"sort"`
	Offset              int           `json:"offset"`
	PageSize            int           `json:"page_size"`
	MaxPageSize         int           `json:"max_page_size"`
	MaxOffset           int           `json:"max_offset"`
	Stable              bool          `json:"stable"`
	HasMore             *bool         `json:"has_more"` // null means the byte budget cut this page short
	FullSQL             string        `json:"full_sql"`
}
type BrowseCount struct {
	SourceResultID string `json:"source_result_id"`
	Total          string `json:"total"`
}
type browsePlan struct {
	context                                                          string // captured session semantics for source-bound reads
	reexecutionConfirmed                                             bool
	fetchSize                                                        int // final append may be smaller than the selected batch size
	view                                                             BrowsePage
	serverVersion                                                    sqlservice.MySQLServerVersion
	table                                                            sqlservice.EditTable
	options                                                          sqlservice.AnalyzeOptions
	sql, policySQL, fullSQL, fullPolicySQL, countSQL, countPolicySQL string
	pageBaseSQL, pageBasePolicySQL, orderSQL                         string
	args                                                             []any
	fingerprint, action, sourceResultID                              string
}

func (v *Server) browseTable(w http.ResponseWriter, r *http.Request) {
	var req browseRequest
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
	// Recover before touching the connection: an in-flight retry is the same
	// execution even when metadata or session SQL mode have since changed.
	s := v.auth(w, r, false)
	if s == nil {
		return
	}
	c := v.findConsole(w, r, s)
	if c == nil {
		return
	}
	s.mu.Lock()
	old := c.requests[req.RequestID]
	s.mu.Unlock()
	if old != nil {
		if old.browse == nil || old.browse.plan == nil || old.browse.plan.fingerprint != string(fingerprint) {
			fail(w, 409, "idempotency_conflict", "Request ID belongs to another browse operation")
			return
		}
		writeJSON(w, 202, old.snapshot())
		return
	}
	if req.Action == "" {
		req.Action = "page"
	}
	if req.Action != "page" && req.Action != "refresh" && req.Action != "append" && req.Action != "count" && req.Action != "export" {
		fail(w, 400, "invalid_browse", "Unknown browse operation")
		return
	}
	if req.Action == "export" && !s.grant.Permissions.Download {
		fail(w, 403, "download_permission_denied", "Result download is not permitted")
		return
	}
	if req.Offset < 0 || req.Offset > browseMaxOffset || req.PageSize < 0 || req.PageSize > min(1000, v.opts.MaxRows) {
		fail(w, 400, "invalid_browse", "Page size or offset exceeds the browse limit")
		return
	}
	s, c, ctx, done := v.editConsole(w, r)
	if s == nil {
		return
	}
	p, err := v.resolveBrowse(ctx, s, c, req)
	if err == nil {
		err = v.auditCatalog(ctx, s, c, "table-browse")
	}
	done()
	if err != nil {
		var api *APIError
		if errors.As(err, &api) {
			fail(w, 409, api.Code, api.Message)
			return
		}
		fail(w, 400, "invalid_browse", err.Error())
		return
	}
	p.fingerprint = string(fingerprint)
	v.submitExecution(w, r, executionIntent{
		export: req.Action == "export",
		browse: &editBrowse{Schema: p.view.Schema, Table: p.view.Table, plan: p},
	}, &executionRequest{
		SQL: p.policySQL, RequestID: req.RequestID, Format: req.Format,
		SubmitReview: req.SubmitReview, ConfirmWarnings: req.ConfirmWarnings,
	})
}

func (v *Server) resolveBrowse(ctx context.Context, s *session, c *console, req browseRequest) (*browsePlan, error) {
	d := s.grant.Dialect()
	p := &browsePlan{action: req.Action, sourceResultID: req.SourceResultID}
	if req.SourceResultID != "" {
		s.mu.Lock()
		result := s.results[req.SourceResultID]
		s.mu.Unlock()
		if result == nil || result.consoleID != c.id || result.browse == nil || result.browse.plan == nil {
			return nil, errors.New("Browse result does not belong to this console")
		}
		previous := result.browse.plan
		if !previous.view.Supported {
			return nil, errors.New("This result supports cached browsing only: " + previous.view.Reason)
		}
		if req.Where != "" || req.Filter != nil || len(req.Sort) != 0 || req.Schema != "" || req.Table != "" {
			return nil, errors.New("A result-bound request cannot replace its filter or target")
		}
		*p = *previous
		p.reexecutionConfirmed = req.ConfirmReexecution
		p.action = req.Action
		p.sourceResultID = req.SourceResultID
		p.view.Offset = req.Offset
		p.fetchSize = 0
		if req.Action == "refresh" && d == dialect.SQLServer && p.context != "" {
			if err := refreshSQLServerBrowseTable(ctx, s, c, p); err != nil {
				return nil, err
			}
		}
		if req.Action == "append" && req.PageSize != 0 && req.PageSize != previous.view.PageSize {
			return nil, errors.New("Restart browsing before changing the batch size")
		}
		// Changing page size creates a fresh scope at the first page.
		if req.PageSize != 0 && req.PageSize != p.view.PageSize {
			p.view.PageSize = req.PageSize
			p.view.Offset = 0
		}
	} else {
		if req.Action != "page" {
			return nil, errors.New("This operation requires an applied browse result")
		}
		if req.Table == "" {
			return nil, errors.New("Select a table to browse")
		}
		var scope sqlservice.Context
		if state := c.initialMySQL; state != nil {
			scope, p.options, p.context = state.scope, state.options, state.fingerprint
		} else if d == dialect.MySQL {
			state, err := readMySQLSession(ctx, c.conn)
			if err == nil {
				scope, p.options, p.context = state.scope, state.options, state.fingerprint
			}
		}
		if p.context == "" {
			var err error
			p.options, err = liveOptions(ctx, c, d)
			if err != nil {
				return nil, err
			}
			p.context, scope, err = readQueryPageContext(ctx, c, d)
			if err != nil {
				// A driver or account can expose its namespace without all paging
				// settings. Initial browsing remains available in that case.
				scope, err = liveContext(ctx, c, d)
				if err != nil {
					return nil, err
				}
			}
		}
		version, reason, err := browseVendorContext(ctx, c, d, p.options)
		if err != nil {
			return nil, err
		}
		table, err := sqlservice.LoadBrowseTable(ctx, c.conn, d, scope, req.Schema, req.Table)
		if err != nil {
			return nil, errors.New("This table is unavailable for authorized browsing; open it as a normal query")
		}
		p.table = table
		p.serverVersion = version
		p.view = BrowsePage{Supported: reason == "", Reason: reason, ServerVersion: version.Product, Database: table.Database, Schema: table.Schema, Table: table.Table, Where: strings.TrimSpace(req.Where), Filter: req.Filter, Sort: append([]browseOrder{}, req.Sort...), Offset: req.Offset, PageSize: req.PageSize, MaxPageSize: min(1000, v.opts.MaxRows), MaxOffset: browseMaxOffset, MaxLoadedRows: v.opts.MaxRows}
		p.view.RequiresReexecution = table.View
		p.reexecutionConfirmed = req.ConfirmReexecution
		if table.View && !req.ConfirmReexecution {
			return nil, &APIError{"query_reexecution_confirmation_required", "This operation reads a view; confirm its SQL before continuing"}
		}
		if p.view.PageSize == 0 {
			p.view.PageSize = min(50, p.view.MaxPageSize)
		}
		if !p.view.Supported {
			if p.view.Where != "" || p.view.Filter != nil || len(p.view.Sort) > 0 || p.view.Offset != 0 {
				return nil, errors.New("This connection supports cached browsing only: " + p.view.Reason)
			}
			// Initial page_size is a preference; legacy browsing retains its cache bound.
			p.policySQL = "SELECT * FROM " + editQualified(table.Schema, table.Table, d)
			switch d {
			case dialect.MariaDB, dialect.OceanBaseMySQL:
				p.policySQL += " LIMIT " + strconv.Itoa(v.opts.MaxRows+1)
			case dialect.SQLServer:
				p.policySQL = "SELECT TOP (" + strconv.Itoa(v.opts.MaxRows+1) + ") * FROM " + editQualified(table.Schema, table.Table, d)
			default:
				p.policySQL += " FETCH FIRST " + strconv.Itoa(v.opts.MaxRows+1) + " ROWS ONLY"
			}
			p.sql = p.policySQL
			return p, nil
		}
		if p.view.Where != "" && p.view.Filter != nil {
			return nil, errors.New("Choose structured conditions or a handwritten WHERE")
		}
		if err = validateBrowseWhere(ctx, c, d, p); err != nil {
			return nil, err
		}
		predicate, policy := "", ""
		if p.view.Filter != nil {
			budget := 100
			predicate, policy, err = buildBrowseFilter(d, p.table, *p.view.Filter, &p.args, 0, &budget)
			if err != nil {
				return nil, err
			}
		} else if p.view.Where != "" {
			predicate = "(\n" + p.view.Where + "\n)"
			policy = predicate
		}
		if len(p.view.Sort) > 8 {
			return nil, errors.New("At most eight sort columns are supported")
		}
		order := []string{}
		seen := map[string]bool{}
		p.view.Sort = append([]browseOrder{}, p.view.Sort...)
		for i, item := range p.view.Sort {
			col := browseColumn(p.table, item.Column)
			if col == nil || seen[col.Name] {
				return nil, errors.New("Sort columns must be distinct visible table columns")
			}
			seen[col.Name] = true
			p.view.Sort[i].Column = col.Name
			part := quoteDatabaseIdentifier(col.Name, d)
			if item.Desc {
				part += " DESC"
			}
			order = append(order, part)
		}
		for _, key := range p.table.PrimaryKey {
			if !seen[key] {
				order = append(order, quoteDatabaseIdentifier(key, d))
				p.view.Sort = append(p.view.Sort, browseOrder{Column: key})
			}
		}
		p.view.Stable = len(p.table.PrimaryKey) > 0
		if d.UsesOracleSyntax() {
			// A binary unique character key need not be unique under the
			// active NLS/column sort collation (e.g. A/a under BINARY_CI).
			// Until both collations are verified, keep the real key/order but
			// report uniqueness in the current ordering as unproven.
			for _, key := range p.table.PrimaryKey {
				col := browseColumn(p.table, key)
				if col != nil {
					typ := strings.ToUpper(col.DataType)
					if strings.Contains(typ, "CHAR") || strings.Contains(typ, "TEXT") {
						p.view.Stable = false
						break
					}
				}
			}
		}
		// The browse key is independent of the actual primary key used by editing.
		// Without a proven non-null unique key, offset instability stays explicit.
		base := " FROM " + editQualified(p.view.Schema, p.view.Table, d)
		if predicate != "" {
			predicate = " WHERE " + predicate
			policy = " WHERE " + policy
		}
		p.fullSQL = "SELECT *" + base + predicate
		p.fullPolicySQL = "SELECT *" + base + policy
		count := "SELECT COUNT(*)"
		switch d {
		case dialect.PostgreSQL:
			count = "SELECT pg_catalog.count(*)"
		case dialect.DB2:
			count = "SELECT SYSIBM.COUNT_BIG(*)"
		case dialect.SQLServer:
			count = "SELECT COUNT_BIG(*)"
			// Another console may hold an uncommitted write. Report the lock
			// before the driver's socket timeout discards this browse connection.
			base += " WITH (NOWAIT)"
		}
		p.countSQL, p.countPolicySQL = count+base+predicate, count+base+policy
		p.pageBaseSQL, p.pageBasePolicySQL = "SELECT *"+base+predicate, "SELECT *"+base+policy
		p.orderSQL = strings.Join(order, ", ")
		if len(order) > 0 {
			p.fullSQL += " ORDER BY " + strings.Join(order, ", ")
			p.fullPolicySQL += " ORDER BY " + strings.Join(order, ", ")
		}
		p.view.FullSQL = p.fullPolicySQL
	}
	if p.view.RequiresReexecution && !p.reexecutionConfirmed {
		return nil, &APIError{"query_reexecution_confirmation_required", "This operation reruns the applied filter; confirm before continuing"}
	}
	if req.Action == "append" {
		previous, err := v.browseAppendSource(s, c.id, p)
		if err != nil {
			return nil, err
		}
		p.fetchSize = min(p.view.PageSize, v.opts.MaxRows-len(previous.Rows))
	}
	switch req.Action {
	case "count":
		p.sql, p.policySQL = p.countSQL, p.countPolicySQL
	case "export":
		p.sql, p.policySQL = p.fullSQL, p.fullPolicySQL
	default:
		size := p.view.PageSize
		if p.fetchSize > 0 {
			size = p.fetchSize
		}
		p.sql = browsePageSQL(d, p.fullSQL, p.pageBaseSQL, p.orderSQL, p.table.Columns, p.view.Offset, size+1)
		p.policySQL = browsePageSQL(d, p.fullPolicySQL, p.pageBasePolicySQL, p.orderSQL, p.table.Columns, p.view.Offset, size+1)
	}
	return p, nil
}

func validateBrowseWhere(ctx context.Context, c *console, d dialect.ID, p *browsePlan) error {
	if p.view.Where == "" {
		return nil
	}
	if d == dialect.PostgreSQL {
		var standard string
		if err := c.conn.QueryRowContext(ctx, "SHOW standard_conforming_strings").Scan(&standard); err != nil {
			return err
		}
		if standard != "on" {
			return errors.New("Handwritten WHERE requires standard_conforming_strings=on; use structured conditions")
		}
	}
	err := sqlservice.ValidateBrowseWhere(d, p.view.Where, p.options, p.table.Columns)
	if err == nil {
		return nil
	}
	if replayErr := sqlservice.ValidateBrowseWhereForReexecution(d, p.view.Where, p.options, p.table.Columns); replayErr != nil {
		return err
	}
	p.view.RequiresReexecution = true
	if !p.reexecutionConfirmed {
		return &APIError{"query_reexecution_confirmation_required", "This filter runs functions or subqueries; confirm the condition before applying it"}
	}
	return nil
}

func browseColumn(table sqlservice.EditTable, name string) *sqlservice.EditColumn {
	for i := range table.Columns {
		if table.Columns[i].Name == name {
			return &table.Columns[i]
		}
	}
	return nil
}
func buildBrowseFilter(d dialect.ID, table sqlservice.EditTable, f browseFilter, args *[]any, depth int, budget *int) (string, string, error) {
	*budget--
	bad := errors.New("Invalid structured filter: use visible columns, bounded AND/OR groups and comparison values")
	if depth > 8 || *budget < 0 {
		return "", "", bad
	}
	if f.Op == "and" || f.Op == "or" {
		if len(f.Children) == 0 || f.Column != "" || len(f.Values) != 0 {
			return "", "", bad
		}
		parts, policies := []string{}, []string{}
		for _, child := range f.Children {
			sql, policy, err := buildBrowseFilter(d, table, child, args, depth+1, budget)
			if err != nil {
				return "", "", err
			}
			parts = append(parts, sql)
			policies = append(policies, policy)
		}
		join := " " + strings.ToUpper(f.Op) + " "
		return "(" + strings.Join(parts, join) + ")", "(" + strings.Join(policies, join) + ")", nil
	}
	col := browseColumn(table, f.Column)
	if col == nil || len(f.Children) > 0 {
		return "", "", bad
	}
	name := quoteDatabaseIdentifier(col.Name, d)
	if f.Op == "is_null" || f.Op == "not_null" {
		if len(f.Values) != 0 {
			return "", "", bad
		}
		suffix := " IS NULL"
		if f.Op == "not_null" {
			suffix = " IS NOT NULL"
		}
		return name + suffix, name + suffix, nil
	}
	operators := map[string]string{"eq": "=", "ne": "<>", "lt": "<", "le": "<=", "gt": ">", "ge": ">=", "like": "LIKE", "not_like": "NOT LIKE", "in": "IN", "not_in": "NOT IN"}
	op, ok := operators[f.Op]
	if !ok || len(f.Values) == 0 || len(f.Values) > 100 || ((f.Op != "in" && f.Op != "not_in") && len(f.Values) != 1) {
		return "", "", bad
	}
	slots, literals := []string{}, []string{}
	for _, raw := range f.Values {
		if len(raw) > 4096 {
			return "", "", bad
		}
		value, err := convertEditValue(*col, &raw)
		if err != nil {
			return "", "", err
		}
		*args = append(*args, value)
		slots = append(slots, editPlaceholder(d, len(*args)))
		lit, err := browsePolicyLiteral(d, fmt.Sprint(value))
		if err != nil {
			return "", "", err
		}
		literals = append(literals, lit)
	}
	sql, policy := strings.Join(slots, ", "), strings.Join(literals, ", ")
	if f.Op == "in" || f.Op == "not_in" {
		sql = "(" + sql + ")"
		policy = "(" + policy + ")"
	}
	return name + " " + op + " " + sql, name + " " + op + " " + policy, nil
}

// Appends are offset-checked twice: at submission and under the worker's console
// lock. Two queued requests for the same segment cannot append it twice.
func (v *Server) browseAppendSource(s *session, consoleID string, p *browsePlan) (*Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := s.results[p.sourceResultID]
	if result == nil || result.consoleID != consoleID || result.editStale || result.Browse == nil || !result.Browse.Supported || result.Browse.Offset != 0 || p.view.Offset != len(result.Rows) {
		return nil, errors.New("Browse result changed; restart continuous scrolling")
	}
	if result.Truncated || result.Browse.HasMore == nil || !*result.Browse.HasMore || len(result.Rows) >= v.opts.MaxRows {
		return nil, errors.New("No further rows can be appended to this cached result")
	}
	return result, nil
}

func (v *Server) collectBrowseAppend(s *session, e *execution, rows *sql.Rows) error {
	defer rows.Close()
	p := e.browse.plan
	previous, err := v.browseAppendSource(s, e.view.ConsoleID, p)
	if err != nil {
		return err
	}
	batch, err := v.readRowsWithMask(s, rows, func(labels []string) []string {
		return e.resultMaskColumns(s.grant.Dialect(), labels)
	}, p)
	if err != nil {
		return err
	}
	appendResultReplay(e, batch)
	published := false
	defer func() {
		if !published {
			s.mu.Lock()
			s.resultBytes -= batch.memoryBytes
			s.mu.Unlock()
		}
	}()
	if rows.NextResultSet() {
		return errors.New("Continuous browsing requires one result set")
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if !slices.Equal(previous.Columns, batch.Columns) {
		return errors.New("Table columns changed; refresh before continuing")
	}
	batch.consoleID, batch.sourceSQL, batch.browse = e.view.ConsoleID, e.resultSQL, e.browse
	batch.sourceAnalysis = e.resultAnalysis
	v.bindResultEditSource(s, e, []*Result{batch}, nil)
	if err := e.restoreSourceReadContext(); err != nil {
		return err
	}
	// Publish a new snapshot under the same ID. Existing readers and cached
	// exports keep immutable prefix slices; cells and opaque row refs never move.
	merged := *previous
	merged.Rows = append(slices.Clone(previous.Rows), batch.Rows...)
	merged.original = append(slices.Clone(previous.original), batch.original...)
	merged.masked = append(slices.Clone(previous.masked), batch.masked...)
	merged.copyLossy = append(slices.Clone(previous.copyLossy), batch.copyLossy...)
	merged.rowRefs = append(slices.Clone(previous.rowRefs), batch.rowRefs...)
	merged.TotalRows = len(merged.Rows)
	merged.memoryBytes += batch.memoryBytes
	merged.Truncated = batch.Truncated
	view := *batch.Browse
	view.Offset = 0
	merged.Browse = &view
	if merged.Truncated {
		view.LimitReached = "bytes"
	} else if merged.TotalRows >= v.opts.MaxRows && view.HasMore != nil && *view.HasMore {
		view.LimitReached = "rows"
		merged.Truncated = true
	}
	s.mu.Lock()
	if s.results[merged.ID] != previous {
		s.mu.Unlock()
		return errors.New("Browse result was released or changed; restart continuous scrolling")
	}
	s.results[merged.ID] = &merged
	s.mu.Unlock()
	published = true
	v.update(s, e, func(x *ExecutionView) {
		x.ResultIDs = append(x.ResultIDs, merged.ID)
		x.StatementResults = append(x.StatementResults, StatementResult{ResultID: merged.ID, SourceSQL: merged.SourceSQL})
	})
	return nil
}

// Keep browse admission independent from informational catalogs and EXPLAIN
// formats. These families have the required LIMIT/metadata contract; each
// connection must still supply complete authorized edit metadata.
func browseVendorVersionSupported(d dialect.ID, version sqlservice.MySQLServerVersion) bool {
	switch d {
	case dialect.MariaDB:
		release, suffix, ok := dialect.ParseMariaDBVersion(version.Product)
		return ok && release.Major > 0 && (suffix == "" || suffix[0] == '.' || suffix[0] == '-')
	case dialect.OceanBaseMySQL:
		// Direct VERSION() banners retain their combined MySQL/OceanBase
		// format gate. A separate product comment can use the release banner.
		parse := dialect.ParseOceanBaseMySQLBanner
		if version.FromVersionComment() {
			parse = dialect.ParseOceanBaseVersion
		}
		release, ok := parse(version.Product)
		return ok && release.Major > 0
	default:
		return false
	}
}

func browseVendorContext(ctx context.Context, c *console, d dialect.ID, options sqlservice.AnalyzeOptions) (version sqlservice.MySQLServerVersion, reason string, err error) {
	switch d {
	case dialect.PostgreSQL, dialect.MySQL, dialect.Oracle, dialect.SQLServer, dialect.Dameng, dialect.DB2, dialect.OceanBaseOracle, dialect.ClickHouse:
		return version, "", nil
	case dialect.MariaDB, dialect.OceanBaseMySQL:
		if err = sqlservice.ValidateBrowseSQLMode(d, options); err != nil {
			return
		}
		version, err = sqlservice.ReadMySQLServerVersion(ctx, c.conn, d)
		if err != nil {
			if ctx.Err() != nil {
				return version, "", ctx.Err()
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return version, "", err
			}
			if version.Source == "" {
				return version, "", errors.New("Unable to verify the server version for database browsing")
			}
			// The version comment was unavailable. Preserve cached browsing
			// after verifying the tenant on this connection.
		}
		if d == dialect.OceanBaseMySQL {
			var mode string
			if err = c.conn.QueryRowContext(ctx, "SELECT @@ob_compatibility_mode").Scan(&mode); err != nil || !strings.EqualFold(mode, "MYSQL") {
				return version, "", errors.New("Database browsing requires a verified OceanBase MySQL tenant")
			}
		}
		if !browseVendorVersionSupported(d, version) {
			return version, "Unable to verify the configured database product/version", nil
		}
		return version, "", nil
	default:
		return version, "legacy_cached_browse", nil
	}
}
