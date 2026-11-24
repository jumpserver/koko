package sqlservice

import (
	"context"
	"database/sql"
	"errors"
	gooraerr "github.com/sijms/go-ora/v2/network"
	"strings"
)

// Oracle before 12c has no ORACLE_MAINTAINED flag. A retry removes only that
// optional edit classification filter. ALL_* authorization, the edit path's
// explicit system-owner guard, caller scope and bind arguments stay unchanged.
func queryCatalogRows(ctx context.Context, conn *sql.Conn, d Dialect, query string, args ...any) (*sql.Rows, error) {
	rows, err := conn.QueryContext(ctx, query, args...)
	if err == nil || d != Oracle || !strings.Contains(query, "ORACLE_MAINTAINED") {
		return rows, err
	}
	var oracle *gooraerr.OracleError
	if !errors.As(err, &oracle) || oracle.ErrCode != 904 {
		return nil, err
	}
	fallback := query
	for _, owner := range []string{"o.OWNER", "f.OWNER", "schemaname"} {
		fallback = strings.ReplaceAll(fallback, oracleApplicationOwner(Oracle, owner), "")
	}
	if fallback == query {
		return nil, err
	}
	return conn.QueryContext(ctx, fallback, args...)
}
