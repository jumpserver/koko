package engine

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jumpserver/koko/pkg/chen/core"
)

// Automatic-mode SQL may open a transaction outside Chen's managed transaction.
// Read the pinned connection only when a client needs to decide whether it is safe to close.
func (v *Server) consoleTransactionState(w http.ResponseWriter, r *http.Request) {
	s, c, ctx, done := v.catalogConsole(w, r)
	if s == nil {
		return
	}
	defer done()
	if err := v.auditCatalog(ctx, s, c, "transaction-state"); err != nil {
		fail(w, 503, "audit_persistence_failed", "Transaction state access could not be audited")
		return
	}
	state := editTxActive
	if c.manualTx == nil {
		var err error
		state, err = editTransactionState(ctx, c, s.grant.Config.Dialect)
		if err != nil {
			state = editTxUnknown
		}
	}
	writeJSON(w, 200, map[string]editTxState{"state": state})
}

type TransactionView struct {
	Supported          bool               `json:"supported"`
	Manual             bool               `json:"manual"`
	State              string             `json:"state"` // idle, active, unknown
	Revision           uint64             `json:"revision"`
	IdleTimeoutSeconds int64              `json:"idle_timeout_seconds,omitempty"`
	Notice             *TransactionNotice `json:"notice,omitempty"`
}

type TransactionNotice struct {
	ID             string    `json:"id"`
	Reason         string    `json:"reason"`
	Outcome        string    `json:"outcome"`
	TimeoutSeconds int64     `json:"timeout_seconds"`
	At             time.Time `json:"at"`
	AuditStatus    string    `json:"audit_status"`
}

type TransactionOutcome struct {
	TransactionView
	Action         string `json:"action,omitempty"`
	Outcome        string `json:"outcome,omitempty"` // committed, rolled_back, implicit_commit, unknown
	PriorCommitted bool   `json:"prior_committed,omitempty"`
}

func supportsManualTransaction(d string) bool {
	switch d {
	case "mysql", "mariadb", "oceanbase-mysql", "postgresql", "sqlserver", "oracle", "oceanbase-oracle", "dameng", "db2":
		return true
	}
	return false
}

// Published views are immutable; SSE snapshots hold session.mu and must never
// acquire console.mu (the execution path acquires those locks in reverse order).
func (c *console) view() ConsoleView {
	state := "ready"
	if c.closed.Load() {
		state = "closed"
	} else if c.lost.Load() {
		state = "lost"
	}
	scope := c.currentContext()
	return ConsoleView{ID: c.id, Database: scope.Database, Schema: scope.Schema, State: state, Transaction: c.transaction.Load()}
}

func (v *Server) publishTransaction(s *session, c *console, e *execution, outcome string) {
	previous := c.transaction.Load()
	view := TransactionView{Supported: supportsManualTransaction(s.grant.Config.Dialect), Manual: c.manual, State: "idle", Revision: 1, IdleTimeoutSeconds: v.transactionIdleSeconds()}
	if previous != nil {
		view.Revision = previous.Revision + 1
		view.Notice = previous.Notice
	}
	if c.manualTx != nil {
		view.State = "active"
	}
	if c.lost.Load() {
		view.State = "unknown"
	}
	c.transaction.Store(&view)
	s.emit("console.updated", c.view())
	if e != nil {
		v.update(s, e, func(x *ExecutionView) {
			prior := x.Transaction != nil && x.Transaction.PriorCommitted
			x.Transaction = &TransactionOutcome{TransactionView: view, Action: e.transactionAction, Outcome: outcome, PriorCommitted: prior}
		})
	}
}

func (v *Server) transactionIdleSeconds() int64 {
	if v.opts.TransactionIdleTimeout <= 0 {
		return 0
	}
	return int64(1 + (v.opts.TransactionIdleTimeout-1)/time.Second)
}

// The console worker owns this deadline. Its timer is stopped while a request
// runs, so neither a long SQL statement nor its approval can race this rollback.
// Metadata polling, SSE reconnects and editor typing do not extend transaction life.
func (v *Server) expireManualTransaction(s *session, c *console) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.manual || c.manualTx == nil || c.closed.Load() || c.lost.Load() || c.ctx.Err() != nil {
		return
	}
	if v.opts.TransactionIdleTimeout <= 0 || time.Since(c.transactionActivity) < v.opts.TransactionIdleTimeout {
		return
	}
	start := time.Now()
	err := v.finishManualTransaction(s, c, nil, false)
	notice := &TransactionNotice{ID: newID("tx_"), Reason: "idle_timeout", Outcome: "rolled_back", TimeoutSeconds: v.transactionIdleSeconds(), At: time.Now(), AuditStatus: "recorded"}
	record := core.AuditRecord{ID: notice.ID, SQL: "ROLLBACK", StartedAt: start, FinishedAt: notice.At, RiskClassified: true, Output: fmt.Sprintf("console_id=%s reason=transaction_idle_timeout timeout_seconds=%d rollback_confirmed=%t", c.id, notice.TimeoutSeconds, err == nil)}
	if err != nil {
		notice.Outcome = "unknown"
		record.Error = "Idle transaction rollback could not be confirmed"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if v.provider.Record(ctx, s.grant, record) != nil {
		notice.AuditStatus = "failed"
	}
	cancel()
	view := *c.transaction.Load()
	view.Revision++
	view.Notice = notice
	c.transaction.Store(&view)
	s.emit("console.updated", c.view())
}

func transactionCommand(action, onPending string) (string, bool) {
	if onPending != "" && (action != "auto" || onPending != "commit" && onPending != "rollback") {
		return "", false
	}
	switch action {
	case "manual":
		return "/* Chen manual transaction mode */ BEGIN", true
	case "auto":
		if onPending == "commit" {
			return "/* Chen automatic commit mode */ COMMIT", true
		}
		if onPending == "rollback" {
			return "/* Chen automatic commit mode */ ROLLBACK", true
		}
		return "/* Chen automatic commit mode */", true
	case "commit":
		return "COMMIT", true
	case "rollback":
		return "ROLLBACK", true
	}
	return "", false
}

func (v *Server) transactionAction(s *session, c *console, e *execution, audit *core.AuditRecord) error {
	if !supportsManualTransaction(s.grant.Config.Dialect) {
		return &APIError{"transaction_unsupported", "This database does not support manual transactions"}
	}
	commands := []string{e.sql}
	if e.transactionAction == "manual" {
		commands = append(commands, "BEGIN", "ROLLBACK")
	}
	// Mode-switch audit labels contain a comment. Authorize the plain database
	// boundary as well, including policies anchored at the start of a command.
	if e.transactionAction == "auto" && c.manualTx != nil {
		switch e.transactionOnPending {
		case "commit":
			commands = append(commands, "COMMIT")
		case "rollback":
			commands = append(commands, "ROLLBACK")
		}
	}
	if err := v.authorizeExecution(s, e, commands, audit); err != nil {
		var policy *core.PolicyError
		if errors.As(err, &policy) {
			audit.RiskLevel, audit.ACLID, audit.GroupID = policy.RiskLevel, policy.ACLID, policy.GroupID
			if policy.Code == "command_warning_confirmation_required" {
				audit.RiskLevel = 0
			}
			return &APIError{policy.Code, policy.Error()}
		}
		return &APIError{"command_denied", err.Error()}
	}
	if e.ctx.Err() != nil || e.cancelRequested.Load() {
		return context.Canceled
	}
	for _, warning := range e.policyWarnings {
		if warning.RiskLevel > audit.RiskLevel {
			audit.RiskLevel, audit.ACLID, audit.GroupID = warning.RiskLevel, warning.ACLID, warning.GroupID
		}
	}
	outcome := ""
	switch e.transactionAction {
	case "manual":
		if !c.manual {
			state, err := editTransactionState(e.ctx, c, s.grant.Config.Dialect)
			if err != nil || state != editTxIdle {
				return &APIError{"transaction_busy", "Finish the existing SQL transaction before enabling manual mode"}
			}
			c.manual = true
		}
	case "auto":
		if c.manualTx != nil {
			if e.transactionOnPending == "" {
				return &APIError{"transaction_pending", "Commit or roll back the pending transaction before switching modes"}
			}
			commit := e.transactionOnPending == "commit"
			if err := v.finishManualTransaction(s, c, e, commit); err != nil {
				return err
			}
			outcome = "rolled_back"
			if commit {
				outcome = "committed"
			}
		}
		c.manual = false
	case "commit", "rollback":
		if !c.manual || c.manualTx == nil {
			return &APIError{"transaction_idle", "There is no pending manual transaction"}
		}
		commit := e.transactionAction == "commit"
		if err := v.finishManualTransaction(s, c, e, commit); err != nil {
			return err
		}
		outcome = "rolled_back"
		if commit {
			outcome = "committed"
		}
	}
	v.publishTransaction(s, c, e, outcome)
	return nil
}

func (v *Server) beginManualTransaction(s *session, c *console, e *execution) error {
	if !c.manual || c.manualTx != nil {
		return nil
	}
	// The console already owns a pinned sql.Conn. A sql.Tx would hold its
	// close lock across requests, deadlocking Conn's ErrBadConn cleanup from
	// metadata or SQL. Keep the driver boundary under Raw's connection lock;
	// request cancellation never races an implicit database/sql rollback.
	err := c.conn.Raw(func(raw any) error {
		conn, ok := raw.(driver.ConnBeginTx)
		if !ok {
			return &APIError{"transaction_unsupported", "This database does not support manual transactions"}
		}
		tx, err := conn.BeginTx(context.WithoutCancel(e.ctx), driver.TxOptions{})
		if err != nil {
			return err
		}
		c.manualTx = tx
		return nil
	})
	if err != nil {
		return err
	}
	v.publishTransaction(s, c, e, "")
	return nil
}

func (v *Server) finishManualTransaction(s *session, c *console, e *execution, commit bool) error {
	if c.manualTx == nil {
		return nil
	}
	tx := c.manualTx
	c.manualTx = nil
	err := c.conn.Raw(func(any) error {
		if commit {
			return tx.Commit()
		}
		return tx.Rollback()
	})
	if err != nil {
		invalidateEditConnection(s, c)
		v.publishTransaction(s, c, e, "unknown")
		return &APIError{"transaction_outcome_unknown", "The transaction outcome could not be confirmed; reconnect and verify the data before retrying"}
	}
	// Cached rows and edit plans may have been read from the rolled-back state.
	s.mu.Lock()
	for _, result := range s.results {
		if result.consoleID == c.id {
			result.editStale = true
		}
	}
	s.mu.Unlock()
	outcome := "rolled_back"
	if commit {
		outcome = "committed"
	}
	v.publishTransaction(s, c, e, outcome)
	return nil
}

// Closing a console discards its physical connection, even if cleanup fails;
// an open transaction must never return to the shared connection pool.
func (v *Server) closeManualTransaction(s *session, c *console) {
	if !c.manual {
		return
	}
	if c.manualTx != nil {
		start := time.Now()
		err := c.conn.Raw(func(any) error { return c.manualTx.Rollback() })
		c.manualTx = nil
		record := core.AuditRecord{ID: newID("tx_"), SQL: "ROLLBACK", StartedAt: start, FinishedAt: time.Now(), RiskClassified: true, Output: fmt.Sprintf("console_id=%s closed; rollback_confirmed=%t", c.id, err == nil)}
		if err != nil {
			record.Error = "Transaction cleanup could not be confirmed"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = v.provider.Record(ctx, s.grant, record)
		cancel()
	}
	_ = c.conn.Raw(func(any) error { return driver.ErrBadConn })
}
