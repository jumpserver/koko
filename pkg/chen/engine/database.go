package engine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jumpserver/koko/internal/dbconn"
	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/dialect"
)

func makeTLS(c core.ConnectionConfig) (*tls.Config, error) {
	return dbconn.MakeTLS(c, false)
}

func openDatabase(c core.ConnectionConfig, max int) (*sql.DB, error) {
	return regularPool.openDatabase(c, max)
}

func (pool databasePool) openDatabase(c core.ConnectionConfig, max int) (*sql.DB, error) {
	t, err := makeTLS(c)
	if err != nil {
		return nil, databaseTLSError{err}
	}
	var db *sql.DB
	switch dialect.ID(strings.ToLower(c.Dialect)) {
	case dialect.MySQL, dialect.MariaDB:
		c.DialContext = databaseDialer{dial: c.DialContext}.DialContext
		connector, e := dbconn.MySQLConnector(c, dbconn.MySQLOptions{
			TLS: t, ParseTime: false, ClientFoundRows: true,
			Timeout: databaseDialTimeout, ReadTimeout: pool.readTimeout(), WriteTimeout: databaseWriteTimeout,
		})
		if e != nil {
			return nil, errors.New("invalid MySQL connection configuration")
		}
		db = openConnector(connector)
	case dialect.PostgreSQL:
		if os.Getenv("PGSERVICE") != "" {
			return nil, errors.New("PGSERVICE cannot override server-managed database connections")
		}
		cfg, e := pgx.ParseConfig("host=127.0.0.1 port=5432 user=chen password='' dbname=chen sslmode=disable sslrootcert='' sslcert='' sslkey='' sslpassword='' sslnegotiation=postgres target_session_attrs=any connect_timeout=10")
		if e != nil {
			return nil, errors.New("invalid PostgreSQL connection configuration")
		}
		cfg.Host = c.Host
		cfg.Port = uint16(c.Port)
		cfg.User = c.User
		cfg.Password = c.Password
		cfg.Database = c.Database
		if cfg.Database == "" {
			// Match the PostgreSQL JDBC default used by Java Chen: an omitted
			// database selects the authenticated user's database.
			cfg.Database = cfg.User
		}
		cfg.TLSConfig = t
		cfg.Fallbacks = nil
		cfg.DialFunc = databaseDialer{dial: cfg.DialFunc, readTimeout: pool.readTimeout(), writeTimeout: databaseWriteTimeout}.DialContext
		configurePostgresTransport(cfg, c, t)
		cfg.SSLNegotiation = "postgres"
		cfg.ConnectTimeout = databaseDialTimeout
		cfg.RuntimeParams = map[string]string{"application_name": "Chen Go"}
		cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
		cfg.BuildContextWatcherHandler = func(conn *pgconn.PgConn) ctxwatch.Handler {
			return &pgconn.CancelRequestContextWatcherHandler{Conn: conn, CancelRequestDelay: 0, DeadlineDelay: 3 * time.Second}
		}
		db = openConnector(stdlib.GetConnector(*cfg))
	default:
		db, err = pool.extraDatabase(c, t)
		if err != nil {
			return nil, err
		}
	}
	db.SetMaxOpenConns(max)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return db, nil
}

// Negotiate in DialFunc so pgx cancellation uses the same protected transport.
// pgx's generic TLS fallback retries certificate failures in plaintext, and its
// default CancelRequest opens a plain socket even for a TLS-authenticated session.
func configurePostgresTransport(cfg *pgx.ConnConfig, c core.ConnectionConfig, t *tls.Config) {
	if !c.TLS.Enabled || t == nil {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(c.TLS.Mode))
	if mode == "allow" {
		// Existing PostgreSQL configurations may request plaintext first with sslmode=allow.
		cfg.TLSConfig = nil
		cfg.Fallbacks = []*pgconn.FallbackConfig{{Host: c.Host, Port: uint16(c.Port), TLSConfig: t}}
		return
	}
	dial := cfg.DialFunc
	cfg.TLSConfig, cfg.Fallbacks = nil, nil
	cfg.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		secure, err := postgresTLSConnection(ctx, conn, t, mode == "prefer")
		if err != nil {
			conn.Close()
			return nil, databaseTLSError{err}
		}
		return secure, nil
	}
}

func postgresTLSConnection(ctx context.Context, conn net.Conn, cfg *tls.Config, prefer bool) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, databaseLoginTimeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()); close(stopped) })
	defer func() {
		if !stop() {
			<-stopped
		}
		_ = conn.SetDeadline(time.Time{})
	}()
	request := make([]byte, 8)
	binary.BigEndian.PutUint32(request[:4], 8)
	binary.BigEndian.PutUint32(request[4:], 80877103)
	for len(request) > 0 {
		n, err := conn.Write(request)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, io.ErrShortWrite
		}
		request = request[n:]
	}
	var reply [1]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return nil, err
	}
	if reply[0] == 'N' && prefer {
		return conn, nil
	}
	if reply[0] != 'S' {
		return nil, errors.New("PostgreSQL backend refused TLS")
	}
	secure := tls.Client(conn, cfg)
	if err := secure.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	return secure, nil
}

type databaseTLSError struct{ error }

func safeConnectionError(err error, message string) *APIError {
	if diagnostic := safeDatabaseError(err); diagnostic != nil && diagnostic.Code == "database_tls_error" {
		return diagnostic
	}
	return &APIError{"connection_failed", message}
}

func safeDatabaseError(err error) *APIError {
	if err == nil {
		return nil
	}
	var configError databaseTLSError
	var verificationError *tls.CertificateVerificationError
	var hostnameError x509.HostnameError
	var authorityError x509.UnknownAuthorityError
	var invalidCertificate x509.CertificateInvalidError
	if errors.As(err, &configError) || errors.As(err, &verificationError) || errors.As(err, &hostnameError) ||
		errors.As(err, &authorityError) || errors.As(err, &invalidCertificate) {
		return &APIError{"database_tls_error", "Database TLS failed; check the asset SSL mode, CA, certificate hostname/expiry and client certificate/key."}
	}
	// Driver errors are useful SQL diagnostics; connection strings and network
	// errors are never reflected. SQL text is already owned by this session.
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return &APIError{fmt.Sprintf("MYSQL_%d", me.Number), me.Message}
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return &APIError{pe.Code, pe.Message}
	}
	if api := extraDatabaseError(err); api != nil {
		return api
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &APIError{"query_timeout", "Query deadline exceeded"}
	}
	if errors.Is(err, context.Canceled) {
		return &APIError{"query_canceled", "Query cancellation requested"}
	}
	return &APIError{"database_error", "Database operation failed; the connection may no longer be usable"}
}
