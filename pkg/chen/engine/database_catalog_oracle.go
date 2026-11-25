package engine

import (
	"context"
	"database/sql/driver"
	"time"

	goora "github.com/sijms/go-ora/v2"
)

type catalogOracleConnector struct{ *goora.OracleConnector }

func (c catalogOracleConnector) Connect(ctx context.Context) (driver.Conn, error) {
	// The native handshake watcher races Session.Connect when it disconnects.
	// The catalog dialer interrupts the socket for the entire login instead.
	login := ctx.Value(catalogLoginKey{}).(*catalogLogin)
	raw, err := c.OracleConnector.Connect(context.WithoutCancel(ctx))
	if err != nil {
		return nil, err
	}
	return &catalogOracleConn{raw.(*goora.Connection), login.sockets}, nil
}

type catalogOracleConn struct {
	*goora.Connection
	sockets *catalogSockets
}

func (c *catalogOracleConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stop := c.sockets.watch(ctx)
	rows, err := c.Connection.QueryContext(context.WithoutCancel(ctx), query, args)
	if err != nil {
		stop()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return &catalogOracleRows{rows.(*goora.DataSet), stop}, nil
}

func (c *catalogOracleConn) IsValid() bool {
	return c.sockets.valid()
}

func (c *catalogOracleConn) Close() error {
	// Logoff reads a server reply even when the pool closes an idle connection.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stop := c.sockets.watch(ctx)
	defer stop()
	return c.Connection.Close()
}

type catalogOracleRows struct {
	*goora.DataSet
	stop func()
}

func (r *catalogOracleRows) Close() error {
	defer r.stop()
	return r.DataSet.Close()
}
