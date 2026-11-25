package engine

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jumpserver/koko/pkg/chen/core"
)

type ExecutionConfirmation struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	SQL       string    `json:"sql"`
	ExpiresAt time.Time `json:"expires_at"`
}

// A confirmation resumes its accepted execution; it must never submit the
// already completed prefix again or approve a later statement by accident.
func (v *Server) confirmExecution(w http.ResponseWriter, r *http.Request) {
	s := v.auth(w, r, false)
	if s == nil {
		return
	}
	var request struct {
		ID       string `json:"confirmation_id"`
		Accepted *bool  `json:"accepted"`
	}
	if !decode(w, r, &request) {
		return
	}
	if request.ID == "" || request.Accepted == nil {
		fail(w, 400, "invalid_confirmation", "A confirmation identity and decision are required")
		return
	}
	s.mu.Lock()
	e := s.executions[r.PathValue("eid")]
	s.mu.Unlock()
	if e == nil {
		fail(w, 404, "not_found", "Execution not found")
		return
	}
	e.mu.Lock()
	if previous, ok := e.confirmationDecisions[request.ID]; ok {
		e.mu.Unlock()
		if previous != *request.Accepted {
			fail(w, 409, "confirmation_conflict", "This confirmation already has a different decision")
			return
		}
		writeJSON(w, 202, e.snapshot())
		return
	}
	pending := e.view.Confirmation
	if pending == nil || pending.ID != request.ID || !time.Now().Before(pending.ExpiresAt) || e.ctx.Err() != nil || e.cancelRequested.Load() {
		e.mu.Unlock()
		fail(w, 409, "confirmation_expired", "This confirmation is no longer pending")
		return
	}
	if e.confirmationDecisions == nil {
		e.confirmationDecisions = map[string]bool{}
	}
	e.confirmationDecisions[request.ID] = *request.Accepted
	e.confirmationResponse <- *request.Accepted
	e.mu.Unlock()
	writeJSON(w, 202, e.snapshot())
}

func (v *Server) waitExecutionConfirmation(s *session, e *execution, kind, sql string) error {
	// Java's review decision and notify-warning dialogs both expire after 300s.
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Minute)
	defer cancel()
	e.pauseDeadline()
	defer e.resumeDeadline()
	expires, _ := ctx.Deadline()
	response := make(chan bool, 1)
	v.update(s, e, func(view *ExecutionView) {
		e.confirmationResponse = response
		view.Confirmation = &ExecutionConfirmation{ID: newID("confirm_"), Kind: kind, SQL: sql, ExpiresAt: expires}
		view.Status = "waiting_review"
		view.Review = nil
	})
	defer v.update(s, e, func(view *ExecutionView) {
		view.Confirmation = nil
		e.confirmationResponse = nil
	})
	select {
	case accepted := <-response:
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.grant.Check(); err != nil {
			return err
		}
		if e.cancelRequested.Load() {
			return context.Canceled
		}
		if accepted {
			return nil
		}
		e.cancel()
		return &core.PolicyError{Code: "command_" + kind + "_canceled"}
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			e.cancel()
			return &core.PolicyError{Code: "command_" + kind + "_timed_out"}
		}
		return ctx.Err()
	}
}
