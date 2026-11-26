package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/event"
	"github.com/jumpserver/koko/internal/kael/identity"
	"github.com/jumpserver/koko/internal/kael/model"
	"github.com/jumpserver/koko/internal/kael/policy"
	"github.com/jumpserver/koko/internal/kael/ports"
	agentruntime "github.com/jumpserver/koko/internal/kael/runtime"
	"go.uber.org/zap"
)

type ErrorKind string

const (
	Invalid     ErrorKind = "invalid"
	NotFound    ErrorKind = "not_found"
	Conflict    ErrorKind = "conflict"
	Forbidden   ErrorKind = "forbidden"
	Unavailable ErrorKind = "unavailable"
	RateLimited ErrorKind = "rate_limited"
	Internal    ErrorKind = "internal"
)

type Error struct {
	Kind      ErrorKind
	Code      string
	Detail    string
	Retryable bool
	cause     error
}

func (e *Error) Error() string { return e.Detail }
func (e *Error) Unwrap() error { return e.cause }
func serviceError(kind ErrorKind, code, detail string, cause error) error {
	return &Error{Kind: kind, Code: code, Detail: detail, cause: cause}
}
func translateStore(err error) error {
	if errors.Is(err, ports.ErrCapacity) {
		return &Error{Kind: Unavailable, Code: "storage_capacity_exceeded", Detail: "local AI history storage is full or disk space is low; retry after space has been freed", Retryable: true, cause: err}
	}
	if errors.Is(err, ports.ErrUnavailable) {
		return &Error{Kind: Unavailable, Code: "storage_unavailable", Detail: "runtime storage is temporarily unavailable", Retryable: true, cause: err}
	}
	if errors.Is(err, ports.ErrNotFound) {
		return serviceError(NotFound, "not_found", "resource was not found", err)
	}
	if errors.Is(err, ports.ErrConflict) {
		return serviceError(Conflict, "conflict", "resource conflicts with existing state", err)
	}
	return serviceError(Internal, "storage_failed", "storage operation failed", err)
}

type Options struct {
	Store             ports.Store
	Engine            agentruntime.Engine
	Bus               *event.Bus
	Logger            *zap.Logger
	InstanceID        string
	Workers           int
	RunTimeout        time.Duration
	ToolResultTimeout time.Duration
	PanelLease        time.Duration
	RegistrationLease time.Duration
	EventRetention    time.Duration
	ArtifactDir       string
	MaxArtifactBytes  int64
	Capability        ports.CapabilityProvider
	StorageKind       string
	StorageDurable    bool
}

type Service struct {
	store              ports.Store
	engine             agentruntime.Engine
	bus                *event.Bus
	logger             *zap.Logger
	instanceID         string
	workers            int
	runTimeout         time.Duration
	toolResultTimeout  time.Duration
	panelLease         time.Duration
	registrationLease  time.Duration
	eventRetention     time.Duration
	artifactDir        string
	maxArtifactBytes   int64
	capability         ports.CapabilityProvider
	storageKind        string
	storageDurable     bool
	wake               chan struct{}
	stop               chan struct{}
	done               chan struct{}
	startOnce          sync.Once
	stopOnce           sync.Once
	registryCancel     context.CancelFunc
	lifecycleMu        sync.Mutex
	started            bool
	startErr           error
	activeMu           sync.Mutex
	active             map[string]context.CancelFunc
	credentialsMu      sync.Mutex
	runCredentials     map[string]identity.CoreCredentials
	accountPasswordsMu sync.Mutex
	accountPasswords   map[string]string
	approvalSecrets    map[string]map[string]string
}

func New(options Options) (*Service, error) {
	if options.Store == nil || options.Engine == nil || options.Bus == nil || options.InstanceID == "" {
		return nil, fmt.Errorf("service dependencies are incomplete")
	}
	if options.Logger == nil {
		options.Logger = zap.NewNop()
	}
	if options.Workers < 1 {
		options.Workers = 1
	}
	if options.RunTimeout <= 0 || options.RunTimeout > time.Hour {
		options.RunTimeout = 30 * time.Minute
	}
	if options.PanelLease <= 0 {
		options.PanelLease = 2 * time.Minute
	}
	if options.ToolResultTimeout <= 0 {
		options.ToolResultTimeout = 45 * time.Second
	}
	if options.RegistrationLease <= 0 {
		options.RegistrationLease = options.PanelLease
	}
	if options.EventRetention <= 0 {
		options.EventRetention = 24 * time.Hour
	}
	if options.MaxArtifactBytes <= 0 {
		options.MaxArtifactBytes = 20 * 1024 * 1024
	}
	if options.StorageKind == "" {
		options.StorageKind = "memory"
	}
	return &Service{store: options.Store, engine: options.Engine, bus: options.Bus, logger: options.Logger, instanceID: options.InstanceID, workers: options.Workers, runTimeout: options.RunTimeout, toolResultTimeout: options.ToolResultTimeout, panelLease: options.PanelLease, registrationLease: options.RegistrationLease, eventRetention: options.EventRetention, artifactDir: options.ArtifactDir, maxArtifactBytes: options.MaxArtifactBytes, capability: options.Capability, storageKind: options.StorageKind, storageDurable: options.StorageDurable, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), active: make(map[string]context.CancelFunc)}, nil
}

func (s *Service) Start(ctx context.Context) error {
	s.startOnce.Do(func() {
		if err := s.store.Ready(ctx); err != nil {
			s.startErr = fmt.Errorf("initialize store: %w", err)
			return
		}
		if err := os.MkdirAll(s.artifactDir, 0o700); err != nil {
			s.startErr = fmt.Errorf("create artifact directory: %w", err)
			return
		}
		now := time.Now().UTC()
		if err := s.store.Transaction(ctx, func(tx ports.Tx) error { return tx.Maintain(now, now.Add(-s.eventRetention)) }); err != nil {
			s.startErr = fmt.Errorf("maintain runtime state: %w", err)
			return
		}
		var registryCtx context.Context
		var registryCancel context.CancelFunc
		if s.capability != nil {
			registryCtx, registryCancel = context.WithCancel(context.Background())
		}
		var workers sync.WaitGroup
		workers.Add(s.workers + 1)
		if registryCtx != nil {
			workers.Add(1)
		}
		s.lifecycleMu.Lock()
		s.registryCancel = registryCancel
		s.started = true
		s.lifecycleMu.Unlock()
		for index := 0; index < s.workers; index++ {
			go func() { defer workers.Done(); s.worker() }()
		}
		go func() { defer workers.Done(); s.maintenance() }()
		if registryCtx != nil {
			go func() { defer workers.Done(); s.loadCapabilityRegistry(registryCtx) }()
		}
		go func() { workers.Wait(); close(s.done) }()
		s.signalWorker()
	})
	return s.startErr
}

func (s *Service) Close() {
	s.stopOnce.Do(func() {
		defer func() {
			s.credentialsMu.Lock()
			clear(s.runCredentials)
			s.credentialsMu.Unlock()
		}()
		s.lifecycleMu.Lock()
		started := s.started
		registryCancel := s.registryCancel
		s.lifecycleMu.Unlock()
		if !started {
			return
		}
		if registryCancel != nil {
			registryCancel()
		}
		close(s.stop)
		s.activeMu.Lock()
		for _, cancel := range s.active {
			cancel()
		}
		s.activeMu.Unlock()
		<-s.done
	})
}
func (s *Service) Ready(ctx context.Context) error { return s.store.Ready(ctx) }

// The Core schema can be slow just after Core starts. Retry in the background
// until it is loaded once, without blocking Kael's listener or health check.
func (s *Service) loadCapabilityRegistry(ctx context.Context) {
	for {
		loadCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		info, err := s.capability.Refresh(loadCtx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			s.logger.Info("Core capability registry ready", zap.Any("registry", info))
			return
		}
		s.logger.Warn("Core capability registry load failed; retrying", zap.Error(err))
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (s *Service) Metrics(ctx context.Context) (map[string]int64, error) {
	var result map[string]int64
	err := s.store.View(ctx, func(tx ports.Tx) error {
		var err error
		result, err = tx.RuntimeMetrics(time.Now().UTC().Add(-24 * time.Hour))
		return err
	})
	if err != nil {
		return nil, err
	}
	if metricsStore, ok := s.store.(interface {
		PersistenceMetrics(context.Context) (map[string]int64, error)
	}); ok {
		persistenceMetrics, metricsErr := metricsStore.PersistenceMetrics(ctx)
		if metricsErr != nil {
			return nil, metricsErr
		}
		for key, value := range persistenceMetrics {
			result[key] = value
		}
	}
	return result, err
}
func (s *Service) Bootstrap() map[string]any {
	return map[string]any{"agent_engine": "codex", "agent_protocol_version": 1, "api_version": domain.APIVersion, "protocol_version": domain.ProtocolVersion, "capability_version": domain.CapabilityVersion, "cluster_id": "kael", "instance_id": s.instanceID, "storage": map[string]any{"kind": s.storageKind, "durable": s.storageDurable}, "features": map[string]bool{"conversations": true, "panel_capabilities": true, "service_capabilities": s.capability != nil, "platform_gateway": s.capability != nil, "artifacts": true, "branch": true, "regenerate": true, "background": false, "sse_replay": true, "transcription": false, "web_search": false, "notifications": false}, "limits": map[string]any{"max_context_bytes": domain.MaxContextBytes, "max_tools": domain.MaxTools, "max_tool_calls": domain.MaxToolCalls, "max_queued_runs": domain.MaxQueuedRuns, "max_artifact_bytes": s.maxArtifactBytes, "event_retention_seconds": int64(s.eventRetention.Seconds())}}
}
func (s *Service) Profiles(principal domain.Principal) []policy.Profile {
	available := policy.Available(principal)
	result := make([]policy.Profile, 0, len(available))
	for _, profile := range available {
		if profile.CoreAPIEnabled && s.capability == nil {
			continue
		}
		result = append(result, profile)
	}
	return result
}
func (s *Service) ModelInfo() model.Info   { return s.engine.Info() }
func (s *Service) MaxArtifactBytes() int64 { return s.maxArtifactBytes }

func (s *Service) maintenance() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			now := time.Now().UTC()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := s.store.Transaction(ctx, func(tx ports.Tx) error { return tx.Maintain(now, now.Add(-s.eventRetention)) })
			if err == nil {
				err = s.pruneRunCredentials(ctx)
			}
			cancel()
			if err != nil {
				s.logger.Error("maintain runtime state", zap.Error(err))
			}
		}
	}
}

func (s *Service) RefreshPlatformRegistry(ctx context.Context, principal domain.Principal) (map[string]any, error) {
	if !principal.IsSuperuser {
		return nil, serviceError(Forbidden, "admin_required", "superuser permission is required", nil)
	}
	if s.capability == nil {
		return nil, serviceError(Unavailable, "platform_registry_not_configured", "the platform registry gateway is not configured", nil)
	}
	result, err := s.capability.Refresh(ctx)
	if err != nil {
		return nil, serviceError(Unavailable, "platform_registry_refresh_failed", "the platform registry could not be refreshed", err)
	}
	return result, nil
}

type CreateConversationRequest struct {
	Kind      string          `json:"kind"`
	Assistant string          `json:"assistant"`
	Profile   string          `json:"profile"`
	Surface   string          `json:"surface"`
	Title     string          `json:"title"`
	Metadata  json.RawMessage `json:"metadata"`
}
type UpdateConversationRequest struct {
	Title     *string `json:"title"`
	Assistant *string `json:"assistant"`
	Archived  *bool   `json:"archived"`
}

func normalizeProfile(request CreateConversationRequest) (policy.Profile, error) {
	profileID := strings.TrimSpace(request.Profile)
	if profileID == "" {
		profileID = strings.TrimSpace(request.Assistant)
	}
	if profileID == "" {
		profileID = "general"
	}
	profile, ok := policy.Get(profileID)
	if !ok {
		return policy.Profile{}, serviceError(Invalid, "invalid_profile", "runtime profile is invalid", nil)
	}
	return profile, nil
}

func (s *Service) CreateConversation(ctx context.Context, principal domain.Principal, request CreateConversationRequest) (*domain.Conversation, error) {
	profile, err := normalizeProfile(request)
	if err != nil {
		return nil, err
	}
	if !policy.Authorized(profile, principal) {
		return nil, serviceError(Forbidden, "profile_forbidden", "runtime profile is not available", nil)
	}
	if profile.CoreAPIEnabled && s.capability == nil {
		return nil, serviceError(Unavailable, "service_capability_unavailable", "this assistant requires the configured Platform Gateway", nil)
	}
	kind := strings.TrimSpace(request.Kind)
	if kind == "" {
		kind = profile.Kind
	}
	if kind != "general" && kind != "capability" || kind != profile.Kind {
		return nil, serviceError(Invalid, "invalid_conversation_kind", "conversation kind does not match runtime profile", nil)
	}
	assistant := strings.TrimSpace(request.Assistant)
	if assistant == "" {
		assistant = strings.TrimPrefix(profile.ID, "platform.")
	}
	if len(request.Title) > 512 || !utf8.ValidString(request.Title) {
		return nil, serviceError(Invalid, "invalid_title", "conversation title is invalid", nil)
	}
	metadata, err := sanitizeJSON(request.Metadata, 64*1024)
	if err != nil {
		return nil, serviceError(Invalid, "invalid_metadata", "conversation metadata is invalid", err)
	}
	now := time.Now().UTC()
	conversation := &domain.Conversation{ID: uuid.NewString(), SubjectID: principal.SubjectID, SubjectName: principal.Name, SubjectUsername: principal.Username, OrganizationID: principal.OrganizationID, Kind: kind, Assistant: assistant, Profile: profile.ID, Surface: bounded(request.Surface, 128), Title: request.Title, Status: "active", Metadata: metadata, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err = s.store.Transaction(ctx, func(tx ports.Tx) error {
		if createErr := tx.CreateConversation(conversation); createErr != nil {
			return createErr
		}
		if _, _, createErr := event.Project(tx, "conversation.created", "conversation", conversation.ID, "conversation", event.References{ConversationID: conversation.ID}, conversation, nil, now); createErr != nil {
			return createErr
		}
		return s.audit(tx, principal, "conversation.created", conversation.ID, "", "", nil)
	}); err != nil {
		return nil, translateStore(err)
	}
	return conversation, nil
}

func (s *Service) Conversation(ctx context.Context, principal domain.Principal, id string) (*domain.Conversation, error) {
	var result *domain.Conversation
	err := s.store.View(ctx, func(tx ports.Tx) error {
		var err error
		result, err = tx.Conversation(id, principal, false)
		return err
	})
	if err != nil {
		return nil, translateStore(err)
	}
	return result, nil
}
func pageBounds(offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 {
		limit = 50
	}
	if limit > domain.MaxPageSize {
		limit = domain.MaxPageSize
	}
	return offset, limit
}
func (s *Service) ListConversations(ctx context.Context, principal domain.Principal, kind string, offset, limit int) (domain.Page[domain.Conversation], error) {
	kind = strings.TrimSpace(kind)
	if kind != "" && kind != "general" && kind != "capability" {
		return domain.Page[domain.Conversation]{}, serviceError(Invalid, "invalid_conversation_kind", "conversation kind is invalid", nil)
	}
	offset, limit = pageBounds(offset, limit)
	var values []domain.Conversation
	var count int64
	err := s.store.View(ctx, func(tx ports.Tx) error {
		var err error
		values, count, err = tx.ListConversations(principal, kind, offset, limit)
		return err
	})
	if err != nil {
		return domain.Page[domain.Conversation]{}, translateStore(err)
	}
	return domain.Page[domain.Conversation]{Results: values, Count: count}, nil
}

func (s *Service) UpdateConversation(ctx context.Context, principal domain.Principal, id string, request UpdateConversationRequest) (*domain.Conversation, error) {
	var value *domain.Conversation
	var notify []string
	now := time.Now().UTC()
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		var err error
		value, err = tx.Conversation(id, principal, true)
		if err != nil {
			return err
		}
		if request.Title != nil {
			if len(*request.Title) > 512 || !utf8.ValidString(*request.Title) {
				return serviceError(Invalid, "invalid_title", "conversation title is invalid", nil)
			}
			value.Title = *request.Title
		}
		if request.Assistant != nil {
			profile, ok := policy.Get(*request.Assistant)
			if !ok || profile.Kind != value.Kind || !policy.Authorized(profile, principal) {
				return serviceError(Invalid, "invalid_assistant", "assistant is invalid", nil)
			}
			if profile.CoreAPIEnabled && s.capability == nil {
				return serviceError(Unavailable, "service_capability_unavailable", "this assistant requires the configured Platform Gateway", nil)
			}
			value.Assistant, value.Profile = strings.TrimPrefix(profile.ID, "platform."), profile.ID
		}
		if request.Archived != nil {
			if *request.Archived {
				value.Status = "archived"
				value.ArchivedAt = &now
			} else {
				value.Status = "active"
				value.ArchivedAt = nil
			}
		}
		value.Version++
		value.UpdatedAt = now
		if err = tx.SaveConversation(value); err != nil {
			return err
		}
		panels, err := tx.ListConversationPanels(value.ID, principal.OrganizationID)
		if err != nil {
			return err
		}
		_, deliveries, err := event.Project(tx, "conversation.updated", "conversation", value.ID, "conversation", event.References{ConversationID: value.ID}, value, panels, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return s.audit(tx, principal, "conversation.updated", value.ID, "", "", nil)
	})
	if err != nil {
		var serviceErr *Error
		if errors.As(err, &serviceErr) {
			return nil, err
		}
		return nil, translateStore(err)
	}
	s.bus.Notify(notify...)
	return value, nil
}

func (s *Service) DeleteConversation(ctx context.Context, principal domain.Principal, id string) error {
	now := time.Now().UTC()
	var notify []string
	err := s.store.Transaction(ctx, func(tx ports.Tx) error {
		value, err := tx.Conversation(id, principal, true)
		if err != nil {
			return err
		}
		count, err := tx.ActiveRunCount(id)
		if err != nil {
			return err
		}
		if count > 0 {
			return serviceError(Conflict, "conversation_busy", "conversation has an active run", nil)
		}
		panels, err := tx.ListConversationPanels(id, principal.OrganizationID)
		if err != nil {
			return err
		}
		value.Status = "deleted"
		value.DeletedAt = &now
		value.UpdatedAt = now
		value.Version++
		if err = tx.SaveConversation(value); err != nil {
			return err
		}
		_, deliveries, err := event.Project(tx, "conversation.updated", "conversation", id, "conversation", event.References{ConversationID: id}, map[string]any{"id": id, "status": "deleted"}, panels, now)
		if err != nil {
			return err
		}
		for _, delivery := range deliveries {
			notify = append(notify, delivery.PanelSessionID)
		}
		return s.audit(tx, principal, "conversation.deleted", id, "", "", nil)
	})
	if err != nil {
		var serviceErr *Error
		if errors.As(err, &serviceErr) {
			return err
		}
		return translateStore(err)
	}
	s.bus.Notify(notify...)
	return nil
}

func (s *Service) audit(tx ports.Tx, principal domain.Principal, action, conversationID, panelID, runID string, summary any) error {
	raw, _ := json.Marshal(summary)
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > 16*1024 {
		raw = json.RawMessage(`{"truncated":true}`)
	}
	return tx.CreateAudit(&domain.AuditRecord{ID: uuid.NewString(), SubjectHash: domain.HashBytes([]byte(principal.SubjectID)), OrganizationID: principal.OrganizationID, ConversationID: conversationID, PanelSessionID: panelID, RunID: runID, Action: action, Summary: raw, CreatedAt: time.Now().UTC()})
}

func randomToken() (string, string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(value)
	return token, domain.HashBytes([]byte(token)), nil
}
func bounded(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
func sanitizeJSON(raw json.RawMessage, maximum int) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	if len(raw) > maximum {
		return nil, fmt.Errorf("payload exceeds limit")
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	value = scrub(value)
	return json.Marshal(value)
}
func scrub(value any) any {
	switch item := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, child := range item {
			normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			if strings.Contains(normalized, "password") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "token") || strings.Contains(normalized, "cookie") || strings.Contains(normalized, "private_key") || strings.Contains(normalized, "certificate") || strings.Contains(normalized, "credential") {
				continue
			}
			result[key] = scrub(child)
		}
		return result
	case []any:
		result := make([]any, len(item))
		for index, child := range item {
			result[index] = scrub(child)
		}
		return result
	default:
		return item
	}
}

func copyArtifact(file multipart.File, target string, maximum int64) (int64, string, error) {
	temporary := target + ".pending"
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, "", err
	}
	digest := sha256.New()
	writer := io.MultiWriter(output, digest)
	size, copyErr := io.Copy(writer, io.LimitReader(file, maximum+1))
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil || size > maximum {
		_ = os.Remove(temporary)
		if size > maximum {
			return 0, "", fmt.Errorf("artifact exceeds size limit")
		}
		if copyErr != nil {
			return 0, "", copyErr
		}
		return 0, "", closeErr
	}
	if err = os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return 0, "", err
	}
	return size, hex.EncodeToString(digest.Sum(nil)), nil
}
func safeStorageKey(id string) string { return filepath.Join(id[:2], id) }
