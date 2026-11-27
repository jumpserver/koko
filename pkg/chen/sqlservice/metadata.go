package sqlservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

const catalogTimeout = 3 * time.Second

var _ Catalog = connectionCatalog{}

type connectionCatalog struct {
	conn    *sql.Conn
	dialect dialect.ID
}

func (c connectionCatalog) Relations(ctx context.Context, r MetadataRequest) ([]Relation, error) {
	return Metadata(ctx, c.conn, c.dialect, r)
}
func (c connectionCatalog) Columns(ctx context.Context, r ColumnRequest) ([]Column, error) {
	return Columns(ctx, c.conn, c.dialect, r)
}

func Metadata(ctx context.Context, conn *sql.Conn, d dialect.ID, req MetadataRequest) ([]Relation, error) {
	items := []Relation{}
	if !d.Supported() {
		return items, ErrUnsupportedDialect
	}
	if conn == nil {
		return items, fmt.Errorf("metadata connection is required")
	}
	if req.Limit <= 0 || req.Limit > MaxCompletionItems {
		req.Limit = MaxCompletionItems
	}
	if !utf8.ValidString(req.Prefix) || utf8.RuneCountInString(req.Prefix) > 256 || strings.ContainsRune(req.Prefix, 0) {
		return items, fmt.Errorf("metadata prefix must be valid text of at most 256 characters")
	}
	if usesCatalogObjectSource(d) {
		return relationsFromCatalog(ctx, conn, d, req)
	}
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	var rows *sql.Rows
	var err error
	if d.UsesMySQLSyntax() {
		database := req.Context.Schema
		if database == "" {
			database = req.Context.Database
		}
		rows, err = conn.QueryContext(ctx, `SELECT TABLE_SCHEMA, TABLE_SCHEMA, TABLE_NAME, TABLE_TYPE, COALESCE(TABLE_COMMENT, '')
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = COALESCE(NULLIF(?, ''), DATABASE()) AND TABLE_NAME LIKE ? ESCAPE '!'
ORDER BY TABLE_NAME LIMIT ?`, database, likePrefix(req.Prefix), req.Limit)
	} else {
		rows, err = conn.QueryContext(ctx, `SELECT current_database(), n.nspname, c.relname,
CASE WHEN c.relkind IN ('v', 'm') THEN 'VIEW' ELSE 'BASE TABLE' END, COALESCE(obj_description(c.oid, 'pg_class'), '')
FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
AND (($1 = '' AND n.nspname = ANY(current_schemas(false)) AND pg_catalog.pg_table_is_visible(c.oid)) OR n.nspname = $1)
AND ($2 = '' OR current_database() = $2)
AND c.relname ILIKE $3 ESCAPE '!' AND n.nspname IN (SELECT schema_name FROM information_schema.schemata)
ORDER BY n.nspname, c.relname LIMIT $4`, req.Context.Schema, req.Context.Database, likePrefix(req.Prefix), req.Limit)
	}
	if err != nil {
		return items, fmt.Errorf("list relations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r Relation
		if err := rows.Scan(&r.Database, &r.Schema, &r.Name, &r.Kind, &r.Comment); err != nil {
			return nil, err
		}
		items = append(items, r)
	}
	return items, rows.Err()
}

func relationsFromCatalog(ctx context.Context, conn *sql.Conn, d dialect.ID, req MetadataRequest) ([]Relation, error) {
	items := []Relation{}
	if err := validateCatalogInput(conn, d, req.Context.Database, req.Context.Schema, req.Prefix); err != nil {
		return items, err
	}
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	schema, err := resolveMetadataSchema(ctx, conn, d, req.Context, "")
	if err != nil {
		return items, err
	}
	var clickHouse *clickHouseCapabilities
	if d == dialect.ClickHouse {
		clickHouse, err = probeClickHouseCapabilities(ctx, conn, "objects")
		if err != nil {
			return items, err
		}
	}
	query, err := catalogRelationQuery(d, CatalogRelationRequest{Database: req.Context.Database, Schema: schema, Query: req.Prefix}, true, req.Limit, clickHouse)
	if err != nil {
		return items, err
	}
	rows, err := queryCatalogRows(ctx, conn, d, query.SQL, query.Args...)
	if err != nil {
		return items, fmt.Errorf("list relations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		relation, err := scanCatalogRelation(rows)
		if err != nil {
			return nil, err
		}
		kind := "BASE TABLE"
		if relation.Kind == "view" {
			kind = "VIEW"
		}
		item := Relation{Database: relation.Database, Schema: relation.Schema, Name: relation.Name, Kind: kind, Comment: relation.Comment}
		items = append(items, item)
	}
	return items, rows.Err()
}

func Columns(ctx context.Context, conn *sql.Conn, d dialect.ID, req ColumnRequest) ([]Column, error) {
	items := []Column{}
	if !d.Supported() {
		return items, ErrUnsupportedDialect
	}
	if conn == nil {
		return items, fmt.Errorf("metadata connection is required")
	}
	if req.Table == "" {
		return items, fmt.Errorf("table is required")
	}
	for _, name := range []string{req.Table, req.Schema} {
		if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 256 || strings.ContainsRune(name, 0) {
			return items, fmt.Errorf("identifier must be valid text of at most 256 characters")
		}
	}
	if usesCatalogObjectSource(d) {
		return columnsFromCatalog(ctx, conn, d, req)
	}
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	var rows *sql.Rows
	var err error
	if d.UsesMySQLSyntax() {
		database := req.Schema
		if database == "" {
			database = req.Context.Schema
		}
		if database == "" {
			database = req.Context.Database
		}
		rows, err = conn.QueryContext(ctx, `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE = 'YES', COALESCE(COLUMN_COMMENT, '')
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = COALESCE(NULLIF(?, ''), DATABASE()) AND TABLE_NAME = ?
ORDER BY ORDINAL_POSITION LIMIT 2000`, database, req.Table)
	} else {
		schema := req.Schema
		// Only unqualified names use the connection's live search_path. An
		// explicitly named, catalog-visible schema need not grant USAGE merely
		// to expose column metadata, so resolve it directly in the catalogs.
		reference := `a.attrelid = to_regclass(format('%I', $2)) AND $1 = ''`
		if schema != "" {
			reference = `n.nspname = $1 AND c.relname = $2`
		}
		rows, err = conn.QueryContext(ctx, `SELECT a.attname, pg_catalog.format_type(a.atttypid, a.atttypmod), NOT (a.attnotnull OR t.typnotnull),
COALESCE(col_description(a.attrelid, a.attnum), '')
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_type t ON t.oid = a.atttypid
JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE `+reference+`
AND a.attnum > 0 AND NOT a.attisdropped AND ($3 = '' OR current_database() = $3)
AND n.nspname IN (SELECT schema_name FROM information_schema.schemata)
ORDER BY a.attnum LIMIT 2000`, schema, req.Table, req.Context.Database)
	}
	if err != nil {
		return items, fmt.Errorf("list columns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.Name, &c.DataType, &c.Nullable, &c.Comment); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

func columnsFromCatalog(ctx context.Context, conn *sql.Conn, d dialect.ID, req ColumnRequest) ([]Column, error) {
	items := []Column{}
	if err := validateCatalogInput(conn, d, req.Context.Database, req.Schema, req.Table, req.Context.Schema); err != nil {
		return items, err
	}
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	schema, err := resolveMetadataSchema(ctx, conn, d, req.Context, req.Schema)
	if err != nil {
		return items, err
	}
	if err := CatalogRelationAccess(ctx, conn, d, schema, req.Table); err != nil {
		// SQL Server resolves unqualified tables in the default schema, then
		// dbo. Explicit schema qualifiers must never take this fallback.
		if d != dialect.SQLServer || req.Schema != "" || schema == "dbo" || !errors.Is(err, ErrCatalogNotFound) {
			return items, err
		}
		schema = "dbo"
		if err := CatalogRelationAccess(ctx, conn, d, schema, req.Table); err != nil {
			return items, err
		}
	}
	query, err := catalogColumnQuery(d, CatalogColumnRequest{Database: req.Context.Database, Schema: schema, Table: req.Table}, 2000, catalogBasicColumns)
	if err != nil {
		return items, err
	}
	rows, err := queryCatalogRows(ctx, conn, d, query.SQL, query.Args...)
	if err != nil {
		return items, fmt.Errorf("list columns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item Column
		var database, owner, table string
		var ordinal int
		var comment sql.NullString
		if err := rows.Scan(&database, &owner, &table, &item.Name, &item.DataType, &item.Nullable, &comment, &ordinal); err != nil {
			return nil, err
		}
		item.Comment = comment.String
		item.DataType = strings.TrimSpace(item.DataType)
		items = append(items, item)
	}
	return items, rows.Err()
}

// Resolve the namespace on the authorized database connection, including
// when the request supplies an explicit schema.
func resolveMetadataSchema(ctx context.Context, conn *sql.Conn, d dialect.ID, scope Context, schema string) (string, error) {
	if err := catalogDatabaseAccess(ctx, conn, d, scope.Database); err != nil {
		return "", err
	}
	if schema == "" {
		schema = scope.Schema
	}
	if schema == "" {
		var err error
		schema, err = readCatalogDefaultSchema(ctx, conn, d, scope.Database)
		if err != nil {
			return "", err
		}
	}
	if d == dialect.ClickHouse && schema != scope.Database {
		return "", ErrCatalogForbidden
	}
	return schema, nil
}

func likePrefix(s string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(s) + "%"
}
