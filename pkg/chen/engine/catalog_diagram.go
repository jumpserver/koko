package engine

import (
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
	"net/http"
)

func (v *Server) catalogDiagram(w http.ResponseWriter, r *http.Request) {
	req := v.beginCatalog(w, r, "diagram")
	if req == nil {
		return
	}
	defer req.done()
	data, err := sqlservice.CatalogDiagramRows(req.ctx, req.conn, req.session.grant.Dialect(), sqlservice.DiagramRequest{
		CatalogObjectRequest: sqlservice.CatalogObjectRequest{Database: req.database, Schema: req.scope.schema, Limit: req.scope.limit, AfterSchema: req.after.AfterSchema, AfterName: req.after.AfterName}, Section: req.scope.section, Direction: req.scope.direction, Table: req.scope.table, AfterTable: req.after.AfterTable, Contract: req.after.DiagramContract, Continuation: req.scope.cursor != ""})
	if err != nil {
		v.finishCatalog(w, r, req, 0, err)
		return
	}
	req.scope.diagramContract = data.Contract
	if data.HasMore && len(data.Items) > 0 {
		last := data.Items[len(data.Items)-1]
		schema, name, table := "", "", ""
		if last.Object != nil {
			schema, name = last.Object.Schema, last.Object.Name
		} else {
			schema, name, table = last.Edge.Source.Schema, last.Edge.Name, last.Edge.Source.Name
		}
		data.NextCursor = req.resource.encodeCursor(req.session.id, req.database, req.scope, schema, name, 0, table)
	}
	if v.finishCatalog(w, r, req, len(data.Items), nil) {
		writeJSON(w, 200, data)
	}
}
