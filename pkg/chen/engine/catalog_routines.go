package engine

import (
	"errors"
	"net/http"

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
	if req.session.grant.Dialect() != dialect.PostgreSQL {
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
	rows, err := sqlservice.PostgreSQLRoutineRows(req.ctx, req.conn, sqlservice.RoutineListRequest{
		Database: req.database, Schema: req.scope.schema, Query: req.scope.query, Kind: sqlservice.ObjectKind(req.scope.kind),
		AfterSchema: req.after.AfterSchema, AfterName: req.after.AfterName, AfterKind: sqlservice.ObjectKind(req.after.AfterKind), AfterOID: req.after.AfterOID,
		Limit: req.scope.limit, ServerVersion: version,
	})
	page := catalogPage[sqlservice.ObjectSummary]{Items: rows}
	if len(rows) > req.scope.limit {
		page.HasMore = true
		page.Items = rows[:req.scope.limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = req.nextCursor(catalogPosition{
			AfterSchema: last.Ref.Schema, AfterName: last.Ref.Name, AfterKind: string(last.Ref.Kind), AfterOID: last.OID, ServerVersion: version,
		})
	}
	if v.finishCatalog(w, r, req, len(page.Items), err) {
		writeJSON(w, http.StatusOK, page)
	}
}
