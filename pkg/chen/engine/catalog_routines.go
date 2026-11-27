package engine

import (
	"errors"
	"net/http"
	"path"

	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

var errRoutineCatalogChanged = errors.New("routine catalog version changed")

func (v *Server) catalogCapabilities(w http.ResponseWriter, r *http.Request) {
	req := v.beginCatalog(w, r, "capabilities")
	if req == nil {
		return
	}
	defer req.done()
	data, err := sqlservice.ReadCatalogCapabilities(req.ctx, req.conn, req.session.grant.Dialect(), req.database)
	if v.finishCatalog(w, r, req, len(data.Collections), err) {
		writeJSON(w, http.StatusOK, data)
	}
}

func (v *Server) catalogRoutines(w http.ResponseWriter, r *http.Request, req *catalogRequest) {
	d := req.session.grant.Dialect()
	if d.UsesMySQLSyntax() {
		v.catalogMySQLObjects(w, r, req, d)
		return
	}
	if d != dialect.PostgreSQL || req.scope.collection == "event" {
		v.finishCatalog(w, r, req, 0, sqlservice.ErrCatalogUnsupported)
		return
	}
	version, err := sqlservice.PostgreSQLCatalogVersion(req.ctx, req.conn)
	if err != nil {
		v.finishCatalog(w, r, req, 0, err)
		return
	}
	if req.scope.cursor != "" && req.after.ServerVersion != version {
		v.finishCatalog(w, r, req, 0, errRoutineCatalogChanged)
		return
	}
	var rows []sqlservice.ObjectSummary
	if req.scope.collection == "all" {
		rows, err = sqlservice.PostgreSQLSearchObjects(req.ctx, req.conn, sqlservice.ObjectSearchRequest{
			Database: req.database, Schema: req.scope.schema, Query: req.scope.query, ServerVersion: version, Limit: req.scope.limit,
			After: sqlservice.ObjectSummary{Ref: sqlservice.ObjectRef{Schema: req.after.AfterSchema, Name: req.after.AfterName, Kind: sqlservice.ObjectKind(req.after.AfterKind), Table: req.after.AfterTable}, OID: req.after.AfterOID},
		})
	} else if req.scope.collection == "trigger" {
		if version < 100000 {
			err = sqlservice.ErrCatalogUnsupported
		} else {
			rows, err = sqlservice.PostgreSQLTriggerRows(req.ctx, req.conn, sqlservice.TriggerListRequest{
				ServerVersion: version, Database: req.database, Schema: req.scope.schema, Table: req.scope.table, Query: req.scope.query, FunctionOID: req.scope.functionOID,
				AfterSchema: req.after.AfterSchema, AfterName: req.after.AfterName, AfterTable: req.after.AfterTable, Limit: req.scope.limit,
			})
		}
	} else {
		rows, err = sqlservice.PostgreSQLRoutineRows(req.ctx, req.conn, sqlservice.RoutineListRequest{
			Database: req.database, Schema: req.scope.schema, Query: req.scope.query, Kind: sqlservice.ObjectKind(req.scope.kind),
			AfterSchema: req.after.AfterSchema, AfterName: req.after.AfterName, AfterKind: sqlservice.ObjectKind(req.after.AfterKind), AfterOID: req.after.AfterOID,
			Limit: req.scope.limit, ServerVersion: version,
		})
	}
	page := catalogPage[sqlservice.ObjectSummary]{Items: rows}
	if len(rows) > req.scope.limit {
		page.HasMore = true
		page.Items = rows[:req.scope.limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = req.nextCursor(catalogPosition{
			AfterSchema: last.Ref.Schema, AfterName: last.Ref.Name, AfterKind: string(last.Ref.Kind), AfterOID: last.OID, AfterTable: last.Ref.Table, ServerVersion: version,
		})
	}
	if v.finishCatalog(w, r, req, len(page.Items), err) {
		writeJSON(w, http.StatusOK, page)
	}
}

func (v *Server) catalogProgramObject(w http.ResponseWriter, r *http.Request) {
	op := path.Base(r.URL.Path)
	req := v.beginCatalog(w, r, op)
	if req == nil {
		return
	}
	defer req.done()
	d := req.session.grant.Dialect()
	if d != dialect.PostgreSQL && !d.UsesMySQLSyntax() || d == dialect.PostgreSQL && req.scope.ref.Kind == sqlservice.EventKind || d.UsesMySQLSyntax() && req.scope.oid != 0 {
		v.finishCatalog(w, r, req, 0, sqlservice.ErrCatalogUnsupported)
		return
	}
	if op == "object-detail" {
		var data sqlservice.ObjectDetail
		var err error
		if d.UsesMySQLSyntax() {
			data, err = sqlservice.MySQLObjectDetail(req.ctx, req.conn, d, req.scope.ref)
		} else {
			data, err = sqlservice.PostgreSQLObjectDetail(req.ctx, req.conn, req.scope.ref, req.scope.oid)
		}
		if v.finishCatalog(w, r, req, 1, err) {
			writeJSON(w, 200, data)
		}
		return
	}
	authorize := func(command string) error { return v.provider.Authorize(req.ctx, req.session.grant, command) }
	var data sqlservice.ObjectDefinition
	var err error
	if d.UsesMySQLSyntax() {
		data, err = sqlservice.MySQLObjectDefinitionPage(req.ctx, req.conn, d, req.scope.ref, req.scope.definitionPage, authorize)
	} else {
		data, err = sqlservice.PostgreSQLObjectDefinitionPage(req.ctx, req.conn, req.scope.ref, req.scope.oid, req.scope.definitionPage, authorize)
	}
	var policy *core.PolicyError
	if errors.As(err, &policy) {
		req.outcome = policy.Code
		if !v.finishCatalog(w, r, req, 0, nil) {
			return
		}
		if policy.Code == "command_review_required" || policy.Code == "command_warning_confirmation_required" {
			fail(w, 403, policy.Code, "Reading the definition requires approval in a console")
		} else {
			data.Status = "restricted"
			writeJSON(w, 200, data)
		}
		return
	}
	if err == nil {
		req.outcome = data.Status
	}
	if v.finishCatalog(w, r, req, 1, err) {
		writeJSON(w, 200, data)
	}
}

func (v *Server) catalogMySQLObjects(w http.ResponseWriter, r *http.Request, req *catalogRequest, d dialect.ID) {
	if req.scope.functionOID != 0 {
		v.finishCatalog(w, r, req, 0, sqlservice.ErrCatalogUnsupported)
		return
	}
	p, err := sqlservice.ReadMySQLObjectProfile(req.ctx, req.conn, d)
	if err != nil {
		v.finishCatalog(w, r, req, 0, err)
		return
	}
	if req.scope.cursor != "" && req.after.Contract != p.Contract {
		v.finishCatalog(w, r, req, 0, errRoutineCatalogChanged)
		return
	}
	rows, err := sqlservice.MySQLObjectRows(req.ctx, req.conn, d, p, sqlservice.MySQLObjectListRequest{
		ObjectSearchRequest: sqlservice.ObjectSearchRequest{Database: req.database, Schema: req.scope.schema, Query: req.scope.query, Limit: req.scope.limit,
			After: sqlservice.ObjectSummary{Ref: sqlservice.ObjectRef{Name: req.after.AfterName, Kind: sqlservice.ObjectKind(req.after.AfterKind), Table: req.after.AfterTable}}},
		Collection: req.scope.collection, Kind: sqlservice.ObjectKind(req.scope.kind), Table: req.scope.table})
	page := catalogPage[sqlservice.ObjectSummary]{Items: rows}
	if len(rows) > req.scope.limit {
		page.HasMore = true
		page.Items = rows[:req.scope.limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = req.nextCursor(catalogPosition{AfterName: last.Ref.Name, AfterKind: string(last.Ref.Kind), AfterTable: last.Ref.Table, Contract: p.Contract})
	}
	if v.finishCatalog(w, r, req, len(page.Items), err) {
		writeJSON(w, 200, page)
	}
}
