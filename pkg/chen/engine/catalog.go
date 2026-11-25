package engine

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

// Each session has at most two catalog connections, separately budgeted from
// fixed SQL consoles and their cancellation pool. No USE/SET is issued here.
type catalogResource struct {
	db        *sql.DB
	slots     chan struct{}
	mu        sync.Mutex
	database  string
	cursorKey [32]byte
}
type catalogCursor struct {
	Contract        string `json:"oc,omitempty"`
	DiagramContract string `json:"dc,omitempty"`
	Direction       string `json:"direction,omitempty"`
	Section         string `json:"section,omitempty"`
	Sort            string `json:"sort,omitempty"`
	Limit           int    `json:"limit,omitempty"`
	AfterTable      string `json:"at,omitempty"`
	Version         int    `json:"v"`
	Session         string `json:"s"`
	Database        string `json:"d"`
	Operation       string `json:"o"`
	Schema          string `json:"n,omitempty"`
	Kind            string `json:"k,omitempty"`
	Query           string `json:"q,omitempty"`
	AfterSchema     string `json:"as,omitempty"`
	AfterName       string `json:"an,omitempty"`
	AfterOrdinal    int    `json:"ao,omitempty"`
	Table           string `json:"t,omitempty"`
}
type catalogPage[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}
type catalogScope struct {
	operation, schema, kind, query, table, cursor string
	section, sort, database, direction            string
	limit                                         int
	overviewContract                              string
	diagramContract                               string
}
type catalogRequest struct {
	session  *session
	resource *catalogResource
	conn     *sql.Conn
	ctx      context.Context
	database string
	schema   string
	scope    catalogScope
	after    catalogCursor
	done     func()
}

func parseCatalogScope(r *http.Request, operation string) (catalogScope, error) {
	s := catalogScope{operation: operation, limit: 100}
	allowed := map[string]bool{"cursor": true, "limit": true}
	if operation != "databases" && operation != "ai" {
		allowed["database"] = true
	}
	switch operation {
	case "diagram":
		for _, key := range []string{"database", "schema", "section", "direction", "table"} {
			allowed[key] = true
		}
	case "overview":
		for _, key := range []string{"database", "schema", "section", "q", "sort"} {
			allowed[key] = true
		}
	case "objects", "ai":
		allowed["schema"], allowed["kind"], allowed["q"] = true, true, true
	case "columns", "inspect", "ddl":
		allowed["schema"], allowed["table"] = true, true
	}
	for key, values := range r.URL.Query() {
		if !allowed[key] || len(values) != 1 {
			return s, errors.New("invalid_catalog_request")
		}
	}
	values := r.URL.Query()
	s.section, s.sort, s.database = values.Get("section"), values.Get("sort"), values.Get("database")
	if operation == "overview" {
		if s.sort == "" {
			s.sort = "name_asc"
		}
		if s.sort != "name_asc" && s.sort != "name_desc" {
			return s, errors.New("invalid_catalog_request")
		}
		switch s.section {
		case "tables", "views", "indexes", "statistics", "properties":
		default:
			return s, errors.New("invalid_catalog_request")
		}
		if s.database == "" {
			return s, errors.New("invalid_catalog_request")
		}
		if (s.section == "statistics" || s.section == "properties") && (values.Get("q") != "" || values.Get("cursor") != "") {
			return s, errors.New("invalid_catalog_request")
		}
	}
	if operation == "diagram" {
		s.direction = values.Get("direction")
		if s.database == "" || (s.section != "nodes" && s.section != "edges") || (s.direction != "schema" && s.direction != "incoming") || s.direction == "schema" && values.Get("table") != "" || s.direction == "incoming" && (values.Get("table") == "" || values.Get("schema") == "") {
			return s, errors.New("invalid_catalog_request")
		}
	}
	s.schema, s.kind, s.query, s.table, s.cursor = values.Get("schema"), values.Get("kind"), values.Get("q"), values.Get("table"), values.Get("cursor")
	for _, name := range []string{s.schema, s.query, s.table, s.database} {
		if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 256 || strings.ContainsRune(name, 0) {
			return s, errors.New("invalid_catalog_request")
		}
	}
	if s.kind != "" && s.kind != "table" && s.kind != "view" {
		return s, errors.New("invalid_catalog_request")
	}
	if (operation == "columns" || operation == "inspect" || operation == "ddl") && s.table == "" {
		return s, errors.New("invalid_catalog_request")
	}
	if len(s.cursor) > 4096 {
		return s, errors.New("invalid_catalog_cursor")
	}
	if raw := values.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return s, errors.New("invalid_catalog_request")
		}
		s.limit = min(n, 200)
	}
	if operation == "diagram" {
		s.limit = min(s.limit, 100)
	}
	return s, nil
}

func (v *Server) beginCatalog(w http.ResponseWriter, r *http.Request, operation string) *catalogRequest {
	return v.beginCatalogAccess(w, r, operation, nil)
}

// AI editor metadata uses its existing console connection. Database/root
// contexts use the same independently authorized pool as the resource tree.
func (v *Server) beginCatalogAccess(w http.ResponseWriter, r *http.Request, operation string, editor *aiEditorContext) *catalogRequest {
	s := v.auth(w, r, false)
	if s == nil {
		return nil
	}
	scope, err := parseCatalogScope(r, operation)
	if err != nil {
		fail(w, 400, err.Error(), "Invalid catalog request or page cursor")
		return nil
	}
	if editor != nil && editor.ConsoleID == "" {
		scope.database = editor.Database
	}
	if (operation == "columns" || operation == "inspect" || operation == "ddl") && s.grant.Config.Dialect == "postgresql" && scope.schema == "" {
		fail(w, 400, "invalid_catalog_request", "PostgreSQL columns require an explicit schema")
		return nil
	}
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()
		fail(w, 410, "session_closed", "Session is closed")
		return nil
	}
	if s.catalog == nil {
		db, err := catalogPool.openDatabase(s.grant.Config, 2)
		if err != nil {
			s.mu.Unlock()
			fail(w, 502, "catalog_unavailable", "Unable to initialize catalog access")
			return nil
		}
		db.SetMaxIdleConns(1)
		if separateCatalogDatabase(s.grant.Config.Dialect) {
			// Cross-database reads own a short-lived pool. Do not retain an
			// additional default connection outside the two catalog slots.
			db.SetMaxIdleConns(0)
		}
		db.SetConnMaxIdleTime(time.Minute)
		resource := &catalogResource{db: db, slots: make(chan struct{}, 2)}
		if _, err := rand.Read(resource.cursorKey[:]); err != nil {
			s.mu.Unlock()
			_ = db.Close()
			fail(w, 503, "catalog_unavailable", "Unable to initialize catalog access")
			return nil
		}
		s.catalog = resource
	}
	resource := s.catalog
	s.catalogOps.Add(1)
	s.mu.Unlock()
	ctx, cancel := catalogQueryContext(r.Context(), s.ctx, operation)
	s.grant.SetBusy(true)
	s.grant.Touch()
	finish := func() { cancel(); s.grant.SetBusy(false); s.catalogOps.Done() }
	release, err := acquireCatalogSlots(ctx, operation, resource.slots, v.catalogSlots)
	if err != nil {
		finish()
		catalogError(w, r, s, err)
		return nil
	}
	endOperation := finish
	finish = func() { release(); endOperation() }
	if err := s.grant.Check(); err != nil {
		finish()
		fail(w, 403, "session_unavailable", err.Error())
		return nil
	}
	var conn *sql.Conn
	var current sqlservice.Context
	consoleBound := editor != nil && editor.ConsoleID != ""
	targeted := !consoleBound && scope.database != "" && separateCatalogDatabase(s.grant.Config.Dialect)
	if consoleBound {
		lookup := r.Clone(ctx)
		lookup.SetPathValue("cid", editor.ConsoleID)
		c := v.findConsole(w, lookup, s)
		if c == nil {
			finish()
			return nil
		}
		if !c.mu.TryLock() {
			finish()
			fail(w, 409, "console_busy", "AI metadata is unavailable while this console is executing")
			return nil
		}
		if err := ctx.Err(); err != nil {
			c.mu.Unlock()
			finish()
			catalogError(w, r, s, err)
			return nil
		}
		consoleFinish := finish
		var stopConsole context.CancelFunc
		if pinnedMySQLCatalog(s.grant.Config.Dialect) {
			ctx = sqlservice.WithConsoleCatalogLifetime(ctx, c.ctx)
			stopConsole = interruptCatalogOnCancel(ctx, s, c)
		} else {
			ctx, stopConsole = context.WithTimeout(c.ctx, catalogInspectionTimeout)
		}
		finish = func() { stopConsole(); c.mu.Unlock(); consoleFinish() }
		if c.closed.Load() || c.lost.Load() || c.conn == nil {
			finish()
			fail(w, 409, "connection_lost", "Open a new console; the previous connection state cannot be restored")
			return nil
		}
		conn = c.conn
		current, err = liveContext(sqlservice.CatalogQueryContext(ctx), c, s.grant.Config.Dialect)
	} else {
		db := resource.db
		if targeted {
			cfg := s.grant.Config
			cfg.Database = scope.database
			db, err = catalogPool.openDatabase(cfg, 1)
			if err == nil {
				db.SetMaxIdleConns(0)
				poolFinish := finish
				finish = func() { _ = db.Close(); poolFinish() }
			}
		}
		if err == nil {
			conn, err = db.Conn(ctx)
		}
		if err == nil {
			connFinish := finish
			finish = func() { _ = conn.Close(); connFinish() }
			if s.grant.Config.Dialect == "postgresql" && operation != "databases" {
				err = conn.QueryRowContext(ctx, "SELECT pg_catalog.current_database()").Scan(&current.Database)
			} else {
				current, err = connectionContext(ctx, conn, s.grant.Config.Dialect, sqlservice.Context{Database: s.grant.Config.Database, Schema: s.grant.Config.Schema})
			}
		}
	}
	database := current.Database
	if err != nil {
		finish()
		catalogError(w, r, s, err)
		return nil
	}
	if operation == "databases" && s.grant.Config.Schema != "" {
		switch s.grant.Config.Dialect {
		case "postgresql", "oracle", "dameng", "oceanbase-oracle", "db2":
			current.Schema = s.grant.Config.Schema
		}
	}
	// MySQL's configured database is an initial selection, not an authorization
	// boundary. Catalog SQL checks the connected account's visibility of the
	// requested database without issuing USE on the independent catalog pool.
	mysqlTree := !consoleBound && core.IsMySQLDialect(s.grant.Config.Dialect)
	if mysqlTree && operation != "schemas" && operation != "databases" {
		if scope.database != "" {
			database = scope.database
			if scope.schema != "" && scope.schema != database {
				finish()
				fail(w, 403, "catalog_forbidden", "Catalog schema does not match requested database")
				return nil
			}
		} else if scope.schema != "" {
			database = scope.schema
		}
	}
	if !mysqlTree && scope.database != "" && scope.database != database {
		finish()
		fail(w, 403, "catalog_forbidden", "Catalog database does not match requested connection")
		return nil
	}
	if (operation == "overview" || operation == "diagram") && scope.database != database {
		finish()
		fail(w, 403, "catalog_forbidden", "Catalog database does not match authorized session")
		return nil
	}
	if database == "" && !(mysqlTree && (operation == "schemas" || operation == "databases")) {
		finish()
		fail(w, 409, "catalog_database_required", "Configure a database before browsing its catalog")
		return nil
	}
	if consoleBound {
		if current.Schema == "" || database != editor.Database || current.Schema != editor.Schema {
			finish()
			fail(w, 409, "catalog_scope_changed", "SQL editor scope changed; reopen the query in the current database and schema")
			return nil
		}
	} else if !mysqlTree && !targeted {
		// The default pool remains bound to its original database. Explicit
		// PostgreSQL, SQL Server and ClickHouse targets use independent pools.
		resource.mu.Lock()
		if resource.database == "" {
			resource.database = database
		}
		sameDatabase := resource.database == database
		resource.mu.Unlock()
		if !sameDatabase {
			finish()
			fail(w, 409, "catalog_scope_changed", "Catalog database scope changed")
			return nil
		}
	}
	request := &catalogRequest{session: s, resource: resource, conn: conn, ctx: ctx, database: database, schema: current.Schema, scope: scope, done: finish}
	if scope.cursor != "" {
		after, err := resource.decodeCursor(scope.cursor, s.id, database, scope)
		if err != nil {
			finish()
			fail(w, 400, "invalid_catalog_cursor", "Page cursor does not match this catalog request")
			return nil
		}
		request.after = after
	}
	if err := v.catalogAudit(request, "requested", 0); err != nil {
		finish()
		fail(w, 503, "audit_persistence_failed", "Catalog access could not be audited")
		return nil
	}
	return request
}

func separateCatalogDatabase(dialect string) bool {
	return dialect == "postgresql" || dialect == "sqlserver" || dialect == "clickhouse"
}

func (v *Server) catalogAudit(req *catalogRequest, outcome string, count int) error {
	now := time.Now()
	// A timeout or session closure must still leave its result audit. The
	// session's catalogOps barrier keeps the grant alive until this bounded
	// persistence operation completes, even after its query context is canceled.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(req.ctx), 3*time.Second)
	defer cancel()
	output := fmt.Sprintf("outcome=%s database=%q schema=%q table=%q kind=%s returned=%d", outcome, req.database, req.scope.schema, req.scope.table, req.scope.kind, count)
	if req.scope.operation == "diagram" {
		output += fmt.Sprintf(" direction=%s section=%s", req.scope.direction, req.scope.section)
	}
	if req.scope.operation == "overview" {
		output += fmt.Sprintf(" section=%s sort=%s", req.scope.section, req.scope.sort)
	}
	return v.provider.Record(ctx, req.session.grant, core.AuditRecord{ID: newID("meta_"), RiskClassified: true, SQL: "/* Chen catalog " + req.scope.operation + " */", Output: output, StartedAt: now, FinishedAt: now})
}
func (v *Server) finishCatalog(w http.ResponseWriter, r *http.Request, req *catalogRequest, count int, err error) bool {
	if req.ctx.Err() != nil {
		err = req.ctx.Err()
	}
	outcome := "succeeded"
	if err != nil {
		outcome = "failed"
	}
	if auditErr := v.catalogAudit(req, outcome, count); auditErr != nil {
		fail(w, 503, "audit_persistence_failed", "Catalog result could not be audited")
		return false
	}
	if err != nil {
		catalogError(w, r, req.session, err)
		return false
	}
	req.session.mu.Lock()
	closed := req.session.closed
	req.session.mu.Unlock()
	if closed || req.session.ctx.Err() != nil {
		fail(w, 410, "session_closed", "Session is closed")
		return false
	}
	if err := req.session.grant.Check(); err != nil {
		fail(w, 403, "session_unavailable", err.Error())
		return false
	}
	return true
}
func catalogError(w http.ResponseWriter, r *http.Request, s *session, err error) {
	if diagnostic := safeDatabaseError(err); diagnostic != nil {
		switch diagnostic.Code {
		case "42501", "MSSQL_229", "MSSQL_916", "MSSQL_4060", "CLICKHOUSE_497":
			err = errors.Join(sqlservice.ErrCatalogForbidden, err)
		}
	}
	switch {
	case s.ctx.Err() != nil:
		fail(w, 410, "session_closed", "Session is closed")
	case errors.Is(err, sqlservice.ErrCatalogForbidden):
		fail(w, 403, "catalog_forbidden", "Catalog access is denied")
	case errors.Is(err, sqlservice.ErrDiagramContractChanged):
		fail(w, 409, "diagram_contract_changed", "Catalog product, version or fields changed; refresh the diagram")
	case errors.Is(err, sqlservice.ErrOverviewContractChanged):
		fail(w, 409, "overview_contract_changed", "Catalog product, version or fields changed; refresh the overview")
	case errors.Is(err, sqlservice.ErrCatalogNotFound):
		fail(w, 404, "catalog_not_found", "Object is unavailable or does not exist")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		fail(w, 504, "catalog_timeout", "Catalog request timed out")
	case r.Context().Err() != nil:
		fail(w, 408, "request_canceled", "Catalog request was canceled")
	default:
		fail(w, 502, "catalog_failed", "Unable to read the authorized catalog")
	}
}

func (v *Server) catalogDatabases(w http.ResponseWriter, r *http.Request) {
	req := v.beginCatalog(w, r, "databases")
	if req == nil {
		return
	}
	defer req.done()
	items, err := sqlservice.DatabaseRows(req.ctx, req.conn, sqlservice.Dialect(req.session.grant.Config.Dialect), req.database, req.after.AfterName, req.scope.limit)
	type databaseItem struct {
		Name string `json:"name"`
	}
	result := struct {
		Database string `json:"database"`
		Schema   string `json:"schema"`
		catalogPage[databaseItem]
	}{Database: req.database, Schema: req.schema, catalogPage: catalogPage[databaseItem]{Items: []databaseItem{}}}
	if len(items) > req.scope.limit {
		result.HasMore = true
		items = items[:req.scope.limit]
	}
	for _, item := range items {
		result.Items = append(result.Items, databaseItem{item.Name})
	}
	if result.HasMore {
		result.NextCursor = req.resource.encodeCursor(req.session.id, req.database, req.scope, "", items[len(items)-1].Name, 0)
	}
	if v.finishCatalog(w, r, req, len(items), err) {
		writeJSON(w, 200, result)
	}
}

func (v *Server) catalogSchemas(w http.ResponseWriter, r *http.Request) {
	req := v.beginCatalog(w, r, "schemas")
	if req == nil {
		return
	}
	defer req.done()
	items, err := sqlservice.SchemaRows(req.ctx, req.conn, sqlservice.Dialect(req.session.grant.Config.Dialect), req.database, req.after.AfterName, req.scope.limit)
	type schemaItem struct {
		Name string `json:"name"`
	}
	result := struct {
		Database string `json:"database"`
		catalogPage[schemaItem]
	}{Database: req.database, catalogPage: catalogPage[schemaItem]{Items: []schemaItem{}}}
	if len(items) > req.scope.limit {
		result.HasMore = true
		items = items[:req.scope.limit]
	}
	for _, item := range items {
		result.Items = append(result.Items, schemaItem{item.Name})
	}
	if result.HasMore {
		result.NextCursor = req.resource.encodeCursor(req.session.id, req.database, req.scope, "", items[len(items)-1].Name, 0)
	}
	if v.finishCatalog(w, r, req, len(items), err) {
		writeJSON(w, 200, result)
	}
}
func (v *Server) catalogObjects(w http.ResponseWriter, r *http.Request) {
	req := v.beginCatalog(w, r, "objects")
	if req == nil {
		return
	}
	defer req.done()
	items, err := sqlservice.ObjectRows(req.ctx, req.conn, sqlservice.Dialect(req.session.grant.Config.Dialect), sqlservice.CatalogObjectRequest{Database: req.database, Schema: req.scope.schema, Kind: req.scope.kind, Query: req.scope.query, AfterSchema: req.after.AfterSchema, AfterName: req.after.AfterName, Limit: req.scope.limit})
	if items == nil {
		items = []sqlservice.CatalogObject{}
	}
	result := catalogPage[sqlservice.CatalogObject]{Items: items}
	if len(items) > req.scope.limit {
		result.HasMore = true
		result.Items = items[:req.scope.limit]
		last := result.Items[len(result.Items)-1]
		result.NextCursor = req.resource.encodeCursor(req.session.id, req.database, req.scope, last.Schema, last.Name, 0)
	}
	if v.finishCatalog(w, r, req, len(result.Items), err) {
		writeJSON(w, 200, result)
	}
}
func (v *Server) catalogColumns(w http.ResponseWriter, r *http.Request) {
	req := v.beginCatalog(w, r, "columns")
	if req == nil {
		return
	}
	defer req.done()
	items, err := sqlservice.ColumnRows(req.ctx, req.conn, sqlservice.Dialect(req.session.grant.Config.Dialect), sqlservice.CatalogColumnRequest{Database: req.database, Schema: req.scope.schema, Table: req.scope.table, AfterOrdinal: req.after.AfterOrdinal, Limit: req.scope.limit})
	result := catalogPage[sqlservice.Column]{Items: []sqlservice.Column{}}
	if len(items) > req.scope.limit {
		result.HasMore = true
		items = items[:req.scope.limit]
	}
	for _, item := range items {
		result.Items = append(result.Items, sqlservice.Column{Name: item.Name, DataType: item.DataType, Nullable: item.Nullable, Comment: item.Comment})
	}
	if result.HasMore {
		result.NextCursor = req.resource.encodeCursor(req.session.id, req.database, req.scope, "", "", items[len(items)-1].Ordinal)
	}
	if v.finishCatalog(w, r, req, len(items), err) {
		writeJSON(w, 200, result)
	}
}

func (c *catalogResource) encodeCursor(session, database string, scope catalogScope, afterSchema, afterName string, afterOrdinal int, afterTable ...string) string {
	value := catalogCursor{Version: 1, Session: session, Database: database, Operation: scope.operation, Schema: scope.schema, Kind: scope.kind, Query: scope.query, Table: scope.table, AfterSchema: afterSchema, AfterName: afterName, AfterOrdinal: afterOrdinal}
	if scope.operation == "overview" || scope.operation == "diagram" {
		value.Contract = scope.overviewContract
		value.DiagramContract = scope.diagramContract
		value.Direction = scope.direction
		value.Section = scope.section
		value.Sort = scope.sort
		value.Limit = scope.limit
	}
	if len(afterTable) > 0 {
		value.AfterTable = afterTable[0]
	}
	data, _ := json.Marshal(value)
	mac := hmac.New(sha256.New, c.cursorKey[:])
	_, _ = mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (c *catalogResource) decodeCursor(encoded, session, database string, scope catalogScope) (catalogCursor, error) {
	var value catalogCursor
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 {
		return value, errors.New("invalid cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return value, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return value, err
	}
	mac := hmac.New(sha256.New, c.cursorKey[:])
	_, _ = mac.Write(data)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return value, errors.New("invalid cursor")
	}
	if json.Unmarshal(data, &value) != nil || (scope.operation == "overview" || scope.operation == "diagram") && (value.Direction != scope.direction || value.Section != scope.section || value.Sort != scope.sort || value.Limit != scope.limit) || value.Version != 1 || value.Session != session || value.Database != database || value.Operation != scope.operation || value.Schema != scope.schema || value.Kind != scope.kind || value.Query != scope.query || value.Table != scope.table {
		return value, errors.New("cursor scope mismatch")
	}
	return value, nil
}
