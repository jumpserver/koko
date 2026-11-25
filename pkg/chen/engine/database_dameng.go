package engine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"time"

	dm "gitee.com/chunanyong/dm"
)

// DM v1.8.23's watcher mutates driver maps concurrently with protocol calls.
// Keep it isolated with WithoutCancel. Our separate watcher only closes a
// network-transport snapshot, then joins before synchronous driver cleanup.
// A dispatched cancel has unknown database outcome and discards the console.
const damengDialName = "chen-bounded-dameng"
const damengCatalogDialName = "chen-catalog-dameng"

func init() {
	dm.RegisterDialContext(damengDialName, damengDial(120*time.Second, 15*time.Second))
	dm.RegisterDialContext(damengCatalogDialName, damengDial(catalogSocketReadTimeout, 15*time.Second))
}

func damengDial(readTimeout, writeTimeout time.Duration) func(context.Context, string) (net.Conn, error) {
	return func(ctx context.Context, addr string) (net.Conn, error) {
		dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 2 * time.Second}
		conn, err := dialCatalogTransport(ctx, "tcp", addr, dialer.DialContext)
		if err != nil {
			return nil, err
		}
		return &damengSocket{conn, readTimeout, writeTimeout}, nil
	}
}

// Deadlines apply to each read/write, not to the complete query. A timeout closes
// the transport so vendor cleanup cannot send further requests on a partial frame.
type damengSocket struct {
	net.Conn
	readTimeout, writeTimeout time.Duration
}

func (c *damengSocket) Read(p []byte) (int, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(c.readTimeout)); err != nil {
		_ = c.Conn.Close()
		return 0, err
	}
	n, err := c.Conn.Read(p)
	if err != nil {
		_ = c.Conn.Close()
	}
	return n, err
}
func (c *damengSocket) Write(p []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
		_ = c.Conn.Close()
		return 0, err
	}
	n, err := c.Conn.Write(p)
	if err != nil {
		_ = c.Conn.Close()
	}
	return n, err
}

var errDamengTransport = errors.New("Dameng connection transport failed")

func isDamengTransportError(err error) bool {
	if errors.Is(err, errDamengTransport) {
		return true
	}
	var de *dm.DmError
	if !errors.As(err, &de) {
		return false
	}
	switch de.ErrCode {
	case dm.ECGO_COMMUNITION_ERROR.ErrCode, dm.ECGO_CONNECTION_CLOSED.ErrCode,
		dm.ECGO_CONNECTION_SWITCH_FAILED.ErrCode, dm.ECGO_CONNECTION_SWITCHED.ErrCode:
		return true
	}
	return false
}

func (pool databasePool) openDameng(dsn string) (*sql.DB, error) {
	// Obtain the initialized registered driver without opening a network connection.
	raw, err := sql.Open("dm", dsn)
	if err != nil {
		return nil, err
	}
	d := damengDriver{raw.Driver().(driver.DriverContext), pool}
	_ = raw.Close()
	c, err := d.OpenConnector(dsn)
	if err != nil {
		return nil, err
	}
	return pool.openConnector(c, 2*time.Minute), nil
}

type damengDriver struct {
	driver.DriverContext
	pool databasePool
}

func (d damengDriver) Open(dsn string) (driver.Conn, error) {
	c, err := d.OpenConnector(dsn)
	if err != nil {
		return nil, err
	}
	return c.Connect(context.Background())
}

func (d damengDriver) OpenConnector(dsn string) (driver.Connector, error) {
	c, err := d.DriverContext.OpenConnector(dsn)
	if err != nil {
		return nil, err
	}
	connector := damengConnector{Connector: c, driver: d}
	if d.pool == catalogPool {
		c.(*dm.DmConnector).TransportCancelsLogin = true
		connector.loginSlot = make(chan struct{}, 1)
	}
	return connector, nil
}

type damengConnector struct {
	driver.Connector
	driver    driver.Driver
	loginSlot chan struct{}
}

func (c damengConnector) Driver() driver.Driver { return c.driver }
func (c damengConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	driverCtx := context.WithoutCancel(ctx)
	if c.loginSlot != nil {
		// The vendor connector serializes logins behind a mutex. Allow a
		// second caller to cancel before the first handshake completes.
		select {
		case c.loginSlot <- struct{}{}:
			defer func() { <-c.loginSlot }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		driverCtx = ctx
	}
	raw, err := c.Connector.Connect(driverCtx)
	if ctx.Err() != nil {
		if err == nil {
			_ = raw.Close()
		}
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	conn, ok := raw.(damengContextConn)
	if !ok {
		_ = raw.Close()
		return nil, errors.New("unsupported Dameng driver interfaces")
	}
	return &damengConn{damengContextConn: conn}, nil
}

type damengContextConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
	driver.SessionResetter
	driver.NamedValueChecker
}

var _ damengContextConn = (*dm.DmConnection)(nil)

type damengConn struct {
	damengContextConn
	bad bool // accessed only under database/sql's physical connection lock
}

func (c *damengConn) IsValid() bool { return !c.bad }
func (c *damengConn) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.bad {
		return driver.ErrBadConn
	}
	return nil
}
func (c *damengConn) finish(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		c.discard()
		// Do not return ErrBadConn here: database/sql must not retry a dispatched write.
		return ctx.Err()
	}
	if isDamengTransportError(err) {
		c.discard()
		// Vendor transport errors have the same type as SQL errors. Do not expose
		// endpoint details or let the engine mistake them for a known SQL failure.
		return errDamengTransport
	}
	return err
}
func (c *damengConn) discard() {
	if !c.bad {
		c.bad = true
		// Close only the transport first. Driver close may otherwise send rollback
		// and statement-free RPCs on a canceled or partial protocol frame.
		if snapshot, ok := c.damengContextConn.(interface{ ChenTransportCloser() io.Closer }); ok {
			if transport := snapshot.ChenTransportCloser(); transport != nil {
				_ = transport.Close()
			}
		}
		_ = c.damengContextConn.Close()
	}
}
func (c *damengConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if err := c.check(ctx); err != nil {
		return nil, err
	}
	var tx driver.Tx
	err := c.withTransportCancel(ctx, func() error {
		var err error
		tx, err = c.damengContextConn.BeginTx(context.WithoutCancel(ctx), opts)
		return err
	})
	return tx, c.finish(ctx, err)
}
func (c *damengConn) Ping(ctx context.Context) error {
	if err := c.check(ctx); err != nil {
		return err
	}
	return c.finish(ctx, c.withTransportCancel(ctx, func() error { return c.damengContextConn.Ping(context.WithoutCancel(ctx)) }))
}
func (c *damengConn) ResetSession(ctx context.Context) error {
	if err := c.check(ctx); err != nil {
		return err
	}
	return c.finish(ctx, c.withTransportCancel(ctx, func() error { return c.damengContextConn.ResetSession(context.WithoutCancel(ctx)) }))
}
func (c *damengConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.check(ctx); err != nil {
		return nil, err
	}
	var result driver.Result
	err := c.withTransportCancel(ctx, func() error {
		var err error
		result, err = c.damengContextConn.ExecContext(context.WithoutCancel(ctx), query, args)
		return err
	})
	return result, c.finish(ctx, err)
}
func (c *damengConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.check(ctx); err != nil {
		return nil, err
	}
	var rows driver.Rows
	err := c.withTransportCancel(ctx, func() error {
		var err error
		rows, err = c.damengContextConn.QueryContext(context.WithoutCancel(ctx), query, args)
		return err
	})
	return c.rows(ctx, rows, err)
}
func (c *damengConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}
func (c *damengConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := c.check(ctx); err != nil {
		return nil, err
	}
	var stmt driver.Stmt
	err := c.withTransportCancel(ctx, func() error {
		var err error
		stmt, err = c.damengContextConn.PrepareContext(context.WithoutCancel(ctx), query)
		return err
	})
	if ctx.Err() != nil && stmt != nil {
		_ = stmt.Close()
	}
	if err = c.finish(ctx, err); err != nil {
		return nil, err
	}
	return &damengStmt{stmt.(damengContextStmt), c}, nil
}

type damengContextStmt interface {
	driver.Stmt
	driver.StmtExecContext
	driver.StmtQueryContext
	driver.NamedValueChecker
}

var _ damengContextStmt = (*dm.DmStatement)(nil)

type damengStmt struct {
	damengContextStmt
	conn *damengConn
}

func (s *damengStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if err := s.conn.check(ctx); err != nil {
		return nil, err
	}
	var result driver.Result
	err := s.conn.withTransportCancel(ctx, func() error {
		var err error
		result, err = s.damengContextStmt.ExecContext(context.WithoutCancel(ctx), args)
		return err
	})
	return result, s.conn.finish(ctx, err)
}
func (s *damengStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if err := s.conn.check(ctx); err != nil {
		return nil, err
	}
	var rows driver.Rows
	err := s.conn.withTransportCancel(ctx, func() error {
		var err error
		rows, err = s.damengContextStmt.QueryContext(context.WithoutCancel(ctx), args)
		return err
	})
	return s.conn.rows(ctx, rows, err)
}

// Preserve the vendor's full column metadata and multiple-result-set interfaces.
// database/sql serializes Next/Close, including its own context-triggered Close.
type damengDriverRows interface {
	driver.Rows
	driver.RowsNextResultSet
	driver.RowsColumnTypeDatabaseTypeName
	driver.RowsColumnTypeScanType
	driver.RowsColumnTypeLength
	driver.RowsColumnTypeNullable
	driver.RowsColumnTypePrecisionScale
}

var _ damengDriverRows = (*dm.DmRows)(nil)

type damengRows struct {
	damengDriverRows
	conn *damengConn
	ctx  context.Context
}

func (c *damengConn) rows(ctx context.Context, rows driver.Rows, err error) (driver.Rows, error) {
	// The vendor returns a typed nil *DmRows on query errors. Its interface is
	// non-nil, so handle the error before attempting cancellation cleanup.
	if err != nil {
		return nil, c.finish(ctx, err)
	}
	if ctx.Err() != nil && rows != nil {
		_ = rows.Close()
	}
	if err = c.finish(ctx, err); err != nil {
		return nil, err
	}
	return &damengRows{rows.(damengDriverRows), c, ctx}, nil
}
func (r *damengRows) Close() error {
	return r.conn.finish(r.ctx, r.conn.withTransportCancel(r.ctx, func() error { return r.damengDriverRows.Close() }))
}
func (r *damengRows) Next(dest []driver.Value) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	err := r.conn.withTransportCancel(r.ctx, func() error { return r.damengDriverRows.Next(dest) })
	if isDamengTransportError(err) {
		return r.conn.finish(r.ctx, err)
	}
	if r.ctx.Err() != nil {
		return r.ctx.Err()
	}
	return err
}
func (r *damengRows) NextResultSet() error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	err := r.conn.withTransportCancel(r.ctx, func() error { return r.damengDriverRows.NextResultSet() })
	if isDamengTransportError(err) {
		return r.conn.finish(r.ctx, err)
	}
	if r.ctx.Err() != nil {
		return r.ctx.Err()
	}
	return err
}
