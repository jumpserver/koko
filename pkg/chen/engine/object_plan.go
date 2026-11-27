package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

type objectPlan struct {
	id, consoleID, sql string
	req                sqlservice.ObjectPlanRequest
	commands           sqlservice.ObjectCommands
	expires            time.Time
	recoveryUntil      time.Time // session.mu; set after the worker releases its command data.
	revision           uint64
	requiresImplicit   bool
	bytes              int
	used               bool // session.mu
}

type ObjectStepOutcome struct {
	Durability string `json:"durability,omitempty"`
	Status     string `json:"status"`
	ResultRole string `json:"result_role,omitempty"`
}
type ObjectPlanOutcome struct {
	SideEffectsPossible bool                 `json:"side_effects_possible,omitempty"`
	PlanID              string               `json:"plan_id"`
	Action              string               `json:"action"`
	Ref                 sqlservice.ObjectRef `json:"ref"`
	Steps               []ObjectStepOutcome  `json:"steps"`
	Outcome             string               `json:"outcome"`
	ResultsTruncated    bool                 `json:"results_truncated,omitempty"`
}

func (p *objectPlan) outcome() *ObjectPlanOutcome {
	out := &ObjectPlanOutcome{PlanID: p.id, Action: p.req.Action, Ref: p.commands.Ref, Outcome: "pending"}
	for _, s := range p.commands.Steps {
		out.Steps = append(out.Steps, ObjectStepOutcome{Status: "not_sent", ResultRole: s.ResultRole})
	}
	return out
}

func (v *Server) objectCommands(ctx context.Context, s *session, c *console, req sqlservice.ObjectPlanRequest) (sqlservice.ObjectCommands, error) {
	if req.Action == "fetchCursor" {
		return v.cursorCommands(ctx, s, c, req)
	}
	return sqlservice.BuildObjectCommands(ctx, c.conn, s.grant.Dialect(), req, func(sql string) error { return v.provider.Authorize(ctx, s.grant, sql) }, func(name, value string) (string, bool) {
		masked, hidden := v.provider.Mask(s.grant, name, name, value)
		return fmt.Sprint(masked), hidden
	})
}

func objectPlanAPIError(err error) *APIError {
	var pe *core.PolicyError
	switch {
	case errors.As(err, &pe):
		return &APIError{pe.Code, pe.Error()}
	case errors.Is(err, sqlservice.ErrCatalogForbidden):
		return &APIError{"catalog_forbidden", "The object or its definition is unavailable for this account"}
	case errors.Is(err, sqlservice.ErrCatalogNotFound), errors.Is(err, sqlservice.ErrObjectChanged):
		return &APIError{"object_changed", "The object changed or is no longer visible. Reload it and preview again."}
	case errors.Is(err, sqlservice.ErrCatalogUnsupported):
		return &APIError{"catalog_unsupported", "This operation is not currently supported for the connected database version."}
	case isServerError(err):
		return safeDatabaseError(err)
	default:
		return &APIError{"invalid_object_plan", err.Error()}
	}
}

func (v *Server) previewObjectPlan(w http.ResponseWriter, r *http.Request) {
	var req sqlservice.ObjectPlanRequest
	if !decode(w, r, &req) {
		return
	}
	s, c, ctx, done := v.editConsole(w, r)
	if s == nil {
		return
	}
	defer done()
	if err := v.auditCatalog(ctx, s, c, "object-plan-preview"); err != nil {
		fail(w, 503, "audit_persistence_failed", "Object preview could not be audited")
		return
	}
	tx := c.transaction.Load()
	if tx == nil {
		fail(w, 409, "transaction_changed", "Reload the console transaction state")
		return
	}
	if !tx.Manual {
		state, err := editTransactionState(ctx, c, s.grant.Dialect())
		if err != nil || state != editTxIdle {
			fail(w, 409, "transaction_pending", "Finish the open SQL transaction or use a matching idle console")
			return
		}
	}
	if req.Action == "invoke" && s.grant.Dialect().UsesMySQLSyntax() {
		// MySQL retains user variable names until disconnect, even after SET NULL.
		// Each serialized call initializes and clears the same bounded slots.
		if c.objectBindings == "" {
			c.objectBindings = newID("")[:24]
		}
		req.BindingID = c.objectBindings
	}
	commands, err := v.objectCommands(ctx, s, c, req)
	if err != nil {
		api := objectPlanAPIError(err)
		if errors.Is(err, sqlservice.ErrObjectAlreadyExists) {
			writeJSON(w, 409, map[string]any{"error": api, "conflict_ref": commands.Ref})
			return
		}
		fail(w, 409, api.Code, api.Message)
		return
	}
	source := make([]string, len(commands.Steps))
	for i, step := range commands.Steps {
		source[i] = step.SQL
	}
	sqlText := strings.Join(source, "\n;\n")
	opts, err := liveOptions(ctx, c, s.grant.Dialect())
	if err != nil {
		api := safeDatabaseError(err)
		fail(w, 409, api.Code, api.Message)
		return
	}
	analysis, err := sqlservice.AnalyzeObjectCommands(s.grant.Dialect(), commands, opts)
	if err != nil || len(analysis.Statements) != len(commands.Steps) {
		message := "The native parser could not verify the object command boundaries"
		if err != nil {
			message = err.Error()
		}
		fail(w, 400, "invalid_object_plan", message)
		return
	}
	p := &objectPlan{id: newID("obj_"), consoleID: c.id, req: req, sql: sqlText, commands: commands, revision: tx.Revision, expires: time.Now().Add(15 * time.Minute)}
	if tx.Manual {
		for _, st := range analysis.Statements {
			risk, _ := objectTransactionRisk(&execution{objectPlan: p}, s.grant.Dialect(), st)
			p.requiresImplicit = p.requiresImplicit || risk
		}
	}
	encoded, _ := json.Marshal(req)
	p.bytes = len(encoded) + len(p.sql)*2
	if commands.Recovery != nil {
		captured, _ := json.Marshal(commands.Recovery)
		p.bytes += len(captured)
	}
	for _, step := range commands.Steps {
		p.bytes += len(step.ParserSQL) + len(step.PolicySQL) + len(step.BoundSQL)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.grant.Check() != nil || c.closed.Load() {
		fail(w, 409, "session_unavailable", "Session is unavailable")
		return
	}
	if s.objectPlans == nil {
		s.objectPlans = map[string]*objectPlan{}
	}
	if !s.reserveObjectPreviewLocked(p.bytes) {
		fail(w, 429, "object_plan_limit", "Too many object previews in this session")
		return
	}
	s.objectPlans[p.id] = p
	writeJSON(w, 200, map[string]any{
		"plan_id": p.id, "action": req.Action, "ref": commands.Ref, "execution_user": commands.ExecutionUser,
		"steps": commands.Steps, "sql": p.sql, "recovery": commands.Recovery, "expires_at": p.expires,
		"transaction_revision": tx.Revision, "manual": tx.Manual, "requires_implicit_commit": p.requiresImplicit,
	})
}

// Active commands must survive until dispatch finishes. Completed recovery
// records share a bounded cache and yield their space to new work. Execution
// IDs and their immutable plan identities live in the existing request map.
func (s *session) reserveObjectPreviewLocked(incoming int) bool {
	now := time.Now()
	var bytes, active int
	var completed []*objectPlan
	for id, p := range s.objectPlans {
		if !p.used && now.After(p.expires) {
			delete(s.objectPlans, id)
			continue
		}
		if !p.recoveryUntil.IsZero() {
			if now.After(p.recoveryUntil) {
				p.commands.Recovery = nil
				delete(s.objectPlans, id)
				continue
			}
			completed = append(completed, p)
		} else {
			active++
		}
		bytes += p.bytes
	}
	if active >= 32 || incoming > 32<<20 {
		return false
	}
	slices.SortFunc(completed, func(a, b *objectPlan) int { return a.recoveryUntil.Compare(b.recoveryUntil) })
	for i, p := range completed {
		if len(completed)-i <= 32 && bytes+incoming <= 32<<20 {
			break
		}
		p.commands.Recovery = nil
		delete(s.objectPlans, p.id)
		bytes -= p.bytes
	}
	return bytes+incoming <= 32<<20
}

func (v *Server) applyObjectPlan(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, false)
	if s == nil {
		return
	}
	c := v.findConsole(w, r, s)
	if c == nil {
		return
	}
	var req struct {
		TransactionRevision   *uint64 `json:"transaction_revision,omitempty"`
		RequestID             string  `json:"request_id"`
		SubmitReview          bool    `json:"submit_review"`
		ConfirmWarnings       bool    `json:"confirm_warnings"`
		ConfirmImplicitCommit bool    `json:"confirm_implicit_commit"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.RequestID == "" {
		req.RequestID = r.Header.Get("Idempotency-Key")
	}
	s.mu.Lock()
	p := s.objectPlans[r.PathValue("pid")]
	old := c.requests[req.RequestID]
	if old != nil {
		if old.objectPlan == nil || old.objectPlan.id != r.PathValue("pid") {
			s.mu.Unlock()
			fail(w, 409, "idempotency_conflict", "Request ID already belongs to another operation")
			return
		}
		p = old.objectPlan
	}
	var commandSQL string
	if p != nil {
		commandSQL = p.sql
	}
	s.mu.Unlock()
	if p == nil || p.consoleID != c.id {
		fail(w, 404, "not_found", "Object preview not found")
		return
	}
	if req.TransactionRevision != nil && *req.TransactionRevision != p.revision {
		fail(w, 409, "transaction_changed", "The transaction revision differs from the object preview")
		return
	}
	if old == nil {
		if tx := c.transaction.Load(); tx == nil || tx.Revision != p.revision {
			fail(w, 409, "transaction_changed", "The console transaction changed; preview the object again")
			return
		}
		if p.requiresImplicit && !req.ConfirmImplicitCommit {
			fail(w, 409, "implicit_commit_confirmation_required", "Review and confirm the pending transaction before applying the plan")
			return
		}
	}
	v.submitExecution(w, r, executionIntent{objectPlan: p}, &executionRequest{RequestID: req.RequestID, SQL: commandSQL, SubmitReview: req.SubmitReview, ConfirmWarnings: req.ConfirmWarnings, ConfirmImplicitCommit: req.ConfirmImplicitCommit, TransactionRevision: p.revision})
}

func (v *Server) recheckObject(ctx context.Context, s *session, c *console, p *objectPlan) error {
	if time.Now().After(p.expires) || c.transaction.Load().Revision != p.revision {
		return sqlservice.ErrObjectChanged
	}
	if !c.manual {
		state, err := editTransactionState(ctx, c, s.grant.Dialect())
		if err != nil {
			return err
		}
		if state != editTxIdle {
			return sqlservice.ErrObjectChanged
		}
	}
	return v.checkObjectFingerprint(ctx, s, c, p)
}
func (v *Server) checkObjectFingerprint(ctx context.Context, s *session, c *console, p *objectPlan) error {
	commands, err := v.objectCommands(ctx, s, c, p.req)
	if err != nil {
		return err
	}
	if commands.Fingerprint != p.commands.Fingerprint {
		return sqlservice.ErrObjectChanged
	}
	return s.grant.Check()
}

// Cache only the exact ACL decisions covered by a whole-plan approval. A
// different decision at a later send point requires a fresh preview/approval.
func (v *Server) objectPolicies(ctx context.Context, s *session, commands []string) (map[string]string, error) {
	decisions := make(map[string]string, len(commands))
	for _, command := range commands {
		err := v.provider.Authorize(ctx, s.grant, command)
		if err == nil {
			decisions[command] = ""
			continue
		}
		var pe *core.PolicyError
		if !errors.As(err, &pe) || (pe.Code != "command_review_required" && pe.Code != "command_warning_confirmation_required") {
			return nil, err
		}
		decisions[command] = fmt.Sprintf("%s/%s/%s/%d", pe.Code, pe.ACLID, pe.GroupID, pe.RiskLevel)
	}
	return decisions, s.grant.Check()
}

func objectTransactionRisk(e *execution, d dialect.ID, s sqlservice.Statement) (bool, bool) {
	// CALL in an explicit PostgreSQL transaction cannot commit it. Keep the
	// selected console's transaction unless the operator chose a top-level CALL.
	if e.objectPlan != nil && d.UsesMySQLSyntax() && e.objectPlan.req.Action != "readDefinition" {
		return true, false
	}
	if e.objectPlan != nil && e.objectPlan.req.Action == "invoke" && d == dialect.PostgreSQL && s.Kind == "call" && !e.confirmImplicitCommit {
		return false, false
	}
	if e.objectPlan != nil && d == dialect.PostgreSQL && (s.Kind == "lock" || s.Kind == "fetch" && e.objectPlan.req.Action == "fetchCursor") {
		return false, false
	}
	return sqlservice.ManualTransactionRisk(d, s)
}
