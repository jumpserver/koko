package engine

import (
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
	"net/http"
)

func (v *Server) catalogInspect(w http.ResponseWriter, r *http.Request) {
	req := v.beginCatalog(w, r, "inspect")
	if req == nil {
		return
	}
	defer req.done()
	info, err := sqlservice.InspectTable(req.ctx, req.conn, req.session.grant.Dialect(), sqlservice.CatalogColumnRequest{Database: req.database, Schema: req.scope.schema, Table: req.scope.table})
	if v.finishCatalog(w, r, req, len(info.Columns), err) {
		writeJSON(w, 200, info)
	}
}

func (v *Server) catalogDDL(w http.ResponseWriter, r *http.Request) {
	req := v.beginCatalog(w, r, "ddl")
	if req == nil {
		return
	}
	defer req.done()
	info, err := sqlservice.TableDefinition(req.ctx, req.conn, req.session.grant.Dialect(), sqlservice.CatalogColumnRequest{Database: req.database, Schema: req.scope.schema, Table: req.scope.table})
	if v.finishCatalog(w, r, req, 1, err) {
		writeJSON(w, 200, info)
	}
}
