package sessiontools

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jumpserver/koko/pkg/srvconn"
)

// AgentSQLTransport bounds protocol input before a driver allocates rows and
// allows a close deadline to interrupt driver I/O even while sql.Conn is busy.
type AgentSQLTransport struct {
	mu          sync.Mutex
	connections map[net.Conn]struct{}
	dialContext databaseDialer
	closed      bool
	dialing     int
	remaining   atomic.Int64
}

func (t *AgentSQLTransport) ResetReadLimit() { t.remaining.Store(16 << 20) }
func (t *AgentSQLTransport) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	for conn := range t.connections {
		_ = conn.Close()
	}
}
func (t *AgentSQLTransport) dial(ctx context.Context, network, address string) (net.Conn, error) {
	t.mu.Lock()
	if t.closed || len(t.connections)+t.dialing >= 16 {
		t.mu.Unlock()
		return nil, errors.New("agent database transport is closed or at capacity")
	}
	t.dialing++
	t.mu.Unlock()
	dial := t.dialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	conn, err := dial(ctx, network, address)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dialing--
	if err != nil {
		return nil, err
	}
	if t.closed {
		_ = conn.Close()
		return nil, errors.New("agent database transport is closed")
	}
	if t.connections == nil {
		t.connections = make(map[net.Conn]struct{})
	}
	t.connections[conn] = struct{}{}
	return &agentSQLWireConn{Conn: conn, guard: t}, nil
}

type agentSQLWireConn struct {
	net.Conn
	guard *AgentSQLTransport
}

func (c *agentSQLWireConn) Close() error {
	err := c.Conn.Close()
	c.guard.mu.Lock()
	delete(c.guard.connections, c.Conn)
	c.guard.mu.Unlock()
	return err
}

func (c *agentSQLWireConn) Read(p []byte) (int, error) {
	remaining := c.guard.remaining.Load()
	if remaining <= 0 {
		_ = c.Conn.Close()
		return 0, errors.New("SQL response exceeded its wire limit")
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := c.Conn.Read(p)
	c.guard.remaining.Add(-int64(n))
	return n, err
}

// OpenAgentSQLConnection reuses existing TLS configuration and reserves one
// connection. A reconnect can never silently replace its transaction/context.
// The caller closes the reserved connection before cleanup releases its pool.
func OpenAgentSQLConnection(ctx context.Context, config DatabaseConfig) (*sql.Conn, *AgentSQLTransport, func() error, error) {
	guard := &AgentSQLTransport{dialContext: config.DialContext}
	guard.ResetReadLimit()
	config.DialContext = guard.dial
	var db *sql.DB
	switch config.Protocol {
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
		driverConfig := mysql.NewConfig()
		driverConfig.Net = "tcp"
		driverConfig.Addr = net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
		driverConfig.User = config.Username
		driverConfig.Passwd = config.Password
		driverConfig.DBName = config.Database
		driverConfig.ParseTime = true
		driverConfig.MultiStatements = false
		driverConfig.MaxAllowedPacket = 16 << 20
		driverConfig.Params = map[string]string{"autocommit": "true"}
		driverConfig.DialFunc = guard.dial
		if config.UseSSL {
			tlsConfig, err := buildMySQLTLSConfig(config)
			if err != nil {
				return nil, nil, nil, err
			}
			driverConfig.TLS = tlsConfig
		}
		connector, err := mysql.NewConnector(driverConfig)
		if err != nil {
			return nil, nil, nil, err
		}
		db = sql.OpenDB(connector)
	case srvconn.ProtocolPostgresql:
		dsn, cleanup, err := buildPostgreSQLDSN(config)
		if err != nil {
			return nil, nil, nil, err
		}
		pgConfig, err := pgx.ParseConfig(dsn)
		cleanup()
		if err != nil {
			return nil, nil, nil, err
		}
		configurePostgreSQLTLS(pgConfig, config)
		pgConfig.DialFunc = guard.dial
		pgConfig.BuildFrontend = func(reader io.Reader, writer io.Writer) *pgproto3.Frontend {
			frontend := pgproto3.NewFrontend(reader, writer)
			frontend.SetMaxBodyLen(8 << 20)
			return frontend
		}
		pgConfig.StatementCacheCapacity = 0
		pgConfig.DescriptionCacheCapacity = 0
		pgConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
		db = stdlib.OpenDB(*pgConfig)
	case srvconn.ProtocolSQLServer, srvconn.ProtocolOracle, srvconn.ProtocolClickHouse:
		var err error
		db, err = newNativeSQLDatabase(config)
		if err != nil {
			guard.Close()
			return nil, nil, nil, err
		}
	default:
		return nil, nil, nil, errors.New("native agent SQL connection is unsupported")
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	cleanup := func() error { guard.Close(); return db.Close() }
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, nil, errors.Join(err, cleanup())
	}
	if err = conn.PingContext(ctx); err != nil {
		return nil, nil, nil, errors.Join(err, conn.Close(), cleanup())
	}
	return conn, guard, cleanup, nil
}

// OpenAgentDatabaseExecutor reuses the bounded, stateless Redis/MongoDB tools.
// Automatic command retries are disabled for the agent dispatch contract.
func OpenAgentDatabaseExecutor(ctx context.Context, config DatabaseConfig) (CommandExecutor, *AgentSQLTransport, error) {
	guard := &AgentSQLTransport{dialContext: config.DialContext}
	guard.ResetReadLimit()
	config.DialContext, config.agent = guard.dial, true
	executor, err := NewDatabaseExecutor(ctx, config)
	if err != nil {
		guard.Close()
		return nil, nil, err
	}
	return executor, guard, nil
}

// AgentSQLReturnsRows validates a single SQL statement without imposing the
// browser background executor's session-independent policy. Authorization is
// performed separately against the immutable original statement.
func AgentSQLReturnsRows(command, protocol string) (bool, error) {
	if len(command) == 0 || len(command) > 64<<10 {
		return false, errors.New("SQL statement exceeds the size limit")
	}
	analysis, err := analyzeSQL(command, protocol)
	if err != nil {
		return false, err
	}
	if analysis.multi || analysis.incomplete || analysis.kind == sqlUnknown {
		return false, errors.New("one complete supported SQL statement is required")
	}
	switch analysis.keyword {
	case "SELECT", "SHOW", "DESC", "DESCRIBE", "EXPLAIN", "VALUES", "TABLE", "EXEC", "EXECUTE", "FETCH", "CALL", "EXISTS", "CHECK":
		return true, nil
	}
	return containsTopLevelSQLWord(analysis.words, analysis.depths, "RETURNING") || containsTopLevelSQLWord(analysis.words, analysis.depths, "OUTPUT"), nil
}
