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

	"github.com/ClickHouse/clickhouse-go/v2"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/internal/sessiontools"
	"github.com/jumpserver/koko/pkg/srvconn"
	mssql "github.com/microsoft/go-mssqldb"
	obDriver "github.com/oceanbase/go-oceanbase-driver"
	oracle "github.com/sijms/go-ora/v2/network"
)

type databaseState struct {
	mu        sync.Mutex
	cancel    context.CancelFunc
	sanitizer sessiontools.ValueSanitizer
	transport *sessiontools.AgentSQLTransport
	dialect   string
}

func (c *Connection) openDatabase(ctx context.Context, token *model.ConnectToken) error {
	ctx, cancel := context.WithTimeout(ctx, c.opts.ConnectTimeout)
	defer cancel()
	asset := token.Asset
	config := sessiontools.DatabaseConfig{
		Protocol: token.Protocol, PlatformType: token.Platform.Type.Value,
		Host: asset.Address, Port: asset.ProtocolPort(token.Protocol), ServerName: asset.Address,
		Username: token.Account.Username, Password: token.Account.Secret, Database: asset.SpecInfo.DBName,
		UseSSL: asset.SpecInfo.UseSSL, PGSSLMode: asset.SpecInfo.PgSSLMode,
		CACert: asset.SecretInfo.CaCert, ClientCert: asset.SecretInfo.ClientCert, ClientKey: asset.SecretInfo.ClientKey,
		AllowInvalidCert: asset.SpecInfo.AllowInvalidCert,
		DataMaskingRules: token.DataMaskingRules,
	}
	if setting, ok := token.Platform.GetProtocolSetting(token.Protocol); ok {
		options := setting.GetSetting()
		config.Encrypt = options.Encrypt
		config.DisableEncrypt = token.Protocol == srvconn.ProtocolSQLServer && !options.Encrypt
		config.ClusterMode = options.EnableClusterMode
		config.AuthSource, config.ConnectionOpts = options.AuthSource, options.ConnectionOpts
		if config.PlatformType == "oceanbase" {
			config.TenantMode, _ = setting.Setting["tenant_mode"].(string)
		}
		if token.Protocol == srvconn.ProtocolRedis {
			if raw, exists := setting.Setting["enable_cluster_mode"]; exists {
				config.ClusterMode, _ = strconv.ParseBool(fmt.Sprint(raw))
			}
			if token.Account.IsNull() || !options.AuthUsername {
				config.Username = ""
			}
		}
	}
	dialect, err := sessiontools.DatabaseDialect(config)
	if err != nil {
		return err
	}
	var tunnel *gatewayTunnel
	if token.Gateway != nil {
		tunnel, err = openGatewayTunnel(ctx, token.Gateway, net.JoinHostPort(config.Host, strconv.Itoa(config.Port)), c.opts.ConnectTimeout)
		if err != nil {
			return err
		}
		config.Host, config.Port = "127.0.0.1", tunnel.listener.Addr().(*net.TCPAddr).Port
	}
	if tunnel != nil && (token.Protocol == srvconn.ProtocolSQLServer || token.Protocol == srvconn.ProtocolRedis || token.Protocol == srvconn.ProtocolMongoDB) {
		// Routing and discovery may return other hosts. Keep every driver
		// connection on the authorized gateway, including redirected logins.
		config.Host, config.Port = asset.Address, asset.ProtocolPort(token.Protocol)
		config.DialContext = tunnel.client.DialContext
	}
	var cleanup func() error
	c.dbState = &databaseState{dialect: dialect, sanitizer: sessiontools.NewMySQLSanitizer(token.DataMaskingRules)}
	if token.Protocol == srvconn.ProtocolRedis || token.Protocol == srvconn.ProtocolMongoDB {
		c.executor, c.dbState.transport, err = sessiontools.OpenAgentDatabaseExecutor(ctx, config)
		cleanup = func() error { return nil }
	} else {
		c.sqlConn, c.dbState.transport, cleanup, err = sessiontools.OpenAgentSQLConnection(ctx, config)
	}
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
	query, err := sessiontools.AgentSQLReturnsRows(command, c.dbState.dialect)
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
	var oceanBaseErr *obDriver.MySQLError
	var pgErr *pgconn.PgError
	var mssqlErr mssql.Error
	var oracleErr *oracle.OracleError
	var clickhouseErr *clickhouse.Exception
	// A database error response proves the server handled the statement. A
	// network/cancellation error does not establish whether a write committed.
	if errors.As(err, &mysqlErr) || errors.As(err, &oceanBaseErr) || errors.As(err, &pgErr) || errors.As(err, &mssqlErr) ||
		(errors.As(err, &oracleErr) && !oracleErr.Bad()) || errors.As(err, &clickhouseErr) {
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
	var rollbackErr error
	if c.protocol != srvconn.ProtocolClickHouse {
		_, rollbackErr = c.sqlConn.ExecContext(ctx, "ROLLBACK")
	}
	c.dbState.transport.Close()
	return errors.Join(rollbackErr, c.sqlConn.Close())
}

// gatewayTunnel is a bounded, single-asset SSH forwarding listener. It
// never exposes a proxy to the caller or accepts a destination from tool input.
type gatewayTunnel struct {
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

func openGatewayTunnel(ctx context.Context, gateway *model.Gateway, destination string, timeout time.Duration) (*gatewayTunnel, error) {
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
	tunnel := &gatewayTunnel{listener: listener, client: client, destination: destination, connections: make(map[net.Conn]struct{}), slots: make(chan struct{}, 2), ctx: lifetime, cancel: cancel}
	go tunnel.serve()
	if err = ctx.Err(); err != nil {
		tunnel.Close()
		return nil, err
	}
	return tunnel, nil
}

func (t *gatewayTunnel) serve() {
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

func (t *gatewayTunnel) forward(local net.Conn) {
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

func (t *gatewayTunnel) Close() {
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
