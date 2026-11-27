package sqlservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

const MaxCatalogPageSize = 200

var (
	ErrCatalogForbidden = errors.New("catalog access forbidden")
	ErrCatalogNotFound  = errors.New("catalog object not found")
)

type CatalogSchema struct {
	Database string `json:"database"`
	Name     string `json:"name"`
}
type CatalogColumn struct {
	Database string `json:"database"`
	Schema   string `json:"schema"`
	Table    string `json:"table"`
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	Nullable bool   `json:"nullable"`
	Comment  string `json:"comment"`
	Ordinal  int    `json:"-"`
}
type CatalogColumnRequest struct {
	Database, Schema, Table string
	AfterOrdinal, Limit     int
}

// MySQL lists all databases visible to the connected account, including when
// no default database was selected. Other dialects list schemas in the supplied
// database. Catalog reads never change connection context and return at most
// limit+1 rows for keyset pagination without a COUNT query.
func SchemaRows(ctx context.Context, conn *sql.Conn, d dialect.ID, database, after string, limit int) ([]CatalogSchema, error) {
	items := []CatalogSchema{}
	if err := validateCatalogNames(conn, d, database, after); err != nil {
		return items, err
	}
	ctx, cancel := catalogReadContext(ctx)
	defer cancel()
	if !d.UsesMySQLSyntax() {
		if database == "" {
			return items, ErrCatalogForbidden
		}
		if err := catalogDatabaseAccess(ctx, conn, d, database); err != nil {
			return items, err
		}
	}
	var rows *sql.Rows
	var err error
	if usesCatalogObjectSource(d) {
		var query catalogQuery
		query, err = catalogSchemaQuery(d, database, after, limit)
		if err == nil {
			rows, err = queryCatalogRows(ctx, conn, d, query.SQL, query.Args...)
		}
	} else if d.UsesMySQLSyntax() {
		rows, err = conn.QueryContext(ctx, `SELECT SCHEMA_NAME, SCHEMA_NAME FROM information_schema.SCHEMATA
WHERE BINARY SCHEMA_NAME > BINARY ?
ORDER BY BINARY SCHEMA_NAME LIMIT ?`, after, catalogLimit(limit)+1)
	} else {
		rows, err = conn.QueryContext(ctx, `SELECT catalog_name, schema_name FROM information_schema.schemata
WHERE catalog_name = $1 AND schema_name COLLATE pg_catalog."C" > $2 COLLATE pg_catalog."C"
ORDER BY schema_name COLLATE pg_catalog."C" LIMIT $3`, database, after, catalogLimit(limit)+1)
	}
	if err != nil {
		return items, fmt.Errorf("list catalog schemas: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item CatalogSchema
		if err := rows.Scan(&item.Database, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func ColumnRows(ctx context.Context, conn *sql.Conn, d dialect.ID, req CatalogColumnRequest) ([]CatalogColumn, error) {
	return columnRows(ctx, conn, d, req, false, false)
}

// MySQLSearchColumns reads names and comments for objects already returned by
// RelationRows on this connection. INFORMATION_SCHEMA still applies the account's
// active grants, and each table keeps its own bounded page with one extra row.
// The caller owns one deadline for the whole catalog search.
func MySQLSearchColumns(ctx context.Context, conn *sql.Conn, database string, objects []CatalogRelation, limit int) (map[string][]CatalogColumn, error) {
	// Plain equalities let the metadata optimizer narrow the lookup. Keep the
	// binary comparisons for exact schema and table names.
	const statement = `SELECT TABLE_NAME, COLUMN_NAME, COALESCE(COLUMN_COMMENT, ''), ORDINAL_POSITION
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND BINARY TABLE_SCHEMA = BINARY ? AND BINARY TABLE_NAME = BINARY ?
ORDER BY ORDINAL_POSITION LIMIT ?`
	columns := make(map[string][]CatalogColumn, len(objects))
	if len(objects) == 0 {
		return columns, nil
	}
	parts := make([]string, 0, len(objects))
	args := make([]any, 0, len(objects)*5)
	for _, object := range objects {
		parts = append(parts, "("+statement+")")
		args = append(args, database, object.Name, database, object.Name, catalogLimit(limit)+1)
	}
	query := statement
	if len(parts) > 1 {
		query = strings.Join(parts, " UNION ALL ") + " ORDER BY TABLE_NAME, ORDINAL_POSITION"
	}
	rows, err := conn.QueryContext(CatalogQueryContext(ctx), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var column CatalogColumn
		if err := rows.Scan(&column.Table, &column.Name, &column.Comment, &column.Ordinal); err != nil {
			return nil, err
		}
		columns[column.Table] = append(columns[column.Table], column)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, ctx.Err()
}

// Table operations need the complete definition, not the first directory page.
// The caller has checked access in the requested read/edit mode and owns one
// deadline for all metadata reads. Editing retains its column-grant restrictions.
func tableColumnRows(ctx context.Context, conn *sql.Conn, d dialect.ID, req CatalogColumnRequest, editable bool) ([]CatalogColumn, error) {
	req.AfterOrdinal = 0
	return columnRows(ctx, conn, d, req, true, editable)
}

// A table operation has already admitted this object and needs its complete
// definition. Directory calls check access here and return a bounded page.
func columnRows(ctx context.Context, conn *sql.Conn, d dialect.ID, req CatalogColumnRequest, complete, editable bool) ([]CatalogColumn, error) {
	items := []CatalogColumn{}
	if d.UsesMySQLSyntax() && req.Schema != "" {
		req.Database = req.Schema
	}
	if err := validateCatalogInput(conn, d, req.Database, req.Schema, req.Table); err != nil {
		return items, err
	}
	if req.Table == "" || req.AfterOrdinal < 0 {
		return items, fmt.Errorf("catalog table and nonnegative ordinal are required")
	}
	if (d.UsesMySQLSyntax() || d == dialect.ClickHouse) && req.Schema == "" {
		req.Schema = req.Database
	}
	if req.Schema == "" {
		return items, fmt.Errorf("catalog schema is required")
	}
	if d == dialect.ClickHouse && req.Schema != req.Database {
		return items, ErrCatalogForbidden
	}
	ctx, cancel := MetadataContext(ctx, d)
	defer cancel()
	if !complete {
		if err := catalogDatabaseAccess(ctx, conn, d, req.Database); err != nil {
			return items, err
		}
		access := CatalogRelationAccess
		if editable {
			access = catalogEditableRelationAccess
		}
		if err := access(ctx, conn, d, req.Schema, req.Table); err != nil {
			return items, err
		}
	}
	limit := 0
	if !complete {
		limit = catalogLimit(req.Limit) + 1
	}
	var rows *sql.Rows
	var err error
	if usesCatalogObjectSource(d) {
		var query catalogQuery
		query, err = catalogColumnQuery(d, req, limit, catalogColumnTypeModifiers)
		if err == nil {
			rows, err = queryCatalogRows(ctx, conn, d, query.SQL, query.Args...)
			if err != nil && d == dialect.DB2 && catalogMissingStringUnits(err) {
				query, err = catalogColumnQuery(d, req, limit, catalogDB2LegacyTypeModifiers)
				if err == nil {
					rows, err = queryCatalogRows(ctx, conn, d, query.SQL, query.Args...)
				}
			}
		}
	} else if d.UsesMySQLSyntax() {
		target := "BINARY TABLE_SCHEMA = BINARY ? AND BINARY TABLE_NAME = BINARY ?"
		args := []any{req.Schema, req.Table}
		if d != dialect.MySQL || !complete {
			// Plain equalities narrow the dictionary lookup. Binary comparisons
			// still enforce exact schema and table names.
			target = "TABLE_SCHEMA = ? AND TABLE_NAME = ? AND " + target
			args = append(args, req.Schema, req.Table)
		}
		query := `SELECT TABLE_SCHEMA, TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, COLUMN_TYPE,
IS_NULLABLE = 'YES', COALESCE(COLUMN_COMMENT, ''), ORDINAL_POSITION
FROM information_schema.COLUMNS
WHERE ` + target + ` AND ORDINAL_POSITION > ?
ORDER BY ORDINAL_POSITION`
		args = append(args, req.AfterOrdinal)
		if limit > 0 {
			query += " LIMIT ?"
			args = append(args, limit)
		}
		rows, err = conn.QueryContext(CatalogQueryContext(ctx), query, args...)
	} else {
		query := `SELECT pg_catalog.current_database(), n.nspname, c.relname, a.attname,
pg_catalog.format_type(a.atttypid, a.atttypmod), NOT (a.attnotnull OR t.typnotnull), COALESCE(pg_catalog.col_description(a.attrelid, a.attnum), ''), a.attnum
FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_type t ON t.oid = a.atttypid
WHERE pg_catalog.current_database() = $1 AND n.nspname = $2 AND c.relname = $3 AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
AND a.attnum > $4 AND NOT a.attisdropped AND n.nspname IN (SELECT schema_name FROM information_schema.schemata)`
		if editable {
			query += ` AND pg_catalog.has_schema_privilege(n.oid, 'USAGE')
AND pg_catalog.has_column_privilege(c.oid, a.attnum, 'SELECT,INSERT,UPDATE,REFERENCES')`
		}
		query += " ORDER BY a.attnum"
		args := []any{req.Database, req.Schema, req.Table, req.AfterOrdinal}
		if limit > 0 {
			query += " LIMIT $5"
			args = append(args, limit)
		}
		rows, err = conn.QueryContext(ctx, query, args...)
	}
	if err != nil {
		return items, fmt.Errorf("list catalog columns: %w", err)
	}
	defer rows.Close()
	columnNames, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var seen map[string]bool
	if complete {
		seen = make(map[string]bool)
	}
	for rows.Next() {
		var item CatalogColumn
		var comment sql.NullString
		var length, precision, scale, charLength sql.NullInt64
		var units sql.NullString
		dest := []any{&item.Database, &item.Schema, &item.Table, &item.Name, &item.DataType, &item.Nullable, &comment, &item.Ordinal}
		modifiers := (d.UsesOracleSyntax() || d == dialect.DB2) && len(columnNames) == 13
		if modifiers {
			dest = append(dest, &length, &precision, &scale, &charLength, &units)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if modifiers {
			item.DataType = catalogColumnType(d, item.DataType, length, precision, scale, charLength, units)
		}
		if complete {
			if item.Ordinal <= req.AfterOrdinal || item.Name == "" || seen[item.Name] {
				return nil, fmt.Errorf("column metadata changed while reading the table")
			}
			seen[item.Name] = true
			req.AfterOrdinal = item.Ordinal
		}
		item.Comment = comment.String
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return items, err
	}
	return items, ctx.Err()
}

// Resolve the unqualified relation against the current connection's search_path.
func postgresRelationSchema(ctx context.Context, conn *sql.Conn, table string) (string, error) {
	var schema string
	err := conn.QueryRowContext(ctx, `SELECT n.nspname FROM pg_catalog.pg_class t JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace WHERE t.oid=pg_catalog.to_regclass($1)`, quoteIdentifier(table, dialect.PostgreSQL, '"')).Scan(&schema)
	return schema, err
}

func validateCatalogInput(conn *sql.Conn, d dialect.ID, database string, values ...string) error {
	if err := validateCatalogNames(conn, d, append([]string{database}, values...)...); err != nil {
		return err
	}
	if database == "" {
		return ErrCatalogForbidden
	}
	return nil
}

func validateCatalogNames(conn *sql.Conn, d dialect.ID, values ...string) error {
	if !d.Supported() {
		return ErrUnsupportedDialect
	}
	if conn == nil {
		return fmt.Errorf("catalog connection is required")
	}
	for _, value := range values {
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 256 || strings.ContainsRune(value, 0) {
			return fmt.Errorf("catalog identifier or search exceeds bounds")
		}
	}
	return nil
}
func catalogSearch(query string) string {
	return "%" + strings.TrimSuffix(likePrefix(query), "%") + "%"
}
func catalogLimit(limit int) int {
	if limit <= 0 || limit > MaxCatalogPageSize {
		return MaxCatalogPageSize
	}
	return limit
}

// ConsoleContextNames lists the namespaces the connected principal can select.
// MySQL's object tree uses the same account-visible database namespaces.
func ConsoleContextNames(ctx context.Context, conn *sql.Conn, d dialect.ID, database, after string, limit int) ([]string, error) {
	names := []string{}
	limit = catalogLimit(limit)
	var query string
	switch {
	case d.UsesMySQLSyntax():
		query = "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE BINARY SCHEMA_NAME > BINARY ? ORDER BY BINARY SCHEMA_NAME LIMIT ?"
	case d == dialect.ClickHouse:
		query = "SELECT name FROM system.databases WHERE name > ? ORDER BY name LIMIT ?"
	case d == dialect.SQLServer:
		query = "SELECT TOP (@p2) name FROM sys.databases WHERE HAS_DBACCESS(name) = 1 AND name COLLATE Latin1_General_BIN2 > @p1 ORDER BY name COLLATE Latin1_General_BIN2"
	default:
		schemas, err := SchemaRows(ctx, conn, d, database, after, limit)
		for _, schema := range schemas {
			names = append(names, schema.Name)
		}
		return names, err
	}
	rows, err := conn.QueryContext(ctx, query, after, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
