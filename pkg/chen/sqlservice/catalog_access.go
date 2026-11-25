package sqlservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

// Db2 catalog visibility does not grant data access. Grid editing retains its
// stricter admission check using effective users/groups/roles and DATAACCESS.
// If these routines are unavailable, editing fails rather than broadening scope.
const db2Principals = `(SELECT CURRENT USER AS AUTHID, 'U' AS AUTHTYPE FROM SYSIBM.SYSDUMMY1
UNION SELECT 'PUBLIC', 'G' FROM SYSIBM.SYSDUMMY1
UNION SELECT "GROUP", 'G' FROM TABLE(SYSPROC.AUTH_LIST_GROUPS_FOR_AUTHID(CURRENT USER)) AS G
UNION SELECT ROLENAME, 'R' FROM TABLE(SYSPROC.AUTH_LIST_ROLES_FOR_AUTHID(CURRENT USER, 'U')) AS R)`

const db2TableVisible = `(t.OWNER = CURRENT USER OR EXISTS (
SELECT 1 FROM SYSCAT.TABAUTH a JOIN ` + db2Principals + ` p ON p.AUTHID = a.GRANTEE AND p.AUTHTYPE = a.GRANTEETYPE
WHERE a.TABSCHEMA = t.TABSCHEMA AND a.TABNAME = t.TABNAME AND (a.SELECTAUTH IN ('Y','G') OR a.CONTROLAUTH = 'Y'))
OR EXISTS (SELECT 1 FROM TABLE(SYSPROC.AUTH_LIST_AUTHORITIES_FOR_AUTHID(CURRENT USER, 'U')) AS a
WHERE a.AUTHORITY = 'DATAACCESS' AND 'Y' IN (a.D_USER,a.D_GROUP,a.D_PUBLIC,a.ROLE_USER,a.ROLE_GROUP,a.ROLE_PUBLIC,a.D_ROLE)))`

func catalogDatabaseAccess(ctx context.Context, conn *sql.Conn, d dialect.ID, database string) error {
	if usesCatalogObjectSource(d) {
		return checkCatalogDatabaseName(ctx, conn, d, database)
	}
	var allowed bool
	var err error
	if d.UsesMySQLSyntax() {
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
func CatalogObjectAccess(ctx context.Context, conn *sql.Conn, d dialect.ID, schema, table string) error {
	if d != dialect.PostgreSQL {
		return catalogObjectAccessMode(ctx, conn, d, schema, table, false)
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

func catalogEditableObjectAccess(ctx context.Context, conn *sql.Conn, d dialect.ID, schema, table string) error {
	return catalogObjectAccessMode(ctx, conn, d, schema, table, true)
}

func catalogObjectAccessMode(ctx context.Context, conn *sql.Conn, d dialect.ID, schema, table string, editable bool) error {
	if usesCatalogObjectSource(d) {
		return checkCatalogObjectSource(ctx, conn, d, schema, table, editable)
	}
	if d.UsesMySQLSyntax() {
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

func checkCatalogDatabaseName(ctx context.Context, conn *sql.Conn, d dialect.ID, database string) error {
	var current string
	var query string
	switch d {
	case dialect.SQLServer:
		query = "SELECT DB_NAME()"
	case dialect.DB2:
		query = "SELECT RTRIM(CURRENT SERVER) FROM SYSIBM.SYSDUMMY1"
	case dialect.ClickHouse:
		query = "SELECT currentDatabase()"
	case dialect.Oracle, dialect.Dameng, dialect.OceanBaseOracle:
		// Oracle-family Database identifies a connection service, not a schema.
		// ALL_* stays in that service; comparing it to DB_NAME would incorrectly
		// reject PDB/service aliases. Session authorization fixes this value and
		// the connection itself, and requests cannot switch either of them.
		return nil
	default:
		return ErrUnsupportedDialect
	}
	if err := conn.QueryRowContext(ctx, query).Scan(&current); err != nil {
		return fmt.Errorf("check catalog database: %w", err)
	}
	if current != database {
		return ErrCatalogForbidden
	}
	return nil
}

func checkCatalogObjectSource(ctx context.Context, conn *sql.Conn, d dialect.ID, schema, table string, editable bool) error {
	b := &catalogBinder{dialect: d}
	source, err := b.objectSource(schema, editable)
	if err != nil {
		return err
	}
	query := "SELECT objectname FROM (" + source + ") objects WHERE schemaname = " + b.bind(schema) + " AND objectname = " + b.bind(table)
	rows, err := queryCatalogRows(ctx, conn, d, query, b.args...)
	if err != nil {
		return fmt.Errorf("check catalog object: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return ErrCatalogNotFound
	}
	var name string
	if err := rows.Scan(&name); err != nil {
		return fmt.Errorf("check catalog object: %w", err)
	}
	return nil
}
