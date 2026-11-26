package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/event"
	"github.com/jumpserver/koko/internal/kael/policy"
	"github.com/jumpserver/koko/internal/kael/ports"
	agentruntime "github.com/jumpserver/koko/internal/kael/runtime"
)

type CreatePanelRequest struct {
	ConversationID   string `json:"conversation_id"`
	Surface          string `json:"surface"`
	Profile          string `json:"profile"`
	ClientInstanceID string `json:"client_instance_id"`
	ApprovalMode     string `json:"approval_mode"`
}
type PanelResponse struct {
	domain.PanelSession
	ResumeToken string `json:"resume_token,omitempty"`
}
type ResumePanelRequest struct {
	ResumeToken      string `json:"resume_token"`
	ClientInstanceID string `json:"client_instance_id"`
}
type UpdateContextRequest struct {
	BaseVersion uint64          `json:"base_version"`
	Domain      string          `json:"domain"`
	Surface     string          `json:"surface"`
	Sensitivity string          `json:"sensitivity"`
	Data        json.RawMessage `json:"data"`
}
type RegistrationDefinition struct {
	ClientKey            string          `json:"client_key"`
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	InputSchema          json.RawMessage `json:"input_schema"`
	OutputSchema         json.RawMessage `json:"output_schema"`
	DefinitionVersion    string          `json:"definition_version"`
	Risk                 string          `json:"risk"`
	RequiresConfirmation bool            `json:"requires_confirmation"`
	Annotations          map[string]any  `json:"annotations"`
	Meta                 map[string]any  `json:"_meta"`
}
type ReplaceRegistrationsRequest struct {
	BaseRegistryRevision uint64                   `json:"base_registry_revision"`
	Registrations        []RegistrationDefinition `json:"registrations"`
}
type RegistryResponse struct {
	RegistryRevision uint64                `json:"registry_revision"`
	Registrations    []domain.Registration `json:"registrations"`
}

func (s *Service) CreatePanel(ctx context.Context, principal domain.Principal, request CreatePanelRequest) (*PanelResponse, error) {
	request.ConversationID, request.ClientInstanceID = strings.TrimSpace(request.ConversationID), strings.TrimSpace(request.ClientInstanceID)
	if request.ConversationID == "" || request.ClientInstanceID == "" || len(request.ClientInstanceID) > domain.MaxIdentifierBytes {
		return nil, serviceError(Invalid, "invalid_panel", "conversation and client instance are required", nil)
	}
	token, tokenHash, err := randomToken()
	if err != nil {
		return nil, serviceError(Internal, "token_failed", "panel session could not be created", err)
	}
	now := time.Now().UTC()
	var panel *domain.PanelSession
	var notify []string
	err = s.store.Transaction(ctx, func(tx ports.Tx) error {
		conversation, err := tx.Conversation(request.ConversationID, principal, false)
		if err != nil {
			return err
		}
		profileID := request.Profile
		if profileID == "" {
			profileID = conversation.Profile
		}
		profile, ok := policy.Get(profileID)
		if !ok || profile.ID != conversation.Profile || !policy.Authorized(profile, principal) {
			return serviceError(Forbidden, "profile_forbidden", "panel profile is not available", nil)
		}
		approvalMode := strings.TrimSpace(request.ApprovalMode)
		if approvalMode == "" {
			approvalMode = "auto"
		}
		if approvalMode != "always" && approvalMode != "auto" && approvalMode != "never" {
			return serviceError(Invalid, "invalid_approval_mode", "approval mode is invalid", nil)
		}
		panel = &domain.PanelSession{ID: uuid.NewString(), ConversationID: conversation.ID, SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, Surface: bounded(request.Surface, 128), Profile: profile.ID, ApprovalMode: approvalMode, State: "active", ResumeTokenHash: tokenHash, ClientInstanceID: request.ClientInstanceID, ConnectionOwner: s.instanceID, LeaseExpiresAt: now.Add(s.panelLease), LastHeartbeatAt: now, CreatedAt: now, UpdatedAt: now}
		if panel.Surface == "" {
			panel.Surface = conversation.Surface
			if panel.Surface == "" {
				panel.Surface = "general.chat"
			}
		}
		if err = tx.CreatePanel(panel); err != nil {
			return err
		}
		_, deliveries, err := event.Project(tx, "panel.ready", "panel_session", panel.ID, "panel", event.References{ConversationID: conversation.ID}, panel, []domain.PanelSession{*panel}, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return s.audit(tx, principal, "panel.created", conversation.ID, panel.ID, "", nil)
	})
	if err != nil {
		var serviceErr *Error
		if errors.As(err, &serviceErr) {
			return nil, err
		}
		return nil, translateStore(err)
	}
	s.bus.Notify(notify...)
	return &PanelResponse{PanelSession: *panel, ResumeToken: token}, nil
}

type UpdateApprovalModeRequest struct {
	Mode string `json:"mode"`
}

func (s *Service) UpdateApprovalMode(ctx context.Context, principal domain.Principal, id string, request UpdateApprovalModeRequest) (*domain.PanelSession, error) {
	request.Mode = strings.TrimSpace(request.Mode)
	if request.Mode != "always" && request.Mode != "auto" && request.Mode != "never" {
		return nil, serviceError(Invalid, "invalid_approval_mode", "approval mode is invalid", nil)
	}
	now := time.Now().UTC()
	var panel *domain.PanelSession
	var notify []string
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		var err error
		panel, err = tx.Panel(id, principal, true)
		if err != nil {
			return err
		}
		if panel.State != "active" || panel.LeaseExpiresAt.Before(now) {
			return serviceError(Conflict, "panel_expired", "panel session is not active", nil)
		}
		_, ok := policy.Get(panel.Profile)
		if !ok {
			return serviceError(Forbidden, "approval_mode_forbidden", "approval mode is not allowed by the runtime profile", nil)
		}
		if panel.ApprovalMode == request.Mode {
			return nil
		}
		active, err := tx.ActiveRunCount(panel.ConversationID)
		if err != nil {
			return err
		}
		if active > 0 {
			return serviceError(Conflict, "approval_mode_conflict", "approval mode cannot change while a run is active", nil)
		}
		previous := panel.ApprovalMode
		panel.ApprovalMode, panel.UpdatedAt = request.Mode, now
		if err = tx.SavePanel(panel); err != nil {
			return err
		}
		_, deliveries, err := event.Project(tx, "session.approval_mode_changed", "panel_session", panel.ID, "panel", event.References{ConversationID: panel.ConversationID}, map[string]any{"previous": previous, "current": request.Mode}, []domain.PanelSession{*panel}, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return s.audit(tx, principal, "panel.approval_mode_changed", panel.ConversationID, panel.ID, "", map[string]any{"previous": previous, "current": request.Mode})
	})
	if err != nil {
		return nil, translateOrService(err)
	}
	s.bus.Notify(notify...)
	return panel, nil
}

func (s *Service) HeartbeatPanel(ctx context.Context, principal domain.Principal, id string) (*domain.PanelSession, error) {
	var panel *domain.PanelSession
	now := time.Now().UTC()
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		var err error
		panel, err = tx.Panel(id, principal, true)
		if err != nil {
			return err
		}
		if panel.State != "active" && panel.State != "disconnected" || panel.ClosedAt != nil {
			return serviceError(Conflict, "panel_closed", "panel session is closed", nil)
		}
		panel.State, panel.ConnectionOwner, panel.LastHeartbeatAt, panel.LeaseExpiresAt, panel.UpdatedAt = "active", s.instanceID, now, now.Add(s.panelLease), now
		if err = tx.SavePanel(panel); err != nil {
			return err
		}
		registrations, err := tx.ListRegistrations(panel.ID, panel.RegistryRevision)
		if err != nil {
			return err
		}
		for index := range registrations {
			if registrations[index].State == "active" {
				registrations[index].LeaseExpiresAt = now.Add(s.registrationLease)
				registrations[index].UpdatedAt = now
				if err = tx.SaveRegistration(&registrations[index]); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		var serviceErr *Error
		if errors.As(err, &serviceErr) {
			return nil, err
		}
		return nil, translateStore(err)
	}
	return panel, nil
}

func (s *Service) ResumePanel(ctx context.Context, principal domain.Principal, id string, request ResumePanelRequest) (*PanelResponse, error) {
	if request.ResumeToken == "" || request.ClientInstanceID == "" {
		return nil, serviceError(Invalid, "resume_token_required", "resume token and client instance are required", nil)
	}
	newToken, newHash, err := randomToken()
	if err != nil {
		return nil, serviceError(Internal, "token_failed", "panel session could not be resumed", err)
	}
	now := time.Now().UTC()
	var panel *domain.PanelSession
	var notify []string
	err = s.store.Transaction(ctx, func(tx ports.Tx) error {
		var err error
		panel, err = tx.Panel(id, principal, true)
		if err != nil {
			return err
		}
		if panel.State == "closed" || panel.State == "expired" || panel.ClosedAt != nil || panel.LeaseExpiresAt.Before(now) {
			return serviceError(Conflict, "panel_expired", "panel session can no longer be resumed", nil)
		}
		if panel.ClientInstanceID != request.ClientInstanceID || panel.ResumeTokenHash != domain.HashBytes([]byte(request.ResumeToken)) {
			return serviceError(Forbidden, "resume_forbidden", "panel resume binding is invalid", nil)
		}
		panel.State, panel.ConnectionOwner, panel.ResumeTokenHash, panel.LastHeartbeatAt, panel.LeaseExpiresAt, panel.UpdatedAt = "active", s.instanceID, newHash, now, now.Add(s.panelLease), now
		if err = tx.SavePanel(panel); err != nil {
			return err
		}
		_, deliveries, err := event.Project(tx, "panel.resumed", "panel_session", panel.ID, "panel", event.References{ConversationID: panel.ConversationID}, panel, []domain.PanelSession{*panel}, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return nil
	})
	if err != nil {
		var serviceErr *Error
		if errors.As(err, &serviceErr) {
			return nil, err
		}
		return nil, translateStore(err)
	}
	s.bus.Notify(notify...)
	return &PanelResponse{PanelSession: *panel, ResumeToken: newToken}, nil
}

func (s *Service) ClosePanel(ctx context.Context, principal domain.Principal, id string) error {
	now := time.Now().UTC()
	var notify []string
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		panel, err := tx.Panel(id, principal, true)
		if err != nil {
			return err
		}
		if panel.State == "closed" {
			return nil
		}
		panel.State, panel.ClosedAt, panel.UpdatedAt, panel.ResumeTokenHash = "closed", &now, now, ""
		if err = tx.SavePanel(panel); err != nil {
			return err
		}
		registrations, err := tx.ListRegistrations(id, panel.RegistryRevision)
		if err != nil {
			return err
		}
		for index := range registrations {
			if registrations[index].State == "active" {
				registrations[index].State, registrations[index].UpdatedAt = "revoked", now
				if err = tx.SaveRegistration(&registrations[index]); err != nil {
					return err
				}
			}
		}
		_, deliveries, err := event.Project(tx, "panel.lease_expiring", "panel_session", panel.ID, "panel", event.References{ConversationID: panel.ConversationID}, map[string]any{"state": "closed"}, []domain.PanelSession{*panel}, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return s.audit(tx, principal, "panel.closed", panel.ConversationID, id, "", nil)
	})
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return nil
		}
		var serviceErr *Error
		if errors.As(err, &serviceErr) {
			return err
		}
		return translateStore(err)
	}
	s.bus.Notify(notify...)
	return nil
}

func (s *Service) UpdateContext(ctx context.Context, principal domain.Principal, id string, request UpdateContextRequest) (*domain.ContextSnapshot, error) {
	data, err := sanitizeJSON(request.Data, domain.MaxContextBytes)
	if err != nil {
		return nil, serviceError(Invalid, "invalid_context", "context is invalid or exceeds the configured limit", err)
	}
	digest, err := domain.HashValue(map[string]any{"domain": request.Domain, "surface": request.Surface, "data": json.RawMessage(data)})
	if err != nil {
		return nil, serviceError(Invalid, "invalid_context", "context could not be normalized", err)
	}
	now := time.Now().UTC()
	var snapshot *domain.ContextSnapshot
	var notify []string
	err = s.store.Transaction(ctx, func(tx ports.Tx) error {
		panel, err := tx.Panel(id, principal, true)
		if err != nil {
			return err
		}
		if panel.State != "active" || panel.LeaseExpiresAt.Before(now) {
			return serviceError(Conflict, "panel_expired", "panel session is not active", nil)
		}
		if panel.ContextVersion != request.BaseVersion {
			return serviceError(Conflict, "context_revision_conflict", "context base version is stale", nil)
		}
		version := panel.ContextVersion + 1
		snapshot = &domain.ContextSnapshot{ID: uuid.NewString(), PanelSessionID: id, Version: version, Digest: digest, Domain: bounded(request.Domain, 64), Surface: bounded(request.Surface, 128), Sensitivity: bounded(request.Sensitivity, 32), Data: data, CreatedAt: now}
		if snapshot.Domain == "" {
			snapshot.Domain = strings.TrimPrefix(panel.Profile, "platform.")
		}
		if snapshot.Surface == "" {
			snapshot.Surface = panel.Surface
		}
		if snapshot.Sensitivity == "" {
			snapshot.Sensitivity = "restricted"
		}
		if err = tx.CreateContext(snapshot); err != nil {
			return err
		}
		panel.ContextVersion, panel.ContextDigest, panel.UpdatedAt = version, digest, now
		if err = tx.SavePanel(panel); err != nil {
			return err
		}
		_, deliveries, err := event.Project(tx, "panel.ready", "context", snapshot.ID, "panel", event.References{ConversationID: panel.ConversationID}, map[string]any{"context_version": version, "context_digest": digest}, []domain.PanelSession{*panel}, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return nil
	})
	if err != nil {
		var serviceErr *Error
		if errors.As(err, &serviceErr) {
			return nil, err
		}
		return nil, translateStore(err)
	}
	s.bus.Notify(notify...)
	return snapshot, nil
}

func boolAnnotation(values map[string]any, snake, camel string) bool {
	for _, key := range []string{snake, camel} {
		if value, ok := values[key].(bool); ok {
			return value
		}
	}
	return false
}
func suppliedAnnotations(definition RegistrationDefinition) domain.ToolAnnotations {
	values := definition.Annotations
	if values == nil {
		values = map[string]any{}
	}
	final := false
	for _, key := range []string{"com.jumpserver/finalResult", "final_result"} {
		if value, ok := definition.Meta[key].(bool); ok && value {
			final = true
		}
	}
	annotations := domain.ToolAnnotations{ReadOnly: boolAnnotation(values, "read_only", "readOnlyHint"), Destructive: boolAnnotation(values, "destructive", "destructiveHint"), Idempotent: boolAnnotation(values, "idempotent", "idempotentHint"), OpenWorld: boolAnnotation(values, "open_world", "openWorldHint"), FinalResult: final}
	// Explicit confirmation and risk overrides take precedence over command inference.
	if !definition.RequiresConfirmation && (definition.Risk == "" || definition.Risk == "read") && !annotations.Destructive && !final {
		if value, _ := definition.Meta[policy.CommandPolicyMetaKey].(string); value == policy.ShellReadOnlyPolicy {
			annotations.CommandPolicy = value
		}
	}
	return annotations
}

func (s *Service) ReplaceRegistrations(ctx context.Context, principal domain.Principal, panelID string, request ReplaceRegistrationsRequest) (*RegistryResponse, error) {
	if len(request.Registrations) > domain.MaxTools {
		return nil, serviceError(Invalid, "too_many_registrations", "registration count exceeds the configured limit", nil)
	}
	now := time.Now().UTC()
	var result RegistryResponse
	var notify []string
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		panel, err := tx.Panel(panelID, principal, true)
		if err != nil {
			return err
		}
		if panel.State != "active" || panel.LeaseExpiresAt.Before(now) {
			return serviceError(Conflict, "panel_expired", "panel session is not active", nil)
		}
		if panel.RegistryRevision != request.BaseRegistryRevision {
			return serviceError(Conflict, "registry_revision_conflict", "registry base revision is stale", nil)
		}
		profile, ok := policy.Get(panel.Profile)
		if !ok {
			return serviceError(Forbidden, "profile_forbidden", "panel profile is invalid", nil)
		}
		revision := panel.RegistryRevision + 1
		registrations := make([]domain.Registration, 0, len(request.Registrations))
		seenKey, seenName := map[string]struct{}{}, map[string]struct{}{}
		for _, definition := range request.Registrations {
			definition.ClientKey, definition.Name = strings.TrimSpace(definition.ClientKey), strings.TrimSpace(definition.Name)
			if definition.ClientKey == "" {
				definition.ClientKey = definition.Name
			}
			if definition.ClientKey == "" || definition.Name == "" || len(definition.ClientKey) > domain.MaxIdentifierBytes || len(definition.Name) > domain.MaxIdentifierBytes {
				return serviceError(Invalid, "invalid_registration", "registration name or client key is invalid", nil)
			}
			if _, exists := seenKey[definition.ClientKey]; exists {
				return serviceError(Invalid, "duplicate_registration", "registration client key is duplicated", nil)
			}
			if _, exists := seenName[definition.Name]; exists {
				return serviceError(Invalid, "duplicate_registration", "registration tool name is duplicated", nil)
			}
			seenKey[definition.ClientKey], seenName[definition.Name] = struct{}{}, struct{}{}
			if err = agentruntime.ValidateSchema(definition.InputSchema); err != nil {
				return serviceError(Invalid, "invalid_input_schema", "registration input schema is invalid", err)
			}
			if len(definition.OutputSchema) > 0 {
				if err = agentruntime.ValidateSchema(definition.OutputSchema); err != nil {
					return serviceError(Invalid, "invalid_output_schema", "registration output schema is invalid", err)
				}
			}
			annotations := suppliedAnnotations(definition)
			risk, confirmation := policy.RegistrationPolicy(annotations)
			if definition.Risk != "" && !policy.RiskAllowed("dangerous", definition.Risk) {
				return serviceError(Invalid, "invalid_registration_risk", "registration risk must be read, write, or dangerous", nil)
			}
			if definition.Risk == "dangerous" {
				risk = "dangerous"
			} else if definition.Risk == "write" && risk == "read" {
				risk = "write"
			}
			if !policy.RiskAllowed(profile.MaxRisk, risk) {
				return serviceError(Forbidden, "registration_forbidden", "registration risk exceeds the runtime profile", nil)
			}
			confirmation = confirmation || definition.RequiresConfirmation || risk != "read"
			annotationJSON, _ := json.Marshal(annotations)
			version := strings.TrimSpace(definition.DefinitionVersion)
			if version == "" {
				version = "1"
			}
			digest, err := domain.HashValue(map[string]any{"name": definition.Name, "description": definition.Description, "input_schema": json.RawMessage(definition.InputSchema), "output_schema": json.RawMessage(definition.OutputSchema), "version": version, "risk": risk, "confirmation": confirmation, "annotations": annotations})
			if err != nil {
				return err
			}
			registrations = append(registrations, domain.Registration{ID: uuid.NewString(), PanelSessionID: panelID, BindingKind: "panel", ExecutionBindingID: panelID, ClientKey: definition.ClientKey, Name: definition.Name, Description: bounded(definition.Description, 4096), InputSchema: append(json.RawMessage(nil), definition.InputSchema...), OutputSchema: append(json.RawMessage(nil), definition.OutputSchema...), DefinitionVersion: version, DefinitionDigest: digest, Namespace: policy.Namespace(profile), Risk: risk, RequiresConfirmation: confirmation, AnnotationsJSON: annotationJSON, RegistryRevision: revision, State: "active", LeaseExpiresAt: now.Add(s.registrationLease), CreatedAt: now, UpdatedAt: now})
		}
		if err = tx.SupersedeRegistrations(panelID, panel.RegistryRevision, now); err != nil {
			return err
		}
		if err = tx.CreateRegistrations(registrations); err != nil {
			return err
		}
		panel.RegistryRevision, panel.UpdatedAt = revision, now
		if err = tx.SavePanel(panel); err != nil {
			return err
		}
		result = RegistryResponse{RegistryRevision: revision, Registrations: registrations}
		_, deliveries, err := event.Project(tx, "registration.updated", "panel_session", panelID, "panel", event.References{ConversationID: panel.ConversationID}, result, []domain.PanelSession{*panel}, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return s.audit(tx, principal, "registration.replaced", panel.ConversationID, panelID, "", map[string]any{"revision": revision, "count": len(registrations)})
	})
	if err != nil {
		var serviceErr *Error
		if errors.As(err, &serviceErr) {
			return nil, err
		}
		return nil, translateStore(err)
	}
	s.bus.Notify(notify...)
	return &result, nil
}

func (s *Service) RevokeRegistration(ctx context.Context, principal domain.Principal, id string) error {
	now := time.Now().UTC()
	var notify []string
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		registration, err := tx.Registration(id, true)
		if err != nil {
			return err
		}
		panel, err := tx.Panel(registration.PanelSessionID, principal, true)
		if err != nil {
			return err
		}
		if registration.State == "revoked" {
			return nil
		}
		registration.State, registration.UpdatedAt = "revoked", now
		if err = tx.SaveRegistration(registration); err != nil {
			return err
		}
		_, deliveries, err := event.Project(tx, "registration.revoked", "registration", id, "panel", event.References{ConversationID: panel.ConversationID}, registration, []domain.PanelSession{*panel}, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return nil
	})
	if err != nil {
		return translateStore(err)
	}
	s.bus.Notify(notify...)
	return nil
}

func (s *Service) Deliveries(ctx context.Context, principal domain.Principal, panelID string, after uint64, limit int) ([]domain.PanelDelivery, bool, error) {
	_, limit = pageBounds(0, limit)
	var values []domain.PanelDelivery
	expired := false
	err := s.store.View(ctx, func(tx ports.Tx) error {
		panel, err := tx.Panel(panelID, principal, false)
		if err != nil {
			return err
		}
		if panel.State == "closed" || panel.State == "expired" {
			return serviceError(Conflict, "panel_closed", "panel session is closed", nil)
		}
		values, expired, err = tx.ListDeliveries(panelID, after, limit, time.Now().UTC().Add(-s.eventRetention))
		return err
	})
	if err != nil {
		var serviceErr *Error
		if errors.As(err, &serviceErr) {
			return nil, false, err
		}
		return nil, false, translateStore(err)
	}
	return values, expired, nil
}

func (s *Service) PanelEvents(panelID string) (<-chan struct{}, func()) {
	return s.bus.Subscribe(panelID)
}
func validatePanelBinding(panel *domain.PanelSession, conversationID string, now time.Time) error {
	if panel.ConversationID != conversationID {
		return fmt.Errorf("panel conversation binding is invalid")
	}
	if panel.State != "active" || panel.LeaseExpiresAt.Before(now) {
		return fmt.Errorf("panel session is not active")
	}
	return nil
}
