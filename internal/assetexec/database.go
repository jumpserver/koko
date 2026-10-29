package assetexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/internal/sessiontools"
	"github.com/jumpserver/koko/pkg/srvconn"
)

type databaseState struct {
	mu        sync.Mutex
	cancel    context.CancelFunc
	sanitizer sessiontools.ValueSanitizer
	transport *sessiontools.AgentSQLTransport
}

func (c *Connection) openDatabase(ctx context.Context, token *model.ConnectToken) error {
	ctx, cancel := context.WithTimeout(ctx, c.opts.ConnectTimeout)
	defer cancel()
	asset := token.Asset
	host, port := asset.Address, asset.ProtocolPort(token.Protocol)
	var tunnel *databaseTunnel
	if token.Gateway != nil {
		var err error
		tunnel, err = openDatabaseTunnel(ctx, token.Gateway, net.JoinHostPort(host, strconv.Itoa(port)), c.opts.ConnectTimeout)
		if err != nil {
			return err
		}
		host = "127.0.0.1"
		port = tunnel.listener.Addr().(*net.TCPAddr).Port
	}
	config := sessiontools.DatabaseConfig{
		Protocol: token.Protocol, Host: host, Port: port, ServerName: asset.Address,
		Username: token.Account.Username, Password: token.Account.Secret, Database: asset.SpecInfo.DBName,
		UseSSL: asset.SpecInfo.UseSSL, PGSSLMode: asset.SpecInfo.PgSSLMode,
		CACert: asset.SecretInfo.CaCert, ClientCert: asset.SecretInfo.ClientCert, ClientKey: asset.SecretInfo.ClientKey,
		AllowInvalidCert: asset.SpecInfo.AllowInvalidCert,
	}
	var cleanup func() error
	var err error
	c.dbState = &databaseState{sanitizer: sessiontools.NewMySQLSanitizer(token.DataMaskingRules)}
	c.sqlConn, c.dbState.transport, cleanup, err = sessiontools.OpenAgentSQLConnection(ctx, config)
	if err != nil {
		if tunnel != nil {
			tunnel.Close()
		}
		return err
	}
	c.cleanup = func() error {
		err := cleanup()
		if tunnel != nil {
			tunnel.Close()
		}
		return err
	}
	return nil
}

func (c *Connection) executeSQL(ctx context.Context, command string, emit Emit) (result Result, returnedErr error) {
	query, err := sessiontools.AgentSQLReturnsRows(command, c.protocol)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	c.dbState.mu.Lock()
	c.dbState.cancel = cancel
	c.dbState.mu.Unlock()
	defer func() { cancel(); c.dbState.mu.Lock(); c.dbState.cancel = nil; c.dbState.mu.Unlock() }()
	defer func() {
		if ctx.Err() != nil {
			c.closed.Store(true)
			c.dbState.transport.Close()
			result.ContextLost = true
		}
	}()
	c.dbState.transport.ResetReadLimit()
	result = Result{Dispatched: true}
	if !query {
		executed, execErr := c.sqlConn.ExecContext(ctx, command)
		if execErr != nil {
			return c.databaseError(ctx, result, execErr)
		}
		result.Completed = true
		if affected, affectedErr := executed.RowsAffected(); affectedErr == nil {
			result.RowsAffected = &affected
			err = emit("stdout", []byte(fmt.Sprintf("Rows affected: %d\n", affected)))
		} else {
			err = emit("stdout", []byte("Statement completed\n"))
		}
		return result, err
	}
	rows, err := c.sqlConn.QueryContext(ctx, command)
	if err != nil {
		return c.databaseError(ctx, result, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return result, err
	}
	if len(columns) > 1024 {
		cancel()
		result.Truncated = true
		return result, ErrOutputLimit
	}
	sink := &outputSink{emit: emit, remaining: c.opts.MaxOutputBytes, cancel: cancel}
	if len(columns) > 0 {
		if _, err = sink.write("stdout", []byte(strings.Join(columns, "\t")+"\n")); err != nil {
			return result, err
		}
	}
	values := make([]any, len(columns))
	dest := make([]any, len(columns))
	for index := range values {
		dest[index] = &values[index]
	}
	count := 0
	for rows.Next() {
		if count >= c.opts.MaxRows {
			result.Truncated = true
			cancel()
			return result, ErrOutputLimit
		}
		if err = rows.Scan(dest...); err != nil {
			return result, err
		}
		var line strings.Builder
		for index, value := range values {
			if index > 0 {
				line.WriteByte('\t')
			}
			text := "NULL"
			if value != nil {
				switch v := value.(type) {
				case []byte:
					text = string(v)
				case time.Time:
					text = v.Format(time.RFC3339Nano)
				default:
					text = fmt.Sprint(v)
				}
				text = c.dbState.sanitizer.Sanitize(columns[index], text)
				text = strings.NewReplacer("\t", `\t`, "\r", `\r`, "\n", `\n`).Replace(strings.ToValidUTF8(text, "\uFFFD"))
			}
			if int64(line.Len()+len(text)+1) > sink.remaining {
				result.Truncated = true
				cancel()
				return result, ErrOutputLimit
			}
			line.WriteString(text)
		}
		line.WriteByte('\n')
		if _, err = sink.write("stdout", []byte(line.String())); err != nil {
			result.Truncated = sink.truncated
			return result, err
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return c.databaseError(ctx, result, err)
	}
	if rows.NextResultSet() {
		result.Truncated = true
		cancel()
		return result, errors.New("multiple result sets require a dedicated operation")
	}
	if err = rows.Err(); err != nil {
		return c.databaseError(ctx, result, err)
	}
	result.Completed = true
	return result, nil
}

func (c *Connection) databaseError(ctx context.Context, result Result, err error) (Result, error) {
	var mysqlErr *mysql.MySQLError
	var pgErr *pgconn.PgError
	// A database error response proves the server handled the statement. A
	// network/cancellation error does not establish whether a write committed.
	if errors.As(err, &mysqlErr) || errors.As(err, &pgErr) {
		result.Completed = true
	}
	if ctx.Err() != nil {
		result.Completed = false
	}
	healthCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if pingErr := c.sqlConn.PingContext(healthCtx); pingErr != nil {
		result.ContextLost = true
		c.closed.Store(true)
	}
	return result, err
}

func (c *Connection) closeDatabase() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// The transport is closed at the deadline even if a driver is still
	// holding sql.Conn's lock while processing a cancellation response.
	stop := context.AfterFunc(ctx, c.dbState.transport.Close)
	defer stop()
	if c.dbState != nil {
		c.dbState.mu.Lock()
		if c.dbState.cancel != nil {
			c.dbState.cancel()
		}
		c.dbState.mu.Unlock()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dbState.transport.ResetReadLimit()
	_, rollbackErr := c.sqlConn.ExecContext(ctx, "ROLLBACK")
	c.dbState.transport.Close()
	return errors.Join(rollbackErr, c.sqlConn.Close())
}

// databaseTunnel is a bounded, single-consumer SSH forwarding listener. It
// never exposes a proxy to the caller or accepts a destination from tool input.
type databaseTunnel struct {
	listener    net.Listener
	client      *srvconn.SSHClient
	destination string
	once        sync.Once
	mu          sync.Mutex
	connections map[net.Conn]struct{}
	slots       chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
}

func openDatabaseTunnel(ctx context.Context, gateway *model.Gateway, destination string, timeout time.Duration) (*databaseTunnel, error) {
	account := gateway.Account
	opts := []srvconn.SSHClientOption{srvconn.SSHClientContext(ctx), srvconn.SSHClientHost(gateway.Address), srvconn.SSHClientPort(gateway.Protocols.GetProtocolPort(model.ProtocolSSH)), srvconn.SSHClientUsername(account.Username), srvconn.SSHClientTimeout(int(timeout / time.Second))}
	if account.IsSSHKey() {
		opts = append(opts, srvconn.SSHClientPrivateKey(account.Secret))
	} else {
		opts = append(opts, srvconn.SSHClientPassword(account.Secret))
	}
	client, err := srvconn.NewSSHClient(opts...)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	tunnel := &databaseTunnel{listener: listener, client: client, destination: destination, connections: make(map[net.Conn]struct{}), slots: make(chan struct{}, 2), ctx: lifetime, cancel: cancel}
	go tunnel.serve()
	if err = ctx.Err(); err != nil {
		tunnel.Close()
		return nil, err
	}
	return tunnel, nil
}

func (t *databaseTunnel) serve() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			return
		}
		select {
		case t.slots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		t.mu.Lock()
		if t.ctx.Err() != nil {
			t.mu.Unlock()
			_ = conn.Close()
			<-t.slots
			return
		}
		t.connections[conn] = struct{}{}
		t.mu.Unlock()
		go t.forward(conn)
	}
}

func (t *databaseTunnel) forward(local net.Conn) {
	defer func() { _ = local.Close(); t.mu.Lock(); delete(t.connections, local); t.mu.Unlock(); <-t.slots }()
	ctx, cancel := context.WithTimeout(t.ctx, 20*time.Second)
	defer cancel()
	remote, err := t.client.DialContext(ctx, "tcp", t.destination)
	if err != nil {
		return
	}
	defer remote.Close()
	done := make(chan struct{})
	go func() { _, _ = io.CopyBuffer(remote, local, make([]byte, 32<<10)); _ = remote.Close(); close(done) }()
	_, _ = io.CopyBuffer(local, remote, make([]byte, 32<<10))
	_ = local.Close()
	<-done
}

func (t *databaseTunnel) Close() {
	t.once.Do(func() {
		t.cancel()
		_ = t.listener.Close()
		_ = t.client.Close()
		t.mu.Lock()
		defer t.mu.Unlock()
		for conn := range t.connections {
			_ = conn.Close()
		}
	})
}
