// Package core defines Chen's server-side connection authorization
// contract. Managed deployments obtain credentials directly from Core;
// standalone deployments supply private, operator-managed configuration.
package core

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/lion/gateway"
)

type Provider interface {
	Open(context.Context, string, string) (*Grant, error)
	Connected(context.Context, *Grant) error
	Authorize(context.Context, *Grant, string) error
	Record(context.Context, *Grant, AuditRecord) error
	Close(context.Context, *Grant) error
	Watch(context.Context, *Grant, func(Control))
	Mask(*Grant, string, string, any) (any, bool)
}

// CommandAuthorizer adds interactive review without widening Provider (local
// mode and non-interactive providers need no ticket implementation). Approval is
// scoped to this call's immutable batch; it is never cached on a Grant.
type CommandAuthorizer interface {
	AuthorizeCommand(context.Context, *Grant, AuthorizationRequest) (AuthorizationDecision, error)
}

type AuthorizationRequest struct {
	AffectedRows    *int64 // nil for a console-owned query; grid plans know their change count
	SQL             string
	BatchSQL        string
	SubmitReview    bool
	ConfirmWarnings bool
	OnReview        func(ReviewStatus)
}

type AuthorizationDecision struct {
	NotifyWarning bool
	RiskLevel     int32
	ACLID         string
	GroupID       string
}

type ReviewStatus struct {
	SQL       string    `json:"sql,omitempty"`
	State     string    `json:"state"`
	TicketURL string    `json:"ticket_url,omitempty"`
	Reviewers []string  `json:"reviewers,omitempty"`
	Processor string    `json:"processor,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	ErrorCode string    `json:"error_code,omitempty"`
}

// ConnectionConfig is server-only. Even accidental JSON encoding must not reveal
// credentials, keys or the database address to the browser.
type ConnectionConfig struct {
	Dialect      string    `json:"-"`
	OracleSYSDBA bool      `json:"-"`
	Host         string    `json:"-"`
	Port         int       `json:"-"`
	User         string    `json:"-"`
	Password     string    `json:"-"`
	Database     string    `json:"-"`
	Schema       string    `json:"-"`
	Transport    string    `json:"-"`
	TLS          TLSConfig `json:"-"`
}

type TLSConfig struct {
	Enabled            bool   `json:"-"`
	ServerName         string `json:"-"`
	InsecureSkipVerify bool   `json:"-"`
	Mode               string `json:"-"`
	CACert             string `json:"-"`
	ClientCert         string `json:"-"`
	ClientKey          string `json:"-"`
}

type Permissions struct {
	Connect  bool `json:"connect"`
	Upload   bool `json:"upload"`
	Download bool `json:"download"`
	Copy     bool `json:"copy"`
	Paste    bool `json:"paste"`
}

type Grant struct {
	ID          string           `json:"id"`
	OwnerID     string           `json:"owner_id"`
	OwnerName   string           `json:"owner_name"`
	AssetID     string           `json:"asset_id"`
	AssetName   string           `json:"asset_name"`
	AccountName string           `json:"account_name"`
	Config      ConnectionConfig `json:"-"`
	Permissions Permissions      `json:"permissions"`
	ExpiresAt   time.Time        `json:"expires_at"`
	MaxIdle     time.Duration    `json:"-"`
	MaxDuration time.Duration    `json:"-"`

	mu                 sync.Mutex
	session            *model.Session
	matchers           []commandMatcher
	maskers            []maskRule
	gateway            *gateway.DomainGateway
	created            time.Time
	lastActive         time.Time
	busy               int
	locked             bool
	controlUnavailable bool
	closed             bool
	connected          bool
	closeReason        string
	dynamicEnd         time.Time
	watchers           map[uint64]chan Control
	nextWatcher        uint64
	closeMu            sync.Mutex
	closePersisted     bool
	opening            bool // Open still owns cleanup if termination races initialization.
	terminationOwned   bool // engine must stop database work before remote cleanup
}

var (
	ErrClosed             = errors.New("session_closed")
	ErrLocked             = errors.New("session_locked")
	ErrExpired            = errors.New("permission_expired")
	ErrIdle               = errors.New("idle_disconnect")
	ErrDuration           = errors.New("max_session_timeout")
	ErrRevoked            = errors.New("permission_revoked")
	ErrAuditDisk          = errors.New("audit_persistence_failed")
	ErrControlUnavailable = errors.New("session_control_unavailable")
	// Returned only before any connection-token exchange has been attempted.
	ErrControlNotReady = errors.New("session_control_not_ready")
)

// NewLocalGrant initializes the same connection lifetime accounting used by
// managed grants, without a Core session. Identity display
// fields may be set by the caller before publishing the returned grant.
func NewLocalGrant(id string, config ConnectionConfig, permissions Permissions, maxIdle, maxDuration time.Duration) (*Grant, error) {
	if id == "" || maxIdle <= 0 || maxDuration <= 0 || maxIdle > maxDuration {
		return nil, errors.New("invalid_local_grant_lifetime")
	}
	now := time.Now()
	return &Grant{ID: id, Config: config, Permissions: permissions, ExpiresAt: now.Add(maxDuration), MaxIdle: maxIdle, MaxDuration: maxDuration, created: now, lastActive: now, watchers: make(map[uint64]chan Control)}, nil
}

// CloseLocal revokes a local grant immediately. The standalone resource owner
// remains responsible for persisting its close audit before forgetting identity.
func (g *Grant) CloseLocal() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
}

// Check is intentionally independent of the browser connection. Call it before
// all operations, including metadata and result reads, not only SQL execution.
func (g *Grant) Check() error {
	if g == nil {
		return ErrClosed
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.checkLocked(time.Now())
}

func (g *Grant) checkLocked(now time.Time) error {
	switch {
	case g.closed:
		return ErrClosed
	case !g.ExpiresAt.IsZero() && !now.Before(g.ExpiresAt):
		return ErrExpired
	case !g.dynamicEnd.IsZero() && !now.Before(g.dynamicEnd):
		return ErrRevoked
	case g.MaxDuration > 0 && !now.Before(g.created.Add(g.MaxDuration)):
		return ErrDuration
	case g.MaxIdle > 0 && g.busy == 0 && now.Sub(g.lastActive) >= g.MaxIdle:
		return ErrIdle
	case g.locked:
		return ErrLocked
	case g.controlUnavailable:
		return ErrControlUnavailable
	default:
		return nil
	}
}

func (g *Grant) Touch() {
	g.mu.Lock()
	g.lastActive = time.Now()
	g.mu.Unlock()
}

// SetBusy brackets database work. Completion counts as activity so a long query
// does not immediately trip the idle limit when it finishes.
func (g *Grant) SetBusy(busy bool) {
	g.mu.Lock()
	if busy {
		g.busy++
	} else {
		if g.busy > 0 {
			g.busy--
		}
		g.lastActive = time.Now()
	}
	g.mu.Unlock()
}

type AuditRecord struct {
	ID     string
	SQL    string
	Output string
	// ReplayOutput contains only the result values already masked for display.
	// Command audit keeps its summary while session replay retains the result.
	ReplayOutput string
	Error        string
	StartedAt    time.Time
	FinishedAt   time.Time
	RiskLevel    int32
	ACLID        string
	GroupID      string
	// The execution owner has classified this record, including normal risk.
	// Do not infer a warning again from SQL that may never have been sent.
	RiskClassified bool
}

type Control struct {
	Type   string `json:"type"`
	Reason string `json:"reason,omitempty"`
	Actor  string `json:"actor,omitempty"`
}

// PolicyError carries audit classification even though the SQL was not run.
type PolicyError struct {
	Code      string
	RiskLevel int32
	ACLID     string
	GroupID   string
}

func (e *PolicyError) Error() string { return e.Code }
