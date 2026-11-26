package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/event"
	"github.com/jumpserver/koko/internal/kael/ports"
	"github.com/jumpserver/koko/internal/kael/store"
)

func TestDynamicPanelRegistrations(t *testing.T) {
	ctx := context.Background()
	s := &Service{store: store.NewMemory(), bus: event.NewBus(), panelLease: time.Minute, registrationLease: time.Minute}
	principal := domain.Principal{SubjectID: "user-1", OrganizationID: "org-1"}
	if err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		return tx.CreateConversation(&domain.Conversation{ID: "conversation-1", SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, Kind: "capability", Profile: "workspace", Status: "active"})
	}); err != nil {
		t.Fatal(err)
	}
	panel, err := s.CreatePanel(ctx, principal, CreatePanelRequest{ConversationID: "conversation-1", ClientInstanceID: "resource-1", ApprovalMode: "never"})
	if err != nil {
		t.Fatal(err)
	}
	request := ReplaceRegistrationsRequest{Registrations: []RegistrationDefinition{{Name: "inspect_custom_resource", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`), Annotations: map[string]any{"readOnlyHint": true, "idempotentHint": true}, Meta: map[string]any{"com.jumpserver/finalResult": true}}}}
	for _, other := range []domain.Principal{{SubjectID: "user-2", OrganizationID: principal.OrganizationID}, {SubjectID: principal.SubjectID, OrganizationID: "org-2"}} {
		if _, err = s.ReplaceRegistrations(ctx, other, panel.ID, request); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("another identity could register: %v", err)
		}
	}
	registry, err := s.ReplaceRegistrations(ctx, principal, panel.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	registration := registry.Registrations[0]
	if registration.Risk != "read" || registration.RequiresConfirmation || !registration.Annotations().FinalResult || !registration.Annotations().Idempotent || registration.Namespace != "luna.workspace" {
		t.Fatalf("client policy was not preserved: %+v", registration)
	}
	if _, err = s.ReplaceRegistrations(ctx, principal, panel.ID, request); err == nil {
		t.Fatal("stale registry replacement succeeded")
	}
	run := &domain.Run{ID: "run-1", ConversationID: panel.ConversationID, PanelSessionID: panel.ID, SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, RegistryRevision: registry.RegistryRevision, State: "running"}
	if err = s.store.Transaction(ctx, func(tx ports.Tx) error { return tx.CreateRun(run) }); err != nil {
		t.Fatal(err)
	}
	call, approval, err := s.prepareToolCall(ctx, run, registration, json.RawMessage(`{}`), 0)
	if err != nil || approval != nil || call == nil || call.RequiresConfirmation {
		t.Fatalf("unexpected approval: call=%+v approval=%+v err=%v", call, approval, err)
	}
	if err = s.RevokeRegistration(ctx, principal, registration.ID); err != nil {
		t.Fatal(err)
	}
	err = s.dispatchToolCall(ctx, run, call)
	var serviceErr *Error
	if !errors.As(err, &serviceErr) || serviceErr.Code != "capability_expired" {
		t.Fatalf("revoked tool did not fail its execution binding check: %v", err)
	}
}

func TestRegistrationRiskCeiling(t *testing.T) {
	ctx := context.Background()
	s := &Service{store: store.NewMemory(), bus: event.NewBus(), panelLease: time.Minute, registrationLease: time.Minute}
	principal := domain.Principal{SubjectID: "user-1", OrganizationID: "org-1"}
	if err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		return tx.CreateConversation(&domain.Conversation{ID: "conversation-1", SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, Kind: "capability", Profile: "script", Status: "active"})
	}); err != nil {
		t.Fatal(err)
	}
	panel, err := s.CreatePanel(ctx, principal, CreatePanelRequest{ConversationID: "conversation-1", ClientInstanceID: "editor-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ risk, code string }{{"dangerous", "registration_forbidden"}, {"invalid", "invalid_registration_risk"}, {"read", ""}} {
		registry, err := s.ReplaceRegistrations(ctx, principal, panel.ID, ReplaceRegistrationsRequest{Registrations: []RegistrationDefinition{{Name: "custom_editor_tool", InputSchema: json.RawMessage(`{"type":"object"}`), Risk: tc.risk, RequiresConfirmation: true, Annotations: map[string]any{"read_only": true}, Meta: map[string]any{"final_result": true}}}})
		if tc.code != "" {
			var serviceErr *Error
			if !errors.As(err, &serviceErr) || serviceErr.Code != tc.code {
				t.Fatalf("risk %s: %v", tc.risk, err)
			}
		} else if err != nil || !registry.Registrations[0].RequiresConfirmation || !registry.Registrations[0].Annotations().FinalResult {
			t.Fatalf("custom editor registration failed: %v", err)
		}
	}
}
