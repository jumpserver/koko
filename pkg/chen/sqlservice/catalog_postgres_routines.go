package sqlservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

type ObjectKind string

const (
	FunctionKind  ObjectKind = "function"
	ProcedureKind ObjectKind = "procedure"
	TriggerKind   ObjectKind = "trigger"
)

func (k ObjectKind) IsRoutine() bool { return k == FunctionKind || k == ProcedureKind }

type ObjectTypeRef struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
}

type ObjectRef struct {
	Database   string          `json:"database"`
	Schema     string          `json:"schema"`
	Name       string          `json:"name"`
	Kind       ObjectKind      `json:"kind"`
	Table      string          `json:"table,omitempty"`
	InputTypes []ObjectTypeRef `json:"input_types,omitzero"`
}

type ObjectSummary struct {
	Ref        ObjectRef `json:"ref"`
	OID        uint32    `json:"oid,omitempty"`
	Label      string    `json:"label"`
	Signature  string    `json:"signature,omitempty"`
	ObjectType string    `json:"object_type,omitempty"`
}

type RoutineListRequest struct {
	Database, Schema, Query, AfterSchema, AfterName string
	Kind, AfterKind                                 ObjectKind
	AfterOID                                        uint32
	Limit, ServerVersion                            int
}

func PostgreSQLCatalogVersion(ctx context.Context, conn *sql.Conn) (int, error) {
	var version int
	err := conn.QueryRowContext(ctx, "SELECT pg_catalog.current_setting('server_version_num')::integer").Scan(&version)
	return version, err
}

// PostgreSQLRoutineRows reads at most one page plus a lookahead on the supplied
// connection. proargtypes is the declared input vector, including INOUT and
// variadic array types. The OID identifies the current instance within its database.
func PostgreSQLRoutineRows(ctx context.Context, conn *sql.Conn, req RoutineListRequest) ([]ObjectSummary, error) {
	if err := validateCatalogInput(conn, dialect.PostgreSQL, req.Database, req.Schema, req.Query, req.AfterSchema, req.AfterName); err != nil {
		return nil, err
	}
	if req.Kind != "" && !req.Kind.IsRoutine() {
		return nil, fmt.Errorf("invalid PostgreSQL routine kind")
	}
	if req.AfterSchema != "" || req.AfterName != "" || req.AfterKind != "" || req.AfterOID != 0 {
		if req.AfterSchema == "" || req.AfterName == "" || req.AfterOID == 0 || !req.AfterKind.IsRoutine() || (req.Kind != "" && req.AfterKind != req.Kind) {
			return nil, fmt.Errorf("invalid PostgreSQL routine continuation")
		}
	}
	if req.ServerVersion < 100000 || req.ServerVersion < 110000 && req.Kind == ProcedureKind {
		return nil, ErrCatalogUnsupported
	}
	ctx, cancel := catalogReadContext(ctx)
	defer cancel()
	if err := catalogScopeAccess(ctx, conn, dialect.PostgreSQL, req.Database, req.Schema); err != nil {
		return nil, err
	}
	procedure, filter := postgresRoutineKinds(req.ServerVersion)
	var filterProcedure any
	if req.Kind != "" {
		filterProcedure = req.Kind == ProcedureKind
	}
	// Boolean ordering puts functions before procedures under any collation.
	query := `WITH page AS (
 SELECT n.nspname, p.proname, p.oid, p.proargtypes, ` + procedure + ` AS is_procedure
 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
 WHERE ` + filter + ` AND ($1 = '' OR n.nspname = $1)
 AND ($2::boolean IS NULL OR ` + procedure + ` = $2)
 AND ` + postgresRoutineVisible + `
 AND p.proname ILIKE $3 ESCAPE '!'
 AND (n.nspname, p.proname, ` + procedure + `, p.oid) > ($4::pg_catalog.name, $5::pg_catalog.name, $6::boolean, $7::pg_catalog.oid)
 ORDER BY n.nspname, p.proname, is_procedure, p.oid LIMIT $8
)
SELECT page.nspname, page.proname, page.is_procedure, page.oid,
 pg_catalog.format('%I(%s)', page.proname, pg_catalog.oidvectortypes(page.proargtypes)),
 COALESCE(pg_catalog.json_agg(pg_catalog.json_build_object('schema', ns.nspname, 'name', t.typname)
   ORDER BY a.position) FILTER (WHERE a.position IS NOT NULL), '[]'::pg_catalog.json)
FROM page
LEFT JOIN LATERAL pg_catalog.unnest(page.proargtypes) WITH ORDINALITY AS a(type_oid, position) ON true
LEFT JOIN pg_catalog.pg_type t ON t.oid = a.type_oid
LEFT JOIN pg_catalog.pg_namespace ns ON ns.oid = t.typnamespace
GROUP BY page.nspname, page.proname, page.is_procedure, page.oid, page.proargtypes
ORDER BY page.nspname, page.proname, page.is_procedure, page.oid`
	rows, err := conn.QueryContext(ctx, query, req.Schema, filterProcedure, catalogSearch(req.Query),
		req.AfterSchema, req.AfterName, req.AfterKind == ProcedureKind, req.AfterOID, catalogLimit(req.Limit)+1)
	if err != nil {
		return nil, fmt.Errorf("list PostgreSQL routines: %w", err)
	}
	defer rows.Close()
	items := []ObjectSummary{}
	for rows.Next() {
		var item ObjectSummary
		var types []byte
		var isProcedure bool
		if err := rows.Scan(&item.Ref.Schema, &item.Ref.Name, &isProcedure, &item.OID, &item.Signature, &types); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(types, &item.Ref.InputTypes); err != nil {
			return nil, err
		}
		item.Ref.Database, item.Label = req.Database, item.Ref.Name
		item.Ref.Kind = FunctionKind
		if isProcedure {
			item.Ref.Kind = ProcedureKind
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const postgresRoutineVisible = `n.nspname IN (SELECT schema_name FROM information_schema.schemata)
 AND (pg_catalog.pg_has_role(p.proowner, 'USAGE') OR pg_catalog.has_function_privilege(p.oid, 'EXECUTE'))`

func postgresRoutineKinds(version int) (procedure, filter string) {
	// OIDs below FirstNormalObjectId belong to built-in window functions.
	if version < 110000 {
		return "false", "NOT p.proisagg AND (NOT p.proiswindow OR p.oid >= 16384)"
	}
	return "(p.prokind = 'p')", "(p.prokind IN ('f','p') OR (p.prokind = 'w' AND p.oid >= 16384))"
}
