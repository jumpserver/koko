package engine

import (
	"context"
	"time"

	"github.com/jumpserver/koko/pkg/chen/core"
)

// After a failed routine the only remaining SQL eligible for dispatch is its
// pre-approved, generated SET-to-NULL cleanup. Otherwise discard the physical
// connection: uncollected OUT bindings may contain restricted values.
func (v *Server) clearFailedObjectVariables(s *session, c *console, e *execution, approved map[string]string, maySend bool) bool {
	plan := e.objectPlan
	last := len(plan.commands.Steps) - 1
	if last < 0 || !plan.commands.Steps[last].Cleanup {
		return true
	}
	snapshot := e.snapshot().ObjectPlan
	if snapshot.Steps[0].Status == "not_sent" || snapshot.Steps[last].Status == "succeeded" {
		return true
	}
	if !maySend || c.closed.Load() || c.lost.Load() || snapshot.Steps[last].Status != "not_sent" {
		invalidateEditConnection(s, c)
		return true
	}
	ctx, stop := context.WithTimeout(c.ctx, databaseCleanupTimeout)
	defer stop()
	stmt := plan.commands.Steps[last].SQL
	current, err := v.objectPolicies(ctx, s, []string{stmt})
	decision, known := approved[stmt]
	if err != nil || !known || decision != current[stmt] {
		invalidateEditConnection(s, c)
		return true
	}
	v.update(s, e, func(x *ExecutionView) { x.ObjectPlan.Steps[last].Status = "sent" })
	if v.journal(s, e, "sending_cleanup") != nil {
		v.update(s, e, func(x *ExecutionView) { x.ObjectPlan.Steps[last].Status = "not_sent" })
		invalidateEditConnection(s, c)
		return false
	}
	before := time.Now()
	_, err = c.conn.ExecContext(ctx, stmt)
	msg, status := "Released routine output variables", "succeeded"
	if err != nil {
		msg, status = "Routine output variable cleanup could not be confirmed", "unknown"
		invalidateEditConnection(s, c)
	}
	v.update(s, e, func(x *ExecutionView) {
		x.ObjectPlan.Steps[last].Status = status
		if err == nil {
			x.ObjectPlan.Steps[last].Durability = "session"
		}
	})
	record := core.AuditRecord{ID: e.view.ID + ":cleanup", SQL: stmt, Output: msg, StartedAt: before, FinishedAt: time.Now(), RiskClassified: true}
	if err != nil {
		record.Error = msg
	}
	auditCtx, cancel := context.WithTimeout(context.Background(), databaseCleanupTimeout)
	defer cancel()
	return v.provider.Record(auditCtx, s.grant, record) == nil
}
