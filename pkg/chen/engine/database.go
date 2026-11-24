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
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jumpserver/koko/pkg/chen/core"
)

func makeTLS(c core.ConnectionConfig) (*tls.Config, error) {
	if !c.TLS.Enabled {
		return nil, nil
	}
	mode := strings.ToLower(strings.TrimSpace(c.TLS.Mode))
	if c.Dialect != "postgresql" {
		mode = ""
	}
	switch mode {
	case "", "require", "verify-ca", "verify-full", "prefer", "allow":
	case "disable":
		return nil, nil
	default:
		return nil, errors.New("unsupported database TLS mode; select require, verify-ca or verify-full")
	}
	t := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.TLS.ServerName, InsecureSkipVerify: c.TLS.InsecureSkipVerify}
	if t.ServerName == "" {
		t.ServerName = c.Host
	}
	if c.TLS.CACert != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(c.TLS.CACert)) {
			return nil, errors.New("invalid database CA certificate")
		}
		t.RootCAs = pool
	}
	if mode == "verify-full" || mode == "verify-ca" {
		if c.TLS.InsecureSkipVerify {
			return nil, errors.New("certificate verification mode conflicts with allow-invalid-certificate")
		}
		t.InsecureSkipVerify = false
	}
	// libpq require guarantees encryption; a supplied root CA also verifies the
	// chain, without hostname validation. verify-full uses Go's normal hostname
	// and chain verification. No plaintext fallback is configured.
	if mode == "verify-ca" || ((mode == "require" || mode == "prefer" || mode == "allow") && c.TLS.CACert != "") {
		t.InsecureSkipVerify = true
		t.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("database certificate missing")
			}
			intermediates := x509.NewCertPool()
			for _, cert := range state.PeerCertificates[1:] {
				intermediates.AddCert(cert)
			}
			_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: t.RootCAs, Intermediates: intermediates})
			return err
		}
	} else if mode == "require" || mode == "prefer" || mode == "allow" {
		t.InsecureSkipVerify = true
	}
	if c.TLS.ClientCert != "" || c.TLS.ClientKey != "" {
		cert, err := tls.X509KeyPair([]byte(c.TLS.ClientCert), []byte(c.TLS.ClientKey))
		if err != nil {
			return nil, errors.New("invalid database client certificate")
		}
		t.Certificates = []tls.Certificate{cert}
	}
	return t, nil
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
	switch strings.ToLower(c.Dialect) {
	case "mysql", "mariadb":
		cfg := mysql.NewConfig()
		cfg.Net = "tcp"
		cfg.Addr = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
		cfg.User = c.User
		cfg.Passwd = c.Password
		cfg.DBName = c.Database
		// Keep MySQL date/time text, including zero dates and timezone-free DATETIME.
		cfg.ParseTime = false
		// Match JDBC's matched-row counts, including updates to the same value.
		cfg.ClientFoundRows = true
		cfg.Loc = time.UTC
		cfg.Timeout = 10 * time.Second
		cfg.ReadTimeout = pool.readTimeout(2 * time.Minute)
		cfg.WriteTimeout = 15 * time.Second
		cfg.TLS = t
		connector, e := mysql.NewConnector(cfg)
		if e != nil {
			return nil, errors.New("invalid MySQL connection configuration")
		}
		db = pool.openConnector(connector, 2*time.Minute)
	case "postgresql":
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
		if pool == catalogPool {
			cfg.DialFunc = catalogDialer{dial: cfg.DialFunc}.DialContext
		}
		configurePostgresTransport(cfg, c, t)
		cfg.SSLNegotiation = "postgres"
		cfg.ConnectTimeout = 10 * time.Second
		cfg.RuntimeParams = map[string]string{"application_name": "Chen Go"}
		cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
		cfg.BuildContextWatcherHandler = func(conn *pgconn.PgConn) ctxwatch.Handler {
			return &pgconn.CancelRequestContextWatcherHandler{Conn: conn, CancelRequestDelay: 0, DeadlineDelay: 3 * time.Second}
		}
		db = pool.openConnector(stdlib.GetConnector(*cfg), cfg.ConnectTimeout)
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
		// Legacy standalone-only plaintext-first mode; not an asset UI choice.
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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
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
