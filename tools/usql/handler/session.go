package handler

import (
	"context"
	"database/sql"

	"github.com/xo/usql/drivers"
)

// sessionConn adapts a dedicated connection to the legacy drivers.DB interface.
// SQL Server resets session state (including USE) when a pooled connection is
// checked out again, so an interactive session must retain its connection.
type sessionConn struct{ *sql.Conn }

var _ drivers.DB = (*sessionConn)(nil)

func (c *sessionConn) Exec(query string, args ...interface{}) (sql.Result, error) {
	return c.ExecContext(context.Background(), query, args...)
}

func (c *sessionConn) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return c.QueryContext(context.Background(), query, args...)
}

func (c *sessionConn) QueryRow(query string, args ...interface{}) *sql.Row {
	return c.QueryRowContext(context.Background(), query, args...)
}

func (c *sessionConn) Prepare(query string) (*sql.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}
