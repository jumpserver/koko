package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/event"
	"github.com/jumpserver/koko/internal/kael/ports"
	"github.com/jumpserver/koko/internal/kael/store"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func approvalFixture(t *testing.T) (*Service, domain.Principal, *domain.Run, *domain.ToolCall, *domain.Approval) {
	t.Helper()
	ctx := context.Background()
	s := &Service{store: store.NewMemory(), bus: event.NewBus(), logger: zap.NewNop(), panelLease: time.Minute, registrationLease: time.Minute}
	principal := domain.Principal{SubjectID: "user", OrganizationID: "org"}
	if err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		return tx.CreateConversation(&domain.Conversation{ID: "conversation", SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, Kind: "capability", Profile: "workspace", Status: "active"})
	}); err != nil {
		t.Fatal(err)
	}
	panel, err := s.CreatePanel(ctx, principal, CreatePanelRequest{ConversationID: "conversation", ClientInstanceID: "client", ApprovalMode: "always"})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := s.ReplaceRegistrations(ctx, principal, panel.ID, ReplaceRegistrationsRequest{Registrations: []RegistrationDefinition{{Name: "inspect_resource", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: map[string]any{"readOnlyHint": true}}}})
	if err != nil {
		t.Fatal(err)
	}
	run := &domain.Run{ID: "run", ConversationID: panel.ConversationID, PanelSessionID: panel.ID, SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, RegistryRevision: registry.RegistryRevision, State: "running"}
	if err = s.store.Transaction(ctx, func(tx ports.Tx) error { return tx.CreateRun(run) }); err != nil {
		t.Fatal(err)
	}
	call, approval, err := s.prepareToolCall(ctx, run, registry.Registrations[0], json.RawMessage(`{}`), 0)
	if err != nil || approval == nil {
		t.Fatalf("prepare approval: %v", err)
	}
	return s, principal, run, call, approval
}

func TestApprovalLifetimeIndependentOfHeartbeat(t *testing.T) {
	s, principal, run, call, approval := approvalFixture(t)
	if approval.ExpiresAt.Sub(approval.CreatedAt) != 10*time.Minute {
		t.Fatal("approval lifetime follows the panel lease")
	}
	if _, err := s.HeartbeatPanel(context.Background(), principal, run.PanelSessionID); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Approval(context.Background(), principal, approval.ID)
	if err != nil || !stored.ExpiresAt.Equal(approval.ExpiresAt) {
		t.Fatalf("heartbeat changed approval lifetime: %v", err)
	}
	if _, _, err = s.DecideApproval(context.Background(), principal, approval.ID, ApprovalDecisionRequest{Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	if err = s.dispatchToolCall(context.Background(), run, call); err != nil {
		t.Fatal(err)
	}
}

func TestApprovalExpiryPublishesOnceAndStopsWithoutFailure(t *testing.T) {
	for _, path := range []string{"wait", "decision", "dispatch", "maintenance"} {
		t.Run(path, func(t *testing.T) {
			ctx := context.Background()
			s, principal, run, call, approval := approvalFixture(t)
			logs, observed := observer.New(zap.InfoLevel)
			s.logger = zap.New(logs)
			if err := s.store.Transaction(ctx, func(tx ports.Tx) error {
				approval.ExpiresAt = time.Now().Add(-time.Second)
				if path == "dispatch" {
					now := time.Now()
					approval.State, approval.ResolvedAt = "approved", &now
				}
				return tx.SaveApproval(approval)
			}); err != nil {
				t.Fatal(err)
			}
			var err error
			switch path {
			case "decision":
				_, _, err = s.DecideApproval(ctx, principal, approval.ID, ApprovalDecisionRequest{Decision: "approve"})
			case "dispatch":
				err = s.dispatchToolCall(ctx, run, call)
			case "maintenance":
				if err = s.store.Transaction(ctx, func(tx ports.Tx) error { return tx.Maintain(time.Now(), time.Now().Add(-time.Hour)) }); err != nil {
					t.Fatal(err)
				}
				fallthrough
			default:
				err = s.waitApproval(ctx, run, call, approval)
			}
			if !isApprovalExpired(err) {
				t.Fatalf("expected approval expiry, got %v", err)
			}
			s.finishCancelled(run, nil, err)
			if _, _, err = s.DecideApproval(ctx, principal, approval.ID, ApprovalDecisionRequest{Decision: "approve"}); !isApprovalExpired(err) {
				t.Fatalf("late decision: %v", err)
			}
			stored, err := s.Run(ctx, principal, run.ID)
			if err != nil || stored.State != "cancelled" || stored.ErrorCode != "approval_expired" {
				t.Fatalf("unexpected run: %+v %v", stored, err)
			}
			if observed.FilterLevelExact(zap.ErrorLevel).Len() != 0 {
				t.Fatal("expiry logged as an error")
			}
			if err = s.store.View(ctx, func(tx ports.Tx) error {
				deliveries, _, err := tx.ListDeliveries(run.PanelSessionID, 0, 100, time.Time{})
				resolved := 0
				for _, item := range deliveries {
					if item.Type == "tool.call" || item.Type == "run.failed" {
						t.Fatalf("unexpected event %s", item.Type)
					}
					if item.Type == "approval.resolved" {
						resolved++
					}
				}
				if resolved != 1 {
					t.Fatalf("got %d approval resolutions", resolved)
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRunTimeoutResolvesPendingApproval(t *testing.T) {
	s, principal, run, _, approval := approvalFixture(t)
	s.finishCancelled(run, nil, context.DeadlineExceeded)
	stored, err := s.Approval(context.Background(), principal, approval.ID)
	if err != nil || stored.State != "cancelled" {
		t.Fatalf("approval left pending: %+v %v", stored, err)
	}
}
