package engine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"os"
	"sync"
	"time"
)

type databasePool uint8

const (
	regularPool databasePool = iota
	catalogPool
)

func (p databasePool) readTimeout(regular time.Duration) time.Duration {
	if p == catalogPool {
		return catalogSocketReadTimeout
	}
	return regular
}

func (p databasePool) openConnector(c driver.Connector, loginTimeout time.Duration) *sql.DB {
	if p == catalogPool {
		c = catalogConnector{c, loginTimeout}
	}
	return sql.OpenDB(c)
}

type catalogConnector struct {
	driver.Connector
	loginTimeout time.Duration
}

type catalogLoginKey struct{}

type catalogLogin struct {
	ctx     context.Context
	sockets *catalogSockets
}

func (c catalogConnector) Connect(parent context.Context) (driver.Conn, error) {
	ctx, cancel := context.WithTimeout(parent, c.loginTimeout)
	defer cancel()
	login := &catalogLogin{ctx: ctx, sockets: &catalogSockets{}}
	stop := login.sockets.watch(ctx)
	conn, err := c.Connector.Connect(context.WithValue(ctx, catalogLoginKey{}, login))
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
	return conn, nil
}

// Drivers can open more than one transport during endpoint selection or
// redirects. Cancel the network I/O without touching mutable driver state.
type catalogSockets struct {
	mu     sync.Mutex
	conns  []net.Conn
	closed bool
}

func (s *catalogSockets) add(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = conn.Close()
		return
	}
	s.conns = append(s.conns, conn)
}

func (s *catalogSockets) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, conn := range s.conns {
		_ = conn.Close()
	}
	s.conns = nil
}

func (s *catalogSockets) valid() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed
}

func (s *catalogSockets) watch(ctx context.Context) func() {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { s.close(); close(done) })
	return sync.OnceFunc(func() {
		if !stop() {
			<-done
		}
	})
}

type catalogDialer struct {
	dial         func(context.Context, string, string) (net.Conn, error)
	writeTimeout time.Duration
	http         bool
}

func (d catalogDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := dialCatalogTransport(ctx, network, address, d.dial)
	if err != nil {
		return nil, err
	}
	return &catalogSocket{Conn: conn, read: idleDeadline{limit: catalogSocketReadTimeout}, write: idleDeadline{limit: d.writeTimeout}, http: d.http}, nil
}

func dialCatalogTransport(ctx context.Context, network, address string, dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	login, _ := ctx.Value(catalogLoginKey{}).(*catalogLogin)
	if login != nil && ctx.Done() == nil {
		// Oracle shields the driver's own handshake watcher.
		ctx = login.ctx
	}
	conn, err := dial(ctx, network, address)
	if err != nil {
		return nil, err
	}
	if login != nil {
		login.sockets.add(conn)
	}
	return conn, nil
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

type catalogSocket struct {
	net.Conn
	read, write idleDeadline
	http        bool
}

func (c *catalogSocket) Read(p []byte) (int, error) {
	if err := c.read.renew(c.Conn.SetReadDeadline); err != nil {
		return 0, err
	}
	n, err := c.Conn.Read(p)
	if c.read.expired(err) {
		_ = c.Conn.Close()
	}
	return n, err
}

func (c *catalogSocket) Write(p []byte) (int, error) {
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
		_ = c.Conn.Close()
	}
	return n, err
}

func (c *catalogSocket) SetReadDeadline(t time.Time) error {
	return c.read.set(t, c.Conn.SetReadDeadline)
}
func (c *catalogSocket) SetWriteDeadline(t time.Time) error {
	return c.write.set(t, c.Conn.SetWriteDeadline)
}
func (c *catalogSocket) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}
