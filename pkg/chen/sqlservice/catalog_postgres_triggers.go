package sqlservice

import (
	"context"
	"database/sql"
	"github.com/jumpserver/koko/pkg/chen/dialect"
)

const postgresTriggerFrom = ` FROM pg_catalog.pg_trigger t
 JOIN pg_catalog.pg_class c ON c.oid=t.tgrelid
 JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace `

// Match information_schema.triggers' privilege predicate. Its event projection
// omits TRUNCATE, so read the native row once instead of joining per-event rows.
func postgresTriggerClone(version int) string {
	if version >= 130000 {
		return "t.tgparentid<>0"
	}
	if version >= 110000 {
		return `EXISTS (SELECT 1 FROM pg_catalog.pg_depend d WHERE d.classid='pg_catalog.pg_trigger'::pg_catalog.regclass AND d.objid=t.oid AND d.refclassid=d.classid AND d.deptype IN ('I','P'))`
	}
	return "false"
}

func postgresTriggerVisible(version int) string {
	// PostgreSQL marks partition clones of a user trigger internal as well.
	// Include those clones, while keeping FK and other generated triggers hidden.
	return `(NOT t.tgisinternal OR (t.tgconstraint=0 AND ` + postgresTriggerClone(version) + `)) AND c.relkind IN ('r','p','f')
 AND NOT pg_catalog.pg_is_other_temp_schema(n.oid)
 AND n.nspname IN (SELECT schema_name FROM information_schema.schemata)
 AND (pg_catalog.pg_has_role(c.relowner,'USAGE')
   OR pg_catalog.has_table_privilege(c.oid,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')
   OR pg_catalog.has_any_column_privilege(c.oid,'INSERT,UPDATE,REFERENCES'))`
}

type TriggerListRequest struct {
	ServerVersion                      int
	Database, Schema, Table, Query     string
	FunctionOID                        uint32
	AfterSchema, AfterName, AfterTable string
	Limit                              int
}

func PostgreSQLTriggerRows(ctx context.Context, conn *sql.Conn, req TriggerListRequest) ([]ObjectSummary, error) {
	if err := validateCatalogInput(conn, dialect.PostgreSQL, req.Database, req.Schema, req.Table, req.Query, req.AfterSchema, req.AfterName, req.AfterTable); err != nil {
		return nil, err
	}
	ctx, cancel := catalogReadContext(ctx)
	defer cancel()
	if err := catalogScopeAccess(ctx, conn, dialect.PostgreSQL, req.Database, req.Schema); err != nil {
		return nil, err
	}
	if req.ServerVersion == 0 {
		v, err := PostgreSQLCatalogVersion(ctx, conn)
		if err != nil {
			return nil, err
		}
		req.ServerVersion = v
	}
	if req.ServerVersion < 100000 {
		return nil, ErrCatalogUnsupported
	}
	query := `SELECT n.nspname, t.tgname, c.relname, t.oid ` + postgresTriggerFrom + ` WHERE ` + postgresTriggerVisible(req.ServerVersion) + `
 AND ($1::text='' OR n.nspname::text=$1) AND ($2::text='' OR c.relname::text=$2)
 AND t.tgname ILIKE $3 ESCAPE '!'
 AND (n.nspname,t.tgname,c.relname)>($4::pg_catalog.name,$5::pg_catalog.name,$6::pg_catalog.name)`
	args := []any{req.Schema, req.Table, catalogSearch(req.Query), req.AfterSchema, req.AfterName, req.AfterTable, catalogLimit(req.Limit) + 1}
	if req.FunctionOID != 0 {
		query += ` AND t.tgfoid=$8 AND EXISTS (SELECT 1 FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE p.oid=t.tgfoid AND ` + postgresRoutineVisible + `)`
		args = append(args, req.FunctionOID)
	}
	rows, err := conn.QueryContext(ctx, query+" ORDER BY n.nspname,t.tgname,c.relname LIMIT $7", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ObjectSummary{}
	for rows.Next() {
		item := ObjectSummary{Ref: ObjectRef{Kind: TriggerKind, Database: req.Database}}
		if err := rows.Scan(&item.Ref.Schema, &item.Ref.Name, &item.Ref.Table, &item.OID); err != nil {
			return nil, err
		}
		item.Label = item.Ref.Name
		items = append(items, item)
	}
	return items, rows.Err()
}
