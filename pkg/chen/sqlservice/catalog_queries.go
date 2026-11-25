package sqlservice

import (
	"fmt"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

type catalogQuery struct {
	SQL  string
	Args []any
}

type catalogBinder struct {
	dialect    dialect.ID
	args       []any
	clickHouse *clickHouseCapabilities
}

func (b *catalogBinder) bind(value any) string {
	b.args = append(b.args, value)
	switch b.dialect {
	case dialect.PostgreSQL:
		return fmt.Sprintf("$%d", len(b.args))
	case dialect.SQLServer:
		return fmt.Sprintf("@p%d", len(b.args))
	case dialect.Oracle:
		return fmt.Sprintf(":%d", len(b.args))
	default:
		return "?"
	}
}

func (b *catalogBinder) orderedPage(query string, limit int) catalogQuery {
	// limit is an internal, bounded integer. Literal limits also support Db2
	// versions whose FETCH FIRST syntax does not accept a parameter marker.
	switch b.dialect {
	case dialect.SQLServer:
		query = strings.Replace(query, "SELECT ", fmt.Sprintf("SELECT TOP (%d) ", limit), 1)
	case dialect.Oracle, dialect.Dameng, dialect.OceanBaseOracle:
		query = fmt.Sprintf("SELECT * FROM (%s) WHERE ROWNUM <= %d", query, limit)
	case dialect.DB2:
		query += fmt.Sprintf(" FETCH FIRST %d ROWS ONLY", limit)
	case dialect.ClickHouse:
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	return catalogQuery{query, b.args}
}

// These products build directory, overview and diagram queries from objectSource.
// MySQL-family and PostgreSQL catalogs use their own native queries.
func usesCatalogObjectSource(d dialect.ID) bool {
	return d.UsesOracleSyntax() || d == dialect.SQLServer || d == dialect.DB2 || d == dialect.ClickHouse
}

// objectSource returns the objects visible to the authenticated connection. Db2
// applies its additional data-access checks for grid editing.
func (b *catalogBinder) objectSource(database string, editable bool) (string, error) {
	switch b.dialect {
	case dialect.SQLServer:
		return `SELECT DB_NAME() AS dbname, s.name AS schemaname, o.name AS objectname,
CASE WHEN o.type = 'V' THEN 'view' ELSE 'table' END AS objectkind,
CASE WHEN o.type = 'V' THEN 'view' ELSE 'table' END AS objecttype,
COALESCE(CONVERT(nvarchar(4000), ep.value), '') AS objectcomment
FROM sys.objects o JOIN sys.schemas s ON s.schema_id = o.schema_id
LEFT JOIN sys.extended_properties ep ON ep.class = 1 AND ep.major_id = o.object_id AND ep.minor_id = 0 AND ep.name = 'MS_Description'
WHERE o.type IN ('U','V')`, nil
	case dialect.Oracle, dialect.Dameng, dialect.OceanBaseOracle:
		// ALL_TABLES/ALL_VIEWS can push an owner predicate into the dictionary.
		// ALL_OBJECTS plus a correlated materialized-view lookup is much more
		// expensive for broadly granted accounts (including built-in APEX).
		return `SELECT ` + b.bind(database) + ` AS dbname, o.OWNER AS schemaname, o.OBJECT_NAME AS objectname,
o.OBJECT_KIND AS objectkind, o.OBJECT_KIND AS objecttype, c.COMMENTS AS objectcomment
FROM (SELECT OWNER, TABLE_NAME AS OBJECT_NAME, 'table' AS OBJECT_KIND FROM ALL_TABLES
UNION ALL SELECT OWNER, VIEW_NAME AS OBJECT_NAME, 'view' AS OBJECT_KIND FROM ALL_VIEWS) o
LEFT JOIN ALL_TAB_COMMENTS c ON c.OWNER = o.OWNER AND c.TABLE_NAME = o.OBJECT_NAME`, nil
	case dialect.DB2:
		filter := ""
		if editable {
			filter = " AND " + db2TableVisible
		}
		return `SELECT RTRIM(CURRENT SERVER) AS dbname, RTRIM(t.TABSCHEMA) AS schemaname, RTRIM(t.TABNAME) AS objectname,
CASE WHEN t.TYPE = 'V' THEN 'view' ELSE 'table' END AS objectkind,
CASE WHEN t.TYPE = 'V' THEN 'view' WHEN t.TYPE = 'S' THEN 'materialized_view' WHEN t.TYPE = 'N' THEN 'foreign_table' ELSE 'table' END AS objecttype,
COALESCE(t.REMARKS, '') AS objectcomment
FROM SYSCAT.TABLES t WHERE t.TYPE IN ('T','V','S','N')` + filter, nil
	case dialect.ClickHouse:
		// Local system tables apply SHOW TABLES visibility. Do not replace this
		// with cluster()/remote(), which can have different visibility semantics.
		comment := "NULL"
		if b.clickHouse != nil && b.clickHouse.tableComment == CapabilitySupported {
			comment = "comment"
		}
		return `SELECT database AS dbname, database AS schemaname, name AS objectname,
if(engine IN ('View','MaterializedView','LiveView','WindowView'), 'view', 'table') AS objectkind,
if(engine = 'MaterializedView', 'materialized_view', if(engine IN ('View','LiveView','WindowView'), 'view', 'table')) AS objecttype,
` + comment + ` AS objectcomment FROM system.tables WHERE is_temporary = 0 AND database = ` + b.bind(database), nil
	}
	return "", ErrUnsupportedDialect
}

func catalogNameOrder(d dialect.ID, column string) string {
	if d == dialect.SQLServer {
		return column + " COLLATE Latin1_General_BIN2"
	}
	if d.UsesOracleSyntax() {
		return "NLSSORT(" + column + ", 'NLS_SORT=BINARY')"
	}
	return column
}

func catalogSchemaQuery(d dialect.ID, database, after string, limit int) (catalogQuery, error) {
	b := &catalogBinder{dialect: d}
	var source string
	// Read schema catalogs directly, as the Java resource browser does. Empty
	// schemas and system schemas remain visible; object reads still use native
	// visibility checks, and editing keeps its separate system-schema guard.
	switch d {
	case dialect.SQLServer:
		source = "SELECT DB_NAME() AS dbname, SCHEMA_NAME AS schemaname FROM INFORMATION_SCHEMA.SCHEMATA"
	case dialect.Oracle, dialect.OceanBaseOracle:
		source = "SELECT " + b.bind(database) + " AS dbname, USERNAME AS schemaname FROM ALL_USERS"
	case dialect.Dameng:
		source = "SELECT " + b.bind(database) + " AS dbname, NAME AS schemaname FROM SYSOBJECTS WHERE TYPE$ = 'SCH'"
	case dialect.DB2:
		source = "SELECT RTRIM(CURRENT SERVER) AS dbname, RTRIM(SCHEMANAME) AS schemaname FROM SYSCAT.SCHEMATA"
	case dialect.ClickHouse:
		source = "SELECT currentDatabase() AS dbname, currentDatabase() AS schemaname"
	default:
		return catalogQuery{}, ErrUnsupportedDialect
	}
	query := "SELECT dbname, schemaname FROM (" + source + ") schemas WHERE 1 = 1"
	if after != "" {
		query += " AND " + catalogNameOrder(d, "schemaname") + " > " + catalogNameOrder(d, b.bind(after))
	}
	query += " ORDER BY " + catalogNameOrder(d, "schemaname")
	return b.orderedPage(query, catalogLimit(limit)+1), nil
}

func catalogObjectQuery(d dialect.ID, req CatalogObjectRequest, prefix bool, limit int, clickHouse *clickHouseCapabilities) (catalogQuery, error) {
	b := &catalogBinder{dialect: d, clickHouse: clickHouse}
	source, err := b.objectSource(req.Database, false)
	if err != nil {
		return catalogQuery{}, err
	}
	query := "SELECT dbname, schemaname, objectname, objectkind, objecttype, objectcomment FROM (" + source + ") objects WHERE 1 = 1"
	if req.Schema != "" {
		query += " AND schemaname = " + b.bind(req.Schema)
	}
	if req.Kind != "" {
		query += " AND objectkind = " + b.bind(req.Kind)
	}
	query += b.objectNameFilter(req.Query, prefix)
	if req.AfterSchema != "" || req.AfterName != "" {
		// Each occurrence is bound separately for positional-marker drivers.
		query += " AND (" + catalogNameOrder(d, "schemaname") + " > " + catalogNameOrder(d, b.bind(req.AfterSchema)) + " OR (schemaname = " + b.bind(req.AfterSchema) + " AND " + catalogNameOrder(d, "objectname") + " > " + catalogNameOrder(d, b.bind(req.AfterName)) + "))"
	}
	query += " ORDER BY " + catalogNameOrder(d, "schemaname") + ", " + catalogNameOrder(d, "objectname")
	return b.orderedPage(query, limit), nil
}

// objectNameFilter binds a case-insensitive prefix or substring match.
func (b *catalogBinder) objectNameFilter(query string, prefix bool) string {
	if query == "" {
		return ""
	}
	pattern := likePrefix(query)
	if b.dialect == dialect.ClickHouse {
		// ClickHouse LIKE escapes with a backslash and has no ESCAPE clause.
		pattern = clickHouseLike(query) + "%"
	}
	if !prefix {
		pattern = "%" + pattern
	}
	marker := b.bind(pattern)
	switch b.dialect {
	case dialect.ClickHouse:
		return " AND lowerUTF8(objectname) LIKE lowerUTF8(" + marker + ")"
	case dialect.DB2:
		// Db2 requires an explicit parameter type inside LOWER.
		marker = "CAST(" + marker + " AS VARCHAR(1024))"
	}
	return " AND LOWER(objectname) LIKE LOWER(" + marker + ") ESCAPE '!'"
}

func clickHouseLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(value)
}

type catalogColumnProjection uint8

const (
	catalogBasicColumns catalogColumnProjection = iota
	catalogColumnTypeModifiers
	catalogDB2LegacyTypeModifiers
)

func catalogColumnQuery(d dialect.ID, req CatalogColumnRequest, limit int, projection catalogColumnProjection) (catalogQuery, error) {
	b := &catalogBinder{dialect: d}
	var query string
	switch d {
	case dialect.SQLServer:
		query = `SELECT DB_NAME(), s.name, o.name, c.name,
t.name + CASE WHEN t.name IN ('varchar','char','varbinary','binary') THEN '(' + CASE WHEN c.max_length = -1 THEN 'max' ELSE CONVERT(varchar(10),c.max_length) END + ')'
WHEN t.name IN ('nvarchar','nchar') THEN '(' + CASE WHEN c.max_length = -1 THEN 'max' ELSE CONVERT(varchar(10),c.max_length / 2) END + ')'
WHEN t.name IN ('decimal','numeric') THEN '(' + CONVERT(varchar(10),c.precision) + ',' + CONVERT(varchar(10),c.scale) + ')'
WHEN t.name IN ('time','datetime2','datetimeoffset') THEN '(' + CONVERT(varchar(10),c.scale) + ')'
WHEN t.name='float' THEN '(' + CONVERT(varchar(10),c.precision) + ')' ELSE '' END,
c.is_nullable, COALESCE(CONVERT(nvarchar(4000),ep.value), ''), c.column_id
FROM sys.columns c JOIN sys.objects o ON o.object_id = c.object_id JOIN sys.schemas s ON s.schema_id = o.schema_id
JOIN sys.types t ON t.user_type_id = c.user_type_id
LEFT JOIN sys.extended_properties ep ON ep.class = 1 AND ep.major_id = c.object_id AND ep.minor_id = c.column_id AND ep.name = 'MS_Description'
WHERE o.type IN ('U','V') AND s.name = ` + b.bind(req.Schema) + " AND o.name = " + b.bind(req.Table) + " AND c.column_id > " + b.bind(req.AfterOrdinal) + " ORDER BY c.column_id"
	case dialect.Oracle, dialect.Dameng, dialect.OceanBaseOracle:
		modifiers := ""
		if projection != catalogBasicColumns {
			modifiers = ", c.DATA_LENGTH, c.DATA_PRECISION, c.DATA_SCALE, c.CHAR_LENGTH, c.CHAR_USED"
		}
		query = `SELECT ` + b.bind(req.Database) + `, c.OWNER, c.TABLE_NAME, c.COLUMN_NAME, c.DATA_TYPE,
CASE WHEN c.NULLABLE = 'Y' THEN 1 ELSE 0 END, cc.COMMENTS, c.COLUMN_ID` + modifiers + `
FROM ALL_TAB_COLUMNS c LEFT JOIN ALL_COL_COMMENTS cc ON cc.OWNER = c.OWNER AND cc.TABLE_NAME = c.TABLE_NAME AND cc.COLUMN_NAME = c.COLUMN_NAME
WHERE c.OWNER = ` + b.bind(req.Schema) + " AND c.TABLE_NAME = " + b.bind(req.Table) + " AND c.COLUMN_ID > " + b.bind(req.AfterOrdinal) + " ORDER BY c.COLUMN_ID"
	case dialect.DB2:
		modifiers := ""
		switch projection {
		case catalogColumnTypeModifiers:
			modifiers = ", c.LENGTH, c.LENGTH, c.SCALE, c.STRINGUNITSLENGTH, c.TYPESTRINGUNITS"
		case catalogDB2LegacyTypeModifiers:
			modifiers = ", c.LENGTH, c.LENGTH, c.SCALE, CAST(NULL AS INTEGER), CAST('UNKNOWN' AS VARCHAR(11))"
		}
		query = `SELECT RTRIM(CURRENT SERVER), RTRIM(c.TABSCHEMA), RTRIM(c.TABNAME), RTRIM(c.COLNAME), c.TYPENAME,
CASE WHEN c.NULLS = 'Y' THEN 1 ELSE 0 END, COALESCE(c.REMARKS, ''), c.COLNO + 1` + modifiers + `
FROM SYSCAT.COLUMNS c JOIN SYSCAT.TABLES t ON t.TABSCHEMA = c.TABSCHEMA AND t.TABNAME = c.TABNAME
WHERE c.TABSCHEMA = ` + b.bind(req.Schema) + " AND c.TABNAME = " + b.bind(req.Table) + " AND c.COLNO + 1 > " + b.bind(req.AfterOrdinal) + " ORDER BY c.COLNO"
	case dialect.ClickHouse:
		query = `SELECT database, database, table, name, type,
(startsWith(type, 'Nullable(') OR startsWith(type, 'LowCardinality(Nullable(')), comment, position
FROM system.columns WHERE database = ` + b.bind(req.Database) + " AND table = " + b.bind(req.Table) + " AND position > " + b.bind(req.AfterOrdinal) + " ORDER BY position"
	default:
		return catalogQuery{}, ErrUnsupportedDialect
	}
	if limit == 0 {
		return catalogQuery{SQL: query, Args: b.args}, nil
	}
	return b.orderedPage(query, limit), nil
}
