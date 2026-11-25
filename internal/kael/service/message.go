package service

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/event"
	"github.com/jumpserver/koko/internal/kael/policy"
	"github.com/jumpserver/koko/internal/kael/ports"
)

type CreateMessageRequest struct {
	ID             string               `json:"id"`
	Role           string               `json:"role"`
	Parts          []domain.MessagePart `json:"parts"`
	Content        string               `json:"content"`
	ArtifactIDs    []string             `json:"artifact_ids"`
	IdempotencyKey string               `json:"idempotency_key"`
}

func normalizeMessageRequest(request CreateMessageRequest) (string, json.RawMessage, string, error) {
	role := strings.TrimSpace(request.Role)
	if role == "" {
		role = "user"
	}
	if role != "user" {
		return "", nil, "", serviceError(Invalid, "invalid_message_role", "only user messages can be created", nil)
	}
	parts := append([]domain.MessagePart(nil), request.Parts...)
	if len(parts) == 0 && strings.TrimSpace(request.Content) != "" {
		parts = []domain.MessagePart{{Type: "text", Text: request.Content}}
	}
	if len(parts) == 0 && len(request.ArtifactIDs) == 0 {
		return "", nil, "", serviceError(Invalid, "message_empty", "message must contain text or an artifact", nil)
	}
	content := ""
	for _, part := range parts {
		if part.Type != "text" && part.Type != "artifact" && part.Type != "data" {
			return "", nil, "", serviceError(Invalid, "invalid_message_part", "message part type is invalid", nil)
		}
		if part.Type == "text" {
			if !utf8.ValidString(part.Text) {
				return "", nil, "", serviceError(Invalid, "invalid_message", "message text is invalid", nil)
			}
			content += part.Text
		}
		if part.Type == "artifact" && part.ArtifactID == "" {
			return "", nil, "", serviceError(Invalid, "invalid_artifact", "artifact reference is invalid", nil)
		}
	}
	for _, artifactID := range request.ArtifactIDs {
		parts = append(parts, domain.MessagePart{Type: "artifact", ArtifactID: artifactID})
	}
	if len(content) > domain.MaxMessageBytes {
		return "", nil, "", serviceError(Invalid, "message_too_large", "message text exceeds the configured limit", nil)
	}
	raw, err := json.Marshal(parts)
	if err != nil || len(raw) > domain.MaxMessageBytes*2 {
		return "", nil, "", serviceError(Invalid, "invalid_message", "message parts are invalid", err)
	}
	digest, err := domain.HashValue(map[string]any{"role": role, "parts": parts})
	if err != nil {
		return "", nil, "", err
	}
	return content, raw, digest, nil
}

func (s *Service) CreateMessage(ctx context.Context, principal domain.Principal, conversationID string, request CreateMessageRequest) (*domain.Message, bool, error) {
	content, parts, digest, err := normalizeMessageRequest(request)
	if err != nil {
		return nil, false, err
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	if key == "" {
		key = strings.TrimSpace(request.ID)
	}
	if key == "" {
		key = uuid.NewString()
	}
	if len(key) > domain.MaxIdentifierBytes {
		return nil, false, serviceError(Invalid, "invalid_idempotency_key", "message idempotency key is invalid", nil)
	}
	messageID := strings.TrimSpace(request.ID)
	if messageID == "" {
		messageID = uuid.NewString()
	}
	var message *domain.Message
	duplicate := false
	var notify []string
	now := time.Now().UTC()
	err = s.store.Transaction(ctx, func(tx ports.Tx) error {
		conversation, err := tx.Conversation(conversationID, principal, true)
		if err != nil {
			return err
		}
		if conversation.Status != "active" {
			return serviceError(Conflict, "conversation_inactive", "conversation is not active", nil)
		}
		existing, existingErr := tx.MessageByIdempotency(key, principal)
		if existingErr == nil {
			if existing.ConversationID != conversationID || existing.IdempotencyDigest != digest {
				return serviceError(Conflict, "idempotency_conflict", "message idempotency key was used with another payload", nil)
			}
			message, duplicate = existing, true
			return nil
		}
		if !errors.Is(existingErr, ports.ErrNotFound) {
			return existingErr
		}
		var decodedParts []domain.MessagePart
		_ = json.Unmarshal(parts, &decodedParts)
		for _, part := range decodedParts {
			if part.Type != "artifact" {
				continue
			}
			artifact, artifactErr := tx.Artifact(part.ArtifactID, principal, true)
			if artifactErr != nil {
				return artifactErr
			}
			if artifact.Status != "validated" || artifact.MessageID != "" {
				return serviceError(Conflict, "artifact_unavailable", "artifact cannot be attached", nil)
			}
			artifact.MessageID = messageID
			artifact.Status = "attached"
			if artifactErr = tx.SaveArtifact(artifact); artifactErr != nil {
				return artifactErr
			}
		}
		message = &domain.Message{ID: messageID, ConversationID: conversationID, SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, Role: "user", Status: "completed", Parts: parts, Content: content, ResultCards: json.RawMessage(`[]`), IdempotencyKey: key, IdempotencyDigest: digest, CreatedAt: now, UpdatedAt: now}
		if err = tx.CreateMessage(message); err != nil {
			return err
		}
		conversation.UpdatedAt, conversation.Version = now, conversation.Version+1
		if conversation.Title == "" && content != "" {
			conversation.Title = bounded(content, 80)
		}
		if err = tx.SaveConversation(conversation); err != nil {
			return err
		}
		panels, err := tx.ListConversationPanels(conversationID, principal.OrganizationID)
		if err != nil {
			return err
		}
		_, deliveries, err := event.Project(tx, "message.created", "message", message.ID, "conversation", event.References{ConversationID: conversationID, MessageID: message.ID}, message, panels, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return s.audit(tx, principal, "message.created", conversationID, "", "", map[string]any{"message_id": message.ID})
	})
	if err != nil {
		var serviceErr *Error
		if errors.As(err, &serviceErr) {
			return nil, false, err
		}
		return nil, false, translateStore(err)
	}
	if !duplicate {
		s.bus.Notify(notify...)
	}
	return message, duplicate, nil
}

func (s *Service) ListMessages(ctx context.Context, principal domain.Principal, conversationID string, offset, limit int) (domain.Page[domain.Message], error) {
	offset, limit = pageBounds(offset, limit)
	var values []domain.Message
	var count int64
	err := s.store.View(ctx, func(tx ports.Tx) error {
		if _, err := tx.Conversation(conversationID, principal, false); err != nil {
			return err
		}
		var err error
		values, count, err = tx.ListMessages(conversationID, principal, offset, limit)
		return err
	})
	if err != nil {
		return domain.Page[domain.Message]{}, translateStore(err)
	}
	return domain.Page[domain.Message]{Results: values, Count: count}, nil
}

type BranchRequest struct {
	MessageID string `json:"message_id"`
	Title     string `json:"title"`
}

func (s *Service) Branch(ctx context.Context, principal domain.Principal, conversationID string, request BranchRequest) (*domain.Conversation, error) {
	if request.MessageID == "" {
		return nil, serviceError(Invalid, "message_required", "branch source message is required", nil)
	}
	var branch *domain.Conversation
	now := time.Now().UTC()
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		source, err := tx.Conversation(conversationID, principal, false)
		if err != nil {
			return err
		}
		boundary, err := tx.Message(request.MessageID, principal, false)
		if err != nil {
			return err
		}
		if boundary.ConversationID != conversationID {
			return serviceError(Invalid, "message_mismatch", "branch message does not belong to conversation", nil)
		}
		branch = &domain.Conversation{ID: uuid.NewString(), SubjectID: principal.SubjectID, SubjectName: principal.Name, SubjectUsername: principal.Username, OrganizationID: principal.OrganizationID, Kind: source.Kind, Assistant: source.Assistant, Profile: source.Profile, Surface: source.Surface, Title: request.Title, Status: "active", Metadata: source.Metadata, Version: 1, CreatedAt: now, UpdatedAt: now}
		if branch.Title == "" {
			branch.Title = source.Title
		}
		if err = tx.CreateConversation(branch); err != nil {
			return err
		}
		foundBoundary := false
		for offset := 0; !foundBoundary; offset += domain.MaxPageSize {
			messages, count, listErr := tx.ListMessages(conversationID, principal, offset, domain.MaxPageSize)
			if listErr != nil {
				return listErr
			}
			for _, sourceMessage := range messages {
				copied := sourceMessage
				copied.ID = uuid.NewString()
				copied.ConversationID = branch.ID
				copied.IdempotencyKey = "branch:" + branch.ID + ":" + sourceMessage.ID
				copied.ParentMessageID = sourceMessage.ID
				if err = tx.CreateMessage(&copied); err != nil {
					return err
				}
				if sourceMessage.ID == boundary.ID {
					foundBoundary = true
					break
				}
			}
			if foundBoundary {
				break
			}
			if len(messages) == 0 || offset+len(messages) >= int(count) {
				return serviceError(Conflict, "branch_boundary_missing", "branch source changed while the branch was created", nil)
			}
		}
		return s.audit(tx, principal, "conversation.branched", branch.ID, "", "", map[string]any{"source_conversation_id": conversationID, "source_message_id": request.MessageID})
	})
	if err != nil {
		var serviceErr *Error
		if errors.As(err, &serviceErr) {
			return nil, err
		}
		return nil, translateStore(err)
	}
	return branch, nil
}

func (s *Service) Regenerate(ctx context.Context, principal domain.Principal, messageID, panelID string) (*domain.Run, error) {
	var message *domain.Message
	var conversation *domain.Conversation
	err := s.store.View(ctx, func(tx ports.Tx) error {
		var err error
		message, err = tx.Message(messageID, principal, false)
		if err != nil {
			return err
		}
		conversation, err = tx.Conversation(message.ConversationID, principal, false)
		return err
	})
	if err != nil {
		return nil, translateStore(err)
	}
	if message.Role != "user" {
		return nil, serviceError(Invalid, "invalid_regeneration_source", "regeneration requires a user message", nil)
	}
	capabilityMode := "disabled"
	if profile, ok := policy.Get(conversation.Profile); ok && profile.CoreAPIEnabled {
		capabilityMode = "service"
	} else if conversation.Kind == "capability" {
		capabilityMode = "panel"
	}
	return s.CreateRun(ctx, principal, CreateRunRequest{ConversationID: message.ConversationID, InputMessageID: message.ID, PanelSessionID: panelID, ExecutionMode: "foreground", CapabilityMode: capabilityMode, IdempotencyKey: "regenerate:" + uuid.NewString(), RegeneratedFromID: messageID})
}

func (s *Service) CreateArtifact(ctx context.Context, principal domain.Principal, header *multipart.FileHeader, kind string) (*domain.Artifact, error) {
	if header == nil || header.Size < 0 || header.Size > s.maxArtifactBytes {
		return nil, serviceError(Invalid, "artifact_too_large", "artifact exceeds the configured size limit", nil)
	}
	name := filepath.Base(strings.TrimSpace(header.Filename))
	if name == "." || name == "" || len(name) > 512 {
		return nil, serviceError(Invalid, "invalid_artifact_name", "artifact name is invalid", nil)
	}
	file, err := header.Open()
	if err != nil {
		return nil, serviceError(Invalid, "artifact_open_failed", "artifact could not be read", err)
	}
	defer file.Close()
	id := uuid.NewString()
	storageKey := safeStorageKey(id)
	target := filepath.Join(s.artifactDir, storageKey)
	if err = os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return nil, serviceError(Internal, "artifact_store_failed", "artifact could not be stored", err)
	}
	size, digest, err := copyArtifact(file, target, s.maxArtifactBytes)
	if err != nil {
		return nil, serviceError(Invalid, "artifact_store_failed", err.Error(), err)
	}
	opened, err := os.Open(target)
	if err != nil {
		return nil, serviceError(Internal, "artifact_store_failed", "artifact could not be inspected", err)
	}
	buffer := make([]byte, 512)
	count, _ := opened.Read(buffer)
	_ = opened.Close()
	mediaType := http.DetectContentType(buffer[:count])
	suppliedType := strings.ToLower(strings.TrimSpace(strings.Split(header.Header.Get("Content-Type"), ";")[0]))
	if suppliedType != "" && suppliedType != "application/octet-stream" && !compatibleMediaTypes(suppliedType, mediaType) {
		_ = os.Remove(target)
		return nil, serviceError(Invalid, "artifact_type_mismatch", "artifact content type does not match its content", nil)
	}
	if suppliedType != "" && suppliedType != "application/octet-stream" {
		mediaType = suppliedType
	}
	if kind == "" {
		if strings.HasPrefix(mediaType, "image/") {
			kind = "image"
		} else {
			kind = "file"
		}
	}
	if kind != "image" && kind != "file" {
		_ = os.Remove(target)
		return nil, serviceError(Invalid, "invalid_artifact_kind", "artifact kind is invalid", nil)
	}
	if kind == "image" {
		imageFile, openErr := os.Open(target)
		if openErr != nil {
			_ = os.Remove(target)
			return nil, serviceError(Internal, "artifact_store_failed", "artifact could not be inspected", openErr)
		}
		imageConfig, _, decodeErr := image.DecodeConfig(imageFile)
		_ = imageFile.Close()
		if decodeErr != nil || !strings.HasPrefix(mediaType, "image/") || imageConfig.Width < 1 || imageConfig.Height < 1 || int64(imageConfig.Width)*int64(imageConfig.Height) > domain.MaxImagePixels {
			_ = os.Remove(target)
			return nil, serviceError(Invalid, "invalid_image", "image content is invalid or exceeds the pixel limit", decodeErr)
		}
	}
	extractedText := ""
	if kind == "file" && textArtifact(mediaType, name) {
		textFile, openErr := os.Open(target)
		if openErr == nil {
			text, readErr := io.ReadAll(io.LimitReader(textFile, domain.MaxExtractedTextBytes+1))
			_ = textFile.Close()
			if readErr == nil && utf8.Valid(text) {
				if len(text) > domain.MaxExtractedTextBytes {
					text = text[:domain.MaxExtractedTextBytes]
				}
				extractedText = string(text)
			}
		}
	}
	if sensitiveArtifactName(name) || extractedText != "" && sanitizeAuditText(extractedText) != strings.TrimSpace(extractedText) {
		_ = os.Remove(target)
		return nil, serviceError(Invalid, "artifact_sensitive", "artifact name or extracted text contains sensitive data", nil)
	}
	artifact := &domain.Artifact{ID: id, SubjectID: principal.SubjectID, OrganizationID: principal.OrganizationID, Status: "validated", Kind: kind, Name: name, MediaType: mediaType, Size: size, Digest: digest, StorageKey: storageKey, ExtractedText: extractedText, CreatedAt: time.Now().UTC()}
	if err = s.store.Transaction(ctx, func(tx ports.Tx) error {
		if createErr := tx.CreateArtifact(artifact); createErr != nil {
			return createErr
		}
		return s.audit(tx, principal, "artifact.created", "", "", "", map[string]any{"artifact_id": id, "kind": kind, "size": size})
	}); err != nil {
		_ = os.Remove(target)
		return nil, translateStore(err)
	}
	return artifact, nil
}

func (s *Service) Artifact(ctx context.Context, principal domain.Principal, id string) (*domain.Artifact, string, error) {
	var artifact *domain.Artifact
	err := s.store.View(ctx, func(tx ports.Tx) error { var err error; artifact, err = tx.Artifact(id, principal, false); return err })
	if err != nil {
		return nil, "", translateStore(err)
	}
	return artifact, filepath.Join(s.artifactDir, artifact.StorageKey), nil
}
func (s *Service) DeleteArtifact(ctx context.Context, principal domain.Principal, id string) error {
	var target string
	now := time.Now().UTC()
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		artifact, err := tx.Artifact(id, principal, true)
		if err != nil {
			return err
		}
		if artifact.MessageID != "" {
			return serviceError(Conflict, "artifact_referenced", "artifact is referenced by a message", nil)
		}
		artifact.Status, artifact.DeletedAt = "deleted", &now
		target = filepath.Join(s.artifactDir, artifact.StorageKey)
		return tx.SaveArtifact(artifact)
	})
	if err != nil {
		var serviceErr *Error
		if errors.As(err, &serviceErr) {
			return err
		}
		return translateStore(err)
	}
	if removeErr := os.Remove(target); removeErr != nil && !os.IsNotExist(removeErr) {
		return serviceError(Internal, "artifact_delete_failed", "artifact content could not be deleted", removeErr)
	}
	return nil
}

func (s *Service) UnsupportedTranscription() error {
	return serviceError(Unavailable, "transcription_not_configured", "speech transcription is not configured", nil)
}

func compatibleMediaTypes(supplied, detected string) bool {
	detected = strings.ToLower(strings.TrimSpace(strings.Split(detected, ";")[0]))
	if supplied == detected || supplied == "image/jpg" && detected == "image/jpeg" {
		return true
	}
	return strings.HasPrefix(supplied, "text/") && detected == "text/plain"
}

func textArtifact(mediaType, name string) bool {
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	extension := strings.ToLower(filepath.Ext(name))
	return map[string]bool{".json": true, ".yaml": true, ".yml": true, ".xml": true, ".csv": true, ".sql": true, ".md": true, ".log": true}[extension]
}

func sensitiveArtifactName(name string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "_", " ", "_").Replace(name))
	for _, marker := range []string{"password", "passwd", "private_key", "secret", "access_key", "api_key", "credential"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
