package engine

import (
	"context"
	"database/sql/driver"
	"io"
	"reflect"
)

func (c *directDatabaseConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := c.directDatabaseCalls.QueryContext(ctx, query, args)
	return c.wrapRows(rows, err)
}

func (c *databaseConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	raw, err := c.databaseDriverConn.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	stmt, ok := raw.(databaseQueryStatement)
	if !ok {
		return raw, nil // ClickHouse prepared batches only support Exec.
	}
	wrapped := &databaseStmt{stmt, c}
	if converter, ok := raw.(driver.ColumnConverter); ok {
		return struct {
			*databaseStmt
			driver.ColumnConverter
		}{wrapped, converter}, nil
	}
	return wrapped, nil
}

type databaseQueryStatement interface {
	driver.Stmt
	driver.StmtQueryContext
	driver.StmtExecContext
}

type databaseStmt struct {
	databaseQueryStatement
	conn *databaseConn
}

func (s *databaseStmt) CheckNamedValue(value *driver.NamedValue) error {
	if checker, ok := s.databaseQueryStatement.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(value)
	}
	return s.conn.CheckNamedValue(value)
}

func (s *databaseStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := s.databaseQueryStatement.QueryContext(ctx, args)
	return s.conn.wrapRows(rows, err)
}

func (s *databaseStmt) Close() error {
	return s.conn.sockets.runCleanup(s.databaseQueryStatement.Close)
}

func (c *databaseConn) wrapRows(raw driver.Rows, err error) (driver.Rows, error) {
	if err != nil {
		return nil, err // Includes ErrSkip so database/sql can prepare the query.
	}
	rows := &databaseRows{Rows: raw, sockets: c.sockets}
	if sets, ok := raw.(driver.RowsNextResultSet); ok {
		return &databaseResultSets{databaseRows: rows, sets: sets}, nil
	}
	return rows, nil
}

// Bound Close at driver level so QueryRow.Scan, implicit database/sql closes
// and error returns from metadata queries get the same drain budget.
type databaseRows struct {
	driver.Rows
	sockets *databaseSockets
}

func (r *databaseRows) Close() error { return r.sockets.runCleanup(r.Rows.Close) }

// Forward native metadata using database/sql's defaults for missing properties.
func (r *databaseRows) ColumnTypeDatabaseTypeName(i int) string {
	if cols, ok := r.Rows.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return cols.ColumnTypeDatabaseTypeName(i)
	}
	return ""
}
func (r *databaseRows) ColumnTypeLength(i int) (int64, bool) {
	if cols, ok := r.Rows.(driver.RowsColumnTypeLength); ok {
		return cols.ColumnTypeLength(i)
	}
	return 0, false
}
func (r *databaseRows) ColumnTypeNullable(i int) (bool, bool) {
	if cols, ok := r.Rows.(driver.RowsColumnTypeNullable); ok {
		return cols.ColumnTypeNullable(i)
	}
	return false, false
}
func (r *databaseRows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if cols, ok := r.Rows.(driver.RowsColumnTypePrecisionScale); ok {
		return cols.ColumnTypePrecisionScale(i)
	}
	return 0, 0, false
}
func (r *databaseRows) ColumnTypeScanType(i int) reflect.Type {
	if cols, ok := r.Rows.(driver.RowsColumnTypeScanType); ok {
		return cols.ColumnTypeScanType(i)
	}
	return reflect.TypeFor[any]()
}

type databaseResultSets struct {
	*databaseRows
	sets      driver.RowsNextResultSet
	exhausted bool
}

func (r *databaseResultSets) Next(dest []driver.Value) error {
	err := r.Rows.Next(dest)
	r.exhausted = err == io.EOF
	return err
}

func (r *databaseResultSets) HasNextResultSet() bool { return r.sets.HasNextResultSet() }

func (r *databaseResultSets) NextResultSet() error {
	if r.exhausted {
		r.exhausted = false
		return r.sets.NextResultSet()
	}
	// The caller stopped early (for example at MaxRows). MySQL discards
	// pending rows on the wire before it returns the next set.
	return r.sockets.runCleanup(r.sets.NextResultSet)
}
