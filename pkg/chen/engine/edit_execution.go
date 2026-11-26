package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/dialect"
)

// Grid saves use a transaction or savepoint when supported by the table.
// Never replay a save after an uncertain commit or a nontransactional failure.
func (v *Server) runEdits(s *session, c *console, e *execution) {
	c.mu.Lock()
	var touched bool
	defer func() {
		e.finishDatabaseWork()
		e.stopDeadline()
		e.cancel()
		if e.cancelRequested.Load() {
			<-e.cancelDone
		}
		if touched && c.manualTx != nil {
			c.transactionActivity = time.Now()
		}
		c.mu.Unlock()
	}()
	start := time.Now()
	audit := core.AuditRecord{ID: e.view.ID, SQL: e.sql, StartedAt: start, RiskClassified: true}
	triggeredWarnings := map[string]core.AuditRecord{}
	defer func() {
		audit.FinishedAt = time.Now()
		view := e.snapshot()
		outcome := "pending"
		if view.Edit != nil {
			outcome = view.Edit.Outcome
		}
		audit.Output = fmt.Sprintf("execution_id=%s edit_plan=%s state=%s outcome=%s", view.ID, e.edit.id, view.Status, outcome)
		if view.CancelReason != "" {
			audit.Output += " cancel_reason=" + view.CancelReason
		}
		if view.Transaction != nil {
			audit.Output += fmt.Sprintf(" transaction_mode=%t transaction_state=%s transaction_outcome=%s", view.Transaction.Manual, view.Transaction.State, view.Transaction.Outcome)
		}
		if view.Error != nil {
			audit.Error = audit.Output + "; " + view.Error.Message
		}
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		status := "recorded"
		if v.provider.Record(ctx, s.grant, audit) != nil {
			status = "failed"
		}
		for _, warning := range triggeredWarnings {
			if warning.ACLID == audit.ACLID && warning.GroupID == audit.GroupID && warning.RiskLevel == audit.RiskLevel {
				continue
			}
			warning.StartedAt, warning.FinishedAt = audit.StartedAt, audit.FinishedAt
			warning.Output, warning.Error = audit.Output+"; policy_warning", audit.Error
			if v.provider.Record(ctx, s.grant, warning) != nil {
				status = "failed"
			}
		}
		end := time.Now()
		v.update(s, e, func(x *ExecutionView) { x.AuditStatus = status; x.FinishedAt = &end })
		if v.journal(s, e, "finished") != nil {
			v.update(s, e, func(x *ExecutionView) { x.AuditStatus = "failed" })
		}
	}()
	failEdit := func(err error) {
		e.finishDatabaseWork()
		if c.manual && touched && c.manualTx != nil {
			if cleanupErr := v.finishManualTransaction(s, c, e, false); cleanupErr != nil {
				err = cleanupErr
			}
		}
		status := "failed"
		if e.ctx.Err() != nil || e.cancelRequested.Load() {
			status = "canceled"
		}
		api := editSafeFailure(err)
		if api.Code == "transaction_outcome_unknown" || api.Code == "edit_nontransactional_failed" {
			status = "unknown"
			v.update(s, e, func(x *ExecutionView) { x.Edit.Outcome = "unknown"; x.Edit.RefreshRequired = true })
		}
		if status == "canceled" && e.deadlineExpired.Load() {
			api = safeDatabaseError(context.DeadlineExceeded)
		}
		v.update(s, e, func(x *ExecutionView) { x.Status = status; x.Error = api })
	}
	if err := e.ctx.Err(); err != nil {
		failEdit(editError("edit_canceled", "Save was canceled before execution"))
		return
	}
	if c.lost.Load() || c.closed.Load() {
		failEdit(editError("connection_lost", "Reconnect before saving"))
		return
	}
	if err := s.grant.Check(); err != nil {
		failEdit(editError("session_unavailable", err.Error()))
		return
	}
	s.grant.SetBusy(true)
	defer s.grant.SetBusy(false)
	if v.journal(s, e, "accepted") != nil {
		failEdit(editError("audit_persistence_failed", "Save intent could not be recorded; no changes were sent"))
		return
	}
	policy := []string{}
	for _, command := range e.edit.commands {
		policy = append(policy, command.policySQL)
	}
	if err := v.authorizeExecution(s, e, policy, &audit); err != nil {
		var pe *core.PolicyError
		if errors.As(err, &pe) {
			audit.RiskLevel, audit.ACLID, audit.GroupID = pe.RiskLevel, pe.ACLID, pe.GroupID
			if pe.Code == "command_warning_confirmation_required" {
				audit.RiskLevel = 0
			}
			failEdit(editError(pe.Code, pe.Error()))
		} else {
			failEdit(editError("command_denied", "Saving is not permitted"))
		}
		return
	}
	if e.ctx.Err() != nil || e.cancelRequested.Load() {
		failEdit(editError("edit_canceled", "Save was canceled before execution"))
		return
	}
	p := e.edit
	s.mu.Lock()
	result := s.results[p.resultID]
	unavailable := p.used || time.Now().After(p.expires) || result == nil || result.editStale || s.closed
	if !unavailable {
		p.used = true
	}
	s.mu.Unlock()
	if unavailable {
		failEdit(editError("edit_plan_expired", "Refresh this result and create a new preview"))
		return
	}
	if p.source == "csv" && !s.grant.Permissions.Upload {
		failEdit(editError("upload_permission_denied", "CSV import is not permitted"))
		return
	}
	// Revalidate table metadata immediately before touching data. Existing
	// previews cannot outlive changed PK/type/generated-column definitions.
	if err := e.beginDatabaseWork(s, c); err != nil {
		failEdit(err)
		return
	}
	ctx, stop := context.WithTimeout(e.ctx, 5*time.Second)
	resolved, resolveErr := v.resolveEdit(ctx, s, c, result)
	stop()
	if resolveErr != nil || resolved == nil || editSchemaFingerprint(resolved) != p.schemaFingerprint {
		failEdit(editError("edit_schema_changed", "Table metadata changed; refresh and preview the changes again"))
		return
	}
	started := time.Now()
	v.update(s, e, func(x *ExecutionView) { x.Status = "running"; x.StartedAt = &started })
	if err := v.beginManualTransaction(s, c, e); err != nil {
		failEdit(err)
		return
	}
	if c.manual {
		touched = true
	}
	boundary, err := beginEditBoundary(e.ctx, c, s.grant.Dialect())
	if err != nil {
		failEdit(err)
		return
	}
	touched = true
	attempted := false
	applied := 0
	markNontransactionalFailure := func() {
		s.mu.Lock()
		result.editStale = true
		s.mu.Unlock()
		v.update(s, e, func(x *ExecutionView) {
			x.Status = "unknown"
			x.Error = editError("edit_nontransactional_failed", "The save failed on a nontransactional table. Some changes may already be saved; refresh and verify the data before saving again.")
			x.Edit.Outcome = "unknown"
			x.Edit.AffectedRows = applied
			x.Edit.DatabaseCommitted = applied > 0
			x.Edit.RefreshRequired = true
		})
	}
	rollback := func(cause error) {
		e.finishDatabaseWork()
		// A successful ROLLBACK does not undo changes to a nontransactional
		// table. Do not report those changes as rolled back.
		nontransactional := attempted && !resolved.table.Transactional
		if nontransactional {
			cause = editError("edit_nontransactional_failed", "The save failed on a nontransactional table. Some changes may already be saved; refresh and verify the data before saving again.")
			defer markNontransactionalFailure()
		}
		if c.manual {
			// A manual-mode error rolls back the entire user transaction, not
			// just the grid savepoint. A doomed SQL Server transaction can no
			// longer roll back to a savepoint but can still roll back in full.
			boundary.restoreOracleTransaction()
			if !nontransactional {
				v.update(s, e, func(x *ExecutionView) { x.Edit.Outcome = "rolled_back" })
			}
			failEdit(cause)
			return
		}
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		rollbackErr := boundary.rollback(cleanup)
		done()
		if rollbackErr != nil {
			invalidateEditConnection(s, c)
			v.update(s, e, func(x *ExecutionView) {
				x.Status = "unknown"
				x.Error = editError("edit_rollback_unknown", "Rollback could not be confirmed. Reconnect and verify the data before repeating this save")
				x.Edit.Outcome = "unknown"
				x.Edit.RefreshRequired = true
			})
			return
		}
		if !nontransactional {
			v.update(s, e, func(x *ExecutionView) { x.Edit.Outcome = "rolled_back" })
		}
		failEdit(cause)
	}
	for _, command := range p.commands {
		if e.ctx.Err() != nil || e.cancelRequested.Load() {
			rollback(editError("edit_canceled", "Save was canceled and its changes were rolled back"))
			return
		}
		if err := s.grant.Check(); err != nil {
			rollback(editError("session_unavailable", "Session changed; this save was rolled back"))
			return
		}
		if warning, ok := e.policyWarnings[command.policySQL]; ok {
			warningKey := warning.ACLID + ":" + warning.GroupID
			triggeredWarnings[warningKey] = core.AuditRecord{ID: e.view.ID + ":warning:" + warningKey, SQL: e.sql, RiskLevel: warning.RiskLevel, ACLID: warning.ACLID, GroupID: warning.GroupID}
			if warning.RiskLevel > audit.RiskLevel {
				audit.RiskLevel, audit.ACLID, audit.GroupID = warning.RiskLevel, warning.ACLID, warning.GroupID
			}
		}
		attempted = true
		result, execErr := boundary.exec(e.ctx, command.sql, command.args...)
		if execErr != nil {
			if s.grant.Dialect() == dialect.DB2 {
				if api := db2EditDiagnostic(execErr, command.argColumns, &resolved.table); api != nil {
					execErr = api
				}
			}
			rollback(execErr)
			return
		}
		count, countErr := result.RowsAffected()
		if countErr != nil || count != 1 {
			rollback(editError("edit_conflict", "The primary-key row was not found, or the operation did not affect exactly one row. This save was rolled back; refresh the data and try again."))
			return
		}
		applied++
	}
	e.finishDatabaseWork()
	if e.ctx.Err() != nil || e.cancelRequested.Load() {
		rollback(editError("edit_canceled", "Save was canceled and its changes were rolled back"))
		return
	}
	if err := s.grant.Check(); err != nil {
		rollback(editError("session_unavailable", "Session changed; this save was rolled back"))
		return
	}
	// Once the save crosses its commit boundary, a late user cancel must
	// not interrupt COMMIT or RELEASE SAVEPOINT on the pinned connection.
	if err := e.beginTransactionBoundary(); err != nil {
		rollback(err)
		return
	}
	cleanup, done := context.WithTimeout(c.ctx, 5*time.Second)
	err = boundary.finish(cleanup)
	done()
	if err != nil {
		if boundary.tx == nil {
			rollback(err)
			return
		}
		invalidateEditConnection(s, c)
		s.mu.Lock()
		result.editStale = true
		s.mu.Unlock()
		v.update(s, e, func(x *ExecutionView) {
			x.Status = "unknown"
			x.Error = editError("edit_commit_unknown", "Commit was sent but not confirmed. Verify the data before repeating this save")
			x.Edit.Outcome = "unknown"
			x.Edit.RefreshRequired = true
		})
		return
	}
	s.mu.Lock()
	result.editStale = true
	s.mu.Unlock()
	if c.manual {
		v.publishTransaction(s, c, e, "")
	}
	v.update(s, e, func(x *ExecutionView) {
		x.Status = "succeeded"
		x.Edit.ChangesApplied = true
		x.Edit.DatabaseCommitted = boundary.tx != nil || !resolved.table.Transactional
		x.Edit.AffectedRows = p.count
		x.Edit.RefreshRequired = true
		x.Edit.Outcome = "staged"
		if x.Edit.DatabaseCommitted {
			x.Edit.Outcome = "committed"
		}
		x.StatementResults = append(x.StatementResults, StatementResult{RowsAffected: fmt.Sprint(p.count)})
	})
}
