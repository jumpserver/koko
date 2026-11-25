package sqlservice

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

// LoadBrowseTable needs authorized read metadata, not identity/default flags
// required by editing. A successful read never grants grid write permission.
func LoadBrowseTable(ctx context.Context, conn *sql.Conn, d dialect.ID, scope Context, schema, table string) (EditTable, error) {
	out := EditTable{Database: scope.Database, Schema: schema, Table: table}
	if !d.Supported() {
		return out, ErrUnsupportedDialect
	}
	if d.UsesMySQLSyntax() && schema != "" {
		out.Database = schema
	}
	if err := validateCatalogInput(conn, d, out.Database, schema, table); err != nil {
		return out, err
	}
	if table == "" {
		return out, ErrCatalogNotFound
	}
	ctx, cancel := MetadataContext(ctx, d)
	defer cancel()
	if d == dialect.MySQL {
		out.Schema = out.Database
		indexed, err := mysqlBrowseColumns(ctx, conn, &out)
		if err != nil {
			return out, err
		}
		if len(out.Columns) > 0 {
			return withBrowseKey(ctx, conn, d, out, indexed)
		}
		// The exact name was not visible. Use the regular access checks
		// and lower_case_table_names resolution for this less common case.
	}
	if err := catalogDatabaseAccess(ctx, conn, d, out.Database); err != nil {
		return out, err
	}
	if d.UsesMySQLSyntax() || d == dialect.ClickHouse {
		if schema == "" {
			schema = scope.Database
		}
		if d == dialect.ClickHouse && schema != scope.Database {
			return out, ErrCatalogForbidden
		}
	} else if schema == "" {
		var err error
		switch d {
		case dialect.PostgreSQL:
			schema, err = postgresRelationSchema(ctx, conn, table)
		case dialect.SQLServer:
			err = conn.QueryRowContext(ctx, `SELECT OBJECT_SCHEMA_NAME(OBJECT_ID(@p1))`, quoteIdentifier(table, d, '[')).Scan(&schema)
		default:
			schema, err = extraDefaultSchema(ctx, conn, d, scope.Database)
		}
		if err != nil {
			return out, err
		}
	}
	out.Schema = schema
	r := CatalogColumnRequest{Database: out.Database, Schema: schema, Table: table}
	// The same visible catalog row proves both object access and the kind
	// used to decide if the user must confirm reading a view.
	isView, err := BrowseRelationIsView(ctx, conn, d, r)
	if errors.Is(err, sql.ErrNoRows) {
		switch {
		case d.UsesMySQLSyntax():
			out.Table, err = editMySQLCanonicalTable(ctx, conn, schema, table)
			if err == nil {
				r.Table = out.Table
				isView, err = BrowseRelationIsView(ctx, conn, d, r)
			}
		case d == dialect.PostgreSQL:
			// Distinguish an inaccessible schema from a missing or unbrowsable
			// relation only on a miss.
			if accessErr := CatalogObjectAccess(ctx, conn, d, schema, table); accessErr != nil {
				err = accessErr
			}
		}
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrCatalogNotFound
		}
	}
	if err != nil {
		return out, err
	}
	out.View = isView
	cols, err := tableColumnRows(ctx, conn, d, r, false)
	if err != nil {
		return out, err
	}
	if len(cols) == 0 {
		return out, fmt.Errorf("browse column metadata is unavailable")
	}
	for _, c := range cols {
		out.Columns = append(out.Columns, EditColumn{Name: c.Name, DataType: c.DataType, Nullable: c.Nullable, Supported: EditTypeSupported(d, c.DataType)})
	}
	return withBrowseKey(ctx, conn, d, out, true)
}

// Read the authenticated account's visible table kind and columns together.
// Every usable unique key has an indexed leading column. COLUMN_KEY is only
// a hint to inspect STATISTICS; it never establishes uniqueness on its own.
func mysqlBrowseColumns(ctx context.Context, conn *sql.Conn, table *EditTable) (bool, error) {
	from, args := mysqlVisibleColumnsSQL(table.Schema, table.Table, true)
	rows, err := conn.QueryContext(ctx, `SELECT t.TABLE_TYPE, c.COLUMN_NAME, c.COLUMN_TYPE, c.IS_NULLABLE='YES', c.COLUMN_KEY`+from, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var indexed bool
	for rows.Next() {
		var column EditColumn
		var kind, key string
		if err := rows.Scan(&kind, &column.Name, &column.DataType, &column.Nullable, &key); err != nil {
			return false, err
		}
		table.View = kind != "BASE TABLE"
		column.Supported = EditTypeSupported(dialect.MySQL, column.DataType)
		table.Columns = append(table.Columns, column)
		indexed = indexed || key != ""
	}
	return indexed, rows.Err()
}

func mysqlVisibleColumnsSQL(schema, table string, includeViews bool) (string, []any) {
	target, args := mysqlCatalogTarget(schema, table)
	return mysqlVisibleColumnsFrom(target, includeViews) + " ORDER BY c.ORDINAL_POSITION", args
}

func mysqlVisibleColumnsFrom(target string, includeViews bool) string {
	kind := "t.TABLE_TYPE='BASE TABLE'"
	if includeViews {
		kind = "t.TABLE_TYPE IN ('BASE TABLE','VIEW','SYSTEM VIEW')"
	}
	// Ordinary equality permits dictionary index lookups. The extra binary
	// comparisons keep names exact even on case-insensitive catalog collations.
	return ` FROM information_schema.TABLES t
JOIN information_schema.COLUMNS c USING (TABLE_SCHEMA, TABLE_NAME)
JOIN information_schema.SCHEMATA s ON s.SCHEMA_NAME=t.TABLE_SCHEMA
CROSS JOIN ` + target + `
WHERE t.TABLE_SCHEMA=target.db AND t.TABLE_NAME=target.tbl AND ` + kind + `
AND BINARY t.TABLE_SCHEMA=BINARY target.db AND BINARY t.TABLE_NAME=BINARY target.tbl
AND BINARY c.TABLE_SCHEMA=BINARY t.TABLE_SCHEMA AND BINARY c.TABLE_NAME=BINARY t.TABLE_NAME
AND BINARY s.SCHEMA_NAME=BINARY t.TABLE_SCHEMA`
}

// The only data embedded into these fixed catalog reads is the hexadecimal
// encoding of ASCII names. Exact catalog lookups compare other names as
// bound bytes: an implicit string conversion in the derived table could
// truncate UTF-8 bytes when the session uses a different character set.
func mysqlCatalogTarget(schema, table string) (string, []any) {
	for _, name := range []string{schema, table} {
		for i := 0; i < len(name); i++ {
			if name[i] >= utf8.RuneSelf {
				return "(SELECT BINARY ? AS db, BINARY ? AS tbl) AS target", []any{schema, table}
			}
		}
	}
	return "(SELECT _ascii X'" + hex.EncodeToString([]byte(schema)) + "' AS db, _ascii X'" + hex.EncodeToString([]byte(table)) + "' AS tbl) AS target", nil
}

func withBrowseKey(ctx context.Context, conn *sql.Conn, d dialect.ID, out EditTable, indexed bool) (EditTable, error) {
	if out.View || !indexed {
		return out, ctx.Err()
	}
	// Only complete non-null, unconditional unique keys promise a tie breaker.
	keys, err := browseUniqueKey(ctx, conn, d, out)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		// Key discovery is optional read metadata. Missing index visibility
		// never grants stability and never turns a readable table into an
		// editable one; the page still executes with the account's rights.
		return out, nil
	}
	out.PrimaryKey = keys
	return out, nil
}

// BrowseRelationIsView also detects a table replaced by a view between pages.
// Reads never infer that a sorting key or view projection is a writable identity.
func BrowseRelationIsView(ctx context.Context, conn *sql.Conn, d dialect.ID, r CatalogColumnRequest) (bool, error) {
	if d == dialect.MySQL {
		var view bool
		target, args := mysqlCatalogTarget(r.Schema, r.Table)
		err := conn.QueryRowContext(ctx, `SELECT TABLE_TYPE <> 'BASE TABLE' FROM information_schema.TABLES CROSS JOIN `+target+`
WHERE TABLE_SCHEMA=target.db AND TABLE_NAME=target.tbl AND BINARY TABLE_SCHEMA=BINARY target.db AND BINARY TABLE_NAME=BINARY target.tbl
AND TABLE_TYPE IN ('BASE TABLE','VIEW','SYSTEM VIEW')`, args...).Scan(&view)
		return view, err
	}
	if d == dialect.ClickHouse {
		var engine string
		err := conn.QueryRowContext(ctx, `SELECT engine FROM system.tables WHERE database=? AND name=? AND is_temporary=0`, r.Schema, r.Table).Scan(&engine)
		return strings.Contains(strings.ToLower(engine), "view"), err
	}
	if d.UsesMySQLSyntax() {
		kinds := "'BASE TABLE','VIEW','SYSTEM VIEW'"
		if d == dialect.MariaDB {
			kinds += ",'SYSTEM VERSIONED'"
		}
		var view bool
		err := conn.QueryRowContext(ctx, `SELECT TABLE_TYPE IN ('VIEW','SYSTEM VIEW') FROM information_schema.TABLES
WHERE BINARY TABLE_SCHEMA=BINARY ? AND BINARY TABLE_NAME=BINARY ? AND TABLE_TYPE IN (`+kinds+")", r.Schema, r.Table).Scan(&view)
		return view, err
	}
	if d == dialect.PostgreSQL {
		var view bool
		err := conn.QueryRowContext(ctx, `SELECT c.relkind IN ('v','m') FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2
AND c.relkind IN ('r','p','v','m') AND n.nspname IN (SELECT schema_name FROM information_schema.schemata)`, r.Schema, r.Table).Scan(&view)
		return view, err
	}
	b := &catalogBinder{dialect: d}
	source, err := extraObjectSource(d, b, r.Schema)
	if err != nil {
		return false, err
	}
	// Read the directory's object type, not merely its table/view grouping:
	// Db2 nicknames and materialized query tables are not base tables.
	query := "SELECT objecttype FROM (" + source + ") objects WHERE schemaname=" + b.bind(r.Schema) +
		" AND objectname=" + b.bind(r.Table) + " AND objecttype IN ('table','view') ORDER BY objecttype"
	var kind string
	if err := conn.QueryRowContext(ctx, query, b.args...).Scan(&kind); err != nil {
		return false, err
	}
	switch strings.TrimRight(kind, " ") {
	case "view":
		return true, nil
	case "table":
		return false, nil
	default:
		return false, ErrCatalogNotFound
	}
}

const mysqlPageEngines = " IN ('InnoDB','MyISAM','MEMORY','Aria')"

const MySQLPageSettings = "COALESCE(DATABASE(), ''), CURRENT_USER(), @@session.sql_mode, @@session.time_zone, @@session.character_set_connection, @@session.collation_connection"
const MySQLSessionFields = MySQLPageSettings + ", VERSION(), CONNECTION_ID()"

// The first row always contains the live session, even for a missing table
// or a view. Following rows stream the base table's visible column names.
func MySQLStarSessionRows(ctx context.Context, conn *sql.Conn, hint QueryPageRelation) (*sql.Rows, error) {
	target, args := mysqlCatalogTarget(hint.Schema, hint.Name)
	if hint.Schema == "" {
		target = "(SELECT DATABASE() AS db, tbl FROM " + target + ") AS target"
	}
	query := "SELECT " + MySQLSessionFields + ", NULL AS column_name, NULL AS safe, 0 AS ordinal " +
		"UNION ALL SELECT NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,c.COLUMN_NAME,t.ENGINE" + mysqlPageEngines +
		",c.ORDINAL_POSITION" + mysqlVisibleColumnsFrom(target, false) + " ORDER BY ordinal"
	return conn.QueryContext(ctx, query, args...)
}

// For a direct SELECT *, validate the same base table and engine as query
// paging and retain just its visible column names, in physical order.
func MySQLQueryStarColumns(ctx context.Context, conn *sql.Conn, schema, table string) ([]string, error) {
	from, args := mysqlVisibleColumnsSQL(schema, table, false)
	rows, err := conn.QueryContext(ctx, "SELECT c.COLUMN_NAME, t.ENGINE"+mysqlPageEngines+from, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var name string
		var safe bool
		if err := rows.Scan(&name, &safe); err != nil {
			return nil, err
		}
		if !safe {
			return nil, nil
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

// Query paging can automatically rerun a read only when its actual source is
// an ordinary supported table. MySQL's filesystem case mode also applies to
// qualified identifiers in user SQL.
func MySQLQueryPageSource(ctx context.Context, conn *sql.Conn, schema, table string) (bool, error) {
	target, args := mysqlCatalogTarget(schema, table)
	var safe bool
	const selectSource = "SELECT TABLE_TYPE='BASE TABLE' AND ENGINE" + mysqlPageEngines + " FROM information_schema.TABLES"
	if len(args) != 0 {
		// User SQL resolves identifiers in its session charset and case mode.
		// Preserve direct parameter coercion for non-ASCII identifiers.
		err := conn.QueryRowContext(ctx, selectSource+`
WHERE TABLE_SCHEMA=COALESCE(NULLIF(?,''),DATABASE()) AND TABLE_NAME=?
AND (@@lower_case_table_names<>0 OR (BINARY TABLE_SCHEMA=BINARY COALESCE(NULLIF(?,''),DATABASE()) AND BINARY TABLE_NAME=BINARY ?))`, schema, table, schema, table).Scan(&safe)
		return safe, err
	}
	resolvedSchema := "target.db"
	if schema == "" {
		// DATABASE() can contain non-ASCII characters even when the table
		// name is ASCII. Keep the server's own result charset.
		resolvedSchema = "DATABASE()"
	}
	err := conn.QueryRowContext(ctx, selectSource+` CROSS JOIN `+target+`
WHERE TABLE_SCHEMA=`+resolvedSchema+` AND TABLE_NAME=target.tbl
AND (@@lower_case_table_names<>0 OR (BINARY TABLE_SCHEMA=BINARY `+resolvedSchema+` AND BINARY TABLE_NAME=BINARY target.tbl))`).Scan(&safe)
	return safe, err
}

func browseUniqueKey(ctx context.Context, conn *sql.Conn, d dialect.ID, table EditTable) ([]string, error) {
	q, args := browseUniqueKeySQL(d, table.Schema, table.Table)
	if q == "" {
		return nil, nil
	}
	rows, err := conn.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("read browse ordering keys: %w", err)
	}
	defer rows.Close()
	type key struct {
		columns []string
		count   int
		invalid bool
	}
	keys := map[string]*key{}
	count := 0
	for rows.Next() {
		count++
		if count > 4096 {
			return nil, fmt.Errorf("browse key metadata exceeds its limit")
		}
		var name string
		var column sql.NullString
		var ordinal, total int
		if err := rows.Scan(&name, &column, &ordinal, &total); err != nil {
			return nil, err
		}
		k := keys[name]
		if k == nil {
			k = &key{count: total}
			keys[name] = k
		}
		if name == "" || total < 1 || total > len(table.Columns) || k.count != total || ordinal != len(k.columns)+1 || !column.Valid {
			k.invalid = true
		}
		col := column.String
		if d == dialect.DB2 {
			col = strings.TrimRight(col, " ")
		}
		found := false
		for _, c := range table.Columns {
			if c.Name == col {
				found = true
				if c.Nullable || !c.Supported {
					k.invalid = true
				}
				break
			}
		}
		if !found {
			k.invalid = true
		}
		for _, old := range k.columns {
			if old == col {
				k.invalid = true
			}
		}
		k.columns = append(k.columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	var best []string
	for _, name := range names {
		k := keys[name]
		if !k.invalid && len(k.columns) == k.count && (best == nil || len(k.columns) < len(best)) {
			best = k.columns
		}
	}
	return best, nil
}
func browseUniqueKeySQL(d dialect.ID, schema, table string) (string, []any) {
	b := catalogBinder{dialect: d}
	s, t := b.bind(schema), b.bind(table)
	if d == dialect.PostgreSQL {
		s, t = "$1", "$2"
	}
	var q string
	switch {
	case d.UsesMySQLSyntax():
		// MySQL 5.7 has no window functions. A correlated count preserves complete
		// key size without relying on the visibility of any individual column.
		q = `SELECT x.INDEX_NAME,x.COLUMN_NAME,x.SEQ_IN_INDEX,(SELECT COUNT(*) FROM information_schema.STATISTICS y WHERE BINARY y.TABLE_SCHEMA=BINARY x.TABLE_SCHEMA AND BINARY y.TABLE_NAME=BINARY x.TABLE_NAME AND BINARY y.INDEX_NAME=BINARY x.INDEX_NAME) FROM information_schema.STATISTICS x WHERE BINARY x.TABLE_SCHEMA=BINARY ` + s + ` AND BINARY x.TABLE_NAME=BINARY ` + t + ` AND x.NON_UNIQUE=0 AND NOT EXISTS (SELECT 1 FROM information_schema.STATISTICS z WHERE BINARY z.TABLE_SCHEMA=BINARY x.TABLE_SCHEMA AND BINARY z.TABLE_NAME=BINARY x.TABLE_NAME AND BINARY z.INDEX_NAME=BINARY x.INDEX_NAME AND (z.SUB_PART IS NOT NULL OR z.COLUMN_NAME IS NULL)) ORDER BY BINARY x.INDEX_NAME,x.SEQ_IN_INDEX`
	case d == dialect.PostgreSQL:
		q = `SELECT ix.relname,a.attname,u.ord,COALESCE((to_jsonb(i)->>'indnkeyatts')::int,i.indnatts) FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class t ON t.oid=i.indrelid JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace JOIN pg_catalog.pg_class ix ON ix.oid=i.indexrelid CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY u(attnum,ord) LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid=t.oid AND a.attnum=u.attnum JOIN pg_catalog.pg_opclass op ON op.oid=i.indclass[(u.ord-1)::int] WHERE n.nspname=` + s + ` AND t.relname=` + t + ` AND i.indisunique AND i.indisvalid AND i.indisready AND i.indimmediate AND i.indpred IS NULL AND i.indexprs IS NULL AND op.opcdefault AND i.indcollation[(u.ord-1)::int]=a.attcollation AND u.ord<=COALESCE((to_jsonb(i)->>'indnkeyatts')::int,i.indnatts) ORDER BY ix.relname,u.ord`
	case d == dialect.SQLServer:
		q = `SELECT i.name,c.name,k.key_ordinal,(SELECT COUNT(*) FROM sys.index_columns z WHERE z.object_id=i.object_id AND z.index_id=i.index_id AND z.key_ordinal>0) FROM sys.indexes i JOIN sys.tables t ON t.object_id=i.object_id JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.index_columns k ON k.object_id=i.object_id AND k.index_id=i.index_id JOIN sys.columns c ON c.object_id=k.object_id AND c.column_id=k.column_id WHERE s.name=` + s + ` AND t.name=` + t + ` AND i.is_unique=1 AND i.is_disabled=0 AND i.is_hypothetical=0 AND i.has_filter=0 AND k.key_ordinal>0 ORDER BY i.name,k.key_ordinal`
	case d.UsesOracleSyntax():
		// Valid, non-partitioned physical unique indexes cover standalone
		// indexes as well as enforced constraints. Hidden/function key columns
		// and nullable columns are rejected against the visible column set.
		s2, t2 := b.bind(schema), b.bind(table)
		q = `SELECT 'I:'||i.OWNER||'.'||i.INDEX_NAME,k.COLUMN_NAME,k.COLUMN_POSITION,COUNT(*) OVER(PARTITION BY i.OWNER,i.INDEX_NAME) FROM ALL_INDEXES i JOIN ALL_IND_COLUMNS k ON k.INDEX_OWNER=i.OWNER AND k.INDEX_NAME=i.INDEX_NAME AND k.TABLE_OWNER=i.TABLE_OWNER AND k.TABLE_NAME=i.TABLE_NAME WHERE i.TABLE_OWNER=` + s + ` AND i.TABLE_NAME=` + t + ` AND i.UNIQUENESS='UNIQUE' AND i.STATUS='VALID' AND i.PARTITIONED='NO' UNION ALL SELECT 'C:'||c.OWNER||'.'||c.CONSTRAINT_NAME,k.COLUMN_NAME,k.POSITION,COUNT(*) OVER(PARTITION BY c.OWNER,c.CONSTRAINT_NAME) FROM ALL_CONSTRAINTS c JOIN ALL_CONS_COLUMNS k ON k.OWNER=c.OWNER AND k.CONSTRAINT_NAME=c.CONSTRAINT_NAME AND k.TABLE_NAME=c.TABLE_NAME WHERE c.OWNER=` + s2 + ` AND c.TABLE_NAME=` + t2 + ` AND c.CONSTRAINT_TYPE IN ('P','U') AND c.STATUS='ENABLED' AND c.VALIDATED='VALIDATED' AND c.DEFERRABLE='NOT DEFERRABLE' ORDER BY 1,3`

	case d == dialect.DB2:
		q = `SELECT i.INDSCHEMA||'.'||i.INDNAME,RTRIM(c.COLNAME),c.COLSEQ,i.COLCOUNT FROM SYSCAT.INDEXES i JOIN SYSCAT.INDEXCOLUSE c ON c.INDSCHEMA=i.INDSCHEMA AND c.INDNAME=i.INDNAME WHERE i.TABSCHEMA=` + s + ` AND i.TABNAME=` + t + ` AND i.UNIQUERULE IN ('P','U') AND c.COLORDER IN ('A','D') ORDER BY i.INDNAME,c.COLSEQ`
	default:
		return "", nil
	}
	return q, b.args
}
