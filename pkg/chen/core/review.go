package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/session"
)

func (p *CoreProvider) AuthorizeCommand(ctx context.Context, g *Grant, request AuthorizationRequest) (AuthorizationDecision, error) {
	decision := AuthorizationDecision{}
	err := p.Authorize(ctx, g, request.SQL)
	if g == nil {
		return decision, err
	}
	matched := matchPolicy(g, request.SQL)
	if matched != nil {
		decision.ACLID, decision.GroupID = matched.aclID, matched.groupID
		switch matched.action {
		case model.ActionWarning, model.ActionNotifyAndWarn:
			decision.RiskLevel = int32(model.WarningLevel)
		case model.ActionReject, model.ActionReview:
			var policy *PolicyError
			if errors.As(err, &policy) {
				decision.RiskLevel = policy.RiskLevel
			}
		}
	}
	var policy *PolicyError
	if errors.As(err, &policy) && policy.Code == "command_warning_confirmation_required" {
		decision.NotifyWarning = true
		if request.ConfirmWarnings {
			return decision, nil
		}
		return decision, err
	}
	if !errors.As(err, &policy) || policy.Code != "command_review_required" {
		return decision, err
	}
	batch := request.BatchSQL
	if batch == "" {
		batch = request.SQL
	}
	if !request.SubmitReview {
		return decision, err
	}
	command := batch
	if batch != request.SQL {
		command = fmt.Sprintf("Batch SQL:\n%s\n\nReview-triggering SQL:\n%s", batch, request.SQL)
	}
	affected := "unknown"
	if request.AffectedRows != nil {
		affected = fmt.Sprint(*request.AffectedRows)
	}
	return p.waitCommandReview(ctx, g, decision, "Affected rows: "+affected+"\n"+command, request.OnReview)
}

func (d AuthorizationDecision) policyError(code string) error {
	return &PolicyError{Code: code, RiskLevel: d.RiskLevel, ACLID: d.ACLID, GroupID: d.GroupID}
}

func (p *CoreProvider) waitCommandReview(parent context.Context, g *Grant, decision AuthorizationDecision, batch string, callback func(ReviewStatus)) (result AuthorizationDecision, resultErr error) {
	ctx, cancel := context.WithTimeout(parent, p.options.ReviewTimeout)
	defer cancel()
	expiresAt, _ := ctx.Deadline()
	status := ReviewStatus{State: "pending", ExpiresAt: expiresAt}
	notify := func() {
		if callback != nil {
			copy := status
			copy.Reviewers = append([]string(nil), status.Reviewers...)
			callback(copy)
		}
	}
	finish := func(state, code string, risk int32) (AuthorizationDecision, error) {
		status.State, status.ErrorCode = state, code
		decision.RiskLevel = int32(risk)
		notify()
		if code == "" {
			return decision, nil
		}
		return decision, decision.policyError(code)
	}
	contextFailure := func(err error) (AuthorizationDecision, error) {
		if errors.Is(err, context.DeadlineExceeded) {
			return finish("timed_out", "command_review_timed_out", model.ReviewCancel)
		}
		return finish("canceled", "command_review_canceled", model.ReviewCancel)
	}
	info, err := p.createReviewTicket(ctx, g.ID, decision.ACLID, batch)
	if err != nil {
		if contextErr := reviewContextError(ctx, time.Now()); contextErr != nil {
			return contextFailure(contextErr)
		}
		return finish("failed", "command_review_failed", model.ReviewReject)
	}
	terminal := false
	defer func() {
		if !terminal {
			if err := p.enqueueTicketCancel(info.CloseReq); err != nil {
				resultErr = errors.Join(resultErr, err)
			}
		}
	}()
	status.TicketURL = safeTicketURL(info.TicketDetailUrl)
	status.Reviewers = append([]string(nil), info.Reviewers...)
	notify()
	first := true
	var grantErr error
	response, err := session.PollCommandReview(func() (model.TicketState, error) {
		if !first && !waitContext(ctx, p.options.ReviewPollInterval) {
			return model.TicketState{}, reviewContextError(ctx, time.Now())
		}
		first = false
		if err := reviewContextError(ctx, time.Now()); err != nil {
			return model.TicketState{}, err
		}
		if grantErr = g.Check(); grantErr != nil {
			return model.TicketState{}, grantErr
		}
		response, err := callCore(ctx, p, func() (model.TicketState, error) { return p.client.CheckConfirmStatusByRequestInfo(info.CheckReq) }, nil)
		if contextErr := reviewContextError(ctx, time.Now()); contextErr != nil {
			return response, contextErr
		}
		if err == nil {
			status.Processor = response.Processor
		}
		return response, err
	}, nil)
	if err != nil {
		if contextErr := reviewContextError(ctx, time.Now()); contextErr != nil {
			return contextFailure(contextErr)
		}
		if grantErr != nil {
			return finish("canceled", grantErr.Error(), model.ReviewCancel)
		}
		return finish("failed", "command_review_failed", model.ReviewReject)
	}
	terminal = true
	switch response.State {
	case model.TicketApproved:
		// An approval arriving concurrently with lock/revocation/expiry is
		// not permission to execute. The engine checks again before sending SQL.
		if err := g.Check(); err != nil {
			return finish("canceled", err.Error(), model.ReviewCancel)
		}
		if err := reviewContextError(ctx, time.Now()); err != nil {
			return contextFailure(err)
		}
		return finish("approved", "", model.ReviewAccept)
	case model.TicketRejected:
		return finish("rejected", "command_review_rejected", model.ReviewReject)
	default: // The poller only returns approved, rejected or closed without an error.
		return finish("canceled", "command_review_canceled", model.ReviewCancel)
	}
}

// A child RPC can observe the shared deadline before the parent's timer callback
// updates Err. Check the actual review deadline as well; an earlier RPC timeout
// alone must not be mistaken for expiry of the whole review.
func reviewContextError(ctx context.Context, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !now.Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

func (p *CoreProvider) createReviewTicket(ctx context.Context, sessionID, aclID, command string) (*model.CommandTicketInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A create can succeed after cancellation. Always inspect its returned close
	// descriptor before releasing ownership, so a late ticket is canceled.
	info, err := callCore(ctx, p, func() (model.CommandTicketInfo, error) {
		return session.SubmitCommandReview(p.client, sessionID, aclID, command, "")
	}, func(late model.CommandTicketInfo) {
		if validTicketRequest(late.CloseReq, false) {
			if err := p.enqueueTicketCancel(late.CloseReq); err != nil {
				p.report("late_review_cleanup_persistence_failed")
			}
		}
	})
	if err != nil || ctx.Err() != nil || !validTicketRequest(info.CheckReq, true) || !validTicketRequest(info.CloseReq, false) {
		if validTicketRequest(info.CloseReq, false) {
			if cleanupErr := p.enqueueTicketCancel(info.CloseReq); cleanupErr != nil {
				return nil, cleanupErr
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("command_review_create_failed")
	}
	return &info, nil
}

func validTicketRequest(info model.ReqInfo, check bool) bool {
	u, err := url.ParseRequestURI(info.URL)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/api/") {
		return false
	}
	if check {
		return strings.EqualFold(info.Method, "GET")
	}
	return strings.EqualFold(info.Method, "DELETE") || strings.EqualFold(info.Method, "PUT")
}

func safeTicketURL(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.User != nil || value == "" {
		return ""
	}
	if u.Scheme == "" && u.Host == "" && strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") {
		return value
	}
	if (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
		return value
	}
	return ""
}

func (p *CoreProvider) enqueueTicketCancel(request model.ReqInfo) error {
	if err := p.spool.put(envelope{TicketCancel: &request}); err != nil {
		return errors.Join(ErrAuditDisk, err)
	}
	p.signal()
	return nil
}
