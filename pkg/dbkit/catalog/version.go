package catalog

import (
	"context"
	"database/sql"
)

// MySQLVersionSQL is also used as the provenance of the observed version.
const MySQLVersionSQL = "SELECT VERSION()"

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// ReadMySQLVersion reads the compatibility version on the caller's connection.
// No connection is opened here; the caller owns the deadline and any vendor-specific fallback.
func ReadMySQLVersion(ctx context.Context, conn rowQuerier) (string, error) {
	var version string
	err := conn.QueryRowContext(ctx, MySQLVersionSQL).Scan(&version)
	return version, err
}
