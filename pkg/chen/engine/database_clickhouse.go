package engine

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

// ClickHouse HTTP sends a database URL parameter on every request. Java's
// JDBC driver updates that client-side selection after a successful USE;
// clickhouse-go leaves it fixed at login. Keep the selection per physical
// connection so consoles and the independent catalog cannot affect each other.
type clickHouseHTTPConnector struct{ options clickhouse.Options }

// Managed custom ports have no transport encoded in the asset. Only the
// server's explicit Native-port HTTP response can select Native, before any
// business SQL is sent. Auth, TLS and timeout errors never select a protocol.
type clickHouseAutoConnector struct {
	options clickhouse.Options
	pool    databasePool
}

func (c *clickHouseAutoConnector) Driver() driver.Driver {
	return clickhouse.Connector(&c.options).Driver()
}

func (c *clickHouseAutoConnector) Connect(ctx context.Context) (driver.Conn, error) {
	options := c.options
	options.Protocol = clickhouse.HTTP
	conn, err := (&clickHouseHTTPConnector{options: options}).Connect(ctx)
	var httpError *clickhouse.HTTPError
	if ctx.Err() != nil || !errors.As(err, &httpError) || httpError.StatusCode != http.StatusBadRequest ||
		!strings.Contains(httpError.Error(), "is for clickhouse-client program") ||
		!strings.Contains(httpError.Error(), "for HTTP.") {
		return conn, err
	}
	options.Protocol = clickhouse.Native
	if c.pool == catalogPool {
		return catalogClickHouseNative(options).Connect(ctx)
	}
	return clickhouse.Connector(&options).Connect(ctx)
}

func catalogClickHouseHTTPTransport(rt *http.Transport) (http.RoundTripper, error) {
	rt.ResponseHeaderTimeout = 0
	rt.DialContext = catalogDialer{dial: rt.DialContext, http: true, writeTimeout: 15 * time.Second}.DialContext
	return rt, nil
}

func catalogClickHouseNative(options clickhouse.Options) driver.Connector {
	// Native sets ReadTimeout once for the entire result. Let the socket
	// enforce its renewable read limit while keeping explicit caller deadlines.
	options.ReadTimeout = time.Duration(math.MaxInt64)
	dialer := catalogDialer{dial: (&net.Dialer{Timeout: options.DialTimeout}).DialContext, writeTimeout: 15 * time.Second}
	options.DialContext = func(ctx context.Context, addr string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil || options.TLS == nil {
			return conn, err
		}
		// A custom native dialer owns TLS negotiation. HTTP negotiates TLS
		// separately in net/http, including the HTTP-first auto probe.
		secure := tls.Client(conn, options.TLS)
		if err := secure.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return secure, nil
	}
	return clickhouse.Connector(&options)
}

func (c *clickHouseHTTPConnector) Driver() driver.Driver {
	return clickhouse.Connector(&c.options).Driver()
}

func (c *clickHouseHTTPConnector) Connect(ctx context.Context) (driver.Conn, error) {
	options := c.options
	database := &atomic.Value{}
	database.Store(options.Auth.Database)
	transport := options.TransportFunc
	var active http.RoundTripper
	var baseTransport *http.Transport
	legacy := false
	options.TransportFunc = func(base *http.Transport) (http.RoundTripper, error) {
		baseTransport = base
		var next http.RoundTripper = base
		if transport != nil {
			var err error
			next, err = transport(base)
			if err != nil {
				return nil, err
			}
		}
		if legacy {
			var err error
			next, err = newClickHouseLegacyTransport(next)
			if err != nil {
				return nil, err
			}
		}
		active = next
		return &clickHouseDatabaseTransport{next: next, database: database}, nil
	}
	raw, err := clickhouse.Connector(&options).Connect(ctx)
	if ctx.Err() == nil && clickHouseLegacyHandshakeError(err) {
		// Only the driver's own failed connection handshake may select legacy
		// HTTP. Do not retry authentication, TLS, timeout or business SQL errors.
		if closer, ok := active.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		} else if baseTransport != nil {
			baseTransport.CloseIdleConnections()
		}
		legacy = true
		raw, err = clickhouse.Connector(&options).Connect(ctx)
	}
	if err != nil {
		return nil, err
	}
	conn, ok := raw.(clickHouseHTTPDriver)
	if !ok {
		_ = raw.Close()
		return nil, errors.New("unsupported ClickHouse HTTP driver interfaces")
	}
	return &clickHouseHTTPConn{clickHouseHTTPDriver: conn, database: database, legacy: legacy}, nil
}

// Forward the pinned driver's optional interfaces, including named value and
// batch handling. Compatibility is confined to successful USE state and empty
// SELECT metadata on confirmed legacy HTTP connections.
type clickHouseHTTPDriver interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.NamedValueChecker
	driver.Pinger
	driver.SessionResetter
}

type clickHouseHTTPConn struct {
	clickHouseHTTPDriver
	database *atomic.Value
	legacy   bool
}

func (c *clickHouseHTTPConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := c.clickHouseHTTPDriver.QueryContext(ctx, query, args)
	if err != nil || !c.legacy || len(rows.Columns()) != 0 {
		return rows, err
	}
	// 20.8 HTTP Native emits no header at all for an empty SELECT, including
	// the catalog's LIMIT 0 capability probe. Use the existing statement
	// scanner to admit one SELECT, then ask the server to analyze its schema.
	// DESCRIBE does not execute the SELECT; business SQL is never replayed.
	analysis, analysisErr := sqlservice.Analyze(dialect.ClickHouse, query)
	if analysisErr != nil || len(analysis.Statements) != 1 || analysis.Statements[0].Kind != "select" || analysis.Statements[0].MayWrite {
		return rows, nil
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	metadata, err := c.clickHouseHTTPDriver.QueryContext(ctx, "DESCRIBE TABLE (\n"+analysis.Statements[0].SQL+"\n)", args)
	if err != nil {
		return nil, err
	}
	defer metadata.Close()
	var result clickHouseEmptyRows
	values := make([]driver.Value, len(metadata.Columns()))
	if len(values) < 2 {
		return nil, errors.New("legacy ClickHouse SELECT schema is unavailable")
	}
	for {
		if err := metadata.Next(values); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		name, nameOK := clickHouseMetadataString(values[0])
		typ, typeOK := clickHouseMetadataString(values[1])
		if !nameOK || !typeOK || name == "" || typ == "" || len(result.names) >= 4096 {
			return nil, errors.New("invalid legacy ClickHouse SELECT schema")
		}
		result.names = append(result.names, name)
		result.types = append(result.types, typ)
	}
	if err := metadata.Close(); err != nil {
		return nil, err
	}
	if len(result.names) == 0 {
		return nil, errors.New("legacy ClickHouse SELECT schema is empty")
	}
	return &result, nil
}

func clickHouseMetadataString(value driver.Value) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case []byte:
		return string(value), true
	default:
		return "", false
	}
}

type clickHouseEmptyRows struct{ names, types []string }

func (r *clickHouseEmptyRows) Columns() []string                       { return r.names }
func (r *clickHouseEmptyRows) Close() error                            { return nil }
func (r *clickHouseEmptyRows) Next([]driver.Value) error               { return io.EOF }
func (r *clickHouseEmptyRows) ColumnTypeDatabaseTypeName(i int) string { return r.types[i] }
func (r *clickHouseEmptyRows) ColumnTypeNullable(i int) (bool, bool) {
	return strings.HasPrefix(r.types[i], "Nullable("), true
}

func (c *clickHouseHTTPConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	result, err := c.clickHouseHTTPDriver.ExecContext(ctx, query, args)
	if err == nil {
		if database, ok := sqlservice.ClickHouseUseDatabase(query); ok {
			c.database.Store(database)
		}
	}
	return result, err
}

type clickHouseDatabaseTransport struct {
	next     http.RoundTripper
	database *atomic.Value
}

func (t *clickHouseDatabaseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	current := request.Clone(request.Context())
	query := current.URL.Query()
	query.Set("database", t.database.Load().(string))
	current.URL.RawQuery = query.Encode()
	return t.next.RoundTrip(current)
}

func (t *clickHouseDatabaseTransport) CloseIdleConnections() {
	if closer, ok := t.next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
