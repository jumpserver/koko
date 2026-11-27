package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

// Optional fields are omitted when encoding a prepared browse or paging request,
// preserving the original five-field request's JSON byte budget.
type executionRequest struct {
	ConsoleMode           bool   `json:"console_mode,omitempty"`
	TransactionAction     string `json:"transaction_action,omitempty"`
	TransactionOnPending  string `json:"transaction_on_pending,omitempty"`
	TransactionRevision   uint64 `json:"transaction_revision,omitempty"`
	ConfirmImplicitCommit bool   `json:"confirm_implicit_commit,omitempty"`
	PlanMode              string `json:"plan_mode,omitempty"`
	Format                string `json:"format"`
	SQL                   string `json:"sql"`
	RequestID             string `json:"request_id"`
	DocumentVersion       int    `json:"document_version,omitempty"`
	SubmitReview          bool   `json:"submit_review"`
	ConfirmWarnings       bool   `json:"confirm_warnings"`
}

// Internal plans are server-owned and never decoded from the HTTP body.
type executionIntent struct {
	objectPlan *objectPlan
	export     bool
	browse     *editBrowse
	queryPage  *queryPagePlan
}

func (v *Server) submit(w http.ResponseWriter, r *http.Request) {
	v.submitExecution(w, r, executionIntent{}, nil)
}

// Prepared operations repeat authorization after resolving their plan, just as
// ordinary requests do before decoding. A nil prepared request reads the body.
func (v *Server) submitExecution(w http.ResponseWriter, r *http.Request, intent executionIntent, prepared *executionRequest) {
	s := v.auth(w, r, false)
	if s == nil {
		return
	}
	c := v.findConsole(w, r, s)
	if c == nil {
		return
	}
	if intent.export && !s.grant.Permissions.Download {
		fail(w, 403, "download_permission_denied", "Result download is not permitted")
		return
	}
	var req executionRequest
	if prepared == nil {
		if !decode(w, r, &req) {
			return
		}
	} else {
		// Preserve JSON's invalid UTF-8 normalization and the generated request
		// size limit without rebuilding an HTTP request or replacing its body.
		body, _ := json.Marshal(prepared)
		if len(body) > maxJSONRequestBytes || json.Unmarshal(body, &req) != nil {
			fail(w, 400, "invalid_request", "Invalid JSON request")
			return
		}
	}
	if req.RequestID == "" {
		req.RequestID = r.Header.Get("Idempotency-Key")
	}
	v.admitExecution(w, s, c, req, intent)
}

func (v *Server) admitExecution(w http.ResponseWriter, s *session, c *console, req executionRequest, intent executionIntent) {
	isExport, browse, queryPage := intent.export, intent.browse, intent.queryPage
	if req.ConsoleMode && (req.PlanMode != "" || req.TransactionAction != "" || isExport || browse != nil || queryPage != nil) {
		fail(w, 400, "invalid_console_mode", "Raw console mode requires an ordinary execution request")
		return
	}
	if req.PlanMode != "" && (req.PlanMode != "ESTIMATED" || isExport || browse != nil || queryPage != nil) {
		fail(w, 400, "invalid_plan_mode", "Estimated plans require an ordinary execution request")
		return
	}
	if !isExport && req.Format != "" {
		fail(w, 400, "invalid_request", "Export format requires the exports endpoint")
		return
	}
	if isExport && (req.Format != "csv" && req.Format != "xlsx") {
		fail(w, 400, "invalid_export_format", "Choose CSV or XLSX")
		return
	}
	if req.TransactionAction != "" {
		command, valid := transactionCommand(req.TransactionAction, req.TransactionOnPending)
		if !valid || req.SQL != "" || req.PlanMode != "" || isExport || browse != nil || queryPage != nil || req.ConfirmImplicitCommit || req.TransactionRevision == 0 {
			fail(w, 400, "invalid_transaction_action", "Invalid transaction action")
			return
		}
		req.SQL = command
	} else if req.TransactionOnPending != "" {
		fail(w, 400, "invalid_transaction_action", "Transaction resolution requires a mode action")
		return
	}
	if intent.objectPlan == nil && strings.TrimSpace(req.SQL) == "" || sqlservice.DocumentExceedsLimit(req.SQL) {
		fail(w, 400, "invalid_sql", "SQL is empty or too large")
		return
	}
	if req.RequestID == "" || len(req.RequestID) > 128 {
		fail(w, 400, "request_id_required", "A stable request_id is required")
		return
	}
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil || s.consoles[c.id] != c || c.closed.Load() || c.lost.Load() || c.ctx.Err() != nil || s.grant.Check() != nil {
		s.mu.Unlock()
		fail(w, 409, "session_unavailable", "Session or console changed while submitting SQL")
		return
	}
	if old := c.requests[req.RequestID]; old != nil {
		s.mu.Unlock()
		if old.objectPlan != intent.objectPlan || old.consoleMode != req.ConsoleMode || old.transactionAction != req.TransactionAction || old.transactionOnPending != req.TransactionOnPending || old.transactionRevision != req.TransactionRevision || old.confirmImplicitCommit != req.ConfirmImplicitCommit || (old.queryPage != nil) != (queryPage != nil) || (queryPage != nil && old.queryPage.fingerprint != queryPage.fingerprint) || old.planMode != req.PlanMode || old.objectPlan == nil && old.sql != req.SQL || old.submitReview != req.SubmitReview || old.confirmWarnings != req.ConfirmWarnings || (old.export != nil) != isExport || (old.export != nil && old.export.format != req.Format) || (old.browse != nil) != (browse != nil) || old.edit != nil || (browse != nil && browse.plan != nil && (old.browse == nil || old.browse.plan == nil || old.browse.plan.fingerprint != browse.plan.fingerprint)) {
			fail(w, 409, "idempotency_conflict", "Request ID already belongs to different SQL")
			return
		}
		writeJSON(w, 202, old.snapshot())
		return
	}
	if p := intent.objectPlan; p != nil && (p.used || time.Now().After(p.expires)) {
		s.mu.Unlock()
		fail(w, 409, "object_plan_expired", "Preview was used or expired; preview again")
		return
	}
	if s.activeExecutionCountLocked() >= 256 {
		s.mu.Unlock()
		fail(w, 429, "execution_limit", "Too many active executions; wait for pending work to finish")
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	e := &execution{view: ExecutionView{ID: newID("exe_"), ConsoleID: c.id, Status: "queued", ResultIDs: []string{}, StatementResults: []StatementResult{}, AuditStatus: "pending"}, sql: req.SQL, documentVersion: req.DocumentVersion, ctx: ctx, cancel: cancel, cancelDone: make(chan struct{}), submitReview: req.SubmitReview, confirmWarnings: req.ConfirmWarnings}
	e.objectPlan = intent.objectPlan
	if e.objectPlan != nil {
		e.view.ObjectPlan = e.objectPlan.outcome()
	}
	e.consoleMode = req.ConsoleMode
	e.transactionAction, e.transactionOnPending, e.transactionRevision, e.confirmImplicitCommit = req.TransactionAction, req.TransactionOnPending, req.TransactionRevision, req.ConfirmImplicitCommit
	if req.TransactionAction != "" && c.transaction.Load() != nil {
		e.view.Transaction = &TransactionOutcome{TransactionView: *c.transaction.Load(), Action: req.TransactionAction}
	}
	e.planMode = req.PlanMode
	if e.planMode != "" {
		e.view.Plan = &PlanIdentity{RequestID: req.RequestID, Mode: e.planMode, Dialect: s.grant.Config.Dialect}
	}
	e.browse = browse
	e.queryPage = queryPage
	if e.browse != nil {
		e.view.SQL = req.SQL
	}
	if isExport {
		j, err := v.newExportLocked(s, req.Format, "query", e)
		if err != nil {
			s.mu.Unlock()
			cancel()
			exportAPIError(w, err)
			return
		}
		e.export = j
		e.view.ExportID = j.id
	}
	e.startDeadline(s, v.opts.QueryTimeout)
	select {
	case c.queue <- e:
		if e.objectPlan != nil {
			e.objectPlan.used = true
		}
		s.executions[e.view.ID] = e
		c.requests[req.RequestID] = e
	default:
		s.mu.Unlock()
		cancel()
		e.stopDeadline()
		if e.export != nil {
			e.export.cleanup(false)
		}
		fail(w, 429, "queue_full", "Console queue is full")
		return
	}
	s.mu.Unlock()
	s.grant.Touch()
	s.emit("execution.updated", e.snapshot())
	writeJSON(w, 202, e.snapshot())
}
