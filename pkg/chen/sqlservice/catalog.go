package sqlservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
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
type CatalogObject struct {
	Database   string `json:"database"`
	Schema     string `json:"schema"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	ObjectType string `json:"object_type"`
	Comment    string `json:"comment"`
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
type CatalogObjectRequest struct {
	Database, Schema, Kind, Query, AfterSchema, AfterName string
	Limit                                                 int
}
type CatalogColumnRequest struct {
	Database, Schema, Table string
	AfterOrdinal, Limit     int
}

// MySQL lists all databases visible to the connected account, including when
// no default database was selected. Other dialects list schemas in the supplied
// database. Catalog reads never change connection context and return at most
// limit+1 rows for keyset pagination without a COUNT query.
func SchemaRows(ctx context.Context, conn *sql.Conn, dialect Dialect, database, after string, limit int) ([]CatalogSchema, error) {
	items := []CatalogSchema{}
	if err := validateCatalogNames(conn, dialect, database, after); err != nil {
		return items, err
	}
	ctx, cancel := catalogReadContext(ctx)
	defer cancel()
	if !IsMySQLDialect(dialect) {
		if database == "" {
			return items, ErrCatalogForbidden
		}
		if err := catalogDatabaseAccess(ctx, conn, dialect, database); err != nil {
			return items, err
		}
	}
	var rows *sql.Rows
	var err error
	if extraCatalogDialect(dialect) {
		var query catalogQuery
		query, err = extraSchemaQuery(dialect, database, after, limit)
		if err == nil {
			rows, err = queryCatalogRows(ctx, conn, dialect, query.SQL, query.Args...)
		}
	} else if IsMySQLDialect(dialect) {
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

func ObjectRows(ctx context.Context, conn *sql.Conn, dialect Dialect, req CatalogObjectRequest) ([]CatalogObject, error) {
	items := []CatalogObject{}
	if err := validateCatalogInput(conn, dialect, req.Database, req.Schema, req.Query, req.AfterSchema, req.AfterName); err != nil {
		return items, err
	}
	if req.Kind != "" && req.Kind != "table" && req.Kind != "view" {
		return items, fmt.Errorf("catalog kind must be table or view")
	}
	if (IsMySQLDialect(dialect) || dialect == ClickHouse) && req.Schema != "" && req.Schema != req.Database {
		return items, ErrCatalogForbidden
	}
	ctx, cancel := catalogReadContext(ctx)
	defer cancel()
	if err := catalogDatabaseAccess(ctx, conn, dialect, req.Database); err != nil {
		return items, err
	}
	if dialect == PostgreSQL && req.Schema != "" {
		if err := catalogSchemaAccess(ctx, conn, req.Schema); err != nil {
			return items, err
		}
	}
	var rows *sql.Rows
	var err error
	if extraCatalogDialect(dialect) {
		var query catalogQuery
		var clickHouse *clickHouseCapabilities
		if dialect == ClickHouse {
			clickHouse, err = probeClickHouseCapabilities(ctx, conn, "objects")
			if err != nil {
				return items, err
			}
		}
		query, err = extraObjectQuery(dialect, req, false, catalogLimit(req.Limit)+1, clickHouse)
		if err == nil {
			rows, err = queryCatalogRows(ctx, conn, dialect, query.SQL, query.Args...)
		}
	} else if IsMySQLDialect(dialect) {
		// MySQL's INFORMATION_SCHEMA applies the current account's table and
		// column privileges itself, including active roles. No grant parsing or
		// elevated helper account is used.
		schemaMatch := "BINARY TABLE_SCHEMA = BINARY ?"
		args := []any{req.Database}
		if dialect == MySQL || dialect == MariaDB {
			// Let the dictionary narrow the schema before the exact check.
			schemaMatch = "TABLE_SCHEMA = ? AND " + schemaMatch
			args = append(args, req.Database)
		}
		args = append(args, req.Kind, req.Kind, req.Kind, catalogSearch(req.Query), req.AfterSchema, req.AfterSchema, req.AfterName, catalogLimit(req.Limit)+1)
		rows, err = conn.QueryContext(CatalogQueryContext(ctx), `SELECT TABLE_SCHEMA, TABLE_SCHEMA, TABLE_NAME,
CASE WHEN TABLE_TYPE = 'VIEW' THEN 'view' ELSE 'table' END,
CASE WHEN TABLE_TYPE = 'VIEW' THEN 'view' ELSE 'table' END, COALESCE(TABLE_COMMENT, '')
FROM information_schema.TABLES
WHERE `+schemaMatch+` AND TABLE_TYPE IN ('BASE TABLE', 'VIEW', 'SYSTEM VIEW', 'SYSTEM VERSIONED')
AND (? = '' OR (? = 'table' AND TABLE_TYPE <> 'VIEW') OR (? = 'view' AND TABLE_TYPE = 'VIEW'))
AND TABLE_NAME LIKE ? ESCAPE '!'
AND (BINARY TABLE_SCHEMA > BINARY ? OR (BINARY TABLE_SCHEMA = BINARY ? AND BINARY TABLE_NAME > BINARY ?))
ORDER BY BINARY TABLE_SCHEMA, BINARY TABLE_NAME LIMIT ?`, args...)
	} else {
		rows, err = conn.QueryContext(ctx, `SELECT pg_catalog.current_database(), n.nspname, c.relname,
CASE WHEN c.relkind IN ('v', 'm') THEN 'view' ELSE 'table' END,
CASE c.relkind WHEN 'm' THEN 'materialized_view' WHEN 'f' THEN 'foreign_table' WHEN 'p' THEN 'partitioned_table' WHEN 'v' THEN 'view' ELSE 'table' END,
COALESCE(pg_catalog.obj_description(c.oid, 'pg_class'), '')
FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE pg_catalog.current_database() = $1 AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
AND ($2 = '' OR n.nspname = $2) AND n.nspname IN (SELECT schema_name FROM information_schema.schemata)
AND ($3 = '' OR ($3 = 'table' AND c.relkind IN ('r', 'p', 'f')) OR ($3 = 'view' AND c.relkind IN ('v', 'm')))
AND c.relname ILIKE $4 ESCAPE '!'
AND (n.nspname, c.relname) > ($5::pg_catalog.name, $6::pg_catalog.name)
ORDER BY n.nspname, c.relname LIMIT $7`, req.Database, req.Schema, req.Kind, catalogSearch(req.Query), req.AfterSchema, req.AfterName, catalogLimit(req.Limit)+1)
	}
	if err != nil {
		return items, fmt.Errorf("list catalog objects: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item CatalogObject
		var comment sql.NullString
		if err := rows.Scan(&item.Database, &item.Schema, &item.Name, &item.Kind, &item.ObjectType, &comment); err != nil {
			return nil, err
		}
		item.Comment = comment.String
		items = append(items, item)
	}
	return items, rows.Err()
}

func ColumnRows(ctx context.Context, conn *sql.Conn, dialect Dialect, req CatalogColumnRequest) ([]CatalogColumn, error) {
	return columnRows(ctx, conn, dialect, req, false, false)
}

// MySQLSearchColumns reads names and comments for objects already returned by
// ObjectRows on this connection. INFORMATION_SCHEMA still applies the account's
// active grants, and each table keeps its own bounded page with one extra row.
// The caller owns one deadline for the whole catalog search.
func MySQLSearchColumns(ctx context.Context, conn *sql.Conn, database string, objects []CatalogObject, limit int) (map[string][]CatalogColumn, error) {
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
func tableColumnRows(ctx context.Context, conn *sql.Conn, dialect Dialect, req CatalogColumnRequest, editable bool) ([]CatalogColumn, error) {
	req.AfterOrdinal = 0
	return columnRows(ctx, conn, dialect, req, true, editable)
}

// A table operation has already admitted this object and needs its complete
// definition. Directory calls check access here and return a bounded page.
func columnRows(ctx context.Context, conn *sql.Conn, dialect Dialect, req CatalogColumnRequest, complete, editable bool) ([]CatalogColumn, error) {
	items := []CatalogColumn{}
	if IsMySQLDialect(dialect) && req.Schema != "" {
		req.Database = req.Schema
	}
	if err := validateCatalogInput(conn, dialect, req.Database, req.Schema, req.Table); err != nil {
		return items, err
	}
	if req.Table == "" || req.AfterOrdinal < 0 {
		return items, fmt.Errorf("catalog table and nonnegative ordinal are required")
	}
	if (IsMySQLDialect(dialect) || dialect == ClickHouse) && req.Schema == "" {
		req.Schema = req.Database
	}
	if req.Schema == "" {
		return items, fmt.Errorf("catalog schema is required")
	}
	if dialect == ClickHouse && req.Schema != req.Database {
		return items, ErrCatalogForbidden
	}
	ctx, cancel := MetadataContext(ctx, dialect)
	defer cancel()
	if !complete {
		if err := catalogDatabaseAccess(ctx, conn, dialect, req.Database); err != nil {
			return items, err
		}
		access := CatalogObjectAccess
		if editable {
			access = catalogEditableObjectAccess
		}
		if err := access(ctx, conn, dialect, req.Schema, req.Table); err != nil {
			return items, err
		}
	}
	limit := 0
	if !complete {
		limit = catalogLimit(req.Limit) + 1
	}
	var rows *sql.Rows
	var err error
	if extraCatalogDialect(dialect) {
		var query catalogQuery
		query, err = extraColumnQuery(dialect, req, limit, true)
		if err == nil {
			rows, err = queryCatalogRows(ctx, conn, dialect, query.SQL, query.Args...)
			if err != nil && dialect == DB2 && catalogMissingStringUnits(err) {
				query, err = extraColumnQuery(dialect, req, limit, true, false)
				if err == nil {
					rows, err = queryCatalogRows(ctx, conn, dialect, query.SQL, query.Args...)
				}
			}
		}
	} else if IsMySQLDialect(dialect) {
		target := "BINARY TABLE_SCHEMA = BINARY ? AND BINARY TABLE_NAME = BINARY ?"
		args := []any{req.Schema, req.Table}
		if dialect != MySQL || !complete {
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
		modifiers := (IsOracleDialect(dialect) || dialect == DB2) && len(columnNames) == 13
		if modifiers {
			dest = append(dest, &length, &precision, &scale, &charLength, &units)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if modifiers {
			item.DataType = catalogColumnType(dialect, item.DataType, length, precision, scale, charLength, units)
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

func catalogDatabaseAccess(ctx context.Context, conn *sql.Conn, dialect Dialect, database string) error {
	if extraCatalogDialect(dialect) {
		return extraCatalogDatabaseAccess(ctx, conn, dialect, database)
	}
	var allowed bool
	var err error
	if IsMySQLDialect(dialect) {
		err = conn.QueryRowContext(CatalogQueryContext(ctx), `SELECT EXISTS (SELECT 1 FROM information_schema.SCHEMATA WHERE BINARY SCHEMA_NAME = BINARY ?)`, database).Scan(&allowed)
	} else {
		err = conn.QueryRowContext(ctx, `SELECT pg_catalog.current_database() = $1`, database).Scan(&allowed)
	}
	if err != nil {
		return fmt.Errorf("check catalog database: %w", err)
	}
	if !allowed {
		return ErrCatalogForbidden
	}
	return nil
}
func catalogSchemaAccess(ctx context.Context, conn *sql.Conn, schema string) error {
	var allowed bool
	err := conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)`, schema).Scan(&allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCatalogNotFound
	}
	if err != nil {
		return fmt.Errorf("check catalog schema: %w", err)
	}
	if !allowed {
		return ErrCatalogForbidden
	}
	return nil
}

// CatalogObjectAccess checks visibility of an exact relation in the catalog. PostgreSQL and Db2 expose catalog
// rows independently of SELECT privileges on the described table. Actual reads
// still run as the account, and editing uses the separate admission check below.
func CatalogObjectAccess(ctx context.Context, conn *sql.Conn, dialect Dialect, schema, table string) error {
	if dialect != PostgreSQL {
		return catalogObjectAccessMode(ctx, conn, dialect, schema, table, false)
	}
	var schemaVisible, objectExists bool
	err := conn.QueryRowContext(ctx, `SELECT
EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = $1),
EXISTS (SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p', 'v', 'm', 'f'))`, schema, table).Scan(&schemaVisible, &objectExists)
	if err != nil {
		return fmt.Errorf("check catalog object: %w", err)
	}
	if !schemaVisible {
		return ErrCatalogForbidden
	}
	if !objectExists {
		return ErrCatalogNotFound
	}
	return nil
}

func catalogEditableObjectAccess(ctx context.Context, conn *sql.Conn, dialect Dialect, schema, table string) error {
	return catalogObjectAccessMode(ctx, conn, dialect, schema, table, true)
}

func catalogObjectAccessMode(ctx context.Context, conn *sql.Conn, dialect Dialect, schema, table string, editable bool) error {
	if extraCatalogDialect(dialect) {
		return extraCatalogObjectAccess(ctx, conn, dialect, schema, table, editable)
	}
	if IsMySQLDialect(dialect) {
		var exists bool
		query := `SELECT EXISTS (SELECT 1 FROM information_schema.TABLES
WHERE BINARY TABLE_SCHEMA = BINARY ? AND BINARY TABLE_NAME = BINARY ? AND TABLE_TYPE IN ('BASE TABLE', 'VIEW', 'SYSTEM VIEW', 'SYSTEM VERSIONED'))`
		err := conn.QueryRowContext(CatalogQueryContext(ctx), query, schema, table).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check catalog object: %w", err)
		}
		// MySQL hides inaccessible tables in INFORMATION_SCHEMA. Absence may
		// mean nonexistent or hidden; do not probe user tables to distinguish.
		if !exists {
			return ErrCatalogNotFound
		}
		return nil
	}
	_, err := postgresEditableObjectKind(ctx, conn, schema, table)
	return err
}

func postgresEditableObjectKind(ctx context.Context, conn *sql.Conn, schema, table string) (string, error) {
	var kind string
	var schemaAllowed, objectAllowed, columnsAllowed bool
	err := conn.QueryRowContext(ctx, `SELECT c.relkind::text, pg_catalog.has_schema_privilege(n.oid, 'USAGE'),
(pg_catalog.has_table_privilege(c.oid, 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR pg_catalog.has_any_column_privilege(c.oid, 'SELECT,INSERT,UPDATE,REFERENCES')),
pg_catalog.has_any_column_privilege(c.oid, 'SELECT,INSERT,UPDATE,REFERENCES')
FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p', 'v', 'm', 'f')`, schema, table).Scan(&kind, &schemaAllowed, &objectAllowed, &columnsAllowed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCatalogNotFound
	}
	if err != nil {
		return "", fmt.Errorf("check catalog object: %w", err)
	}
	if !schemaAllowed || !objectAllowed || !columnsAllowed {
		return "", ErrCatalogForbidden
	}
	return kind, nil
}
func validateCatalogInput(conn *sql.Conn, dialect Dialect, database string, values ...string) error {
	if err := validateCatalogNames(conn, dialect, append([]string{database}, values...)...); err != nil {
		return err
	}
	if database == "" {
		return ErrCatalogForbidden
	}
	return nil
}

func validateCatalogNames(conn *sql.Conn, dialect Dialect, values ...string) error {
	if !supported(dialect) {
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
func ConsoleContextNames(ctx context.Context, conn *sql.Conn, d Dialect, database, after string, limit int) ([]string, error) {
	names := []string{}
	limit = catalogLimit(limit)
	var query string
	switch {
	case IsMySQLDialect(d):
		query = "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE BINARY SCHEMA_NAME > BINARY ? ORDER BY BINARY SCHEMA_NAME LIMIT ?"
	case d == ClickHouse:
		query = "SELECT name FROM system.databases WHERE name > ? ORDER BY name LIMIT ?"
	case d == SQLServer:
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
