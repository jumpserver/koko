package sqlservice

import (
	"context"
	"database/sql"
	"fmt"
)

// DatabaseRows discovers database roots using the authenticated account. A
// database name is not a schema for engines whose connection selects a service
// or database: Oracle-family and Db2 expose only that connection's database.
// Like SchemaRows, it returns limit+1 rows for keyset pagination.
func DatabaseRows(ctx context.Context, conn *sql.Conn, dialect Dialect, database, after string, limit int) ([]CatalogSchema, error) {
	items := []CatalogSchema{}
	if err := validateCatalogNames(conn, dialect, database, after); err != nil {
		return items, err
	}
	if IsMySQLDialect(dialect) {
		return SchemaRows(ctx, conn, dialect, database, after, limit)
	}
	ctx, cancel := catalogReadContext(ctx)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return items, err
	}
	var query string
	switch dialect {
	case PostgreSQL:
		// Match the native catalog shown by Java. Seeing a database name does
		// not grant CONNECT: expanding it opens an independently authorized
		// target connection, which can still be denied by the server.
		query = `SELECT d.datname, d.datname FROM pg_catalog.pg_database d
WHERE NOT d.datistemplate
AND d.datname COLLATE pg_catalog."C" > $1 COLLATE pg_catalog."C"
ORDER BY d.datname COLLATE pg_catalog."C" LIMIT $2`
	case SQLServer:
		query = `SELECT TOP (@p2) name, name FROM sys.databases
WHERE name COLLATE Latin1_General_BIN2 > @p1
ORDER BY name COLLATE Latin1_General_BIN2`
	case ClickHouse:
		query = `SELECT name, name FROM system.databases WHERE name > ? ORDER BY name LIMIT ?`
	default:
		if database == "" {
			return items, ErrCatalogForbidden
		}
		if err := catalogDatabaseAccess(ctx, conn, dialect, database); err != nil {
			return items, err
		}
		if database > after {
			items = append(items, CatalogSchema{Database: database, Name: database})
		}
		return items, nil
	}
	rows, err := conn.QueryContext(ctx, query, after, catalogLimit(limit)+1)
	if err != nil {
		return items, fmt.Errorf("list catalog databases: %w", err)
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
