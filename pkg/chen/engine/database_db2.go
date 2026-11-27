package engine

import (
	"context"
	"crypto/tls"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync/atomic"

	db2 "github.com/go-db2/go-db2"
	"github.com/jumpserver/koko/internal/dbconn"
	"github.com/jumpserver/koko/pkg/chen/core"
)

func (pool databasePool) openDB2(c core.ConnectionConfig, t *tls.Config) (*sql.DB, error) {
	cfg, err := db2Config(c, t)
	if err != nil {
		return nil, err
	}
	cfg.DialContext = databaseDialer{dial: cfg.DialContext, readTimeout: pool.readTimeout(), writeTimeout: databaseWriteTimeout}.DialContext
	return openConnector(&db2Connector{Connector: db2.NewConnector(cfg)}), nil
}

func db2Config(c core.ConnectionConfig, t *tls.Config) (*db2.Config, error) {
	cfg, err := dbconn.DB2Config(c, t)
	if err != nil {
		return nil, err
	}
	cfg.Timeout = databaseDialTimeout
	cfg.ClientApplName = "Chen Go"
	return cfg, nil
}

type db2Connector struct{ *db2.Connector }

func (c *db2Connector) Connect(ctx context.Context) (driver.Conn, error) {
	raw, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	login := ctx.Value(databaseLoginKey{}).(*databaseLogin)
	return &db2Conn{Conn: raw.(*db2.Conn), sockets: login.sockets}, nil
}

// Keep transaction ownership and original server cancellation diagnostics on the
// same physical connection. database/sql can replace Rows.Err with ctx.Err.
type db2Conn struct {
	*db2.Conn
	sockets       *databaseSockets
	inTransaction atomic.Bool
	uncertain     atomic.Bool
	cancellations atomic.Uint64
}

func (c *db2Conn) ChenTransactionState() string {
	if !c.IsValid() {
		return string(editTxUnknown)
	}
	if c.inTransaction.Load() {
		return string(editTxActive)
	}
	return string(editTxIdle)
}
func (c *db2Conn) ChenCancellationSequence() uint64 { return c.cancellations.Load() }

func (c *db2Conn) IsValid() bool {
	return c.sockets.valid() && !c.uncertain.Load() && c.Conn.IsValid()
}

func (c *db2Conn) ResetSession(ctx context.Context) error {
	if c.uncertain.Load() {
		return driver.ErrBadConn
	}
	return c.Conn.ResetSession(ctx)
}

func (c *db2Conn) observe(err error) error {
	if db2ServerCancellation(err) {
		c.cancellations.Add(1)
	}
	return err
}
func (c *db2Conn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *db2Conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.Conn.BeginTx(ctx, opts)
	if err != nil {
		return nil, c.observe(err)
	}
	c.inTransaction.Store(true)
	return &db2Tx{Tx: tx, conn: c}, nil
}

type db2Tx struct {
	driver.Tx
	conn *db2Conn
}

func (t *db2Tx) finish(err error) error {
	if err != nil {
		t.conn.uncertain.Store(true)
	} else {
		t.conn.inTransaction.Store(false)
	}
	return err
}
func (t *db2Tx) Commit() error   { return t.finish(t.Tx.Commit()) }
func (t *db2Tx) Rollback() error { return t.finish(t.Tx.Rollback()) }
func (c *db2Conn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}
func (c *db2Conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	stmt, err := c.Conn.PrepareContext(ctx, query)
	if err != nil {
		return nil, c.observe(err)
	}
	return &db2Stmt{Stmt: stmt.(*db2.Stmt), conn: c}, nil
}
func (c *db2Conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	result, err := c.Conn.ExecContext(ctx, query, args)
	return result, c.observe(err)
}
func (c *db2Conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := c.Conn.QueryContext(ctx, query, args)
	if err != nil {
		return nil, c.observe(err)
	}
	return c.wrapRows(ctx, rows), nil
}

type db2Stmt struct {
	*db2.Stmt
	conn *db2Conn
}

func (s *db2Stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	result, err := s.Stmt.ExecContext(ctx, args)
	return result, s.conn.observe(err)
}
func (s *db2Stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := s.Stmt.QueryContext(ctx, args)
	if err != nil {
		return nil, s.conn.observe(err)
	}
	return s.conn.wrapRows(ctx, rows), nil
}

func (c *db2Conn) wrapRows(ctx context.Context, rows driver.Rows) *db2Rows {
	r := &db2Rows{Rows: rows.(*db2.Rows), conn: c}
	// Db2 closes cursors with a background context, including a network
	// round trip. Keep cancellation armed until the cursor is released.
	r.stop = c.sockets.watch(ctx)
	return r
}

type db2Rows struct {
	*db2.Rows
	conn *db2Conn
	stop func()
}

func (r *db2Rows) Close() error {
	defer r.stop()
	return r.conn.observe(r.conn.sockets.runCleanup(r.Rows.Close))
}

func (r *db2Rows) Next(values []driver.Value) error { return r.conn.observe(r.Rows.Next(values)) }
func (r *db2Rows) NextResultSet() error             { return r.conn.observe(r.Rows.NextResultSet()) }
func db2DatabaseError(err error) *APIError {
	if db2UncertainNativeError(err) {
		return nil
	}
	var native *db2.Error
	if errors.As(err, &native) {
		return &APIError{"DB2_SQL_ERROR", native.Error()}
	}
	return nil
}
