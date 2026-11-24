package standalone

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jumpserver/koko/pkg/chen/core"
)

const (
	DefaultMaxIdle     = 15 * time.Minute
	DefaultMaxDuration = 8 * time.Hour
)

// AuditEntry is a private local audit record. Raw rows, connection passwords,
// certificate keys and URL/session bearer tokens are never written here.
type AuditEntry struct {
	Version     int       `json:"version"`
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	SessionID   string    `json:"session_id"`
	Selector    string    `json:"selector"`
	Name        string    `json:"name"`
	Protocol    string    `json:"protocol"`
	RemoteAddr  string    `json:"remote_addr"`
	Timestamp   time.Time `json:"timestamp"`
	ExecutionID string    `json:"execution_id,omitempty"`
	SQL         string    `json:"sql,omitempty"`
	Output      string    `json:"output,omitempty"`
	Error       string    `json:"error,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	FinishedAt  time.Time `json:"finished_at,omitempty"`
	RiskLevel   int32     `json:"risk_level,omitempty"`
}

type localSession struct {
	mu        sync.Mutex
	remote    string
	connected bool
	closed    bool
}

type Provider struct {
	backends map[string]BackendConfig // immutable private copy
	auditDir string
	mu       sync.RWMutex
	sessions map[*core.Grant]*localSession
	auditMu  sync.Mutex
}

func NewProvider(config *Config, auditDir string) (*Provider, error) {
	if config == nil || len(config.backends) == 0 {
		return nil, errors.New("standalone_config_required")
	}
	if err := core.PrepareAuditDirectory(auditDir); err != nil {
		return nil, err
	}
	return &Provider{backends: config.backends, auditDir: auditDir, sessions: make(map[*core.Grant]*localSession)}, nil
}

// Configuration and local audit storage are checked before the provider is published.
func (p *Provider) Ready(ctx context.Context) error { return ctx.Err() }

func (p *Provider) Open(ctx context.Context, selector, remoteAddr string) (*core.Grant, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	backend, exists := p.backends[selector]
	if !exists {
		return nil, errors.New("selector_not_found")
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, errors.New("secure_random_unavailable")
	}
	permissions := core.Permissions{Connect: true, Copy: true, Paste: true, Download: true, Upload: true}
	g := core.NewGrant("local_"+id.String(), backend.connectionConfig(), permissions,
		time.Now().Add(DefaultMaxDuration), DefaultMaxIdle, DefaultMaxDuration)
	g.OwnerID, g.OwnerName = "standalone", "Standalone"
	g.AssetID, g.AssetName, g.AccountName = backend.Selector, backend.Name, backend.Name
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		remoteAddr = host
	}
	ls := &localSession{remote: remoteAddr}
	if err := p.write(g, ls, AuditEntry{Type: "connection_opened"}); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		g.CloseLocal()
		if closeErr := p.write(g, ls, AuditEntry{Type: "connection_closed", Error: "open_canceled"}); closeErr != nil {
			return nil, closeErr
		}
		return nil, err
	}
	p.mu.Lock()
	p.sessions[g] = ls
	p.mu.Unlock()
	return g, nil
}

func (p *Provider) find(g *core.Grant) (*localSession, error) {
	p.mu.RLock()
	ls := p.sessions[g]
	p.mu.RUnlock()
	if ls == nil {
		return nil, core.ErrClosed
	}
	return ls, nil
}

func (p *Provider) Connected(ctx context.Context, g *core.Grant) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := g.Check(); err != nil {
		return err
	}
	ls, err := p.find(g)
	if err != nil {
		return err
	}
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.closed {
		return core.ErrClosed
	}
	if ls.connected {
		return nil
	}
	if err := p.write(g, ls, AuditEntry{Type: "connection_connected"}); err != nil {
		return err
	}
	ls.connected = true
	return nil
}

func (p *Provider) Authorize(ctx context.Context, g *core.Grant, sql string) error {
	if _, err := p.find(g); err != nil {
		return err
	}
	return g.Authorize(ctx, sql)
}

func (p *Provider) AuthorizeCommand(ctx context.Context, g *core.Grant, request core.AuthorizationRequest) (core.AuthorizationDecision, error) {
	return core.AuthorizationDecision{}, p.Authorize(ctx, g, request.SQL)
}

// Record acknowledges only a synced local record. The engine retains SQL
// outcome separately if persistence fails after database execution.
func (p *Provider) Record(_ context.Context, g *core.Grant, record core.AuditRecord) error {
	ls, err := p.find(g)
	if err != nil {
		return err
	}
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.closed {
		return core.ErrClosed
	}
	if record.StartedAt.IsZero() {
		record.StartedAt = time.Now()
	}
	if record.FinishedAt.IsZero() {
		record.FinishedAt = time.Now()
	}
	return p.write(g, ls, AuditEntry{Type: "query", ExecutionID: record.ID, SQL: record.SQL, Output: record.Output, Error: record.Error, StartedAt: record.StartedAt, FinishedAt: record.FinishedAt, RiskLevel: record.RiskLevel})
}

func (p *Provider) Close(_ context.Context, g *core.Grant) error {
	if g == nil {
		return nil
	}
	ls, err := p.find(g)
	if err != nil {
		return nil
	}
	g.CloseLocal()
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if ls.closed {
		return nil
	}
	if err := p.write(g, ls, AuditEntry{Type: "connection_closed"}); err != nil {
		return err
	}
	ls.closed = true
	p.mu.Lock()
	delete(p.sessions, g)
	p.mu.Unlock()
	return nil
}

// The engine already checks Grant deadlines once per second and owns database
// cancellation/closure. Standalone mode has no external administrative stream.
func (p *Provider) Watch(ctx context.Context, _ *core.Grant, _ func(core.Control)) {
	<-ctx.Done()
}
func (p *Provider) Mask(_ *core.Grant, _, _ string, value any) (any, bool) { return value, false }

func (p *Provider) write(g *core.Grant, ls *localSession, entry AuditEntry) error {
	p.auditMu.Lock()
	defer p.auditMu.Unlock()
	id, err := uuid.NewRandom()
	if err != nil {
		return errors.New("secure_random_unavailable")
	}
	entry.Version = 1
	entry.ID, entry.SessionID = "audit_"+id.String(), g.ID
	entry.Selector, entry.Name, entry.Protocol = g.AssetID, g.AssetName, g.Config.Dialect
	entry.RemoteAddr, entry.Timestamp = ls.remote, time.Now().UTC()
	entry.SQL = g.RedactCredentials(entry.SQL)
	entry.Output = core.BoundedAuditOutput(g.RedactCredentials(entry.Output))
	entry.Error = core.BoundedAuditOutput(g.RedactCredentials(entry.Error))
	name := fmt.Sprintf("%020d-%s.json", entry.Timestamp.UnixNano(), entry.ID)
	if err := core.WriteAuditJSON(p.auditDir, name, entry); err != nil {
		return errors.Join(core.ErrAuditDisk, errors.New("standalone_audit_write_failed"))
	}
	return nil
}

var _ core.Provider = (*Provider)(nil)
