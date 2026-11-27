package sqlservice

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

// CatalogRelation is the table/view projection used by browsing, completion
// and table operations. ObjectType retains the native relation subtype.
type CatalogRelation struct {
	Database   string `json:"database"`
	Schema     string `json:"schema"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	ObjectType string `json:"object_type"`
	Comment    string `json:"comment"`
}

type CatalogRelationRequest struct {
	Database, Schema, Kind, Query, AfterSchema, AfterName string
	Limit                                                 int
}

// The original exported names describe the relation projection of /catalog/objects.
type CatalogObject = CatalogRelation
type CatalogObjectRequest = CatalogRelationRequest

func ObjectRows(ctx context.Context, conn *sql.Conn, d dialect.ID, req CatalogObjectRequest) ([]CatalogObject, error) {
	return RelationRows(ctx, conn, d, req)
}

func CatalogObjectAccess(ctx context.Context, conn *sql.Conn, d dialect.ID, schema, table string) error {
	return CatalogRelationAccess(ctx, conn, d, schema, table)
}

const postgresRelationProjection = `pg_catalog.current_database(), n.nspname, c.relname,
CASE WHEN c.relkind IN ('v', 'm') THEN 'view' ELSE 'table' END,
CASE c.relkind WHEN 'm' THEN 'materialized_view' WHEN 'f' THEN 'foreign_table' WHEN 'p' THEN 'partitioned_table' WHEN 'v' THEN 'view' ELSE 'table' END`

func RelationRows(ctx context.Context, conn *sql.Conn, d dialect.ID, req CatalogRelationRequest) ([]CatalogRelation, error) {
	items := []CatalogRelation{}
	if err := validateCatalogInput(conn, d, req.Database, req.Schema, req.Query, req.AfterSchema, req.AfterName); err != nil {
		return items, err
	}
	if req.Kind != "" && req.Kind != "table" && req.Kind != "view" {
		return items, fmt.Errorf("catalog kind must be table or view")
	}
	ctx, cancel := catalogReadContext(ctx)
	defer cancel()
	if err := catalogScopeAccess(ctx, conn, d, req.Database, req.Schema); err != nil {
		return items, err
	}
	var rows *sql.Rows
	var err error
	if usesCatalogObjectSource(d) {
		var query catalogQuery
		var clickHouse *clickHouseCapabilities
		if d == dialect.ClickHouse {
			clickHouse, err = probeClickHouseCapabilities(ctx, conn, "objects")
			if err != nil {
				return items, err
			}
		}
		query, err = catalogRelationQuery(d, req, false, catalogLimit(req.Limit)+1, clickHouse)
		if err == nil {
			rows, err = queryCatalogRows(ctx, conn, d, query.SQL, query.Args...)
		}
	} else if d.UsesMySQLSyntax() {
		// MySQL's INFORMATION_SCHEMA applies the current account's table and
		// column privileges itself, including active roles. No grant parsing or
		// elevated helper account is used.
		schemaMatch := "BINARY TABLE_SCHEMA = BINARY ?"
		args := []any{req.Database}
		if d == dialect.MySQL || d == dialect.MariaDB {
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
		rows, err = conn.QueryContext(ctx, `SELECT `+postgresRelationProjection+`,
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
		item, err := scanCatalogRelation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanCatalogRelation(rows *sql.Rows) (CatalogRelation, error) {
	var relation CatalogRelation
	var comment sql.NullString
	err := rows.Scan(&relation.Database, &relation.Schema, &relation.Name, &relation.Kind, &relation.ObjectType, &comment)
	relation.Comment = comment.String
	return relation, err
}
