package engine

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/dialect"
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
type catalogPage[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}
type catalogRequest struct {
	session  *session
	resource *catalogResource
	conn     *sql.Conn
	ctx      context.Context
	database string
	schema   string
	scope    catalogScope
	after    catalogPosition
	done     func()
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
	if (operation == "columns" || operation == "inspect" || operation == "ddl") && s.grant.Dialect() == dialect.PostgreSQL && scope.schema == "" {
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
		if separateCatalogDatabase(s.grant.Dialect()) {
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
	targeted := !consoleBound && scope.database != "" && separateCatalogDatabase(s.grant.Dialect())
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
		if pinnedMySQLCatalog(s.grant.Dialect()) {
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
		current, err = liveContext(sqlservice.CatalogQueryContext(ctx), c, s.grant.Dialect())
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
			checkout, cancel := context.WithTimeout(ctx, databaseCheckoutTimeout)
			conn, err = db.Conn(checkout)
			cancel()
		}
		if err == nil {
			connFinish := finish
			finish = func() { _ = conn.Close(); connFinish() }
			if s.grant.Dialect() == dialect.PostgreSQL && operation != "databases" {
				err = conn.QueryRowContext(ctx, "SELECT pg_catalog.current_database()").Scan(&current.Database)
			} else {
				current, err = connectionContext(ctx, conn, s.grant.Dialect(), sqlservice.Context{Database: s.grant.Config.Database, Schema: s.grant.Config.Schema})
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
		switch s.grant.Dialect() {
		case dialect.PostgreSQL, dialect.Oracle, dialect.Dameng, dialect.OceanBaseOracle, dialect.DB2:
			current.Schema = s.grant.Config.Schema
		}
	}
	// MySQL's configured database is an initial selection, not an authorization
	// boundary. Catalog SQL checks the connected account's visibility of the
	// requested database without issuing USE on the independent catalog pool.
	mysqlTree := !consoleBound && s.grant.Dialect().UsesMySQLSyntax()
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
		after, err := request.decodeCursor(scope.cursor)
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

func separateCatalogDatabase(d dialect.ID) bool {
	return d == dialect.PostgreSQL || d == dialect.SQLServer || d == dialect.ClickHouse
}

func (v *Server) catalogAudit(req *catalogRequest, outcome string, count int) error {
	now := time.Now()
	// A timeout or session closure must still leave its result audit. The
	// session's catalogOps barrier keeps the grant alive until this bounded
	// persistence operation completes, even after its query context is canceled.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(req.ctx), 3*time.Second)
	defer cancel()
	output := fmt.Sprintf("outcome=%s database=%q schema=%q table=%q kind=%s returned=%d", outcome, req.database, req.scope.schema, req.scope.table, req.scope.kind, count)
	if req.scope.collection != "" {
		output += " collection=" + req.scope.collection
	}
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
	case errors.Is(err, sqlservice.ErrCatalogUnsupported):
		fail(w, 422, "catalog_operation_unsupported", "This operation is not currently supported for the connected database version.")
	case errors.Is(err, errRoutineCatalogChanged):
		fail(w, 409, "catalog_contract_changed", "Catalog version changed; refresh the routine list")
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

type catalogName struct {
	Name string `json:"name"`
}

func (req *catalogRequest) namePage(rows []sqlservice.CatalogSchema) catalogPage[catalogName] {
	page := catalogPage[catalogName]{Items: []catalogName{}}
	if len(rows) > req.scope.limit {
		rows = rows[:req.scope.limit]
		page.HasMore = true
		page.NextCursor = req.nextCursor(catalogPosition{AfterName: rows[len(rows)-1].Name})
	}
	for _, row := range rows {
		page.Items = append(page.Items, catalogName{Name: row.Name})
	}
	return page
}

func (v *Server) catalogDatabases(w http.ResponseWriter, r *http.Request) {
	req := v.beginCatalog(w, r, "databases")
	if req == nil {
		return
	}
	defer req.done()
	items, err := sqlservice.DatabaseRows(req.ctx, req.conn, req.session.grant.Dialect(), req.database, req.after.AfterName, req.scope.limit)
	result := struct {
		Database string `json:"database"`
		Schema   string `json:"schema"`
		catalogPage[catalogName]
	}{req.database, req.schema, req.namePage(items)}
	if v.finishCatalog(w, r, req, len(result.Items), err) {
		writeJSON(w, 200, result)
	}
}

func (v *Server) catalogSchemas(w http.ResponseWriter, r *http.Request) {
	req := v.beginCatalog(w, r, "schemas")
	if req == nil {
		return
	}
	defer req.done()
	items, err := sqlservice.SchemaRows(req.ctx, req.conn, req.session.grant.Dialect(), req.database, req.after.AfterName, req.scope.limit)
	result := struct {
		Database string `json:"database"`
		catalogPage[catalogName]
	}{req.database, req.namePage(items)}
	if v.finishCatalog(w, r, req, len(result.Items), err) {
		writeJSON(w, 200, result)
	}
}
func (v *Server) catalogObjects(w http.ResponseWriter, r *http.Request) {
	req := v.beginCatalog(w, r, "objects")
	if req == nil {
		return
	}
	defer req.done()
	if req.scope.collection == "routine" {
		v.catalogRoutines(w, r, req)
		return
	}
	items, err := sqlservice.RelationRows(req.ctx, req.conn, req.session.grant.Dialect(), req.relationRequest())
	if items == nil {
		items = []sqlservice.CatalogRelation{}
	}
	result := catalogPage[sqlservice.CatalogRelation]{Items: items}
	if len(items) > req.scope.limit {
		result.HasMore = true
		result.Items = items[:req.scope.limit]
		last := result.Items[len(result.Items)-1]
		result.NextCursor = req.nextCursor(catalogPosition{AfterSchema: last.Schema, AfterName: last.Name})
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
	items, err := sqlservice.ColumnRows(req.ctx, req.conn, req.session.grant.Dialect(), sqlservice.CatalogColumnRequest{Database: req.database, Schema: req.scope.schema, Table: req.scope.table, AfterOrdinal: req.after.AfterOrdinal, Limit: req.scope.limit})
	result := catalogPage[sqlservice.Column]{Items: []sqlservice.Column{}}
	if len(items) > req.scope.limit {
		result.HasMore = true
		items = items[:req.scope.limit]
	}
	for _, item := range items {
		result.Items = append(result.Items, sqlservice.Column{Name: item.Name, DataType: item.DataType, Nullable: item.Nullable, Comment: item.Comment})
	}
	if result.HasMore {
		result.NextCursor = req.nextCursor(catalogPosition{AfterOrdinal: items[len(items)-1].Ordinal})
	}
	if v.finishCatalog(w, r, req, len(result.Items), err) {
		writeJSON(w, 200, result)
	}
}
