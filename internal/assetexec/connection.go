// Package assetexec implements bounded asset I/O for already-authorized
// connection tokens. Authorization, durable dispatch and audit delivery belong
// to the calling execution service; this package never obtains user credentials.
package assetexec

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/internal/sessiontools"
	"github.com/jumpserver/koko/pkg/srvconn"
)

var (
	ErrUnsupported = errors.New("asset operation is unsupported")
	ErrPermission  = errors.New("asset operation is not permitted")
	ErrContextLost = errors.New("asset connection context is no longer available")
	ErrOutputLimit = errors.New("asset output exceeded its evidence limit")
)

// These are execution effects, not grants. Core still authorizes each asset,
// account, protocol and action before the node can execute it.
var protocolModes = map[string]string{
	srvconn.ProtocolSSH: "shell", srvconn.ProtocolSFTP: "files",
	srvconn.ProtocolMySQL: "database", srvconn.ProtocolMariadb: "database", srvconn.ProtocolPostgresql: "database",
	srvconn.ProtocolSQLServer: "database", srvconn.ProtocolOracle: "database", srvconn.ProtocolClickHouse: "database",
	srvconn.ProtocolRedis: "database", srvconn.ProtocolMongoDB: "database",
}

func ProtocolModes() map[string]string    { return maps.Clone(protocolModes) }
func ProtocolMode(protocol string) string { return protocolModes[protocol] }

type Options struct {
	JMS            *service.JMService
	RemoteAddr     string
	LoginFrom      string
	ConnectTimeout time.Duration
	MaxOutputBytes int64
	MaxFileBytes   int64
	MaxRows        int
}

// Request is immutable after approval. Cwd and Env apply only to this command.
type Request struct {
	Command string            `json:"command"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Emit must durably accept each chunk or return an error. Calls are serialized.
type Emit func(stream string, chunk []byte) error

type Result struct {
	Dispatched    bool   `json:"dispatched"`
	Completed     bool   `json:"completed"`
	ExitCode      *int   `json:"exit_code,omitempty"`
	RowsAffected  *int64 `json:"rows_affected,omitempty"`
	Truncated     bool   `json:"truncated,omitempty"`
	StopConfirmed bool   `json:"stop_confirmed,omitempty"`
	ContextLost   bool   `json:"context_lost,omitempty"`
}

type Connection struct {
	protocol  string
	opts      Options
	actions   model.Actions
	mu        sync.Mutex
	closed    atomic.Bool
	closeOnce sync.Once
	ssh       *srvconn.SSHClient
	executor  sessiontools.CommandExecutor
	sftp      *srvconn.UserSftpConn
	sqlConn   *sql.Conn
	dbState   *databaseState
	cleanup   func() error
	closeErr  error
}

func Open(ctx context.Context, token *model.ConnectToken, opts Options) (*Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if token == nil || token.Id == "" || token.User.ID == "" || token.Asset.ID == "" || !token.Actions.EnableConnect() {
		return nil, ErrPermission
	}
	if token.FaceMonitorToken != "" {
		return nil, fmt.Errorf("%w: continuous face monitoring requires the controlled terminal", ErrUnsupported)
	}
	if token.Account.SuFrom != nil {
		return nil, fmt.Errorf("%w: account switching requires the controlled terminal", ErrUnsupported)
	}
	if !token.Asset.IsSupportProtocol(token.Protocol) {
		return nil, ErrPermission
	}
	if err := supportedCredential(token.Account.SecretType.Value, token.Protocol == srvconn.ProtocolSSH || token.Protocol == srvconn.ProtocolSFTP); err != nil {
		return nil, err
	}
	if token.Gateway != nil {
		if err := supportedCredential(token.Gateway.Account.SecretType.Value, true); err != nil {
			return nil, err
		}
		if token.Gateway.Account.IsSSHCertificate() {
			return nil, fmt.Errorf("%w: gateway SSH certificate requires a gateway-scoped credential", ErrUnsupported)
		}
	}
	if opts.ConnectTimeout <= 0 || opts.ConnectTimeout > time.Minute {
		opts.ConnectTimeout = 20 * time.Second
	}
	if opts.MaxOutputBytes <= 0 || opts.MaxOutputBytes > 64<<20 {
		opts.MaxOutputBytes = 8 << 20
	}
	if opts.MaxFileBytes <= 0 || opts.MaxFileBytes > 1<<30 {
		opts.MaxFileBytes = 64 << 20
	}
	if opts.MaxRows <= 0 || opts.MaxRows > 10000 {
		opts.MaxRows = 1000
	}
	c := &Connection{protocol: token.Protocol, actions: append(model.Actions(nil), token.Actions...), opts: opts}
	var err error
	switch c.protocol {
	case srvconn.ProtocolSSH:
		connectCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeout)
		c.ssh, err = srvconn.NewSSHClientWithTokenContext(connectCtx, token, int(opts.ConnectTimeout/time.Second))
		cancel()
	case srvconn.ProtocolSFTP:
		err = c.openSFTP(ctx, token)
	default:
		if ProtocolMode(c.protocol) == "database" {
			err = c.openDatabase(ctx, token)
		} else {
			err = fmt.Errorf("%w: protocol %s", ErrUnsupported, c.protocol)
		}
	}
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Connection) Protocol() string { return c.protocol }

func supportedCredential(kind string, ssh bool) error {
	if kind == "" || kind == "password" || (ssh && (kind == "ssh_key" || kind == "ssh_certificate")) {
		return nil
	}
	return fmt.Errorf("%w: credential requires the controlled terminal", ErrUnsupported)
}

func (c *Connection) Execute(ctx context.Context, request Request, emit Emit) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() {
		return Result{ContextLost: true}, ErrContextLost
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if emit == nil {
		return Result{}, errors.New("durable output consumer is required")
	}
	switch ProtocolMode(c.protocol) {
	case "shell":
		return c.executeSSH(ctx, request, emit)
	case "database":
		if request.Cwd != "" || len(request.Env) != 0 {
			return Result{}, errors.New("cwd and env apply only to SSH")
		}
		if c.protocol == srvconn.ProtocolRedis || c.protocol == srvconn.ProtocolMongoDB {
			return c.executeDatabaseCommand(ctx, request.Command, emit)
		}
		return c.executeSQL(ctx, request.Command, emit)
	default:
		return Result{}, ErrUnsupported
	}
}

func (c *Connection) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		// Closing transports also unblocks operations in progress. Do not wait
		// for the operation mutex before closing a blocked SSH/SFTP transport.
		if c.sftp != nil {
			c.sftp.InterruptTransports()
			c.sftp.Close()
		}
		if c.ssh != nil {
			c.closeErr = c.ssh.Close()
		}
		if c.executor != nil {
			c.dbState.transport.Close()
			c.closeErr = c.executor.Close()
		}
		if c.sqlConn != nil {
			c.closeErr = c.closeDatabase()
		}
		if c.cleanup != nil {
			c.closeErr = errors.Join(c.closeErr, c.cleanup())
		}
	})
	return c.closeErr
}
