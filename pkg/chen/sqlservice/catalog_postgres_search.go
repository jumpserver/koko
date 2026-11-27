package sqlservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

// Rank is part of this collection's cursor order. Keep the comparison on the
// database so the page boundary uses its name comparison for all three sources.
var postgresSearchKinds = []ObjectKind{"", "table", "view", FunctionKind, ProcedureKind, TriggerKind}

type ObjectSearchRequest struct {
	Database, Schema, Query string
	After                   ObjectSummary
	Limit, ServerVersion    int
}

func PostgreSQLSearchObjects(ctx context.Context, conn *sql.Conn, req ObjectSearchRequest) ([]ObjectSummary, error) {
	if err := validateCatalogInput(conn, dialect.PostgreSQL, req.Database, req.Schema, req.Query, req.After.Ref.Schema, req.After.Ref.Name, req.After.Ref.Table); err != nil {
		return nil, err
	}
	if req.ServerVersion < 100000 {
		return nil, ErrCatalogUnsupported
	}
	afterKind := slices.Index(postgresSearchKinds, req.After.Ref.Kind)
	if afterKind < 0 {
		return nil, ErrCatalogNotFound
	}
	ctx, cancel := catalogReadContext(ctx)
	defer cancel()
	if err := catalogScopeAccess(ctx, conn, dialect.PostgreSQL, req.Database, req.Schema); err != nil {
		return nil, err
	}
	procedure, routines := postgresRoutineKinds(req.ServerVersion)
	sources := []string{
		`SELECT n.nspname,c.relname,CASE WHEN c.relkind IN ('v','m') THEN 2 ELSE 1 END,
  ''::pg_catalog.name,c.oid, CASE c.relkind WHEN 'm' THEN 'materialized_view' WHEN 'p' THEN 'partitioned_table'
    WHEN 'f' THEN 'foreign_table' WHEN 'v' THEN 'view' ELSE 'table' END
  FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
  WHERE c.relkind IN ('r','p','v','m','f') AND n.nspname IN (SELECT schema_name FROM information_schema.schemata)`,
		`SELECT n.nspname,p.proname,CASE WHEN ` + procedure + ` THEN 4 ELSE 3 END,''::pg_catalog.name,p.oid,''
  FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace WHERE ` + routines + ` AND ` + postgresRoutineVisible,
		`SELECT n.nspname,t.tgname,5,c.relname,t.oid,''` + postgresTriggerFrom + ` WHERE ` + postgresTriggerVisible(req.ServerVersion),
	}
	// Apply the same keyset predicate and LIMIT to every source, then resolve
	// signatures only for the selected combined page. No definitions are fetched.
	const keyOrder = "schema_name,object_name,kind,parent,oid"
	for i, src := range sources {
		sources[i] = `(SELECT * FROM (` + src + `) AS s(schema_name,object_name,kind,parent,oid,subtype)
  WHERE ($1::text='' OR schema_name::text=$1) AND object_name ILIKE $2 ESCAPE '!'
   AND (` + keyOrder + `)>($3::pg_catalog.name,$4::pg_catalog.name,$5::integer,$6::pg_catalog.name,$7::pg_catalog.oid)
  ORDER BY ` + keyOrder + ` LIMIT $8)`
	}
	query := `WITH page AS (SELECT * FROM (` + strings.Join(sources, " UNION ALL ") + `) objects ORDER BY ` + keyOrder + ` LIMIT $8)
 SELECT page.schema_name,page.object_name,page.kind,page.parent,page.oid,page.subtype,
 CASE WHEN p.oid IS NOT NULL THEN pg_catalog.format('%I(%s)',p.proname,pg_catalog.oidvectortypes(p.proargtypes)) ELSE '' END,
 CASE WHEN p.oid IS NOT NULL THEN COALESCE((SELECT pg_catalog.jsonb_agg(
  pg_catalog.jsonb_build_object('schema',ns.nspname,'name',t.typname) ORDER BY a.pos)
  FROM pg_catalog.unnest(p.proargtypes) WITH ORDINALITY a(id,pos)
  JOIN pg_catalog.pg_type t ON t.oid=a.id JOIN pg_catalog.pg_namespace ns ON ns.oid=t.typnamespace),'[]'::pg_catalog.jsonb) END
 FROM page LEFT JOIN pg_catalog.pg_proc p ON page.kind IN (3,4) AND p.oid=page.oid
 ORDER BY page.schema_name,page.object_name,page.kind,page.parent,page.oid`
	rows, err := conn.QueryContext(ctx, query, req.Schema, catalogSearch(req.Query), req.After.Ref.Schema, req.After.Ref.Name, afterKind, req.After.Ref.Table, req.After.OID, catalogLimit(req.Limit)+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ObjectSummary{}
	for rows.Next() {
		var item ObjectSummary
		var kind int
		var types []byte
		if err := rows.Scan(&item.Ref.Schema, &item.Ref.Name, &kind, &item.Ref.Table, &item.OID, &item.ObjectType, &item.Signature, &types); err != nil {
			return nil, err
		}
		item.Ref.Database, item.Ref.Kind, item.Label = req.Database, postgresSearchKinds[kind], item.Ref.Name
		if len(types) > 0 {
			if err := json.Unmarshal(types, &item.Ref.InputTypes); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
