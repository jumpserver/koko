package engine

import (
	"errors"
	"net/http"

	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

// A recovery record belongs to the execution that attempted the replacement.
// Return the captured definition only after checking the current console and
// account. Restoration starts with another explicit preview.
func (v *Server) objectRecovery(w http.ResponseWriter, r *http.Request) {
	s, c, ctx, done := v.editConsole(w, r)
	if s == nil {
		return
	}
	defer done()
	s.mu.Lock()
	e := s.executions[r.PathValue("eid")]
	s.mu.Unlock()
	if e == nil || e.objectPlan == nil || e.objectPlan.consoleID != c.id || e.objectPlan.commands.Recovery == nil {
		fail(w, 404, "not_found", "No recovery record for this console and execution")
		return
	}
	view := e.snapshot()
	if !terminal(view.Status) {
		fail(w, 409, "console_busy", "Wait for the replacement to finish")
		return
	}
	p := e.objectPlan
	record := p.commands.Recovery
	scope := &catalogRequest{session: s, conn: c.conn, ctx: ctx, database: p.commands.Ref.Database, scope: catalogScope{
		operation: "object-recovery", ref: p.commands.Ref, kind: string(p.commands.Ref.Kind), schema: p.commands.Ref.Schema,
	}}
	if err := v.catalogAudit(scope, "requested", 0); err != nil {
		fail(w, 503, "audit_persistence_failed", "Recovery access could not be audited")
		return
	}
	check := func() (string, error) {
		var database, user, role string
		roleSQL := "''"
		if record.HasRoles {
			roleSQL = "COALESCE(CURRENT_ROLE(),'')"
		}
		if err := c.conn.QueryRowContext(ctx, "SELECT COALESCE(DATABASE(),''), CURRENT_USER(), "+roleSQL).Scan(&database, &user, &role); err != nil {
			return "", err
		}
		if database != scope.database || user != record.User || role != record.Role {
			return "", sqlservice.ErrCatalogForbidden
		}
		authorize := func(command string) error { return v.provider.Authorize(ctx, s.grant, command) }
		if err := authorize(record.ShowSQL); err != nil {
			return "", sqlservice.ErrCatalogForbidden
		}
		current, err := sqlservice.MySQLObjectDefinition(ctx, c.conn, s.grant.Dialect(), p.commands.Ref, authorize)
		if errors.Is(err, sqlservice.ErrCatalogNotFound) && len(view.ObjectPlan.Steps) > 0 && view.ObjectPlan.Steps[0].Status != "not_sent" {
			return "absent_or_hidden", sqlservice.CheckMySQLRecoveryAccess(ctx, c.conn, p.commands.Ref, record)
		}
		if err != nil {
			return "", err
		}
		if current.Status != "complete" {
			return "", sqlservice.ErrCatalogForbidden
		}
		return "visible", nil
	}
	status, err := check()
	if v.finishCatalog(w, r, scope, 1, err) {
		writeJSON(w, 200, map[string]any{"ref": p.commands.Ref, "definition": record.Definition, "environment": record.Environment, "current_status": status})
	}
}
