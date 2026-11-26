package engine

import (
	"context"
	"errors"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/chen/core"
)

// The SQL deadline includes queueing and database work, but an operator waiting
// for a reviewer gets the separate, bounded approval deadline from the provider.
func (e *execution) startDeadline(s *session, d time.Duration) {
	if d <= 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.deadlineAt = time.Now().Add(d)
	e.deadlineTimer = time.AfterFunc(d, func() { e.requestCancellation(s, "timeout") })
}

func (e *execution) pauseDeadline() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.deadlinePaused && e.deadlineTimer != nil && e.deadlineTimer.Stop() {
		e.remaining = time.Until(e.deadlineAt)
		e.deadlinePaused = true
	}
}

func (e *execution) resumeDeadline() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.deadlinePaused && e.deadlineTimer != nil {
		e.deadlinePaused = false
		e.deadlineAt = time.Now().Add(e.remaining)
		e.deadlineTimer.Reset(e.remaining)
	}
}

func (e *execution) stopDeadline() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopDeadlineLocked()
}

func (e *execution) stopDeadlineLocked() {
	if e.deadlineTimer != nil {
		e.deadlineTimer.Stop()
		e.deadlineTimer = nil
	}
}

// After this point the driver owns COMMIT or ROLLBACK. Its bounded reply
// determines the reported outcome even when the query budget expires.
func (e *execution) beginTransactionBoundary() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancelRequested.Load() {
		return context.Canceled
	}
	if err := e.ctx.Err(); err != nil {
		return err
	}
	e.stopDeadlineLocked()
	e.transactionBoundary = true
	return nil
}

func (v *Server) authorizeExecution(s *session, e *execution, commands []string, audit *core.AuditRecord) error {
	var required, warningRequired *core.PolicyError
	reviews := map[string]string{}
	// Preflight the current statement and its generated commands, or the whole
	// grid edit plan. Ordinary scripts call this separately for each statement.
	for _, command := range commands {
		if err := v.provider.Authorize(e.ctx, s.grant, command); err != nil {
			var policy *core.PolicyError
			if !errors.As(err, &policy) {
				return err
			}
			if policy.Code == "command_warning_confirmation_required" {
				if warningRequired == nil {
					warningRequired = policy
				}
				continue
			}
			if policy.Code != "command_review_required" {
				return err
			}
			if required == nil {
				required = policy
			}
			reviews[command] = policy.ACLID + "\x00" + policy.GroupID
		}
	}
	// A grid preview confirms the changes, not an ACL warning. Bind that
	// decision to the current execution and present its masked batch once.
	confirmWarnings, submitReview := e.edit == nil && e.confirmWarnings, e.submitReview
	if warningRequired != nil && !confirmWarnings {
		if err := v.waitExecutionConfirmation(s, e, "warning", audit.SQL); err != nil {
			var policy *core.PolicyError
			if errors.As(err, &policy) {
				policy.ACLID, policy.GroupID = warningRequired.ACLID, warningRequired.GroupID
			}
			return err
		}
		confirmWarnings = true
	}
	if required != nil && !submitReview {
		if err := v.waitExecutionConfirmation(s, e, "review", audit.SQL); err != nil {
			if errors.Is(err, context.Canceled) {
				err = &core.PolicyError{Code: "command_review_canceled"}
			}
			var policy *core.PolicyError
			if errors.As(err, &policy) {
				policy.ACLID, policy.GroupID = required.ACLID, required.GroupID
				policy.RiskLevel = model.ReviewCancel
			}
			return err
		}
		submitReview = true
	}
	defer e.resumeDeadline()
	e.policyWarnings = make(map[string]core.AuthorizationDecision)
	approved := map[string]bool{}
	seen := map[string]bool{}
	var affectedRows *int64
	if e.edit != nil {
		count := int64(len(e.edit.commands))
		affectedRows = &count
	}
	for _, command := range commands {
		key := reviews[command]
		if seen[command] || key != "" && approved[key] {
			continue
		}
		seen[command] = true
		decision, err := v.provider.AuthorizeCommand(e.ctx, s.grant, core.AuthorizationRequest{
			SQL: command, BatchSQL: audit.SQL, AffectedRows: affectedRows, SubmitReview: submitReview, ConfirmWarnings: confirmWarnings,
			OnReview: func(review core.ReviewStatus) {
				review.SQL = command
				if e.edit != nil {
					// Grid identities can contain masked primary-key values. The
					// user's review panel receives the same masked SQL as preview.
					for _, change := range e.edit.commands {
						if change.policySQL == command {
							review.SQL = change.previewSQL
							break
						}
					}
				}
				if review.State == "pending" {
					e.pauseDeadline()
				}
				v.update(s, e, func(view *ExecutionView) {
					view.Review = &review
					if review.State == "pending" && !e.cancelRequested.Load() {
						view.Status = "waiting_review"
					}
				})
			},
		})
		if err != nil {
			return err
		}
		if key != "" {
			approved[key] = true
		}
		if decision.RiskLevel == model.WarningLevel && decision.ACLID != "" {
			e.policyWarnings[command] = decision
		}
		if decision.RiskLevel != model.WarningLevel && decision.RiskLevel >= audit.RiskLevel && decision.ACLID != "" {
			audit.RiskLevel, audit.ACLID, audit.GroupID = decision.RiskLevel, decision.ACLID, decision.GroupID
		}
	}
	return s.grant.Check()
}
