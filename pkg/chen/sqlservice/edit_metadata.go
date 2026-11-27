package sqlservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

// ErrEditColumnFlagsUnknown never includes a driver message or user values.
var ErrEditColumnFlagsUnknown = errors.New("This database version or account does not expose reliable generated/identity column flags")

type EditColumn struct {
	Name       string `json:"name"`
	DataType   string `json:"data_type"`
	Nullable   bool   `json:"nullable"`
	Generated  bool   `json:"generated"`
	Identity   bool   `json:"identity"`
	HasDefault bool   `json:"has_default"`
	Default    string `json:"default,omitempty"`
	Supported  bool   `json:"supported"`
	PrimaryKey bool   `json:"primary_key"`
}

// DB2ColumnID identifies a physical column in Db2's SQL0407N diagnostic.
type DB2ColumnID struct {
	Tablespace, Table, Column int64
}

type EditTable struct {
	View           bool                   `json:"-"`
	DB2ColumnNames map[DB2ColumnID]string `json:"-"`
	// LoadEditTable establishes this capability; read-only browsing does not.
	Transactional bool         `json:"transactional"`
	Database      string       `json:"database"`
	Schema        string       `json:"schema"`
	Table         string       `json:"table"`
	Columns       []EditColumn `json:"columns"`
	PrimaryKey    []string     `json:"primary_key"`
}

// LoadEditTable reads the authenticated connection's visible metadata. Missing
// metadata and views fail closed; no user-table query is used as an alternate
// discovery path. Read the complete definition before deriving write
// permissions so a partial definition cannot omit required insert values.
func LoadEditTable(ctx context.Context, conn *sql.Conn, d dialect.ID, scope Context, schema, table string) (EditTable, error) {
	out := EditTable{Database: scope.Database, Schema: schema, Table: table}
	if d == dialect.ClickHouse || !d.Supported() {
		return out, fmt.Errorf("%w: transactional grid editing is unavailable", ErrNotEditable)
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
		engine, possibleKey, err := mysqlEditColumns(ctx, conn, &out)
		if err != nil {
			return out, err
		}
		if len(out.Columns) > 0 {
			out.Transactional, err = editMySQLTransactional(ctx, conn, engine)
			if err != nil {
				return out, err
			}
			if !possibleKey {
				return out, ctx.Err()
			}
			return loadEditPrimaryKey(ctx, conn, d, out)
		}
		// Preserve case folding and the distinction between an inaccessible
		// schema, a missing table and an unsupported object on exact misses.
	}
	if err := catalogDatabaseAccess(ctx, conn, d, out.Database); err != nil {
		return out, err
	}
	if d.UsesMySQLSyntax() {
		if schema == "" {
			schema = out.Database
		}
	} else if d == dialect.PostgreSQL && schema == "" {
		var err error
		schema, err = postgresRelationSchema(ctx, conn, table)
		if err != nil {
			return out, fmt.Errorf("resolve editable table: %w", err)
		}
	} else if schema == "" && d == dialect.SQLServer {
		// SQL Server first searches the default schema and then dbo.
		if err := conn.QueryRowContext(ctx, `SELECT OBJECT_SCHEMA_NAME(OBJECT_ID(@p1, 'U'))`, quoteIdentifier(table, d, '[')).Scan(&schema); err != nil {
			return out, fmt.Errorf("resolve editable table: %w", err)
		}
	} else if schema == "" {
		var err error
		schema, err = readCatalogDefaultSchema(ctx, conn, d, scope.Database)
		if err != nil {
			return out, err
		}
	}
	out.Schema = schema
	r := CatalogColumnRequest{Database: out.Database, Schema: schema, Table: table}
	kind, err := editableTableKind(ctx, conn, d, &r)
	if err != nil {
		return out, err
	}
	out.Table = r.Table
	if !editBaseTableKind(d, kind) {
		return out, fmt.Errorf("%w: a base table is required", ErrNotEditable)
	}
	out.Transactional = true
	if d.UsesMySQLSyntax() {
		out.Transactional, err = editMySQLTransactional(ctx, conn, kind)
		if err != nil {
			return out, err
		}
	}
	cols, err := tableColumnRows(ctx, conn, d, r, true)
	if err != nil {
		return out, err
	}
	if len(cols) == 0 {
		return out, fmt.Errorf("%w: incomplete column metadata", ErrNotEditable)
	}
	flagsQuery, err := editColumnFlagsQuery(d, r)
	if err != nil {
		return out, err
	}
	rows, err := conn.QueryContext(ctx, flagsQuery.SQL, flagsQuery.Args...)
	if err != nil {
		return out, fmt.Errorf("read generated column metadata: %w", err)
	}
	visibleColumns := make(map[string]bool, len(cols))
	for _, col := range cols {
		visibleColumns[editNameKey(col.Name, d)] = true
	}
	flags := map[string]EditColumn{}
	for rows.Next() {
		var col EditColumn
		var generated, identity sql.NullBool
		var tablespaceID, tableID, columnID sql.NullInt64
		dest := []any{&col.Name, &generated, &identity, &col.HasDefault}
		if d == dialect.DB2 {
			dest = append(dest, &tablespaceID, &tableID, &columnID)
		}
		if err = rows.Scan(dest...); err != nil {
			break
		}
		if d == dialect.DB2 {
			col.Name = strings.TrimRight(col.Name, " ")
		}
		key := editNameKey(col.Name, d)
		// Some flag catalogs also expose hidden/system columns. They do not
		// belong to the visible table definition and cannot grant write access.
		if !visibleColumns[key] {
			continue
		}
		if tablespaceID.Valid && tableID.Valid && columnID.Valid {
			if out.DB2ColumnNames == nil {
				out.DB2ColumnNames = make(map[DB2ColumnID]string)
			}
			out.DB2ColumnNames[DB2ColumnID{tablespaceID.Int64, tableID.Int64, columnID.Int64}] = col.Name
		}
		if !generated.Valid || !identity.Valid {
			err = ErrEditColumnFlagsUnknown
			break
		}
		col.Generated, col.Identity = generated.Bool, identity.Bool
		if _, duplicate := flags[key]; duplicate {
			err = fmt.Errorf("ambiguous column metadata")
			break
		}
		flags[key] = col
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return out, fmt.Errorf("read complete generated column metadata: %w", err)
	}
	for _, col := range cols {
		item, ok := flags[editNameKey(col.Name, d)]
		if !ok {
			return out, fmt.Errorf("%w: generated column metadata is missing", ErrNotEditable)
		}
		item.Name, item.DataType, item.Nullable = col.Name, col.DataType, col.Nullable
		item.Supported = EditTypeSupported(d, col.DataType)
		out.Columns = append(out.Columns, item)
	}
	return loadEditPrimaryKey(ctx, conn, d, out)
}

// The MySQL dictionary exposes all column attributes in one visible row.
// COLUMN_KEY only tells us whether a primary key lookup might be needed.
// Resolve the actual key (including composite key order) from constraints.
func mysqlEditColumns(ctx context.Context, conn *sql.Conn, table *EditTable) (string, bool, error) {
	from, args := mysqlVisibleColumnsSQL(table.Schema, table.Table, false)
	rows, err := conn.QueryContext(ctx, `SELECT COALESCE(t.ENGINE,''), c.COLUMN_NAME, c.COLUMN_TYPE, c.IS_NULLABLE='YES',
COALESCE(c.EXTRA LIKE '%GENERATED%' AND c.EXTRA NOT LIKE '%DEFAULT_GENERATED%',0), COALESCE(c.EXTRA LIKE '%auto_increment%',0),
c.COLUMN_DEFAULT IS NOT NULL, c.COLUMN_KEY`+from, args...)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	var engine string
	var possibleKey bool
	for rows.Next() {
		var column EditColumn
		var key sql.NullString
		if err := rows.Scan(&engine, &column.Name, &column.DataType, &column.Nullable, &column.Generated, &column.Identity, &column.HasDefault, &key); err != nil {
			return "", false, err
		}
		column.Supported = EditTypeSupported(dialect.MySQL, column.DataType)
		table.Columns = append(table.Columns, column)
		possibleKey = possibleKey || !key.Valid || strings.EqualFold(key.String, "PRI")
	}
	return engine, possibleKey, rows.Err()
}

func loadEditPrimaryKey(ctx context.Context, conn *sql.Conn, d dialect.ID, out EditTable) (EditTable, error) {
	r := CatalogColumnRequest{Database: out.Database, Schema: out.Schema, Table: out.Table}
	verifyKeySize := d == dialect.MariaDB || d == dialect.OceanBaseMySQL
	pk, _, ok := inspectionQuery(d, r, "primary_key", verifyKeySize, true)
	if !ok {
		return out, fmt.Errorf("%w: primary key metadata is unavailable", ErrNotEditable)
	}
	rows, err := conn.QueryContext(ctx, pk.SQL, pk.Args...)
	if err != nil {
		return out, fmt.Errorf("read primary key: %w", err)
	}
	expectedKeySize := 0
	keyConstraint := ""
	for rows.Next() {
		var constraint, name string
		var ordinal, keySize int
		dest := []any{&constraint, &name, &ordinal}
		if verifyKeySize {
			dest = append(dest, &keySize)
		}
		if err = rows.Scan(dest...); err != nil {
			break
		}
		if d == dialect.DB2 {
			name = strings.TrimRight(name, " ")
		}
		if verifyKeySize {
			if ordinal != len(out.PrimaryKey)+1 || keySize < ordinal || keySize > len(out.Columns) || constraint == "" || (keyConstraint != "" && (keyConstraint != constraint || expectedKeySize != keySize)) {
				err = fmt.Errorf("primary key metadata is incomplete or ambiguous")
				break
			}
			expectedKeySize, keyConstraint = keySize, constraint
		}
		if len(out.PrimaryKey) >= len(out.Columns) {
			err = fmt.Errorf("primary key metadata exceeds the table definition")
			break
		}
		found := false
		for i := range out.Columns {
			if editNameKey(out.Columns[i].Name, d) == editNameKey(name, d) {
				if out.Columns[i].PrimaryKey {
					break
				}
				out.Columns[i].PrimaryKey = true
				name = out.Columns[i].Name
				found = true
				break
			}
		}
		if !found {
			err = fmt.Errorf("primary key column is not visible")
			break
		}
		out.PrimaryKey = append(out.PrimaryKey, name)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return out, fmt.Errorf("read complete primary key: %w", err)
	}
	if verifyKeySize && expectedKeySize != len(out.PrimaryKey) {
		return out, fmt.Errorf("primary key metadata is incomplete")
	}
	return out, ctx.Err()
}

func editMySQLCanonicalTable(ctx context.Context, conn *sql.Conn, schema, table string) (string, error) {
	var mode int
	if err := conn.QueryRowContext(ctx, `SELECT @@lower_case_table_names`).Scan(&mode); err != nil {
		return "", fmt.Errorf("resolve editable table case mode: %w", err)
	}
	if mode != 1 && mode != 2 {
		return "", ErrCatalogNotFound
	}
	rows, err := conn.QueryContext(ctx, `SELECT TABLE_NAME FROM information_schema.TABLES WHERE BINARY TABLE_SCHEMA=BINARY ? AND BINARY LOWER(TABLE_NAME)=BINARY LOWER(?) AND TABLE_TYPE IN ('BASE TABLE','VIEW') LIMIT 2`, schema, table)
	if err != nil {
		return "", fmt.Errorf("resolve editable table name: %w", err)
	}
	defer rows.Close()
	var name string
	count := 0
	for rows.Next() {
		if err := rows.Scan(&name); err != nil {
			return "", err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if count != 1 {
		return "", ErrCatalogNotFound
	}
	return name, nil
}

func editMySQLTransactional(ctx context.Context, conn *sql.Conn, engine string) (bool, error) {
	if strings.EqualFold(engine, "InnoDB") {
		return true, nil
	}
	var transactions sql.NullString
	err := conn.QueryRowContext(ctx, `SELECT TRANSACTIONS FROM information_schema.ENGINES WHERE ENGINE=?`, engine).Scan(&transactions)
	if err != nil {
		return false, fmt.Errorf("read storage engine transaction support: %w", err)
	}
	if transactions.Valid {
		switch strings.ToUpper(strings.TrimSpace(transactions.String)) {
		case "YES":
			return true, nil
		case "NO":
			return false, nil
		}
	}
	return false, fmt.Errorf("storage engine transaction support is unavailable")
}

func editBaseTableKind(d dialect.ID, kind string) bool {
	if d.UsesMySQLSyntax() {
		// The metadata query itself restricts TABLE_TYPE to BASE TABLE.
		return !strings.EqualFold(kind, "VIEW")
	}
	if d == dialect.PostgreSQL {
		return kind == "r" || kind == "p"
	}
	return kind == "TABLE"
}

// A visible base-table row proves both the object kind and metadata access.
// Read the wider directory only when it can distinguish an invisible object,
// a visible unsupported object, or a case-folded MySQL table name.
func editableTableKind(ctx context.Context, conn *sql.Conn, d dialect.ID, r *CatalogColumnRequest) (string, error) {
	if d == dialect.PostgreSQL {
		return postgresEditableObjectKind(ctx, conn, r.Schema, r.Table)
	}
	readKind := func() (string, error) {
		q, err := editBaseTableQuery(d, *r)
		if err != nil {
			return "", err
		}
		var kind string
		err = conn.QueryRowContext(ctx, q.SQL, q.Args...).Scan(&kind)
		return kind, err
	}
	kind, err := readKind()
	if errors.Is(err, sql.ErrNoRows) {
		accessErr := catalogEditableRelationAccess(ctx, conn, d, r.Schema, r.Table)
		if d.UsesMySQLSyntax() && errors.Is(accessErr, ErrCatalogNotFound) {
			r.Table, accessErr = editMySQLCanonicalTable(ctx, conn, r.Schema, r.Table)
			if accessErr == nil {
				kind, err = readKind()
			}
		}
		if accessErr != nil {
			return "", accessErr
		}
	}
	if err != nil {
		return "", fmt.Errorf("read editable table kind: %w", err)
	}
	return kind, nil
}

func editBaseTableQuery(d dialect.ID, r CatalogColumnRequest) (catalogQuery, error) {
	b := catalogBinder{dialect: d}
	s, t := b.bind(r.Schema), b.bind(r.Table)
	var q string
	switch {
	case d.UsesMySQLSyntax():
		q = `SELECT COALESCE(ENGINE,'') FROM information_schema.TABLES WHERE BINARY TABLE_SCHEMA=BINARY ` + s + ` AND BINARY TABLE_NAME=BINARY ` + t + ` AND TABLE_TYPE='BASE TABLE'`
	case d == dialect.SQLServer:
		q = `SELECT 'TABLE' FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id WHERE s.name=` + s + ` AND t.name=` + t + ` AND t.is_ms_shipped=0`
	case d.UsesOracleSyntax():
		q = `SELECT 'TABLE' FROM ALL_TABLES WHERE OWNER=` + s + ` AND TABLE_NAME=` + t
	case d == dialect.DB2:
		q = `SELECT 'TABLE' FROM SYSCAT.TABLES t WHERE t.TABSCHEMA=` + s + ` AND t.TABNAME=` + t + ` AND t.TYPE='T' AND ` + db2TableVisible
	default:
		return catalogQuery{}, ErrUnsupportedDialect
	}
	return catalogQuery{SQL: q, Args: b.args}, nil
}

func editColumnFlagsQuery(d dialect.ID, r CatalogColumnRequest) (catalogQuery, error) {
	b := catalogBinder{dialect: d}
	if d == dialect.Dameng {
		// DM exposes the identity column's zero-based ID to table readers.
		// ALL_TAB_COLS numbers the same columns starting at one.
		s, t := b.bind(r.Schema), b.bind(r.Table)
		q := `SELECT c.COLUMN_NAME, CASE c.VIRTUAL_COLUMN WHEN 'YES' THEN 1 WHEN 'NO' THEN 0 END,
CASE WHEN c.COLUMN_ID=SF_GET_IDENTITY_COLID(o.OBJECT_ID)+1 THEN 1 ELSE 0 END,
CASE WHEN c.DEFAULT_LENGTH>0 THEN 1 ELSE 0 END
FROM SYS.ALL_TAB_COLS c JOIN SYS.ALL_OBJECTS o
ON o.OWNER=c.OWNER AND o.OBJECT_NAME=c.TABLE_NAME AND o.OBJECT_TYPE='TABLE' AND o.OBJECT_ID IS NOT NULL
WHERE c.OWNER=` + s + ` AND c.TABLE_NAME=` + t + ` ORDER BY c.COLUMN_ID`
		return catalogQuery{SQL: q, Args: b.args}, nil
	}
	s, t := b.bind(r.Schema), b.bind(r.Table)
	var q string
	switch {
	case d.UsesMySQLSyntax():
		q = `SELECT COLUMN_NAME, CASE WHEN EXTRA LIKE '%GENERATED%' AND EXTRA NOT LIKE '%DEFAULT_GENERATED%' THEN 1 ELSE 0 END, CASE WHEN EXTRA LIKE '%auto_increment%' THEN 1 ELSE 0 END, CASE WHEN COLUMN_DEFAULT IS NOT NULL THEN 1 ELSE 0 END FROM information_schema.COLUMNS WHERE BINARY TABLE_SCHEMA=BINARY ` + s + ` AND BINARY TABLE_NAME=BINARY ` + t + ` ORDER BY ORDINAL_POSITION`
	case d == dialect.PostgreSQL:
		q = `SELECT a.attname, COALESCE(to_jsonb(a)->>'attgenerated','')<>'', COALESCE(to_jsonb(a)->>'attidentity','')<>'', ad.oid IS NOT NULL FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class t ON t.oid=a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace LEFT JOIN pg_catalog.pg_attrdef ad ON ad.adrelid=t.oid AND ad.adnum=a.attnum WHERE n.nspname=` + s + ` AND t.relname=` + t + ` AND a.attnum>0 AND NOT a.attisdropped AND pg_catalog.has_column_privilege(t.oid,a.attnum,` + postgresColumnPrivileges + `) ORDER BY a.attnum`
	case d == dialect.SQLServer:
		q = `SELECT c.name, CASE WHEN c.is_computed=1 OR c.generated_always_type<>0 OR c.system_type_id=189 THEN 1 ELSE 0 END, c.is_identity, CASE WHEN c.default_object_id<>0 THEN 1 ELSE 0 END FROM sys.columns c JOIN sys.tables t ON t.object_id=c.object_id JOIN sys.schemas s ON s.schema_id=t.schema_id WHERE s.name=` + s + ` AND t.name=` + t + ` ORDER BY c.column_id`
	case d == dialect.OceanBaseOracle:
		// Several OB releases expose IDENTITY_COLUMN as a NULL placeholder.
		// Preserve unknown instead of treating it as a non-identity column.
		q = `SELECT COLUMN_NAME, CASE WHEN VIRTUAL_COLUMN='YES' THEN 1 WHEN VIRTUAL_COLUMN='NO' THEN 0 ELSE NULL END, CASE WHEN IDENTITY_COLUMN='YES' THEN 1 WHEN IDENTITY_COLUMN='NO' THEN 0 ELSE NULL END, CASE WHEN DEFAULT_LENGTH>0 THEN 1 ELSE 0 END FROM ALL_TAB_COLS WHERE OWNER=` + s + ` AND TABLE_NAME=` + t + ` AND HIDDEN_COLUMN='NO' ORDER BY COLUMN_ID`
	case d.UsesOracleSyntax():
		q = `SELECT COLUMN_NAME, CASE WHEN VIRTUAL_COLUMN='YES' THEN 1 ELSE 0 END, CASE WHEN IDENTITY_COLUMN='YES' THEN 1 ELSE 0 END, CASE WHEN DEFAULT_LENGTH>0 THEN 1 ELSE 0 END FROM ALL_TAB_COLS WHERE OWNER=` + s + ` AND TABLE_NAME=` + t + ` AND HIDDEN_COLUMN='NO' ORDER BY COLUMN_ID`
	case d == dialect.DB2:
		// For partitioned tables TBSPACE is NULL and the table IDs are
		// logical. A partition's physical ID cannot be inferred from them.
		q = `SELECT RTRIM(c.COLNAME), CASE WHEN c.GENERATED IN ('A','D') THEN 1 ELSE 0 END,
CASE WHEN c.IDENTITY='Y' THEN 1 ELSE 0 END, CASE WHEN c.DEFAULT IS NOT NULL THEN 1 ELSE 0 END,
CASE WHEN t.TBSPACE IS NOT NULL THEN t.TBSPACEID END, t.TABLEID, c.COLNO
FROM SYSCAT.COLUMNS c JOIN SYSCAT.TABLES t ON t.TABSCHEMA=c.TABSCHEMA AND t.TABNAME=c.TABNAME
WHERE c.TABSCHEMA=` + s + ` AND c.TABNAME=` + t + ` ORDER BY c.COLNO`
	default:
		return catalogQuery{}, ErrUnsupportedDialect
	}
	return catalogQuery{SQL: q, Args: b.args}, nil
}

// EditTypeName removes the catalog type's modifier or member list. ENUM/SET
// members may contain parentheses, so the final ')' closes that list.
func EditTypeName(dataType string) string {
	t := strings.ToLower(strings.TrimSpace(dataType))
	if i := strings.IndexByte(t, '('); i >= 0 {
		end := strings.LastIndexByte(t, ')')
		if end < i {
			return ""
		}
		t = t[:i] + t[end+1:]
	}
	return strings.Join(strings.Fields(t), " ")
}

// EditTypeSupported is intentionally narrower than result display. Binary,
// arrays, spatial/user-defined types and LOB locators need explicit codecs.
func EditTypeSupported(d dialect.ID, dataType string) bool {
	t := EditTypeName(dataType)
	if strings.HasSuffix(t, " unsigned") && d.UsesMySQLSyntax() {
		t = strings.TrimSuffix(t, " unsigned")
	}
	if d == dialect.SQLServer && (t == "timestamp" || t == "rowversion") {
		return false
	}
	switch t {
	case "tinyint", "smallint", "mediumint", "int", "integer", "bigint", "int2", "int4", "int8", "smallserial", "serial", "bigserial", "decimal", "numeric", "number", "dec", "real", "float", "double", "double precision", "float4", "float8", "binary_float", "binary_double":
		return true
	case "char", "character", "bpchar", "varchar", "character varying", "varchar2", "nvarchar", "nvarchar2", "longnvarchar", "nchar", "text", "ntext", "tinytext", "mediumtext", "longtext", "graphic", "vargraphic", "long varchar":
		return true
	case "enum", "set":
		return d.UsesMySQLSyntax()
	case "money", "smallmoney":
		return d == dialect.SQLServer
	case "date", "time", "time without time zone", "timestamp", "timestamp without time zone", "datetime", "datetime2", "smalldatetime":
		return true
	case "timestamp with time zone":
		return d == dialect.PostgreSQL || d == dialect.Oracle || d == dialect.SQLServer || d == dialect.OceanBaseOracle
	case "timestamptz", "datetimeoffset":
		return d == dialect.PostgreSQL || d == dialect.Oracle || d == dialect.SQLServer
	case "boolean", "bool":
		return d == dialect.PostgreSQL || d.UsesMySQLSyntax() || d == dialect.DB2
	case "bit":
		return d == dialect.SQLServer
	case "uuid":
		return d == dialect.PostgreSQL
	case "uniqueidentifier":
		return d == dialect.SQLServer
	case "json":
		// Oracle uses the opt-in server text define plus UTF-8 JSON binds.
		// OceanBase has its own text wire codec; neither path decodes OSON.
		return d == dialect.PostgreSQL || d.UsesMySQLSyntax() || d == dialect.Oracle || d == dialect.OceanBaseOracle
	case "jsonb":
		return d == dialect.PostgreSQL || d.UsesMySQLSyntax()
	default:
		return false
	}
}
