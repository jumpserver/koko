package engine

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

func (v *Server) executionStatus(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, true)
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()
		fail(w, 410, "session_closed", "Session is closed")
		return
	}
	e := s.executions[r.PathValue("eid")]
	s.mu.Unlock()
	if e == nil {
		fail(w, 404, "not_found", "Execution not found")
		return
	}
	writeJSON(w, 200, e.snapshot())
}
func (v *Server) cancelExecution(w http.ResponseWriter, r *http.Request) {
	v.cancelExecutionWithResponse(w, r, func(view ExecutionView) any { return view })
}

// Export cancellation shares the execution policy and state transition, while
// returning the export resource expected by the endpoint's client.
func (v *Server) cancelExecutionWithResponse(w http.ResponseWriter, r *http.Request, response func(ExecutionView) any) {
	s := v.auth(w, r, true)
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()
		fail(w, 410, "session_closed", "Session is closed")
		return
	}
	e := s.executions[r.PathValue("eid")]
	s.mu.Unlock()
	if e == nil {
		fail(w, 404, "not_found", "Execution not found")
		return
	}
	status := http.StatusAccepted
	switch e.requestCancellation(s, "user") {
	case cancelFinished:
		status = http.StatusOK
	case cancelUnsupported:
		fail(w, http.StatusConflict, "cancel_unsupported", "The installed database driver cannot cancel an active query")
		return
	case cancelTransactionBoundary:
		fail(w, http.StatusConflict, "transaction_cancel_unsupported", "Transaction boundaries cannot be canceled")
		return
	}
	writeJSON(w, status, response(e.snapshot()))
}
func (v *Server) worker(s *session, c *console) {
	defer s.workers.Done()
	defer s.releaseClosedConsoleResults(c)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var idle <-chan time.Time
	for {
		select {
		case e := <-c.queue:
			timer.Stop()
			// A queued commit and the timeout can become ready together. Check
			// the actual deadline before accepting work on an expired transaction.
			if idle != nil {
				v.expireManualTransaction(s, c)
			}
			idle = nil
			v.run(s, c, e)
			c.mu.Lock()
			if c.manualTx != nil && !c.lost.Load() && !c.closed.Load() && v.opts.TransactionIdleTimeout > 0 {
				timer.Reset(time.Until(c.transactionActivity.Add(v.opts.TransactionIdleTimeout)))
				idle = timer.C
			}
			c.mu.Unlock()
		case <-idle:
			idle = nil
			v.expireManualTransaction(s, c)
		case <-c.ctx.Done():
			for {
				select {
				case e := <-c.queue:
					v.run(s, c, e)
				default:
					return
				}
			}
		}
	}
}
func (v *Server) update(s *session, e *execution, fn func(*ExecutionView)) {
	e.mu.Lock()
	fn(&e.view)
	e.view.Revision++
	e.mu.Unlock()
	s.emit("execution.updated", e.snapshot())
	if e.export != nil {
		e.export.emit()
	}
}

func interruptExecutionBackend(s *session, c *console, e *execution) {
	ctx, cancel := context.WithTimeout(context.Background(), databaseCancelTimeout)
	defer cancel()
	if err := cancelBackend(ctx, s, c, e); err != nil {
		slog.Warn("database_interrupt_failed", "dialect", s.grant.Dialect(), "execution", e.view.ID, "error", err)
	}
}

func (v *Server) run(s *session, c *console, e *execution) {
	if e.edit != nil {
		v.runEdits(s, c, e)
		return
	}
	if e.export != nil && v.acquireExportSlot(e.ctx) {
		defer func() { <-v.exportSlots }()
	}
	c.mu.Lock()
	d := s.grant.Dialect()
	initialMySQL := c.initialMySQL
	c.initialMySQL = nil
	userSQLStarted := false
	preflightStartedTransaction := false
	var db2CancelCheckpoint uint64
	if d == dialect.DB2 {
		db2CancelCheckpoint = db2CancellationCheckpoint(c)
	}
	var databaseTouched bool
	defer func() {
		e.finishDatabaseWork()
		e.stopDeadline()
		e.cancel()
		if e.cancelRequested.Load() {
			<-e.cancelDone
		}
		if p := e.objectPlan; p != nil {
			s.mu.Lock()
			// The immutable plan identity and execution outcome suffice for retries.
			// The authorized recovery record is retained separately from dispatched SQL.
			p.req = sqlservice.ObjectPlanRequest{}
			p.sql, e.sql = "", ""
			p.commands.Steps = nil
			e.objectDecisions = nil
			e.policyWarnings = nil
			if r := p.commands.Recovery; r != nil {
				p.recoveryUntil = time.Now().Add(15 * time.Minute)
				p.bytes = len(r.Definition) + len(r.ShowSQL) + len(r.User) + len(r.Role) + len(r.Definer)
				for k, v := range r.Environment {
					p.bytes += len(k) + len(v)
				}
				s.reserveObjectPreviewLocked(0)
			} else {
				delete(s.objectPlans, p.id)
			}
			s.mu.Unlock()
		}
		if userSQLStarted && c.manualTx != nil {
			c.transactionActivity = time.Now()
		}
		c.mu.Unlock()
	}()
	if e.planMode != "" {
		e.mu.Lock()
		identity := *e.view.Plan
		scope := c.currentContext()
		identity.Database, identity.Schema = scope.Database, scope.Schema
		e.view.Plan = &identity
		e.mu.Unlock()
	}
	start := time.Now()
	audit := core.AuditRecord{ID: e.view.ID, SQL: e.sql, StartedAt: start, RiskClassified: true}
	var runErr error
	currentInFlight := false
	completedStatements := 0
	objectTransaction := false
	mysqlObject := e.objectPlan != nil && d.UsesMySQLSyntax() && e.objectPlan.req.Action != "readDefinition"
	mysqlInvoke := mysqlObject && e.objectPlan.req.Action == "invoke"
	var objectPolicies map[string]string
	confirmedCancel := false
	triggeredWarnings := map[string]core.AuditRecord{}
	auditStatus := "recorded"
	auditPending := true
	finishAudit := func(succeeded bool) error {
		if !auditPending {
			return nil
		}
		auditPending = false
		defer func() { e.replayOutput, e.auditRows = "", 0 }()
		audit.FinishedAt = time.Now()
		view := e.snapshot()
		state := view.Status
		if succeeded {
			state = "succeeded"
		}
		if audit.Output == "" && e.replayOutput != "" {
			audit.Output = fmt.Sprintf("Query OK, %d rows discovered", e.auditRows)
		}
		audit.ReplayOutput = e.replayOutput
		audit.Output += fmt.Sprintf(" execution_id=%s state=%s completed_statements=%d", view.ID, state, completedStatements)
		if view.CancelReason != "" {
			audit.Output += " cancel_reason=" + view.CancelReason
		}
		if view.Transaction != nil {
			audit.Output += fmt.Sprintf(" transaction_mode=%t transaction_state=%s transaction_outcome=%s prior_committed=%t", view.Transaction.Manual, view.Transaction.State, view.Transaction.Outcome, view.Transaction.PriorCommitted)
		}
		if plan := e.objectPlan; plan != nil {
			audit.Output += fmt.Sprintf(" object_plan=%s fingerprint=%s action=%s object=%q step=%d", plan.id, plan.commands.Fingerprint, plan.req.Action, plan.commands.Ref.Name, e.objectStep+1)
		}
		if e.queryPage != nil {
			audit.Output += queryPageAudit(e.queryPage)
		}
		if view.Plan != nil {
			audit.Output += fmt.Sprintf(" plan_mode=%s generated_sql=%q auxiliary_sql=%q", view.Plan.Mode, e.planSQL, planAuthorizationSQL(d, e.planSQL))
		}
		if e.export != nil {
			out := e.export.snapshot()
			audit.Output += fmt.Sprintf(" export_id=%s format=%s rows=%d bytes=%d", out.ID, out.Format, out.Rows, out.Bytes)
		}
		if runErr != nil {
			if view.Error != nil {
				audit.Error = audit.Output + "; " + view.Error.Message
			} else {
				audit.Error = audit.Output + "; " + runErr.Error()
			}
		}
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		recordErr := v.provider.Record(ctx, s.grant, audit)
		for _, warning := range triggeredWarnings {
			if audit.RiskLevel == warning.RiskLevel && audit.ACLID == warning.ACLID && audit.GroupID == warning.GroupID {
				continue
			}
			warning.StartedAt, warning.FinishedAt = audit.StartedAt, audit.FinishedAt
			warning.Output = audit.Output + "; policy_warning"
			warning.Error = audit.Error
			if err := v.provider.Record(ctx, s.grant, warning); err != nil {
				recordErr = errors.Join(recordErr, err)
			}
		}
		if recordErr != nil {
			auditStatus = "failed"
		}
		return recordErr
	}
	defer func() {
		_ = finishAudit(false)
		end := time.Now()
		v.update(s, e, func(x *ExecutionView) { x.AuditStatus = auditStatus; x.FinishedAt = &end })
		if err := v.journal(s, e, "finished"); err != nil {
			v.update(s, e, func(x *ExecutionView) { x.AuditStatus = "failed" })
		}
		if e.export != nil {
			e.export.finishExecution()
		}
		if databaseTouched && runErr != nil && !c.closed.Load() && !c.lost.Load() {
			ping, done := context.WithTimeout(context.Background(), time.Second)
			defer done()
			if c.conn.PingContext(ping) != nil {
				invalidateEditConnection(s, c)
			}
		}
	}()
	unknownTransaction := func() *APIError {
		invalidateEditConnection(s, c)
		v.publishTransaction(s, c, e, "unknown")
		return &APIError{"transaction_outcome_unknown", "SQL left an unverified transaction state; reconnect and verify the data"}
	}
	failure := func(err error, api *APIError) {
		e.finishDatabaseWork()
		runErr = err
		if d == dialect.DB2 && db2CancellationAcknowledged(c, db2CancelCheckpoint) {
			confirmedCancel = true
		}
		var maskedDiagnostic bool
		if isServerError(err) {
			api, maskedDiagnostic = v.maskDatabaseDiagnostic(s, api)
		}
		status := "failed"
		if api.Code == "transaction_outcome_unknown" {
			status = "unknown"
		} else if confirmedCancel {
			status = "canceled"
		} else if currentInFlight {
			if !isServerError(err) {
				status = "unknown"
				api = &APIError{"execution_unknown", "The database outcome is unknown; do not automatically repeat this SQL"}
			}
		} else if e.ctx.Err() != nil || e.cancelRequested.Load() {
			status = "canceled"
		}
		if status == "canceled" && e.deadlineExpired.Load() {
			api = safeDatabaseError(context.DeadlineExceeded)
		}
		var nativeStepStatus string
		if mysqlInvoke && currentInFlight && !c.lost.Load() && !c.closed.Load() {
			// An ERR packet (including SIGNAL) has no transaction flags. A
			// protocol ping refreshes them without changing the transaction.
			probe, done := context.WithTimeout(context.Background(), databaseCleanupTimeout)
			probeErr := c.conn.PingContext(probe)
			state := editTxUnknown
			if probeErr == nil {
				state, probeErr = editTransactionState(probe, c, d)
			}
			done()
			if probeErr != nil || state != editTxIdle {
				nativeStepStatus = status
				api, status = unknownTransaction(), "unknown"
			}
		}
		rolledBack := false
		if c.manual && e.transactionAction == "" && (userSQLStarted || preflightStartedTransaction) && c.manualTx != nil {
			var rollbackErr error
			if preflightStartedTransaction && !userSQLStarted {
				rollbackErr = v.rollbackSourcePreflight(s, c, e)
			} else {
				rollbackErr = v.finishManualTransaction(s, c, e, false)
			}
			if err := rollbackErr; err != nil {
				status = "unknown"
				api = &APIError{"transaction_outcome_unknown", err.Error()}
			} else {
				rolledBack = true
				if status == "unknown" {
					status = "failed"
				}
			}
		}
		if objectTransaction {
			rollback := e.objectBoundaryAudit("ROLLBACK")
			cleanup, stop := context.WithTimeout(context.Background(), databaseCleanupTimeout)
			rbErr := v.recheckObjectPolicies(cleanup, s, objectPolicies, rollback.SQL)
			if rbErr == nil {
				_, rbErr = c.conn.ExecContext(cleanup, rollback.SQL)
			}
			stop()
			objectTransaction = false
			rollback.FinishedAt = time.Now()
			rollback.Output = fmt.Sprintf("execution_id=%s object_plan=%s rollback_confirmed=%t", e.view.ID, e.objectPlan.id, rbErr == nil)
			if rbErr != nil {
				status = "unknown"
				api = &APIError{"transaction_outcome_unknown", "Object transaction rollback could not be confirmed"}
				rollback.Error = api.Message
				invalidateEditConnection(s, c)
			} else {
				rolledBack = true
			}
			auditCtx, cancel := context.WithTimeout(context.Background(), databaseCleanupTimeout)
			if v.provider.Record(auditCtx, s.grant, rollback) != nil {
				auditStatus = "failed"
			}
			cancel()
		}
		if restoreErr := e.restoreSourceReadContext(); restoreErr != nil {
			runErr = errors.Join(runErr, restoreErr)
			status = "unknown"
			api = sourceContextError(restoreErr)
		}
		if completedStatements > 0 && !rolledBack {
			message := fmt.Sprintf("%d earlier statement(s) completed and may already have taken effect. Do not automatically repeat the whole script. ", completedStatements)
			if status == "unknown" {
				api = &APIError{"execution_unknown", message + api.Message}
			} else {
				code := "script_partial_failure"
				if status == "canceled" {
					code = "script_partial_cancel"
				}
				api = &APIError{code, message + api.Message}
			}
		}
		v.update(s, e, func(x *ExecutionView) {
			if o := x.ObjectPlan; o != nil {
				if userSQLStarted && (e.objectPlan.req.Action == "invoke" || e.objectPlan.commands.Steps[e.objectStep].ReplacesExisting) {
					o.SideEffectsPossible = true
				}
				if rolledBack || status == "unknown" {
					for i := range o.Steps {
						if o.Steps[i].Durability == "pending" {
							if rolledBack {
								o.Steps[i].Durability = "rolled_back"
							} else {
								o.Steps[i].Durability = "unknown"
							}
						}
					}
				}
				if step := &o.Steps[e.objectStep]; step.Status == "sent" {
					step.Status = status
					if nativeStepStatus != "" {
						step.Status, step.Durability = nativeStepStatus, "unknown"
					}
				}
				switch {
				case rolledBack:
					o.Outcome = "rolled_back"
				case status == "unknown":
					o.Outcome = "unknown"
				case completedStatements == len(o.Steps):
					o.Outcome = "committed"
				case completedStatements > 0:
					o.Outcome = "partial"
				default:
					o.Outcome = "failed"
				}
			}
			x.Status = status
			x.Error = api
			x.maskedDiagnostic = maskedDiagnostic && status == "failed"
			if current := c.transaction.Load(); current != nil {
				if x.Transaction == nil {
					x.Transaction = &TransactionOutcome{Action: e.transactionAction}
				}
				x.Transaction.TransactionView = *current
			}
		})
		if mysqlObject && userSQLStarted {
			if !v.clearFailedObjectVariables(s, c, e, objectPolicies, auditStatus != "failed" && api.Code != "audit_persistence_failed" && status != "unknown") {
				auditStatus = "failed"
			}
		}
		if (d == dialect.Dameng || d == dialect.DB2) && databaseTouched && (e.ctx.Err() != nil || e.cancelRequested.Load() || confirmedCancel) {
			invalidateEditConnection(s, c)
		}
	}
	denyCommand := func(err error) {
		code := "command_denied"
		var policy *core.PolicyError
		if errors.As(err, &policy) {
			audit.RiskLevel, audit.ACLID, audit.GroupID = policy.RiskLevel, policy.ACLID, policy.GroupID
			code = policy.Code
		}
		failure(err, &APIError{code, err.Error()})
	}
	if err := e.ctx.Err(); err != nil {
		failure(err, safeDatabaseError(err))
		return
	}
	if c.lost.Load() || c.closed.Load() {
		failure(errors.New("connection_lost"), &APIError{"connection_lost", "Open a new console; connection state was lost"})
		return
	}
	if err := s.grant.Check(); err != nil {
		failure(err, &APIError{"session_unavailable", err.Error()})
		return
	}
	s.grant.SetBusy(true)
	defer s.grant.SetBusy(false)
	if err := v.journal(s, e, "accepted"); err != nil {
		failure(err, &APIError{"audit_persistence_failed", "Execution intent could not be recorded; SQL was not sent"})
		return
	}
	if e.transactionAction != "" {
		if current := c.transaction.Load(); current == nil || current.Revision != e.transactionRevision {
			failure(errors.New("transaction_changed"), &APIError{"transaction_changed", "Transaction state changed; review it before retrying"})
			return
		}
		v.update(s, e, func(x *ExecutionView) { x.Status = "running"; x.StartedAt = &start })
		if err := v.transactionAction(s, c, e, &audit); err != nil {
			api := safeDatabaseError(err)
			var typed *APIError
			if errors.As(err, &typed) {
				api = typed
			}
			failure(err, api)
			return
		}
		v.update(s, e, func(x *ExecutionView) { x.Status = "succeeded"; x.StartedAt = &start })
		return
	}
	if err := e.beginDatabaseWork(s, c); err != nil {
		failure(err, safeDatabaseError(err))
		return
	}
	if d.UsesMySQLWire() {
		databaseTouched = true
	}
	var mysqlState *mysqlSessionState
	var mysqlHint *resultStarSource
	if d == dialect.MySQL && initialMySQL == nil && e.browse == nil && e.queryPage == nil && e.planMode == "" && e.export == nil {
		if target, ok := sqlservice.MySQLStarHint(e.sql); ok {
			state, hint, err := readMySQLStarSession(e.ctx, c.conn, target)
			if err == nil {
				initialMySQL, mysqlHint = &state, hint
			}
		}
	}
	readOptions := func() (sqlservice.AnalyzeOptions, error) {
		if d != dialect.MySQL {
			return liveOptions(e.ctx, c, d)
		}
		if initialMySQL != nil {
			mysqlState, initialMySQL = initialMySQL, nil
			return mysqlState.options, nil
		}
		state, err := readMySQLSession(e.ctx, c.conn)
		if err != nil {
			if e.cancelRequested.Load() {
				return sqlservice.AnalyzeOptions{}, context.Canceled
			}
			// Some compatible servers support SQL mode but not every paging
			// variable. Ordinary SQL can still run without replay metadata.
			mysqlState = nil
			return liveOptions(e.ctx, c, d)
		}
		mysqlState = &state
		return state.options, nil
	}
	opts, err := readOptions()
	if err != nil {
		failure(err, safeDatabaseError(err))
		return
	}
	sourceContext := ""
	if e.queryPage != nil {
		sourceContext = e.queryPage.context
	} else if e.browse != nil && e.browse.plan != nil {
		sourceContext = e.browse.plan.context
	}
	if sourceContext != "" && (mysqlState == nil || mysqlState.fingerprint != sourceContext) {
		guard, err := prepareSourceReadContext(e.ctx, c, d, sourceContext)
		if err != nil {
			failure(err, sourceContextError(err))
			return
		}
		if guard != nil {
			// PostgreSQL LOCAL settings must live inside the eventual manual
			// transaction. Otherwise a later rollback could undo restoration
			// and leave the connection in the result's old search_path.
			if d == dialect.PostgreSQL && c.manual {
				wasIdle := c.manualTx == nil
				if err := v.beginManualTransaction(s, c, e); err != nil {
					failure(err, safeDatabaseError(err))
					return
				}
				preflightStartedTransaction = wasIdle
			}
			e.restoreReadContext = func() error {
				// A late KILL must finish before USE/SET restores the namespace.
				e.finishDatabaseWork()
				return guard.restore(s)
			}
			databaseTouched = true
			if err := guard.enter(e.ctx); err != nil {
				failure(err, sourceContextError(err))
				return
			}
		}
	}
	runSQL := e.sql
	planVersion := ""
	var browseArgs []any
	if e.browse != nil && e.browse.plan != nil {
		p := e.browse.plan
		contextMatches := sourceContext != ""
		if !contextMatches {
			scope, scopeErr := liveContext(e.ctx, c, d)
			contextMatches = scopeErr == nil && scope.Database == p.view.Database
		}
		if !contextMatches || (p.view.Supported && opts != p.options) {
			failure(errors.New("browse_context_changed"), &APIError{"browse_context_changed", "Database or SQL mode changed; apply the browse conditions again"})
			return
		}
		if p.view.Supported && p.view.ServerVersion != "" {
			version, reason, err := browseVendorContext(e.ctx, c, d, opts)
			if err != nil || reason != "" || version != p.serverVersion {
				failure(errors.New("browse_context_changed"), &APIError{"browse_context_changed", "Server capabilities changed; reopen this table"})
				return
			}
		}
		isView, kindErr := sqlservice.BrowseRelationIsView(e.ctx, c.conn, d, sqlservice.CatalogColumnRequest{Schema: p.view.Schema, Table: p.view.Table})
		if kindErr != nil || isView != p.table.View || (isView && !p.reexecutionConfirmed) {
			failure(errors.New("browse_source_changed"), &APIError{"browse_source_changed", "The table or view changed; reopen it before browsing"})
			return
		}
		if err := validateBrowseWhere(e.ctx, c, d, p); err != nil {
			failure(err, &APIError{"invalid_browse", err.Error()})
			return
		}
		if p.action == "append" {
			if _, err := v.browseAppendSource(s, c.id, p); err != nil {
				failure(err, &APIError{"invalid_browse", err.Error()})
				return
			}
		}
		runSQL, browseArgs = p.sql, p.args
	}
	if e.planMode != "" {
		runSQL, planVersion, err = estimatedPlanSQL(e.ctx, c, d, e.sql, opts)
		if err != nil {
			api := &APIError{err.Error(), "Estimated plans require a supported database and one read SELECT without INTO, locking clauses or nested EXPLAIN"}
			var explicit *APIError
			if errors.As(err, &explicit) {
				api = explicit
			} else if !strings.HasPrefix(err.Error(), "explain_") {
				api = safeDatabaseError(err)
			}
			failure(err, api)
			return
		}
		e.mu.Lock()
		e.planSQL = runSQL
		e.mu.Unlock()
	}
	if e.queryPage != nil {
		if err := validateQueryPage(e.ctx, c, d, e.queryPage, opts, sourceContext != ""); err != nil {
			failure(err, &APIError{"query_page_context_changed", err.Error()})
			return
		}
		runSQL = e.queryPage.generated
	}
	analyze := sqlservice.AnalyzeWithOptions
	if e.consoleMode {
		analyze = sqlservice.AnalyzeConsole
	}
	var analysis sqlservice.Analysis
	if e.objectPlan != nil {
		analysis, err = sqlservice.AnalyzeObjectCommands(d, e.objectPlan.commands, opts)
	} else {
		analysis, err = analyze(d, runSQL, opts)
	}
	// Generated windows may exceed the pinned TiDB grammar. Their source was
	// admitted by the AST; only server-owned wrapping and bounded integers differ.
	if e.queryPage != nil {
		analysis = sqlservice.Analysis{Statements: []sqlservice.Statement{{SQL: runSQL, Kind: "select", ReturnsRows: true}}}
		err = nil
	}
	if e.planMode != "" && specialPlanDialect(d) {
		analysis = sqlservice.Analysis{Statements: []sqlservice.Statement{{SQL: runSQL, Kind: "explain", ReturnsRows: true}}}
		err = nil
	}
	if err != nil {
		failure(err, &APIError{"sql_parse_error", err.Error()})
		return
	}
	if len(analysis.Statements) == 0 {
		failure(errors.New("statement_limit"), &APIError{"statement_limit", "A script must contain at least one statement"})
		return
	}
	if e.export != nil {
		if !s.grant.Permissions.Download {
			failure(errors.New("download_permission_denied"), &APIError{"download_permission_denied", "Result download is not permitted"})
			return
		}
		confirmedQuery := e.queryPage != nil && e.queryPage.view.RequiresReexecution && e.queryPage.reexecutionConfirmed
		if len(analysis.Statements) != 1 || analysis.Statements[0].MayWrite && !confirmedQuery || !analysis.Statements[0].ReturnsRows || (analysis.Statements[0].Kind != "select" && analysis.Statements[0].Kind != "setopr") {
			failure(errors.New("export_requires_single_select"), &APIError{"export_requires_single_select", "Full export requires exactly one read query"})
			return
		}
	}
	implicitCommit := false
	if c.manual && e.planMode == "" {
		for _, statement := range analysis.Statements {
			if statement.Kind == "raw" {
				failure(errors.New("raw_console_manual_mode"), &APIError{"raw_console_manual_mode", "This raw command has unknown transaction behavior; switch to automatic transaction mode before executing it"})
				return
			}
			risk, control := objectTransactionRisk(e, d, statement)
			if control {
				failure(errors.New("transaction_control_sql"), &APIError{"transaction_control_sql", "Use the toolbar to change transactions; transaction and session-control SQL is unavailable in manual mode"})
				return
			}
			implicitCommit = implicitCommit || risk
		}
		if implicitCommit && !e.confirmImplicitCommit {
			failure(errors.New("implicit_commit_confirmation_required"), &APIError{"implicit_commit_confirmation_required", "This SQL may commit the current transaction and cannot be rolled back; confirmation is required"})
			return
		}
		if implicitCommit && (c.transaction.Load() == nil || c.transaction.Load().Revision != e.transactionRevision) {
			failure(errors.New("transaction_changed"), &APIError{"transaction_changed", "Transaction state changed; review the SQL and confirm again"})
			return
		}
	}
	if err := e.ctx.Err(); err != nil {
		failure(err, safeDatabaseError(err))
		return
	}
	if e.cancelRequested.Load() {
		failure(context.Canceled, &APIError{"query_canceled", "Query cancellation requested"})
		return
	}
	executionStart := time.Now()
	var startErr error
	v.update(s, e, func(x *ExecutionView) {
		startErr = e.ctx.Err()
		if startErr == nil && e.cancelRequested.Load() {
			startErr = context.Canceled
		}
		if startErr == nil {
			x.Status, x.StartedAt = "running", &executionStart
		}
	})
	if startErr != nil {
		failure(startErr, safeDatabaseError(startErr))
		return
	}
	if p := e.objectPlan; p != nil {
		if len(p.commands.Steps) != len(analysis.Statements) {
			failure(sqlservice.ErrObjectChanged, objectPlanAPIError(sqlservice.ErrObjectChanged))
			return
		}
		var all []string
		for i, step := range p.commands.Steps {
			if step.PolicySQL != "" {
				all = append(all, step.PolicySQL)
			} else {
				all = append(all, statementSQL(analysis.Statements[i:i+1])...)
			}
		}
		if implicitCommit || len(analysis.Statements) > 1 && d == dialect.PostgreSQL && !c.manual {
			all = append(all, "COMMIT")
		}
		if len(analysis.Statements) > 1 && d == dialect.PostgreSQL && !c.manual {
			all = append(all, "BEGIN", "ROLLBACK")
		}
		e.finishDatabaseWork()
		objectPolicies, err = v.objectPolicies(e.ctx, s, all)
		if err == nil {
			err = v.authorizeExecution(s, e, all, &audit)
		}
		if err != nil {
			var pe *core.PolicyError
			if errors.As(err, &pe) {
				audit.ACLID, audit.GroupID, audit.RiskLevel = pe.ACLID, pe.GroupID, pe.RiskLevel
			}
			failure(err, objectPlanAPIError(err))
			return
		}
		if err = e.beginDatabaseWork(s, c); err != nil {
			failure(err, safeDatabaseError(err))
			return
		}
		if err = v.recheckObject(e.ctx, s, c, p); err != nil {
			failure(err, objectPlanAPIError(err))
			return
		}
		if len(analysis.Statements) > 1 && d == dialect.PostgreSQL && !c.manual {
			audit = e.objectBoundaryAudit("BEGIN")
			if err = v.recheckObjectPolicies(e.ctx, s, objectPolicies, "BEGIN"); err != nil {
				denyCommand(err)
				return
			}
			if _, err = c.conn.ExecContext(e.ctx, "BEGIN"); err != nil {
				failure(err, safeDatabaseError(err))
				return
			}
			objectTransaction = true
			if err = finishAudit(true); err != nil {
				failure(err, &APIError{"audit_persistence_failed", "The object transaction could not be audited; no object steps were sent"})
				return
			}
		}
	}
	for statementIndex, statement := range analysis.Statements {
		e.objectStep = statementIndex
		audit = core.AuditRecord{ID: fmt.Sprintf("%s:statement:%d", e.view.ID, statementIndex+1), SQL: statement.SQL, StartedAt: time.Now(), RiskClassified: true}
		if e.browse != nil || e.queryPage != nil || e.planMode != "" {
			audit.SQL = e.sql
		}
		auditPending = true
		e.replayOutput, e.auditRows = "", 0
		triggeredWarnings = map[string]core.AuditRecord{}
		if err := e.ctx.Err(); err != nil {
			failure(err, safeDatabaseError(err))
			return
		}
		if e.cancelRequested.Load() {
			failure(context.Canceled, &APIError{"query_canceled", "Query cancellation requested"})
			return
		}
		if err := s.grant.Check(); err != nil {
			failure(err, &APIError{"session_unavailable", err.Error()})
			return
		}
		// The first statement still owns the connection on which admission
		// read the parser settings. Earlier statements in a script can SET
		// sql_mode (or change database), so take a new snapshot after each.
		current := opts
		if statementIndex > 0 {
			if err := e.beginDatabaseWork(s, c); err != nil {
				failure(err, safeDatabaseError(err))
				return
			}
			current, err = readOptions()
			if err != nil {
				failure(err, safeDatabaseError(err))
				return
			}
		}
		if (e.planMode != "" || e.browse != nil || e.queryPage != nil || e.objectPlan != nil) && current != opts {
			failure(errors.New("sql_context_changed"), &APIError{"sql_context_changed", "SQL mode changed during admission; submit a new request"})
			return
		}
		// AST facts remain valid while parser options are unchanged. Reuse them
		// for dispatch and edit provenance instead of reparsing each consumer.
		if e.objectPlan == nil && e.queryPage == nil && !(e.planMode != "" && specialPlanDialect(d)) && (statement.Facts == nil || current != opts) {
			checked, err := analyze(d, statement.SQL, current)
			if err != nil || len(checked.Statements) != 1 {
				failure(errors.New("sql_context_changed"), &APIError{"sql_context_changed", "Statement boundaries changed with the connection SQL mode; run statements separately"})
				return
			}

			statement = checked.Statements[0]
		}

		// Match Java's statement boundary: a later rejection or confirmation
		// cannot prevent an earlier statement from executing, nor replay it.
		commands := statementSQL([]sqlservice.Statement{statement})
		if e.objectPlan != nil {
			if command := e.objectPlan.commands.Steps[statementIndex].PolicySQL; command != "" {
				commands = []string{command}
			}
		}
		if e.browse != nil || e.queryPage != nil || e.planMode != "" {
			commands = append([]string{e.sql}, commands...)
		}
		if c.manual && (!mysqlObject || statementIndex == 0) {
			if risk, _ := objectTransactionRisk(e, d, statement); risk {
				commands = append(commands, "COMMIT")
			}
		}
		if e.planMode != "" {
			commands = append(commands, planAuthorizationSQL(d, runSQL)...)
		}
		e.finishDatabaseWork()
		if e.cancelRequested.Load() {
			failure(context.Canceled, &APIError{"query_canceled", "Query cancellation requested"})
			return
		}
		if e.objectPlan != nil {
			err = v.recheckObjectPolicies(e.ctx, s, objectPolicies, commands...)
			for _, sql := range commands {
				if authorization, ok := e.objectDecisions[sql]; ok && authorization.ACLID != "" && authorization.RiskLevel >= audit.RiskLevel {
					audit.ACLID, audit.GroupID, audit.RiskLevel = authorization.ACLID, authorization.GroupID, authorization.RiskLevel
				}
			}
		} else {
			err = v.authorizeExecution(s, e, commands, &audit)
		}
		if err != nil {
			denyCommand(err)
			return
		}
		v.update(s, e, func(view *ExecutionView) { view.Status = "running" })

		if err := e.beginDatabaseWork(s, c); err != nil {
			failure(err, safeDatabaseError(err))
			return
		}
		var starSource *resultStarSource
		if statement.ReturnsRows && e.objectPlan == nil && e.planMode == "" && e.queryPage == nil && e.browse == nil {
			if mysqlHint != nil {
				e.mu.RLock()
				// A later statement or an approval wait may have outlived
				// the dictionary read. Recheck the source after authorization.
				if len(analysis.Statements) != 1 || e.view.Review != nil || len(e.confirmationDecisions) != 0 {
					mysqlHint = nil
				}
				e.mu.RUnlock()
			}
			starSource = v.captureQuerySQLSource(s, c, e, current, statement, mysqlState, mysqlHint)
			if e.cancelRequested.Load() {
				failure(context.Canceled, &APIError{"query_canceled", "Query cancellation requested"})
				return
			}
			if err := e.ctx.Err(); err != nil {
				failure(err, safeDatabaseError(err))
				return
			}
		}
		for _, command := range commands {
			if warning, ok := e.policyWarnings[command]; ok {
				key := warning.ACLID + ":" + warning.GroupID
				shown := command
				if e.objectPlan != nil {
					shown = audit.SQL
				}
				triggeredWarnings[key] = core.AuditRecord{ID: audit.ID + ":warning:" + key, SQL: shown, RiskLevel: warning.RiskLevel, ACLID: warning.ACLID, GroupID: warning.GroupID}
				if warning.RiskLevel > audit.RiskLevel {
					audit.RiskLevel, audit.ACLID, audit.GroupID = warning.RiskLevel, warning.ACLID, warning.GroupID
				}
			}
		}
		if c.manual && e.planMode == "" && (!mysqlObject || statementIndex == 0) {
			if statement.Kind == "raw" {
				failure(errors.New("raw_console_manual_mode"), &APIError{"raw_console_manual_mode", "This raw command has unknown transaction behavior; switch to automatic transaction mode before executing it"})
				return
			}
			risk, control := objectTransactionRisk(e, d, statement)
			if control || (risk && !e.confirmImplicitCommit) {
				failure(errors.New("transaction_changed"), &APIError{"transaction_changed", "SQL transaction behavior changed; submit again"})
				return
			}
			if risk {
				pending := c.manualTx != nil
				// Resolve the previous transaction outside the interrupt window for
				// the upcoming DDL. Record its result before admitting the DDL.
				e.finishDatabaseWork()
				if e.cancelRequested.Load() || e.ctx.Err() != nil {
					failure(context.Canceled, safeDatabaseError(context.Canceled))
					return
				}
				if err := v.finishManualTransaction(s, c, e, true); err != nil {
					failure(err, &APIError{"transaction_outcome_unknown", err.Error()})
					return
				}
				v.publishTransaction(s, c, e, "implicit_commit")
				if pending {
					v.update(s, e, func(x *ExecutionView) { x.Transaction.PriorCommitted = true })
				}
				if err := e.beginDatabaseWork(s, c); err != nil {
					failure(err, safeDatabaseError(err))
					return
				}
			} else {
				if err := sqlservice.CheckTransactionTables(e.ctx, c.conn, d, statement, current); err != nil {
					api := safeDatabaseError(err)
					if api.Code != "MYSQL_1146" && api.Code != "OCEANBASE_1146" {
						api = &APIError{"transaction_table_unsupported", "Manual writes require verifiable transactional base tables"}
					}
					failure(err, api)
					return
				}
				wasIdle := c.manualTx == nil
				if err := v.beginManualTransaction(s, c, e); err != nil {
					failure(err, safeDatabaseError(err))
					return
				}
				preflightStartedTransaction = preflightStartedTransaction || wasIdle
			}
		}
		// Metadata probes can absorb an interrupt as a recoverable error. Do
		// not send the user's SQL if cancellation arrived during that work.
		if e.cancelRequested.Load() {
			failure(context.Canceled, &APIError{"query_canceled", "Query cancellation requested"})
			return
		}
		if e.objectPlan != nil {
			if mysqlObject && statementIndex == 0 && c.manual {
				if err := v.checkObjectFingerprint(e.ctx, s, c, e.objectPlan); err != nil {
					failure(err, objectPlanAPIError(err))
					return
				}
			}
			if statementIndex > 0 && e.objectPlan.commands.Steps[statementIndex-1].VerifyAfter {
				if err := v.checkObjectFingerprint(e.ctx, s, c, e.objectPlan); err != nil {
					failure(err, objectPlanAPIError(err))
					return
				}
			}
			step := e.objectPlan.commands.Steps[statementIndex]
			if step.BoundSQL != "" {
				statement.SQL = step.BoundSQL
			}
			browseArgs = step.Args
			statement.MayWrite = true
			v.update(s, e, func(view *ExecutionView) { view.ObjectPlan.Steps[statementIndex].Status = "sent" })
			if err := v.journal(s, e, "sending"); err != nil {
				v.update(s, e, func(view *ExecutionView) { view.ObjectPlan.Steps[statementIndex].Status = "not_sent" })
				failure(err, &APIError{"audit_persistence_failed", "The object step could not be journaled; SQL was not sent"})
				return
			}
		}
		userSQLStarted = true
		currentInFlight = true
		databaseTouched = true
		if e.planMode != "" {
			err := v.runEstimatedPlan(s, c, e, statement.SQL, planVersion)
			if err != nil {
				var api *APIError
				if errors.As(err, &api) && !c.lost.Load() {
					currentInFlight = false
				}
				confirmedCancel = isServerCancellation(err) || driverConfirmedCancellation(d, err)
				failure(err, planAPIError(err))
				return
			}
		} else if statement.ReturnsRows {
			e.resultSQL = statement.SQL
			e.resultAnalysis = statement.Facts
			if e.objectPlan != nil {
				e.resultSQL = ""
				e.resultAnalysis = nil
			}
			rows, err := c.conn.QueryContext(executionContext(e.ctx, d, e.view.ID), statement.SQL, browseArgs...)
			if err != nil {
				confirmedCancel = isServerCancellation(err) || driverConfirmedCancellation(d, err)
				failure(err, safeDatabaseError(err))
				return
			}
			var collectErr error
			if e.queryPage != nil && e.queryPage.action == "count" {
				collectErr = v.collectQueryCount(s, e, rows)
			} else if e.browse != nil && e.browse.plan != nil && e.browse.plan.action == "count" {
				var total string
				if !rows.Next() {
					collectErr = errors.New("Count returned no value")
				} else {
					collectErr = rows.Scan(&total)
				}
				if collectErr == nil {
					collectErr = rows.Err()
				}
				collectErr = errors.Join(collectErr, rows.Close())
				if collectErr == nil {
					collectErr = e.restoreSourceReadContext()
				}
				if collectErr == nil {
					v.update(s, e, func(x *ExecutionView) {
						x.BrowseCount = &BrowseCount{SourceResultID: e.browse.plan.sourceResultID, Total: total}
					})
				}
			} else if e.browse != nil && e.browse.plan != nil && e.browse.plan.action == "append" {
				collectErr = v.collectBrowseAppend(s, e, rows)
			} else if e.export != nil {
				collectErr = v.collectExport(s, e, rows)
			} else {
				collectErr = v.collectResults(s, e, rows, starSource)
			}
			if err := collectErr; err != nil {
				// database/sql can replace a driver's Rows error with ctx.Err().
				// That alone does not prove the server acknowledged cancellation.
				confirmedCancel = isServerCancellation(err)
				failure(err, sourceContextError(err))
				return
			}
		} else {
			result, err := c.conn.ExecContext(executionContext(e.ctx, d, e.view.ID), statement.SQL, browseArgs...)
			if err != nil {
				confirmedCancel = isServerCancellation(err) || driverConfirmedCancellation(d, err)
				failure(err, safeDatabaseError(err))
				return
			}
			affected, _ := result.RowsAffected()
			audit.Output = fmt.Sprintf("Query OK, %d rows affected", affected)
			v.update(s, e, func(x *ExecutionView) {
				x.StatementResults = append(x.StatementResults, StatementResult{RowsAffected: strconv.FormatInt(affected, 10)})
			})
		}
		unverifiedTransaction := false
		if c.manual && e.planMode == "" || mysqlInvoke {
			if risk, _ := objectTransactionRisk(e, d, statement); risk {
				// A MySQL procedure can open a transaction and return with it
				// still active, including on an automatic console.
				state, probeErr := editTransactionState(e.ctx, c, d)
				unverifiedTransaction = probeErr != nil || state != editTxIdle
			}
		}
		if e.objectPlan != nil {
			v.update(s, e, func(view *ExecutionView) {
				step := &view.ObjectPlan.Steps[statementIndex]
				step.Status = "succeeded"
				switch {
				case e.objectPlan.commands.Steps[statementIndex].SessionVariables:
					step.Durability = "session"
				case unverifiedTransaction:
					step.Durability = "unknown"
				case objectTransaction || c.manualTx != nil:
					step.Durability = "pending"
				default:
					step.Durability = "committed"
				}
			})
		}
		if unverifiedTransaction {
			api := unknownTransaction()
			failure(api, api)
			return
		}
		currentInFlight = false
		// MySQL SLEEP can return a normal row after KILL QUERY. An accepted
		// cancellation must not become a successful read just because the
		// driver returned no error. Completed writes are still counted below.
		if !statement.MayWrite && (e.cancelRequested.Load() || e.ctx.Err() != nil) {
			failure(context.Canceled, &APIError{"query_canceled", "Query cancellation requested"})
			return
		}
		completedStatements++
		// Read the actual scope on the pinned connection, including transaction rollback
		// of a PostgreSQL search_path. Never publish the name requested by the client.
		if sqlservice.ChangesConsoleContext(d, statement) {
			scope, err := liveContext(e.ctx, c, d)
			if err != nil {
				failure(err, safeDatabaseError(err))
				return
			}
			c.publishContext(s, scope)
		}
		// A cancel racing a completed write must preserve its completed count;
		// the failure path rolls back only a transaction owned by this console.
		if e.cancelRequested.Load() || e.ctx.Err() != nil {
			failure(context.Canceled, &APIError{"query_canceled", "Query cancellation requested"})
			return
		}
		e.finishDatabaseWork()
		if e.objectPlan != nil {
			if err := v.journal(s, e, "received"); err != nil {
				failure(err, &APIError{"audit_persistence_failed", "Database acknowledged the object step, but its receipt could not be persisted"})
				return
			}
		}
		if err := finishAudit(true); err != nil {
			failure(err, &APIError{"audit_persistence_failed", "The completed statement could not be audited; remaining statements were not sent"})
			return
		}
	}
	if objectTransaction {
		// The last step's audit may outlive the grant or its COMMIT policy.
		// Record the boundary separately so its outcome cannot be mistaken
		// for the preceding statement's acknowledgement.
		audit = e.objectBoundaryAudit("COMMIT")
		auditPending = true
		triggeredWarnings = nil
		if err := v.recheckObjectPolicies(e.ctx, s, objectPolicies, "COMMIT"); err != nil {
			denyCommand(err)
			return
		}
		if err := e.beginTransactionBoundary(); err != nil {
			failure(err, safeDatabaseError(err))
			return
		}
		cleanup, done := context.WithTimeout(c.ctx, databaseCleanupTimeout)
		_, err := c.conn.ExecContext(cleanup, "COMMIT")
		done()
		objectTransaction = false
		if err != nil {
			invalidateEditConnection(s, c)
			failure(err, &APIError{"transaction_outcome_unknown", "Object transaction commit could not be confirmed"})
			return
		}
	}
	if c.manual && e.planMode == "" {
		outcome := ""
		if tx := e.snapshot().Transaction; tx != nil {
			outcome = tx.Outcome
		}
		// Every successful submission changes the confirmation snapshot, even
		// when it reused the same open transaction.
		v.publishTransaction(s, c, e, outcome)
	}
	// Cancellation may win while the last statement's audit is being recorded.
	// Publish success under the same lock that accepts cancellation requests.
	var canceled bool
	v.update(s, e, func(x *ExecutionView) {
		canceled = e.cancelRequested.Load() || e.ctx.Err() != nil
		if !canceled {
			x.Status = "succeeded"
			if x.ObjectPlan != nil {
				x.ObjectPlan.Outcome = "committed"
				if c.manualTx != nil {
					x.ObjectPlan.Outcome = "staged"
				} else {
					for i := range x.ObjectPlan.Steps {
						if x.ObjectPlan.Steps[i].Durability == "pending" {
							x.ObjectPlan.Steps[i].Durability = "committed"
						}
					}
				}
			}
		}
	})
	if canceled {
		failure(context.Canceled, safeDatabaseError(context.Canceled))
	}
}
func statementSQL(stmts []sqlservice.Statement) []string {
	out := make([]string, 0, len(stmts))
	for _, s := range stmts {
		out = append(out, s.SQL)
		out = append(out, s.PolicySQL...)
	}
	return out
}
func liveOptions(ctx context.Context, c *console, d dialect.ID) (sqlservice.AnalyzeOptions, error) {
	var o sqlservice.AnalyzeOptions
	if d.UsesMySQLSyntax() {
		var version string
		if err := c.conn.QueryRowContext(ctx, "SELECT @@session.sql_mode, VERSION()").Scan(&o.MySQLSQLMode, &version); err != nil {
			return o, err
		}
		o = mysqlAnalyzeOptions(o.MySQLSQLMode, version)
	}
	return o, nil
}
func isServerCancellation(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1317
	}
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "57014" || extraServerCancellation(err)
}

func isServerError(err error) bool {
	var me *mysql.MySQLError
	var pe *pgconn.PgError
	return errors.As(err, &me) || errors.As(err, &pe) || extraDatabaseError(err) != nil
}

func binaryResultType(dbType string) bool {
	dbType = strings.ToUpper(dbType)
	return strings.Contains(dbType, "BLOB") || strings.Contains(dbType, "BINARY") || dbType == "BYTEA" || dbType == "RAW" || dbType == "LONG RAW" || dbType == "IMAGE"
}

func normalizeValue(value any, dbType string) any {
	dbType = strings.ToUpper(dbType)
	switch x := value.(type) {
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case []byte:
		if x == nil {
			return nil
		}
		if binaryResultType(dbType) || !validTextBytes(x) {
			return "0x" + hex.EncodeToString(x)
		}
		return string(x)
	case time.Time:
		switch dbType {
		case "DATE":
			return x.Format("2006-01-02")
		case "DATETIME", "TIMESTAMP":
			return x.Format("2006-01-02 15:04:05.999999999")
		default:
			return x.Format(time.RFC3339Nano)
		}
	case float64:
		if math.IsNaN(x) {
			return "NaN"
		}
		if math.IsInf(x, 1) {
			return "Infinity"
		}
		if math.IsInf(x, -1) {
			return "-Infinity"
		}
		return x
	default:
		return normalizeComposite(value, dbType)
	}
}
func (v *Server) collectResults(s *session, e *execution, rows *sql.Rows, starSource *resultStarSource) (err error) {
	var collected []*Result
	defer func() {
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
		// Once the rows have drained, an already requested interrupt must
		// finish before source metadata uses the same physical connection.
		if e.cancelRequested.Load() {
			e.finishDatabaseWork()
		}
		if err == nil {
			bindObjectCursors(s, e, collected)
		}
		if err == nil && e.resultSQL != "" {
			v.bindResultEditSource(s, e, collected, starSource)
		}
		e.finishDatabaseWork()
		// Source resolution above must see the original namespace. No result
		// may become visible until the user's current namespace is restored.
		if err == nil {
			err = e.restoreSourceReadContext()
		}
		if err != nil && e.restoreReadContext != nil {
			s.mu.Lock()
			for _, result := range collected {
				s.resultBytes -= result.memoryBytes
			}
			s.mu.Unlock()
			return
		}
		for _, result := range collected {
			s.mu.Lock()
			s.results[result.ID] = result
			s.mu.Unlock()
			v.update(s, e, func(x *ExecutionView) {
				x.ResultIDs = append(x.ResultIDs, result.ID)
				x.StatementResults = append(x.StatementResults, StatementResult{ResultID: result.ID, SourceSQL: result.SourceSQL, ResultRole: result.objectRole})
			})
		}
	}()
	for {
		var browse *browsePlan
		if e.browse != nil {
			browse = e.browse.plan
		}
		// Reuse the bounded row reader without publishing a table-browse identity.
		if e.queryPage != nil && e.queryPage.action == "page" {
			browse = &browsePlan{view: BrowsePage{Supported: true, PageSize: e.queryPage.view.PageSize}}
		}
		result, err := v.readRowsWithMask(s, rows, func(labels []string) []string {
			return e.resultMaskColumns(s.grant.Dialect(), labels)
		}, browse)
		if err != nil {
			return err
		}
		if (e.consoleMode || e.objectPlan != nil) && len(result.Columns) == 0 {
			// QueryContext is also used for opaque commands that return no row
			// set. Do not invent an empty table or an unknown affected-row count.
			if !rows.NextResultSet() {
				return rows.Err()
			}
			continue
		}
		result.consoleID, result.sourceSQL, result.browse = e.view.ConsoleID, e.resultSQL, e.browse
		result.sourceAnalysis = e.resultAnalysis
		result.querySource = e.querySource
		if e.queryPage != nil {
			p := *e.queryPage
			if result.Browse != nil {
				p.view.HasMore = result.Browse.HasMore
			}
			result.querySource = &p
			result.sourceSQL = p.original
			// The page wrapper only selects a window of the original projection.
			// Resolve and freeze that original source before publishing rows, using
			// the same column, primary-key and masking checks as the initial query.
			result.sourceAnalysis = nil
			result.Browse = nil
		}
		if e.objectPlan != nil {
			result.objectRole = e.objectPlan.commands.Steps[e.objectStep].ResultRole
			result.editSourceBound = true
			result.editSourceError = "Routine results are read-only"
		}
		result.SourceSQL = result.sourceSQL
		if result.querySource != nil {
			view := result.querySource.view
			result.QueryPage = &view
		}
		appendResultReplay(e, result)
		collected = append(collected, result)
		if !rows.NextResultSet() {
			return rows.Err()
		}
		if e.objectPlan != nil && len(collected) >= 32 {
			v.update(s, e, func(view *ExecutionView) { view.ObjectPlan.ResultsTruncated = true })
			return nil
		}
	}
}
func (v *Server) readRows(s *session, rows *sql.Rows, plans ...*browsePlan) (*Result, error) {
	return v.readRowsWithMask(s, rows, func(labels []string) []string { return labels }, plans...)
}
func (v *Server) readRowsWithMask(s *session, rows *sql.Rows, maskColumns func([]string) []string, plans ...*browsePlan) (*Result, error) {
	reserved := 0
	complete := false
	defer func() {
		if !complete && reserved > 0 {
			s.mu.Lock()
			s.resultBytes -= reserved
			s.mu.Unlock()
		}
	}()
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	result := &Result{ID: newID("res_"), Columns: []Column{}, Rows: [][]any{}}
	limit := v.opts.MaxRows
	if len(plans) > 0 && plans[0] != nil {
		view := plans[0].view
		if view.Supported {
			limit = view.PageSize
			if plans[0].fetchSize > 0 {
				limit = plans[0].fetchSize
			}
			more := false
			view.HasMore = &more
		}
		result.Browse = &view
	}
	labels := make([]string, len(types))
	for i, column := range types {
		result.Columns = append(result.Columns, Column{column.Name(), column.DatabaseTypeName()})
		labels[i] = column.Name()
	}
	sources := make([]string, len(types))
	if maskColumns != nil {
		sources = maskColumns(labels)
	}
	for rows.Next() {
		if result.Browse != nil && result.Browse.Supported && len(result.Rows) >= limit {
			more := true
			result.Browse.HasMore = &more
			break
		}
		raw := make([]any, len(types))
		targets := make([]any, len(types))
		for i := range raw {
			targets[i] = &raw[i]
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		copyLossy := make([]bool, len(raw))
		for i := range raw {
			if data, ok := raw[i].([]byte); ok && data != nil && !binaryResultType(types[i].DatabaseTypeName()) && !validTextBytes(data) {
				copyLossy[i] = true
			}
			raw[i], err = materializeValue(raw[i], types[i].DatabaseTypeName(), v.opts.MaxResultBytes, s.grant.Dialect())
			if err != nil {
				return nil, err
			}
		}
		original := append([]any(nil), raw...)
		masked := make([]bool, len(raw))
		extra := 0
		for i := range raw {
			raw[i], masked[i] = v.provider.Mask(s.grant, sources[i], types[i].Name(), raw[i])
			if masked[i] {
				b, encodeErr := json.Marshal(original[i])
				if encodeErr != nil {
					return nil, encodeErr
				}
				extra += len(b)
			}
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		space := v.opts.MaxResultBytes <= 0 || s.resultBytes+len(encoded)+extra <= v.opts.MaxResultBytes
		if len(result.Rows) < limit && space {
			s.resultBytes += len(encoded) + extra
			reserved += len(encoded) + extra
		}
		s.mu.Unlock()
		if len(result.Rows) >= limit || !space {
			if result.Browse != nil && result.Browse.Supported {
				result.Browse.HasMore = nil
			}
			result.Truncated = true
			break
		}
		result.Rows = append(result.Rows, raw)
		result.original = append(result.original, original)
		result.masked = append(result.masked, masked)
		result.copyLossy = append(result.copyLossy, copyLossy)
		result.rowRefs = append(result.rowRefs, newID("row_"))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result.Browse != nil && result.Browse.Supported {
		if result.Truncated {
			result.Browse.LimitReached = "bytes"
		} else if len(result.Rows) >= v.opts.MaxRows && result.Browse.HasMore != nil && *result.Browse.HasMore {
			result.Browse.LimitReached = "rows"
		}
	}
	result.TotalRows = len(result.Rows)
	result.memoryBytes = reserved
	complete = true
	return result, nil
}
func (v *Server) resultRows(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, false)
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()
		fail(w, 410, "session_closed", "Session is closed")
		return
	}
	result := s.results[r.PathValue("rid")]
	if result == nil {
		s.mu.Unlock()
		fail(w, 404, "not_found", "Result not found")
		return
	}
	page := *result
	offset := positiveInt(r.URL.Query().Get("offset"), 0, len(result.Rows))
	limit := positiveInt(r.URL.Query().Get("limit"), 200, 1000)
	if limit == 0 {
		limit = 200
	}
	end := min(offset+limit, len(result.Rows))
	page.Rows = append([][]any{}, result.Rows[offset:end]...)
	page.Offset = offset
	if page.CursorSource != nil {
		source := *page.CursorSource
		source.Cells = nil
		for _, cell := range page.CursorSource.Cells {
			if cell.Row >= offset && cell.Row < end {
				source.Cells = append(source.Cells, cell)
			}
		}
		page.CursorSource = &source
		if len(source.Cells) == 0 {
			page.CursorSource = nil
		}
	}
	if end < len(result.Rows) {
		page.NextOffset = &end
	}
	s.mu.Unlock()
	writeJSON(w, 200, page)
}
func (v *Server) journal(s *session, e *execution, phase string) error {
	dir := v.opts.StateDir
	if dir == "" {
		dir = "data/go"
	}
	dir = filepath.Join(dir, "executions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	entry := map[string]any{"session_id": s.id, "core_session_id": s.grant.ID, "execution": e.snapshot(), "sql": e.sql, "phase": phase, "recovery": "An unfinished intent has unknown outcome; never replay SQL"}
	if e.planMode != "" {
		entry["generated_sql"] = e.planSQL
		entry["auxiliary_sql"] = planAuthorizationSQL(s.grant.Dialect(), e.planSQL)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".intent-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(name, filepath.Join(dir, e.view.ID+".json")); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
