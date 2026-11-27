package engine

import (
	"encoding/json"
	"net/http"

	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

func (v *Server) catalogOverview(w http.ResponseWriter, r *http.Request) {
	req := v.beginCatalog(w, r, "overview")
	if req == nil {
		return
	}
	defer req.done()
	data, err := sqlservice.CatalogOverviewRows(req.ctx, req.conn, req.session.grant.Dialect(), sqlservice.OverviewRequest{
		CatalogObjectRequest: req.relationRequest(), Section: req.scope.section, Sort: req.scope.sort, AfterTable: req.after.AfterTable, AfterOrdinal: req.after.AfterOrdinal, AfterContract: req.after.Contract})
	if err != nil {
		v.finishCatalog(w, r, req, 0, err)
		return
	}
	if len(data.Rows) > req.scope.limit {
		data.HasMore = true
		data.Rows = data.Rows[:req.scope.limit]
	}
	// A hard serialized page budget complements the connection/time/page limits.
	// Stop before the first oversized row so its key remains on the next page.
	size := 0
	for i, row := range data.Rows {
		b, _ := json.Marshal(row)
		if size+len(b) > 512*1024 {
			data.Rows = data.Rows[:i]
			data.HasMore = true
			data.State = "partial"
			break
		}
		size += len(b)
	}
	if data.HasMore && len(data.Rows) > 0 {
		last := data.Rows[len(data.Rows)-1]
		afterTable := ""
		if req.scope.section == "indexes" {
			afterTable = last.Object.Name
		}
		data.NextCursor = req.nextCursor(catalogPosition{AfterSchema: last.Object.Schema, AfterName: last.Name, AfterOrdinal: last.Ordinal, AfterTable: afterTable, Contract: data.Contract})
	}
	if !v.finishCatalog(w, r, req, len(data.Rows), nil) {
		return
	}
	writeJSON(w, 200, data)
}
