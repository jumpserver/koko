package standalone

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	backend   BackendConfig
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
	if config == nil {
		return nil, errors.New("standalone_config_required")
	}
	normalized, err := normalize(*config)
	if err != nil {
		return nil, err
	}
	if err := privateAuditDir(auditDir); err != nil {
		return nil, err
	}
	p := &Provider{backends: make(map[string]BackendConfig, len(normalized.Backends)), auditDir: auditDir, sessions: make(map[*core.Grant]*localSession)}
	for _, b := range normalized.Backends {
		p.backends[b.Selector] = b
	}
	return p, nil
}

func (p *Provider) Open(ctx context.Context, selector, remoteAddr string) (*core.Grant, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	backend, exists := p.backends[selector]
	if !exists {
		return nil, errors.New("selector_not_found")
	}
	id, err := localID("local_")
	if err != nil {
		return nil, err
	}
	config := core.ConnectionConfig{Dialect: backend.Protocol, OracleSYSDBA: backend.UseSYSDBA, Host: backend.Host, Port: backend.Port, User: backend.User, Password: backend.Password, Database: backend.Database, Schema: backend.Schema, Transport: backend.Transport, TLS: core.TLSConfig{Enabled: backend.UseSSL, ServerName: backend.ServerName, InsecureSkipVerify: backend.AllowInvalidCert, Mode: backend.SSLMode, CACert: backend.CACert, ClientCert: backend.ClientCert, ClientKey: backend.ClientKey}}
	g, err := core.NewLocalGrant(id, config, core.Permissions{Connect: true, Copy: true, Paste: true, Download: true, Upload: true}, DefaultMaxIdle, DefaultMaxDuration)
	if err != nil {
		return nil, err
	}
	g.OwnerID, g.OwnerName = "standalone", "Standalone"
	g.AssetID, g.AssetName, g.AccountName = backend.Selector, backend.Name, backend.Name
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		remoteAddr = host
	}
	ls := &localSession{backend: backend, remote: remoteAddr}
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := p.find(g); err != nil {
		return err
	}
	if err := g.Check(); err != nil {
		return err
	}
	if strings.TrimSpace(sql) == "" {
		return errors.New("empty_sql")
	}
	return nil
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

func privateAuditDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("standalone_audit_directory_required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return auditFailure()
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return auditFailure()
	}
	if os.Chmod(dir, 0700) != nil {
		return auditFailure()
	}
	probe, err := os.CreateTemp(dir, ".probe-")
	if err != nil {
		return auditFailure()
	}
	name := probe.Name()
	defer os.Remove(name)
	err = probe.Sync()
	closeErr := probe.Close()
	if err != nil || closeErr != nil {
		return auditFailure()
	}
	if syncAuditDir(dir) != nil {
		return auditFailure()
	}
	return nil
}

func localID(prefix string) (string, error) {
	var data [24]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", errors.New("secure_random_unavailable")
	}
	return prefix + hex.EncodeToString(data[:]), nil
}
func auditFailure() error {
	return errors.Join(core.ErrAuditDisk, errors.New("standalone_audit_write_failed"))
}
func bounded(value string, max int) string {
	if len(value) <= max {
		return value
	}
	for max > 0 && value[max]&0xc0 == 0x80 {
		max--
	}
	return value[:max]
}

func (p *Provider) write(g *core.Grant, ls *localSession, entry AuditEntry) error {
	p.auditMu.Lock()
	defer p.auditMu.Unlock()
	id, err := localID("audit_")
	if err != nil {
		return err
	}
	entry.Version, entry.ID, entry.SessionID, entry.Selector, entry.Name, entry.Protocol, entry.RemoteAddr, entry.Timestamp = 1, id, g.ID, ls.backend.Selector, ls.backend.Name, ls.backend.Protocol, ls.remote, time.Now().UTC()
	// Configured credentials cannot escape into audit via driver diagnostics.
	for _, secret := range []string{ls.backend.Password, ls.backend.ClientKey} {
		if secret != "" {
			entry.SQL = strings.ReplaceAll(entry.SQL, secret, "[redacted]")
			entry.Output = strings.ReplaceAll(entry.Output, secret, "[redacted]")
			entry.Error = strings.ReplaceAll(entry.Error, secret, "[redacted]")
		}
	}
	entry.Output, entry.Error = bounded(entry.Output, 2048), bounded(entry.Error, 2048)
	data, err := json.Marshal(entry)
	if err != nil {
		return auditFailure()
	}
	f, err := os.CreateTemp(p.auditDir, ".audit-")
	if err != nil {
		return auditFailure()
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return auditFailure()
	}
	name := fmt.Sprintf("%020d-%s.json", entry.Timestamp.UnixNano(), id)
	if os.Rename(temp, filepath.Join(p.auditDir, name)) != nil {
		return auditFailure()
	}
	if syncAuditDir(p.auditDir) != nil {
		return auditFailure()
	}
	return nil
}
func syncAuditDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

var _ core.Provider = (*Provider)(nil)
