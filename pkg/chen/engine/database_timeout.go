package engine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"os"
	"slices"
	"sync"
	"time"
)

type databasePool uint8

const (
	regularPool databasePool = iota
	catalogPool
)

// Regular SQL reads normally have no socket deadline. Dial, login, writes
// and context-free driver cleanup each have independent bounds. The separate
// catalog pool retains its renewable idle-read limit for metadata requests.
const (
	databaseCheckoutTimeout = 10 * time.Second
	databaseDialTimeout     = 10 * time.Second
	databaseLoginTimeout    = 10 * time.Second
	databaseWriteTimeout    = 15 * time.Second
	databaseCleanupTimeout  = 10 * time.Second
)

func (p databasePool) readTimeout() time.Duration {
	if p == catalogPool {
		return catalogSocketReadTimeout
	}
	return 0
}

func openConnector(c driver.Connector) *sql.DB {
	return sql.OpenDB(databaseConnector{c})
}

type databaseConnector struct{ driver.Connector }

type databaseLoginKey struct{}

type databaseLogin struct {
	ctx     context.Context
	sockets *databaseSockets
}

func (c databaseConnector) Connect(parent context.Context) (driver.Conn, error) {
	ctx, cancel := context.WithTimeout(parent, databaseLoginTimeout)
	defer cancel()
	login := &databaseLogin{ctx: ctx, sockets: &databaseSockets{}}
	stop := login.sockets.watch(ctx)
	conn, err := c.Connector.Connect(context.WithValue(ctx, databaseLoginKey{}, login))
	stop()
	if err != nil || ctx.Err() != nil {
		login.sockets.close()
		if err == nil {
			_ = conn.Close()
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	// Preserve the prepared-statement path of drivers such as SQL Server as
	// well as the fast path of drivers implementing direct query and exec.
	native, ok := conn.(databaseDriverConn)
	if !ok {
		login.sockets.close()
		_ = conn.Close()
		return nil, errors.New("unsupported database driver connection interfaces")
	}
	base := &databaseConn{native, login.sockets}
	if direct, ok := conn.(directDatabaseCalls); ok {
		return &directDatabaseConn{base, direct}, nil
	}
	return base, nil
}

type databaseDriverConn interface {
	driver.Conn
	driver.ConnPrepareContext
	driver.ConnBeginTx
	driver.NamedValueChecker
	driver.SessionResetter
	driver.Pinger
}

type directDatabaseCalls interface {
	driver.ExecerContext
	driver.QueryerContext
}

type directDatabaseConn struct {
	*databaseConn
	directDatabaseCalls
}

type databaseConn struct {
	databaseDriverConn
	sockets *databaseSockets
}

func (c *databaseConn) managedConn() *databaseConn { return c }

func rawDatabaseConn(raw any) any {
	if c, ok := raw.(interface{ managedConn() *databaseConn }); ok {
		return c.managedConn().databaseDriverConn
	}
	return raw
}

func (c *databaseConn) IsValid() bool {
	valid, ok := c.databaseDriverConn.(driver.Validator)
	return c.sockets.valid() && (!ok || valid.IsValid())
}

func (c *databaseConn) ResetSession(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, databaseCheckoutTimeout)
	defer cancel()
	stop := c.sockets.watch(ctx)
	err := c.databaseDriverConn.ResetSession(ctx)
	stop()
	// database/sql retries checkout only for ErrBadConn. No user SQL has
	// been dispatched on this connection yet.
	if err != nil || ctx.Err() != nil || !c.sockets.valid() {
		return driver.ErrBadConn
	}
	return nil
}

func (c *databaseConn) Close() error {
	defer c.sockets.close()
	return c.sockets.runCleanup(c.databaseDriverConn.Close)
}

func (c *databaseConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *databaseConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	// Some drivers retain ctx for Commit/Rollback. Limit just this exchange
	// without imposing a deadline on the user's open transaction.
	var tx driver.Tx
	err := c.sockets.runCleanup(func() error {
		var err error
		tx, err = c.databaseDriverConn.BeginTx(ctx, opts)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &databaseTx{tx, c.sockets}, nil
}

type databaseTx struct {
	driver.Tx
	sockets *databaseSockets
}

func (t *databaseTx) Commit() error {
	return t.sockets.runCleanup(t.Tx.Commit)
}

func (t *databaseTx) Rollback() error {
	return t.sockets.runCleanup(t.Tx.Rollback)
}

// Drivers can open more than one transport during endpoint selection or
// redirects. Cancel the network I/O without touching mutable driver state.
type databaseSockets struct {
	mu     sync.Mutex
	conns  []net.Conn
	closed bool
}

func (s *databaseSockets) add(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns = append(s.conns, conn)
	return true
}

func (s *databaseSockets) remove(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, open := range s.conns {
		if open == conn {
			s.conns = slices.Delete(s.conns, i, i+1)
			return true
		}
	}
	return false
}

func (s *databaseSockets) close() {
	s.mu.Lock()
	s.closed = true
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
}

func (s *databaseSockets) valid() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed
}

func (s *databaseSockets) watch(ctx context.Context) func() {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { s.close(); close(done) })
	return sync.OnceFunc(func() {
		if !stop() {
			<-done
		}
	})
}

// Commit, Rollback, Close and some cursor operations have no context in
// database/sql. Interrupt their captured transports and join before reuse.
func (s *databaseSockets) runCleanup(operation func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), databaseCleanupTimeout)
	stop := s.watch(ctx)
	defer cancel()
	err := operation()
	stop()
	if (err == nil || err == io.EOF) && !s.valid() {
		// Some drivers suppress errors during cleanup. A closed transport
		// cannot establish that a transaction boundary was acknowledged.
		return net.ErrClosed
	}
	return err
}

type databaseDialer struct {
	dial                      func(context.Context, string, string) (net.Conn, error)
	readTimeout, writeTimeout time.Duration
	httpSockets               *databaseSockets
}

func (d databaseDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := dialDatabaseTransport(ctx, network, address, d.dial, d.httpSockets)
	if err != nil {
		return nil, err
	}
	if d.readTimeout == 0 && d.writeTimeout == 0 {
		if tracked, ok := conn.(*registeredDatabaseTransport); ok {
			return tracked.Conn, nil
		}
		return conn, nil // Preserve native TCP capabilities for Oracle and MySQL.
	}
	return &databaseSocket{Conn: conn, read: idleDeadline{limit: d.readTimeout}, write: idleDeadline{limit: d.writeTimeout}, http: d.httpSockets != nil}, nil
}

func dialDatabaseTransport(ctx context.Context, network, address string, dial func(context.Context, string, string) (net.Conn, error), sockets *databaseSockets) (net.Conn, error) {
	login, _ := ctx.Value(databaseLoginKey{}).(*databaseLogin)
	if login != nil && ctx.Done() == nil {
		// Oracle shields the driver's own handshake watcher.
		ctx = login.ctx
	}
	if dial == nil {
		dial = (&net.Dialer{Timeout: databaseDialTimeout}).DialContext
	}
	conn, err := dial(ctx, network, address)
	if err != nil {
		return nil, err
	}
	if sockets == nil && login != nil {
		sockets = login.sockets
	}
	if sockets != nil {
		if !sockets.add(conn) {
			_ = conn.Close()
			return nil, net.ErrClosed
		}
		return &registeredDatabaseTransport{conn, sockets}, nil
	}
	return conn, nil
}

type registeredDatabaseTransport struct {
	net.Conn
	sockets *databaseSockets
}

func (c *registeredDatabaseTransport) Close() error {
	if !c.sockets.remove(c.Conn) {
		return nil // The registry already took ownership of closing this socket.
	}
	return c.Conn.Close()
}

// Drivers can change a deadline while a read is blocked, including to
// interrupt it. Apply the earlier of that deadline and the current idle limit.
type idleDeadline struct {
	mu             sync.Mutex
	limit          time.Duration
	idle, explicit time.Time
}

func (d *idleDeadline) renew(set func(time.Time) error) error {
	if d.limit == 0 {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.idle = time.Now().Add(d.limit)
	return d.apply(set)
}

func (d *idleDeadline) set(at time.Time, set func(time.Time) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.explicit = at
	return d.apply(set)
}

func (d *idleDeadline) apply(set func(time.Time) error) error {
	if !d.idle.IsZero() && (d.explicit.IsZero() || d.idle.Before(d.explicit)) {
		return set(d.idle)
	}
	return set(d.explicit)
}

// A timed-out protocol frame cannot be reused for cleanup or another query.
func (d *idleDeadline) expired(err error) bool {
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.idle.IsZero() && !time.Now().Before(d.idle) && (d.explicit.IsZero() || !d.explicit.Before(d.idle))
}

type databaseSocket struct {
	net.Conn
	read, write idleDeadline
	http        bool
}

func (c *databaseSocket) Read(p []byte) (int, error) {
	if err := c.read.renew(c.Conn.SetReadDeadline); err != nil {
		return 0, err
	}
	n, err := c.Conn.Read(p)
	if c.read.expired(err) {
		_ = c.Close()
	}
	return n, err
}

func (c *databaseSocket) Write(p []byte) (int, error) {
	if c.http {
		// net/http may have a read pending from before this request was sent.
		if err := c.read.renew(c.Conn.SetReadDeadline); err != nil {
			return 0, err
		}
	}
	if err := c.write.renew(c.Conn.SetWriteDeadline); err != nil {
		return 0, err
	}
	n, err := c.Conn.Write(p)
	if c.write.expired(err) {
		_ = c.Close()
	}
	return n, err
}

func (c *databaseSocket) SetReadDeadline(t time.Time) error {
	return c.read.set(t, c.Conn.SetReadDeadline)
}
func (c *databaseSocket) SetWriteDeadline(t time.Time) error {
	return c.write.set(t, c.Conn.SetWriteDeadline)
}
func (c *databaseSocket) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}
