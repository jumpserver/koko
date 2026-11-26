package engine

import (
	"context"
	"database/sql/driver"

	goora "github.com/sijms/go-ora/v2"
)

type oracleConnector struct {
	*goora.OracleConnector
	pool databasePool
}

func (c oracleConnector) Connect(ctx context.Context) (driver.Conn, error) {
	// The native handshake watcher races Session.Connect when it disconnects.
	// The database dialer interrupts the socket for the entire login instead.
	login := ctx.Value(databaseLoginKey{}).(*databaseLogin)
	raw, err := c.OracleConnector.Connect(context.WithoutCancel(ctx))
	if err != nil {
		return nil, err
	}
	if c.pool != catalogPool {
		return raw, nil
	}
	return &catalogOracleConn{raw.(*goora.Connection), login.sockets}, nil
}

type catalogOracleConn struct {
	*goora.Connection
	sockets *databaseSockets
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
	return &catalogOracleRows{rows.(*goora.DataSet), stop, c.sockets}, nil
}

type catalogOracleRows struct {
	*goora.DataSet
	stop    func()
	sockets *databaseSockets
}

func (r *catalogOracleRows) Close() error {
	defer r.stop()
	return r.sockets.runCleanup(r.DataSet.Close)
}
