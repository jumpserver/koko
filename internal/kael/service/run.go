package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/event"
	"github.com/jumpserver/koko/internal/kael/model"
	"github.com/jumpserver/koko/internal/kael/policy"
	"github.com/jumpserver/koko/internal/kael/ports"
	agentruntime "github.com/jumpserver/koko/internal/kael/runtime"
	"go.uber.org/zap"
)

type CreateRunRequest struct {
	ConversationID    string `json:"conversation_id"`
	InputMessageID    string `json:"input_message_id"`
	PanelSessionID    string `json:"panel_session_id"`
	ExecutionMode     string `json:"execution_mode"`
	CapabilityMode    string `json:"capability_mode"`
	IdempotencyKey    string `json:"idempotency_key"`
	RegeneratedFromID string `json:"regenerated_from_id,omitempty"`
}

func (s *Service) CreateRun(ctx context.Context, principal domain.Principal, request CreateRunRequest) (*domain.Run, error) {
	request.ConversationID, request.InputMessageID, request.PanelSessionID = strings.TrimSpace(request.ConversationID), strings.TrimSpace(request.InputMessageID), strings.TrimSpace(request.PanelSessionID)
	if request.ExecutionMode == "" {
		request.ExecutionMode = "foreground"
	}
	if request.CapabilityMode == "" {
		request.CapabilityMode = "disabled"
	}
	if request.ExecutionMode != "foreground" && request.ExecutionMode != "background" || request.CapabilityMode != "disabled" && request.CapabilityMode != "panel" && request.CapabilityMode != "service" || request.ExecutionMode == "background" && request.CapabilityMode == "panel" {
		return nil, serviceError(Invalid, "invalid_run_mode", "execution and capability mode combination is invalid", nil)
	}
	if request.ExecutionMode == "background" {
		return nil, serviceError(Unavailable, "background_requires_durable_store", "background runs are unavailable until distributed recovery is configured", nil)
	}
	if request.ConversationID == "" || request.InputMessageID == "" || request.PanelSessionID == "" {
		return nil, serviceError(Invalid, "invalid_run", "conversation, message, and panel session are required", nil)
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	if key == "" {
		key = uuid.NewString()
	}
	digest, _ := domain.HashValue(request)
	now := time.Now().UTC()
	var run *domain.Run
	duplicate := false
	var notify []string
	s.credentialsMu.Lock()
	defer s.credentialsMu.Unlock()
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		existing, existingErr := tx.RunByIdempotency(key, principal)
		if existingErr == nil {
			if existing.IdempotencyDigest != digest {
				return serviceError(Conflict, "idempotency_conflict", "run idempotency key was used with another payload", nil)
			}
			run, duplicate = existing, true
			return nil
		}
		if !errors.Is(existingErr, ports.ErrNotFound) {
			return existingErr
		}
		conversation, err := tx.Conversation(request.ConversationID, principal, true)
		if err != nil {
			return err
		}
		if conversation.Status != "active" {
			return serviceError(Conflict, "conversation_inactive", "conversation is not active", nil)
		}
		message, err := tx.Message(request.InputMessageID, principal, false)
		if err != nil {
			return err
		}
		if message.ConversationID != conversation.ID || message.Role != "user" {
			return serviceError(Invalid, "message_mismatch", "run input message is invalid", nil)
		}
		panel, err := tx.Panel(request.PanelSessionID, principal, true)
		if err != nil {
			return err
		}
		if err = validatePanelBinding(panel, conversation.ID, now); err != nil {
			return serviceError(Conflict, "panel_unavailable", err.Error(), nil)
		}
		profile, ok := policy.Get(conversation.Profile)
		if !ok || panel.Profile != profile.ID || !policy.Authorized(profile, principal) {
			return serviceError(Forbidden, "profile_forbidden", "runtime profile is not available", nil)
		}
		if conversation.Kind == "capability" && request.CapabilityMode != "panel" {
			return serviceError(Invalid, "capability_required", "capability conversation cannot silently downgrade to model-only mode", nil)
		}
		if request.CapabilityMode == "service" && (conversation.Kind != "general" || s.capability == nil) {
			return serviceError(Invalid, "service_capability_unavailable", "service capability is not available for this conversation", nil)
		}
		if profile.CoreAPIEnabled && request.CapabilityMode != "service" {
			return serviceError(Invalid, "service_capability_required", "this assistant requires the configured service capability provider", nil)
		}
		active, err := tx.ActiveRunCount(conversation.ID)
		if err != nil {
			return err
		}
		if active >= domain.MaxQueuedRuns {
			return serviceError(Conflict, "run_queue_full", "conversation run queue is full", nil)
		}
		var contextVersion uint64
		var contextDigest string
		if panel.ContextVersion > 0 {
			contextVersion, contextDigest = panel.ContextVersion, panel.ContextDigest
		}
		registrations := []domain.RunRegistrationSnapshot{}
		if request.CapabilityMode == "panel" {
			current, err := tx.ListRegistrations(panel.ID, panel.RegistryRevision)
			if err != nil {
				return err
			}
			for _, registration := range current {
				if registration.State != "active" || registration.LeaseExpiresAt.Before(now) {
					continue
				}
				registrations = append(registrations, snapshotRegistration(registration))
			}
			if len(registrations) == 0 {
				return serviceError(Conflict, "capability_required", "no active capability registration is available", nil)
			}
		} else if request.CapabilityMode == "service" {
			current, capabilityErr := s.capability.Registrations(ctx, principal, profile.ID)
			if capabilityErr != nil {
				return serviceError(Unavailable, "service_capability_unavailable", "service capability registry is unavailable", capabilityErr)
			}
			for _, registration := range current {
				registrations = append(registrations, snapshotRegistration(registration))
			}
			if len(registrations) == 0 {
				return serviceError(Unavailable, "service_capability_unavailable", "service capability registry is empty", nil)
			}
		}
		registrationJSON, _ := json.Marshal(registrations)
		modelPolicyJSON, _ := json.Marshal(map[string]any{"provider": s.engine.Info().Provider, "model": s.engine.Info().Model, "engine": "codex", "max_tool_calls": domain.MaxToolCalls})
		approvalScope := "panel"
		if request.CapabilityMode == "service" {
			approvalScope = "service"
		}
		approvalPolicyJSON, _ := json.Marshal(map[string]any{"version": "1", "scope": approvalScope, "risk_confirmation": []string{"write", "dangerous"}})
		permissions := append([]string(nil), principal.Permissions...)
		sort.Strings(permissions)
		permissions = slices.Compact(permissions)
		run = &domain.Run{ID: uuid.NewString(), ConversationID: conversation.ID, InputMessageID: message.ID, RegeneratedFromID: request.RegeneratedFromID, PanelSessionID: panel.ID, SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, AuthorizationSuperuser: principal.IsSuperuser, AuthorizationOrgAdmin: principal.IsOrgAdmin, AuthorizationPermissions: permissions, Profile: profile.ID, ProfileVersion: profile.Version, ExecutionMode: request.ExecutionMode, CapabilityMode: request.CapabilityMode, ContextVersion: contextVersion, ContextDigest: contextDigest, RegistryRevision: panel.RegistryRevision, RegistrationSnapshot: registrationJSON, ModelPolicy: modelPolicyJSON, ApprovalPolicy: approvalPolicyJSON, State: "queued", IdempotencyKey: key, IdempotencyDigest: digest, CreatedAt: now, UpdatedAt: now}
		if err = tx.CreateRun(run); err != nil {
			return err
		}
		_, deliveries, err := event.Project(tx, "run.queued", "run", run.ID, "run", event.References{ConversationID: conversation.ID, RunID: run.ID, MessageID: message.ID}, runEventPayload(run), []domain.PanelSession{*panel}, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return s.audit(tx, principal, "run.queued", conversation.ID, panel.ID, run.ID, map[string]any{"execution_mode": run.ExecutionMode, "capability_mode": run.CapabilityMode})
	})
	if err != nil {
		var serviceErr *Error
		if errors.As(err, &serviceErr) {
			return nil, err
		}
		return nil, translateStore(err)
	}
	if !duplicate {
		s.bindRunCredentials(ctx, run)
		s.bus.Notify(notify...)
		s.signalWorker()
	}
	return run, nil
}

func snapshotRegistration(value domain.Registration) domain.RunRegistrationSnapshot {
	return domain.RunRegistrationSnapshot{ID: value.ID, Name: value.Name, Description: value.Description, BindingKind: value.BindingKind, ExecutionBindingID: value.ExecutionBindingID, DefinitionVersion: value.DefinitionVersion, DefinitionDigest: value.DefinitionDigest, Risk: value.Risk, RequiresConfirmation: value.RequiresConfirmation, Annotations: value.Annotations(), InputSchema: append(json.RawMessage(nil), value.InputSchema...), OutputSchema: append(json.RawMessage(nil), value.OutputSchema...)}
}
func runEventPayload(run *domain.Run) map[string]any {
	payload := map[string]any{"state": run.State, "execution_mode": run.ExecutionMode, "capability_mode": run.CapabilityMode, "partial": run.Partial, "finish_reason": run.FinishReason, "error_code": run.ErrorCode, "reason": run.ErrorDetail}
	if run.CancelReason != "" {
		payload["cancel_reason"] = run.CancelReason
	}
	if len(run.Failure) > 0 {
		payload["failure"] = json.RawMessage(run.Failure)
	}
	return payload
}
func (s *Service) Run(ctx context.Context, principal domain.Principal, id string) (*domain.Run, error) {
	var value *domain.Run
	err := s.store.View(ctx, func(tx ports.Tx) error { var err error; value, err = tx.Run(id, principal, false); return err })
	if err != nil {
		return nil, translateStore(err)
	}
	return value, nil
}
func (s *Service) ListRuns(ctx context.Context, principal domain.Principal, conversationID string, offset, limit int) (domain.Page[domain.Run], error) {
	offset, limit = pageBounds(offset, limit)
	var values []domain.Run
	var count int64
	err := s.store.View(ctx, func(tx ports.Tx) error {
		if _, err := tx.Conversation(conversationID, principal, false); err != nil {
			return err
		}
		var err error
		values, count, err = tx.ListRuns(conversationID, principal, offset, limit)
		return err
	})
	if err != nil {
		return domain.Page[domain.Run]{}, translateStore(err)
	}
	return domain.Page[domain.Run]{Results: values, Count: count}, nil
}

func (s *Service) CancelRun(ctx context.Context, principal domain.Principal, id, reason string) (*domain.Run, error) {
	now := time.Now().UTC()
	var run *domain.Run
	var notify []string
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		var err error
		run, err = tx.Run(id, principal, true)
		if err != nil {
			return err
		}
		if run.Terminal() || run.State == "cancelling" {
			return nil
		}
		run.Failure, err = describeFailure(tx, run, "cancelled", "", "continue")
		if err != nil {
			return err
		}
		panel, err := tx.Panel(run.PanelSessionID, principal, true)
		if err != nil {
			return err
		}
		run.CancelReason, run.UpdatedAt = bounded(reason, 512), now
		if run.CancelReason == "" {
			run.CancelReason = "user"
		}
		eventType := "run.cancelled"
		if run.State == "queued" || run.State == "interrupted" {
			run.State, run.FinishedAt = "cancelled", &now
		} else {
			run.State, eventType = "cancelling", "tool.cancel"
		}
		if err = tx.SaveRun(run); err != nil {
			return err
		}
		if approval, approvalErr := tx.PendingApprovalForRun(run.ID, true); approvalErr == nil {
			approval.State, approval.Reason, approval.ResolvedAt, approval.UpdatedAt = "cancelled", "run cancelled", &now, now
			if err = tx.SaveApproval(approval); err != nil {
				return err
			}
			if err = event.ResolveApproval(tx, approval, now); err != nil {
				return err
			}
		}
		payload := runEventPayload(run)
		references := event.References{ConversationID: run.ConversationID, RunID: run.ID, MessageID: run.InputMessageID}
		if call, callErr := tx.ActiveToolCall(run.ID, true); callErr == nil {
			payload["tool_call_id"] = call.ID
			references.ToolCallID = call.ID
			call.State, call.FinishedAt, call.UpdatedAt = "cancelled", &now, now
			if err = tx.SaveToolCall(call); err != nil {
				return err
			}
		}
		_, deliveries, err := event.Project(tx, eventType, "run", run.ID, "run", references, payload, []domain.PanelSession{*panel}, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return s.audit(tx, principal, "run.cancel", run.ConversationID, run.PanelSessionID, run.ID, map[string]any{"reason": run.CancelReason})
	})
	if err != nil {
		return nil, translateOrService(err)
	}
	s.activeMu.Lock()
	cancel := s.active[id]
	s.activeMu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.forgetRunCredentials(id)
	s.bus.Notify(notify...)
	return run, nil
}

func (s *Service) ResumeRun(ctx context.Context, principal domain.Principal, id string) (*domain.Run, error) {
	now := time.Now().UTC()
	var run *domain.Run
	var notify []string
	s.credentialsMu.Lock()
	defer s.credentialsMu.Unlock()
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		var err error
		run, err = tx.Run(id, principal, true)
		if err != nil {
			return err
		}
		if run.State != "interrupted" {
			return serviceError(Conflict, "run_not_resumable", "run is not interrupted", nil)
		}
		if run.ModelRequestCount > 0 {
			return serviceError(Conflict, "execution_rebind_required", "run started model execution before interruption and cannot be replayed safely", nil)
		}
		toolCallCount, err := tx.RunToolCallCount(run.ID)
		if err != nil {
			return err
		}
		if toolCallCount > 0 {
			return serviceError(Conflict, "execution_rebind_required", "run used a capability before interruption and cannot be replayed safely", nil)
		}
		panel, err := tx.Panel(run.PanelSessionID, principal, true)
		if err != nil {
			return err
		}
		if run.CapabilityMode == "panel" {
			if err = validatePanelBinding(panel, run.ConversationID, now); err != nil {
				return serviceError(Conflict, "capability_unavailable", "original panel capability is unavailable", err)
			}
		}
		run.State, run.ErrorCode, run.ErrorDetail, run.ClaimOwner, run.ClaimExpiresAt, run.UpdatedAt = "queued", "", "", "", nil, now
		run.FinishedAt = nil
		run.Failure = nil
		if err = tx.SaveRun(run); err != nil {
			return err
		}
		_, deliveries, err := event.Project(tx, "run.queued", "run", run.ID, "run", event.References{ConversationID: run.ConversationID, RunID: run.ID, MessageID: run.InputMessageID}, runEventPayload(run), []domain.PanelSession{*panel}, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return nil
	})
	if err != nil {
		return nil, translateOrService(err)
	}
	s.bindRunCredentials(ctx, run)
	s.bus.Notify(notify...)
	s.signalWorker()
	return run, nil
}

func translateOrService(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var value *Error
	if errors.As(err, &value) {
		return err
	}
	return translateStore(err)
}

func (s *Service) signalWorker() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *Service) worker() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-s.wake:
		case <-ticker.C:
		}
		for {
			run, err := s.claim()
			if errors.Is(err, ports.ErrNotFound) {
				break
			}
			if err != nil {
				s.logger.Error("claim run", zap.Error(err))
				break
			}
			s.execute(run)
		}
	}
}

func (s *Service) claim() (*domain.Run, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var run *domain.Run
	var notify []string
	now := time.Now().UTC()
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		var err error
		run, err = tx.ClaimRun(s.instanceID, now, now.Add(s.runTimeout+time.Minute))
		if err != nil {
			return err
		}
		panel, err := tx.PanelInternal(run.PanelSessionID, true)
		if err != nil {
			return err
		}
		_, deliveries, err := event.Project(tx, "run.started", "run", run.ID, "run", event.References{ConversationID: run.ConversationID, RunID: run.ID, MessageID: run.InputMessageID}, runEventPayload(run), []domain.PanelSession{*panel}, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return nil
	})
	if err == nil {
		s.bus.Notify(notify...)
	}
	return run, err
}

func (s *Service) execute(run *domain.Run) {
	defer s.forgetRunCredentials(run.ID)
	ctx, cancel := context.WithTimeout(context.Background(), s.runTimeout)
	s.activeMu.Lock()
	s.active[run.ID] = cancel
	s.activeMu.Unlock()
	defer func() { cancel(); s.activeMu.Lock(); delete(s.active, run.ID); s.activeMu.Unlock() }()
	go s.watchCancellation(ctx, run.ID, cancel)
	input, outputMessage, err := s.runtimeInput(ctx, run)
	if err != nil {
		s.finishFailed(run, outputMessage, err)
		return
	}
	deltas := newMessageDeltaCoalescer(ctx, s, run, outputMessage)
	completion, err := s.engine.Execute(ctx, input, agentruntime.Callbacks{ModelStarted: func(sequence int, info model.Info) error { return s.modelStarted(ctx, run, sequence, info) }, ModelCompleted: func(sequence int, result model.Result, duration time.Duration) error {
		if flushErr := deltas.Flush(ctx, false); flushErr != nil {
			return flushErr
		}
		return s.modelCompleted(ctx, run, sequence, result, duration)
	}, MessageDelta: func(text string) error { return deltas.Add(ctx, text) }, CallTool: func(toolCtx context.Context, registration domain.Registration, arguments json.RawMessage, modelDurationMS int64) (agentruntime.ToolObservation, error) {
		if flushErr := deltas.Flush(toolCtx, false); flushErr != nil {
			return agentruntime.ToolObservation{}, flushErr
		}
		return s.callTool(toolCtx, run, registration, arguments, modelDurationMS)
	}})
	flushCtx, cancelFlush := context.WithTimeout(context.Background(), 10*time.Second)
	flushErr := deltas.Close(flushCtx, true)
	cancelFlush()
	if flushErr != nil {
		if err == nil {
			err = flushErr
		} else {
			s.logger.Error("flush message delta", zap.String("run_id", run.ID), zap.Error(flushErr))
		}
	}
	if err != nil {
		// Provider requests can time out while the overall run is still live.
		// Only the run context's deadline means the whole task timed out.
		if ctx.Err() != nil {
			s.finishCancelled(run, outputMessage, ctx.Err())
		} else if errors.Is(err, context.Canceled) || isApprovalExpired(err) {
			s.finishCancelled(run, outputMessage, err)
		} else {
			s.finishFailed(run, outputMessage, err)
		}
		return
	}
	s.finishCompleted(run, outputMessage, completion)
}

func (s *Service) watchCancellation(ctx context.Context, runID string, cancel context.CancelFunc) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var state string
			_ = s.store.View(ctx, func(tx ports.Tx) error {
				run, err := tx.RunInternal(runID, false)
				if err == nil {
					state = run.State
				}
				return err
			})
			if state == "cancelling" || state == "cancelled" {
				cancel()
				return
			}
		}
	}
}

func registrationFromSnapshot(panelID string, revision uint64, value domain.RunRegistrationSnapshot) domain.Registration {
	annotations, _ := json.Marshal(value.Annotations)
	return domain.Registration{ID: value.ID, PanelSessionID: panelID, BindingKind: value.BindingKind, ExecutionBindingID: value.ExecutionBindingID, Name: value.Name, Description: value.Description, DefinitionVersion: value.DefinitionVersion, DefinitionDigest: value.DefinitionDigest, Risk: value.Risk, RequiresConfirmation: value.RequiresConfirmation, AnnotationsJSON: annotations, RegistryRevision: revision, InputSchema: value.InputSchema, OutputSchema: value.OutputSchema, State: "active"}
}

func (s *Service) runtimeInput(ctx context.Context, run *domain.Run) (agentruntime.Input, *domain.Message, error) {
	var messages []domain.Message
	var snapshot *domain.ContextSnapshot
	var output *domain.Message
	var profile policy.Profile
	artifacts := map[string]domain.Artifact{}
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		value, ok := policy.Get(run.Profile)
		if !ok || value.Version != run.ProfileVersion {
			return fmt.Errorf("runtime profile snapshot is unavailable")
		}
		profile = value
		for offset := 0; ; offset += domain.MaxPageSize {
			page, count, err := tx.ListMessages(run.ConversationID, domain.Principal{SubjectID: run.SubjectID, OrganizationID: run.OrganizationID}, offset, domain.MaxPageSize)
			if err != nil {
				return err
			}
			messages = append(messages, page...)
			if int64(len(messages)) >= count {
				break
			}
		}
		if run.ContextVersion > 0 {
			var err error
			snapshot, err = tx.Context(run.PanelSessionID, run.ContextVersion)
			if err != nil {
				return err
			}
			if snapshot.Digest != run.ContextDigest {
				return fmt.Errorf("run context digest mismatch")
			}
		}
		now := time.Now().UTC()
		output = &domain.Message{ID: uuid.NewString(), ConversationID: run.ConversationID, SubjectID: run.SubjectID, OrganizationID: run.OrganizationID, Role: "assistant", Status: "streaming", Parts: json.RawMessage(`[]`), ResultCards: json.RawMessage(`[]`), IdempotencyKey: "run-output:" + run.ID, IdempotencyDigest: run.ID, RegeneratedFromID: run.RegeneratedFromID, CreatedAt: now, UpdatedAt: now}
		if err := tx.CreateMessage(output); err != nil {
			existing, lookupErr := tx.MessageByIdempotency(output.IdempotencyKey, domain.Principal{SubjectID: run.SubjectID, OrganizationID: run.OrganizationID})
			if lookupErr != nil {
				return err
			}
			output = existing
			output.Failure = nil
			output.Status, output.Content, output.Parts, output.ErrorCode, output.ErrorDetail, output.InputTokens, output.OutputTokens, output.UpdatedAt = "streaming", "", json.RawMessage(`[]`), "", "", 0, 0, now
			if err = tx.SaveMessage(output); err != nil {
				return err
			}
		}
		filtered := messages[:0]
		for _, message := range messages {
			if message.ID != output.ID {
				filtered = append(filtered, message)
			}
		}
		messages = filtered
		owner := domain.Principal{SubjectID: run.SubjectID, OrganizationID: run.OrganizationID}
		for _, message := range messages {
			var parts []domain.MessagePart
			if json.Unmarshal(message.Parts, &parts) != nil {
				continue
			}
			for _, part := range parts {
				if part.Type != "artifact" || part.ArtifactID == "" {
					continue
				}
				artifact, artifactErr := tx.Artifact(part.ArtifactID, owner, false)
				if artifactErr != nil {
					return artifactErr
				}
				artifacts[artifact.ID] = *artifact
			}
		}
		run.OutputMessageID, run.UpdatedAt = output.ID, now
		return tx.SaveRun(run)
	})
	if err != nil {
		return agentruntime.Input{}, output, err
	}
	var snapshots []domain.RunRegistrationSnapshot
	if json.Unmarshal(run.RegistrationSnapshot, &snapshots) != nil {
		return agentruntime.Input{}, output, fmt.Errorf("run registration snapshot is invalid")
	}
	registrations := make([]domain.Registration, 0, len(snapshots))
	for _, value := range snapshots {
		registrations = append(registrations, registrationFromSnapshot(run.PanelSessionID, run.RegistryRevision, value))
	}
	modelArtifacts := make(map[string][]model.ContentPart, len(artifacts))
	for id, artifact := range artifacts {
		description := fmt.Sprintf("Attached file %q (%s, %d bytes).", artifact.Name, artifact.MediaType, artifact.Size)
		parts := []model.ContentPart{{Type: "text", Text: description}}
		if artifact.Kind == "image" {
			if artifact.Size > int64(domain.MaxContextBytes)*3/4 {
				return agentruntime.Input{}, output, fmt.Errorf("image artifact exceeds the model context limit")
			}
			content, readErr := os.ReadFile(filepath.Join(s.artifactDir, artifact.StorageKey))
			if readErr != nil {
				return agentruntime.Input{}, output, fmt.Errorf("read image artifact: %w", readErr)
			}
			parts = append(parts, model.ContentPart{Type: "image", MediaType: artifact.MediaType, Data: base64.StdEncoding.EncodeToString(content)})
		} else if artifact.ExtractedText != "" {
			parts[0].Text += "\n" + artifact.ExtractedText
		}
		modelArtifacts[id] = parts
	}
	return agentruntime.Input{Run: *run, ProfileInstructions: profile.Instructions, Context: snapshot, Messages: messages, Registrations: registrations, Artifacts: modelArtifacts, OutputMessageID: output.ID}, output, nil
}

func isApprovalExpired(err error) bool {
	var value *Error
	return errors.As(err, &value) && value.Code == "approval_expired"
}
