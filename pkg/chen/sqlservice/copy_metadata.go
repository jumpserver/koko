package sqlservice

import (
	"context"
	"database/sql"
	"fmt"
)

// Copying cached values does not require transactional grid-write support.
// ClickHouse sorting keys are not unique row identities, so they are never
// exposed as UPDATE predicates. Its INSERT copier still excludes generated
// columns and respects omitted database defaults.
func LoadCopyTable(ctx context.Context, conn *sql.Conn, d Dialect, scope Context, schema, table string) (EditTable, error) {
	if d != ClickHouse {
		return LoadEditTable(ctx, conn, d, scope, schema, table)
	}
	out, err := LoadBrowseTable(ctx, conn, d, scope, schema, table)
	if err != nil {
		return out, err
	}
	if out.View {
		return out, ErrNotEditable
	}
	rows, err := conn.QueryContext(ctx, `SELECT name,default_kind FROM system.columns WHERE database=? AND table=? ORDER BY position`, out.Schema, out.Table)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var name, kind string
		if err := rows.Scan(&name, &kind); err != nil {
			return out, err
		}
		if i >= len(out.Columns) || name != out.Columns[i].Name {
			return out, fmt.Errorf("Copy column metadata changed; refresh the result")
		}
		out.Columns[i].Generated = kind == "ALIAS" || kind == "MATERIALIZED"
		// ClickHouse supplies a type default even without a DEFAULT expression.
		out.Columns[i].HasDefault = !out.Columns[i].Generated
		i++
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if i != len(out.Columns) {
		return out, fmt.Errorf("Copy column metadata is incomplete")
	}
	return out, nil
}
