package sqlservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// The object, its logical identity, its native privileges and every returned
// field are evaluated by one SELECT in the same statement snapshot. This also
// works on a Console connection with an existing transaction and SET ROLE.
type postgresObjectLookup struct {
	from, where string
	args        []any
	version     int
}

func postgresObject(ctx context.Context, conn *sql.Conn, ref ObjectRef, oid uint32) (postgresObjectLookup, error) {
	var q postgresObjectLookup
	if !ValidObjectRef(ref) {
		return q, ErrCatalogNotFound
	}
	v, err := PostgreSQLCatalogVersion(ctx, conn)
	if err != nil {
		return q, err
	}
	if v < 100000 || v < 110000 && ref.Kind == ProcedureKind {
		return q, ErrCatalogUnsupported
	}
	q.version = v
	q.args = []any{ref.Database, ref.Schema, ref.Name, ref.Table, oid}
	q.where = `pg_catalog.current_database()::text=$1 AND n.nspname::text=$2 `
	if ref.Kind == TriggerKind {
		q.from = postgresTriggerFrom
		q.where += `AND t.tgname::text=$3 AND c.relname::text=$4 AND ($5::pg_catalog.oid=0 OR t.oid=$5) AND ` + postgresTriggerVisible(v)
		return q, nil
	}
	procedure, filter := postgresRoutineKinds(v)
	if ref.Kind == FunctionKind {
		procedure = "NOT " + procedure
	}
	q.from = ` FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace `
	q.where += `AND p.proname::text=$3 AND ($5::pg_catalog.oid=0 OR p.oid=$5) AND ` + procedure + ` AND ` + filter + ` AND ` + postgresRoutineVisible + `
 AND $4::pg_catalog.jsonb = COALESCE((SELECT pg_catalog.jsonb_agg(
    pg_catalog.jsonb_build_object('schema',ns.nspname,'name',ty.typname) ORDER BY a.pos)
   FROM pg_catalog.unnest(p.proargtypes) WITH ORDINALITY a(id,pos)
   JOIN pg_catalog.pg_type ty ON ty.oid=a.id JOIN pg_catalog.pg_namespace ns ON ns.oid=ty.typnamespace),'[]'::pg_catalog.jsonb)`
	types := ref.InputTypes
	if types == nil {
		types = []ObjectTypeRef{}
	}
	encoded, err := json.Marshal(types)
	q.args[3] = string(encoded)
	return q, err
}

func postgresObjectReadError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCatalogNotFound
	}
	return err
}

// PG defaults refer to the trailing input arguments. OUT/TABLE parameters
// need not be trailing, so count only input-capable modes for has_default.
const postgresRoutineParameters = `(SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
 'position',a.pos,'name',COALESCE(p.proargnames[a.pos],''),'mode',
 CASE a.mode WHEN 'i' THEN 'IN' WHEN 'o' THEN 'OUT' WHEN 'b' THEN 'INOUT' WHEN 'v' THEN 'VARIADIC' WHEN 't' THEN 'TABLE' END,
 'type',pg_catalog.jsonb_build_object('name',ty.typname,'schema',ns.nspname),'data_type',pg_catalog.format_type(ty.oid,NULL),
 'type_kind',ty.typtype,'has_default',a.mode IN ('i','b','v') AND a.input_pos>p.pronargs-p.pronargdefaults,
 'default',CASE WHEN pg_catalog.pg_has_role(p.proowner,'USAGE') THEN pg_catalog.pg_get_function_arg_default(p.oid,a.pos::int) END
 ) ORDER BY a.pos),'[]'::pg_catalog.jsonb)
 FROM (SELECT arg.*,pg_catalog.count(*) FILTER (WHERE mode IN ('i','b','v')) OVER (ORDER BY pos) AS input_pos
       FROM (SELECT x.id,x.pos,COALESCE(p.proargmodes[x.pos],'i') AS mode
             FROM pg_catalog.unnest(COALESCE(p.proallargtypes,p.proargtypes::pg_catalog.oid[])) WITH ORDINALITY x(id,pos)) arg) a
 JOIN pg_catalog.pg_type ty ON ty.oid=a.id JOIN pg_catalog.pg_namespace ns ON ns.oid=ty.typnamespace)`

func PostgreSQLObjectDetail(ctx context.Context, conn *sql.Conn, ref ObjectRef, oid uint32) (ObjectDetail, error) {
	var result ObjectDetail
	lookup, err := postgresObject(ctx, conn, ref, oid)
	if err != nil {
		return result, err
	}
	result.Ref, result.Label = ref, ref.Name
	if ref.Kind == TriggerKind {
		err = readPostgresTriggerDetail(ctx, conn, lookup, &result)
	} else {
		err = readPostgresRoutineDetail(ctx, conn, lookup, &result)
	}
	return result, postgresObjectReadError(err)
}

func readPostgresRoutineDetail(ctx context.Context, conn *sql.Conn, q postgresObjectLookup, result *ObjectDetail) error {
	window := "p.prokind='w'"
	if q.version < 110000 {
		window = "p.proiswindow"
	}
	query := `SELECT p.oid,pg_catalog.format('%I(%s)',p.proname,pg_catalog.oidvectortypes(p.proargtypes)),
 pg_catalog.pg_has_role(p.proowner,'USAGE'),pg_catalog.has_schema_privilege(n.oid,'USAGE') AND pg_catalog.has_function_privilege(p.oid,'EXECUTE') AND NOT (` + window + `)
 AND NOT (rn.nspname='pg_catalog' AND rt.typname IN ('trigger','event_trigger','internal')),
 pg_catalog.jsonb_build_object('owner',pg_catalog.pg_get_userbyid(p.proowner),'language',l.lanname,
 'return_type',pg_catalog.pg_get_function_result(p.oid),'returns_set',p.proretset,'return_type_kind',rt.typtype,
 'return_type_ref',pg_catalog.jsonb_build_object('schema',rn.nspname,'name',rt.typname),
 'security',CASE WHEN p.prosecdef THEN 'DEFINER' ELSE 'INVOKER' END,
 'volatility',CASE p.provolatile WHEN 'i' THEN 'IMMUTABLE' WHEN 's' THEN 'STABLE' ELSE 'VOLATILE' END,
 'strict',p.proisstrict,'window',` + window + `,'comment',pg_catalog.obj_description(p.oid,'pg_proc')),
 ` + postgresRoutineParameters + q.from + `
 JOIN pg_catalog.pg_language l ON l.oid=p.prolang JOIN pg_catalog.pg_type rt ON rt.oid=p.prorettype
 JOIN pg_catalog.pg_namespace rn ON rn.oid=rt.typnamespace WHERE ` + q.where
	var owner, invocable bool
	var props, params []byte
	if err := conn.QueryRowContext(ctx, query, q.args...).Scan(&result.OID, &result.Signature, &owner, &invocable, &props, &params); err != nil {
		return err
	}
	if err := json.Unmarshal(props, &result.Properties); err != nil {
		return err
	}
	if err := json.Unmarshal(params, &result.Parameters); err != nil {
		return err
	}
	result.Actions = []string{"inspect"}
	if owner {
		result.Actions = append(result.Actions, "definition", "edit", "rename", "drop")
	}
	if invocable {
		result.Actions = append(result.Actions, "invoke")
	}
	result.Properties["invocable"] = invocable
	return nil
}

func readPostgresTriggerDetail(ctx context.Context, conn *sql.Conn, q postgresObjectLookup, result *ObjectDetail) error {
	clone := postgresTriggerClone(q.version)
	// tgargs contains NUL-terminated strings in the database encoding. Split the
	// hex representation at whole 00 bytes before converting each argument.
	query := `SELECT t.oid,c.oid,pg_catalog.pg_has_role(c.relowner,'USAGE'),` + clone + `,
 pg_catalog.jsonb_build_object('state',t.tgenabled,'is_constraint',t.tgconstraint<>0,'is_clone',` + clone + `,
 'deferrable',t.tgdeferrable,'initially_deferred',t.tginitdeferred,'has_condition',t.tgqual IS NOT NULL,
 'timing',CASE WHEN (t.tgtype&2)<>0 THEN 'BEFORE' ELSE 'AFTER' END,
 'orientation',CASE WHEN (t.tgtype&1)<>0 THEN 'ROW' ELSE 'STATEMENT' END,
 'events',pg_catalog.array_remove(ARRAY[CASE WHEN (t.tgtype&4)<>0 THEN 'INSERT' END,CASE WHEN (t.tgtype&16)<>0 THEN 'UPDATE' END,
 CASE WHEN (t.tgtype&8)<>0 THEN 'DELETE' END,CASE WHEN (t.tgtype&32)<>0 THEN 'TRUNCATE' END],NULL),
 'condition',CASE WHEN t.tgqual IS NOT NULL AND pg_catalog.pg_has_role(c.relowner,'USAGE') THEN
   (SELECT i.action_condition FROM information_schema.triggers i WHERE i.event_object_schema=n.nspname
    AND i.event_object_table=c.relname AND i.trigger_name=t.tgname LIMIT 1) END,
 'argument_count',t.tgnargs,'arguments', ARRAY(
    SELECT pg_catalog.convert_from(pg_catalog.decode(m[1],'hex'),pg_catalog.current_setting('server_encoding'))
    FROM pg_catalog.regexp_matches(pg_catalog.encode(t.tgargs,'hex'),'((?:[1-9a-f][0-9a-f]|0[1-9a-f])*)00','g') m),
 'table_kind',c.relkind,'old_table',t.tgoldtable,'new_table',t.tgnewtable,'comment',pg_catalog.obj_description(t.oid,'pg_trigger')),
 f.nspname,f.proname,f.oid ` + q.from + `
 LEFT JOIN LATERAL (SELECT n.nspname,p.proname,p.oid FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid=p.pronamespace
 WHERE p.oid=t.tgfoid AND ` + postgresRoutineVisible + `) f ON true WHERE ` + q.where
	var owner, isClone bool
	var tableOID uint32
	var schema, name sql.NullString
	var funcOID sql.NullInt64
	var props []byte
	if err := conn.QueryRowContext(ctx, query, q.args...).Scan(&result.OID, &tableOID, &owner, &isClone, &props, &schema, &name, &funcOID); err != nil {
		return err
	}
	if err := json.Unmarshal(props, &result.Properties); err != nil {
		return err
	}
	ref := result.Ref
	result.Related = []ObjectSummary{{Ref: ObjectRef{Database: ref.Database, Schema: ref.Schema, Kind: "table", Name: ref.Table}, Label: ref.Table, OID: tableOID}}
	if funcOID.Valid {
		result.Related = append(result.Related, ObjectSummary{Ref: ObjectRef{Database: ref.Database, Schema: schema.String, Kind: FunctionKind, Name: name.String, InputTypes: []ObjectTypeRef{}}, Label: name.String, OID: uint32(funcOID.Int64), Signature: name.String + "()"})
	}
	result.Actions = []string{"inspect"}
	if owner {
		result.Actions = append(result.Actions, "definition", "setState")
		if !isClone {
			result.Actions = append(result.Actions, "edit", "rename", "drop")
		}
	}
	return nil
}

// The definition is decompiled only inside the owner check. pg_get_*def on
// its own can reveal a body even to roles without EXECUTE or table ownership.
func postgresDefinitionSource(ctx context.Context, conn *sql.Conn, ref ObjectRef, oid uint32) (postgresObjectLookup, string, error) {
	q, err := postgresObject(ctx, conn, ref, oid)
	if err != nil {
		return q, "", err
	}
	object, owner, source, rowVersion := "p.oid", "p.proowner", "pg_get_functiondef", "p.xmin::text,p.ctid::text"
	dependencies := `(SELECT pg_catalog.jsonb_agg(pg_catalog.jsonb_build_array(ns.nspname,ty.typname) ORDER BY a.pos)
 FROM pg_catalog.unnest(pg_catalog.array_append(COALESCE(p.proallargtypes,p.proargtypes::pg_catalog.oid[]),p.prorettype)) WITH ORDINALITY a(id,pos)
 JOIN pg_catalog.pg_type ty ON ty.oid=a.id JOIN pg_catalog.pg_namespace ns ON ns.oid=ty.typnamespace)`
	if ref.Kind == TriggerKind {
		object, owner, source, rowVersion = "t.oid", "c.relowner", "pg_get_triggerdef", "t.xmin::text,t.ctid::text"
		dependencies = `(SELECT pg_catalog.jsonb_build_array(ns.nspname,p.proname,p.oid)
    FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace ns ON ns.oid=p.pronamespace WHERE p.oid=t.tgfoid)`
	}
	// The decompiler qualifies identifiers using this connection's search_path.
	// A tuple updated twice in one transaction keeps its xmin but changes ctid.
	// Include names of referenced types/functions so a rename also invalidates it.
	q.from = ` FROM (SELECT ` + object + ` AS oid,pg_catalog.jsonb_build_array($1::text,$2::text,$3::text,$4::text,` + object + "," + rowVersion + "," + dependencies + `) AS version,
 CASE WHEN pg_catalog.pg_has_role(` + owner + `,'USAGE') THEN pg_catalog.` + source + "(" + object + ") END AS body " + q.from + " WHERE " + q.where + " OFFSET 0) resolved"
	return q, source, nil
}

// Expose bound identity values to the policy matcher; the driver still binds
// the original values on the selected physical connection.
func (q postgresObjectLookup) policySQL(query string) string {
	args := make([]string, 0, 2*len(q.args))
	for i, arg := range q.args {
		var literal string
		switch v := arg.(type) {
		case string:
			literal = "E'" + strings.NewReplacer(`\`, `\\`, "'", "''").Replace(v) + "'"
		case uint32:
			literal = strconv.FormatUint(uint64(v), 10)
		}
		args = append(args, "$"+strconv.Itoa(i+1), literal)
	}
	return strings.NewReplacer(args...).Replace(query)
}

func PostgreSQLObjectDefinition(ctx context.Context, conn *sql.Conn, ref ObjectRef, oid uint32, authorize func(string) error) (ObjectDefinition, error) {
	return PostgreSQLObjectDefinitionPage(ctx, conn, ref, oid, DefinitionPageRequest{}, authorize)
}

func PostgreSQLObjectDefinitionPage(ctx context.Context, conn *sql.Conn, ref ObjectRef, oid uint32, page DefinitionPageRequest, authorize func(string) error) (ObjectDefinition, error) {
	if !page.Valid() || page.Version != "" && oid == 0 {
		return ObjectDefinition{}, ErrObjectChanged
	}
	q, source, err := postgresDefinitionSource(ctx, conn, ref, oid)
	result := ObjectDefinition{Ref: ref, Source: source}
	if err != nil {
		return result, err
	}
	// A character may occupy two UTF-16 units. Initially fetch enough to
	// apply the same execution size limit as the editor. Larger definitions
	// are read in bounded windows tied to the actual decompiler output.
	maxUnits, pageChars := strconv.Itoa(MaxDocumentCodeUnits), strconv.Itoa(definitionPageCharacters)
	window := "CASE WHEN pg_catalog.length(body)>" + maxUnits + " THEN " + pageChars + " ELSE " + maxUnits + " END"
	if page.Offset > 0 {
		window = pageChars
	}
	query := "SELECT resolved.oid,pg_catalog.length(body),pg_catalog.substr(body," + strconv.Itoa(page.Offset+1) + "," + window + "),version,CASE WHEN pg_catalog.octet_length(body)>" + maxUnits + " THEN pg_catalog.md5(body) ELSE '' END" + q.from
	if authorize != nil {
		if err := authorize(q.policySQL(query)); err != nil {
			return result, err
		}
	}
	var length sql.NullInt64
	var body sql.NullString
	var version, renderedHash string
	if err := conn.QueryRowContext(ctx, query, q.args...).Scan(&result.OID, &length, &body, &version, &renderedHash); err != nil {
		return result, postgresObjectReadError(err)
	}
	result.Status = "restricted"
	if body.Valid {
		if page.Offset > 0 || length.Int64 > MaxDocumentCodeUnits || DocumentExceedsLimit(body.String) {
			result.Version = definitionFingerprint(version + "\x00" + renderedHash)
			if err := result.setPage(page, body.String, int(length.Int64)); err != nil {
				return ObjectDefinition{}, err
			}
			return result, nil
		}
		result.Text, result.Status, result.Version = body.String, "complete", definitionFingerprint(version)
		if page.Version != "" && page.Version != result.Version {
			return ObjectDefinition{}, ErrObjectChanged
		}
	}
	return result, nil
}
