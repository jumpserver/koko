package sqlservice

import (
	"context"
	"database/sql"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

// These are catalog estimates/allocation counters, never business-table COUNTs.
// Numeric values stay decimal strings at the API boundary. Missing vendor facts
// remain unknown; an absent index or storage statistic is not an observed zero.
func overviewCatalogSource(d dialect.ID, b *catalogBinder, database string, broadSegments bool) (string, error) {
	base, err := b.objectSource(database, false)
	if err != nil {
		return "", err
	}
	fields, join := "", ""
	switch {
	case d == dialect.SQLServer:
		fields = `CASE WHEN o.objectkind='table' THEN r.estimated_rows END AS estimated_rows,
 CASE WHEN o.objectkind='table' AND NOT EXISTS (SELECT 1 FROM sys.indexes ci WHERE ci.object_id=OBJECT_ID(QUOTENAME(o.schemaname)+'.'+QUOTENAME(o.objectname)) AND ci.type IN (5,6)) THEN a.data_bytes END AS data_bytes,
 CASE WHEN o.objectkind='table' AND NOT EXISTS (SELECT 1 FROM sys.indexes ci WHERE ci.object_id=OBJECT_ID(QUOTENAME(o.schemaname)+'.'+QUOTENAME(o.objectname)) AND ci.type IN (5,6)) THEN a.index_bytes END AS index_bytes,
 CASE WHEN o.objectkind='table' AND NOT EXISTS (SELECT 1 FROM sys.indexes ci WHERE ci.object_id=OBJECT_ID(QUOTENAME(o.schemaname)+'.'+QUOTENAME(o.objectname)) AND ci.type IN (5,6)) THEN a.total_bytes END AS total_bytes,
 NULL AS last_analyzed,NULL AS engine,NULL AS character_set,NULL AS collation`
		join = ` LEFT JOIN (SELECT object_id,SUM(CONVERT(decimal(38,0),rows)) AS estimated_rows FROM sys.partitions WHERE index_id IN (0,1) GROUP BY object_id) r ON r.object_id=OBJECT_ID(QUOTENAME(o.schemaname)+'.'+QUOTENAME(o.objectname))
 LEFT JOIN (SELECT p.object_id,SUM(CASE WHEN p.index_id IN (0,1) THEN CONVERT(decimal(38,0),a.total_pages)*8192 ELSE 0 END) AS data_bytes,
 SUM(CASE WHEN p.index_id>1 THEN CONVERT(decimal(38,0),a.total_pages)*8192 ELSE 0 END) AS index_bytes,
 SUM(CONVERT(decimal(38,0),a.total_pages)*8192) AS total_bytes
 FROM sys.partitions p JOIN sys.allocation_units a ON (a.type IN (1,3) AND a.container_id=p.hobt_id) OR (a.type=2 AND a.container_id=p.partition_id) GROUP BY p.object_id) a
 ON a.object_id=OBJECT_ID(QUOTENAME(o.schemaname)+'.'+QUOTENAME(o.objectname))`
	case d.UsesOracleSyntax():
		fields = `t.NUM_ROWS AS estimated_rows,NULL AS data_bytes,NULL AS index_bytes,NULL AS total_bytes,
 TO_CHAR(t.LAST_ANALYZED,'YYYY-MM-DD"T"HH24:MI:SS') AS last_analyzed,NULL AS engine,NULL AS character_set,NULL AS collation`
		join = ` LEFT JOIN ALL_TABLES t ON t.OWNER=o.schemaname AND t.TABLE_NAME=o.objectname`
		if d == dialect.Oracle {
			fields = strings.Replace(fields, "NULL AS data_bytes,NULL AS index_bytes,NULL AS total_bytes", `z.data_bytes AS data_bytes,z.index_bytes AS index_bytes,z.total_bytes AS total_bytes`, 1)
			join += overviewOracleSegmentJoin(broadSegments)
		}
	case d == dialect.DB2:
		fields = `CASE WHEN t.TYPE IN ('T','S') AND t.CARD>=0 THEN t.CARD END AS estimated_rows,
 CASE WHEN t.TYPE IN ('T','S') AND t.NPAGES>=0 THEN DECIMAL(t.NPAGES,31,0)*ts.PAGESIZE END AS data_bytes,
 CASE WHEN t.TYPE IN ('T','S') AND COALESCE(ix.unknown_pages,0)=0 THEN COALESCE(ix.index_bytes,0) END AS index_bytes,
 CASE WHEN t.TYPE IN ('T','S') AND t.NPAGES>=0 AND COALESCE(ix.unknown_pages,0)=0 THEN DECIMAL(t.NPAGES,31,0)*ts.PAGESIZE+COALESCE(ix.index_bytes,0) END AS total_bytes,
 VARCHAR(t.STATS_TIME) AS last_analyzed,NULL AS engine,VARCHAR(t.CODEPAGE) AS character_set,NULL AS collation`
		join = ` JOIN SYSCAT.TABLES t ON t.TABSCHEMA=o.schemaname AND t.TABNAME=o.objectname LEFT JOIN SYSCAT.TABLESPACES ts ON ts.TBSPACEID=t.TBSPACEID
 LEFT JOIN (SELECT i.TABSCHEMA,i.TABNAME,SUM(CASE WHEN i.NLEAF>=0 THEN DECIMAL(i.NLEAF,31,0)*sp.PAGESIZE END) AS index_bytes,
 SUM(CASE WHEN i.NLEAF<0 OR sp.PAGESIZE IS NULL THEN 1 ELSE 0 END) AS unknown_pages FROM SYSCAT.INDEXES i LEFT JOIN SYSCAT.TABLESPACES sp ON sp.TBSPACEID=i.TBSPACEID GROUP BY i.TABSCHEMA,i.TABNAME) ix ON ix.TABSCHEMA=t.TABSCHEMA AND ix.TABNAME=t.TABNAME`
	case d == dialect.ClickHouse:
		rows, bytes := "CAST(NULL AS Nullable(UInt64))", "CAST(NULL AS Nullable(UInt64))"
		if b.clickHouse != nil {
			if b.clickHouse.tableRows == CapabilitySupported {
				rows = "t.total_rows"
			}
			if b.clickHouse.tableBytes == CapabilitySupported {
				bytes = "t.total_bytes"
			}
		}
		fields = rows + ` AS estimated_rows,CAST(NULL AS Nullable(UInt64)) AS data_bytes,CAST(NULL AS Nullable(UInt64)) AS index_bytes,` + bytes + ` AS total_bytes,NULL AS last_analyzed,t.engine AS engine,NULL AS character_set,NULL AS collation`
		join = ` JOIN system.tables t ON t.database=o.schemaname AND t.name=o.objectname`
	default:
		return "", ErrUnsupportedDialect
	}
	properties := `NULL AS tablespace,NULL AS status,NULL AS average_row_length,NULL AS sample_size,NULL AS created`
	switch {
	case d == dialect.SQLServer:
		properties = `NULL AS tablespace,NULL AS status,NULL AS average_row_length,NULL AS sample_size,CONVERT(varchar(30),info.create_date,126) AS created`
		join += ` JOIN sys.objects info ON info.object_id=OBJECT_ID(QUOTENAME(o.schemaname)+'.'+QUOTENAME(o.objectname))`
	case d.UsesOracleSyntax():
		properties = `t.TABLESPACE_NAME AS tablespace,t.STATUS AS status,t.AVG_ROW_LEN AS average_row_length,t.SAMPLE_SIZE AS sample_size,NULL AS created`
	case d == dialect.DB2:
		properties = `t.TBSPACE AS tablespace,t.STATUS AS status,t.AVGROWSIZE AS average_row_length,NULL AS sample_size,VARCHAR(t.CREATE_TIME) AS created`
	}
	return `SELECT o.dbname,o.schemaname,o.objectname,o.objectkind,o.objecttype,o.objectcomment,` + fields + `,` + properties + ` FROM (` + base + `) o` + join, nil
}

func overviewCatalogQuery(d dialect.ID, r OverviewRequest, clickHouse *clickHouseCapabilities, broadSegments bool) (catalogQuery, error) {
	switch r.Section {
	case "tables", "views", "indexes", "statistics", "properties":
	default:
		return catalogQuery{}, nil
	}
	b := &catalogBinder{dialect: d, clickHouse: clickHouse}
	if r.Section == "properties" {
		return overviewCatalogProperties(d, r, b)
	}
	var source string
	var err error
	if r.Section == "indexes" {
		source, err = overviewCatalogIndexes(d, b, r.Database)
	} else {
		source, err = overviewCatalogSource(d, b, r.Database, broadSegments)
	}
	if err != nil {
		return catalogQuery{}, err
	}
	if source == "" {
		return catalogQuery{}, nil
	}
	filter := "1=1"
	if r.Schema != "" {
		filter += " AND schemaname=" + b.bind(r.Schema)
	}
	if r.Section == "statistics" {
		q := `SELECT COALESCE(SUM(CASE WHEN objectkind='table' THEN 1 ELSE 0 END),0) AS tables,COALESCE(SUM(CASE WHEN objectkind='view' THEN 1 ELSE 0 END),0) AS views`
		for _, k := range []string{"estimated_rows", "data_bytes", "index_bytes", "total_bytes"} {
			column := k
			if d == dialect.ClickHouse {
				// Qualify inputs so ClickHouse's global output aliases cannot
				// substitute an aggregate into another aggregate's argument.
				column = "stats." + k
			}
			total := `SUM(CASE WHEN objectkind='table' THEN ` + column + ` END)`
			if d == dialect.ClickHouse {
				// ClickHouse SUM of entirely NULL input returns zero. Keep it
				// unknown until at least one engine reported an actual value.
				total = `if(countIf(objectkind='table' AND ` + column + ` IS NOT NULL)=0,NULL,` + total + `)`
			}
			q += `,` + total + ` AS ` + k + `,SUM(CASE WHEN objectkind='table' AND ` + column + ` IS NULL THEN 1 ELSE 0 END) AS ` + k + `_unknown`
		}
		return catalogQuery{q + ` FROM (` + source + `) stats WHERE ` + filter, b.args}, nil
	}
	columns := []string{"schemaname", "objectname"}
	after := []any{r.AfterSchema, r.AfterName}
	if r.Section == "indexes" {
		columns = append(columns, "index_name", "part_ordinal")
		after = []any{r.AfterSchema, r.AfterTable, r.AfterName, r.AfterOrdinal}
	} else {
		kind := "table"
		if r.Section == "views" {
			kind = "view"
		}
		filter += " AND objectkind=" + b.bind(kind)
	}
	filter += b.objectNameFilter(r.Query, false)
	op, order := ">", "ASC"
	if r.Sort == "name_desc" {
		op, order = "<", "DESC"
	}
	if r.AfterSchema != "" || r.AfterName != "" {
		var terms []string
		for i, col := range columns {
			var equal []string
			for j := 0; j < i; j++ {
				equal = append(equal, columns[j]+"="+b.bind(after[j]))
			}
			left, right := col, b.bind(after[i])
			if col != "part_ordinal" {
				left, right = catalogNameOrder(d, left), catalogNameOrder(d, right)
			}
			equal = append(equal, left+op+right)
			terms = append(terms, "("+strings.Join(equal, " AND ")+")")
		}
		filter += " AND (" + strings.Join(terms, " OR ") + ")"
	}
	var ordering []string
	for _, c := range columns {
		if c != "part_ordinal" {
			c = catalogNameOrder(d, c)
		}
		ordering = append(ordering, c+" "+order)
	}
	projection := `dbname,schemaname,objectname,objectkind,objecttype,objectcomment,estimated_rows,data_bytes,index_bytes,total_bytes,last_analyzed,engine,character_set,collation,tablespace,status,average_row_length,sample_size,created`
	if r.Section == "indexes" {
		projection = `dbname,schemaname,objectname,objectkind,objecttype,index_name,is_unique,method,definition,column_name,expression,part_ordinal,direction,prefix_length,included`
	}
	if r.Section == "indexes" && d.UsesOracleSyntax() {
		projection += ",index_owner"
	}
	page := b.orderedPage(`SELECT `+projection+` FROM (`+source+`) items WHERE `+filter+` ORDER BY `+strings.Join(ordering, ","), catalogLimit(r.Limit)+1)
	if r.Section == "indexes" && d.UsesOracleSyntax() {
		// COLUMN_EXPRESSION is LONG on Oracle. Fetch it only in the final SELECT,
		// after paging ordinary scalar keys; wrapping/sorting a LONG is illegal.
		var finalOrder []string
		for _, c := range columns {
			left := "p." + c
			if c != "part_ordinal" {
				left = catalogNameOrder(d, left)
			}
			finalOrder = append(finalOrder, left+" "+order)
		}
		page.SQL = `SELECT p.dbname,p.schemaname,p.objectname,p.objectkind,p.objecttype,p.index_name,p.is_unique,p.method,p.definition,p.column_name,e.COLUMN_EXPRESSION,p.part_ordinal,p.direction,p.prefix_length,p.included FROM (` + page.SQL + `) p LEFT JOIN ALL_IND_EXPRESSIONS e ON e.INDEX_OWNER=p.index_owner AND e.INDEX_NAME=p.index_name AND e.TABLE_OWNER=p.schemaname AND e.TABLE_NAME=p.objectname AND e.COLUMN_POSITION=p.part_ordinal ORDER BY ` + strings.Join(finalOrder, ",")
	}
	return page, nil
}

func overviewCatalogIndexes(d dialect.ID, b *catalogBinder, database string) (string, error) {
	if d == dialect.ClickHouse && (b.clickHouse == nil || b.clickHouse.skippingIndexes != CapabilitySupported) {
		return "", nil
	}
	objects, err := b.objectSource(database, false)
	if err != nil {
		return "", err
	}
	prefix := `SELECT o.dbname,o.schemaname,o.objectname,o.objectkind,o.objecttype,`
	switch {
	case d == dialect.SQLServer:
		return prefix + `i.name AS index_name,CASE WHEN i.is_unique=1 THEN 'true' ELSE 'false' END AS is_unique,i.type_desc AS method,NULL AS definition,c.name AS column_name,NULL AS expression,COALESCE(ic.index_column_id,0) AS part_ordinal,CASE WHEN ic.is_included_column=1 THEN NULL WHEN ic.is_descending_key=1 THEN 'DESC' ELSE 'ASC' END AS direction,NULL AS prefix_length,CONVERT(varchar(1),ic.is_included_column) AS included FROM (` + objects + `) o JOIN sys.indexes i ON i.object_id=OBJECT_ID(QUOTENAME(o.schemaname)+'.'+QUOTENAME(o.objectname)) LEFT JOIN sys.index_columns ic ON ic.object_id=i.object_id AND ic.index_id=i.index_id LEFT JOIN sys.columns c ON c.object_id=ic.object_id AND c.column_id=ic.column_id WHERE i.name IS NOT NULL AND i.is_hypothetical=0`, nil
	case d.UsesOracleSyntax():
		return prefix + `i.INDEX_NAME AS index_name,CASE WHEN i.UNIQUENESS='UNIQUE' THEN 'true' ELSE 'false' END AS is_unique,i.INDEX_TYPE AS method,NULL AS definition,c.COLUMN_NAME AS column_name,NULL AS expression,c.COLUMN_POSITION AS part_ordinal,c.DESCEND AS direction,NULL AS prefix_length,NULL AS included,i.OWNER AS index_owner FROM (` + objects + `) o JOIN ALL_INDEXES i ON i.TABLE_OWNER=o.schemaname AND i.TABLE_NAME=o.objectname LEFT JOIN ALL_IND_COLUMNS c ON c.INDEX_OWNER=i.OWNER AND c.INDEX_NAME=i.INDEX_NAME AND c.TABLE_OWNER=i.TABLE_OWNER AND c.TABLE_NAME=i.TABLE_NAME`, nil
	case d == dialect.DB2:
		return prefix + `RTRIM(i.INDNAME) AS index_name,CASE WHEN i.UNIQUERULE IN ('P','U') THEN 'true' ELSE 'false' END AS is_unique,i.INDEXTYPE AS method,NULL AS definition,RTRIM(c.COLNAME) AS column_name,NULL AS expression,c.COLSEQ AS part_ordinal,c.COLORDER AS direction,NULL AS prefix_length,NULL AS included FROM (` + objects + `) o JOIN SYSCAT.INDEXES i ON i.TABSCHEMA=o.schemaname AND i.TABNAME=o.objectname LEFT JOIN SYSCAT.INDEXCOLUSE c ON c.INDSCHEMA=i.INDSCHEMA AND c.INDNAME=i.INDNAME`, nil
	case d == dialect.ClickHouse:
		return prefix + `i.name AS index_name,NULL AS is_unique,i.type AS method,NULL AS definition,NULL AS column_name,i.expr AS expression,0 AS part_ordinal,NULL AS direction,NULL AS prefix_length,NULL AS included FROM (` + objects + `) o JOIN system.data_skipping_indices i ON i.database=o.schemaname AND i.table=o.objectname`, nil
	}
	return "", ErrUnsupportedDialect
}

func overviewCatalogProperties(d dialect.ID, r OverviewRequest, b *catalogBinder) (catalogQuery, error) {
	var q string
	switch {
	case d == dialect.SQLServer:
		q = `SELECT name AS database_name,collation_name AS collation,state_desc AS state,recovery_model_desc AS recovery_model,CONVERT(varchar(10),compatibility_level) AS compatibility_level,CONVERT(varchar(30),create_date,126) AS created FROM sys.databases WHERE name=` + b.bind(r.Database)
	case d.UsesOracleSyntax():
		if r.Schema == "" {
			q = `SELECT SYS_CONTEXT('USERENV','CURRENT_SCHEMA') AS current_schema FROM DUAL`
		} else {
			source, err := b.objectSource(r.Database, false)
			if err != nil {
				return catalogQuery{}, err
			}
			q = `SELECT USERNAME AS schema_name,TO_CHAR(CREATED,'YYYY-MM-DD"T"HH24:MI:SS') AS created FROM ALL_USERS u WHERE (USERNAME=SYS_CONTEXT('USERENV','CURRENT_SCHEMA') OR EXISTS (SELECT 1 FROM (` + source + `) o WHERE o.schemaname=u.USERNAME)) AND USERNAME=` + b.bind(r.Schema)
		}
	case d == dialect.DB2:
		if r.Schema == "" {
			q = `SELECT RTRIM(CURRENT SERVER) AS database_name,RTRIM(CURRENT SCHEMA) AS current_schema FROM SYSIBM.SYSDUMMY1`
		} else {
			source, err := b.objectSource(r.Database, false)
			if err != nil {
				return catalogQuery{}, err
			}
			q = `SELECT RTRIM(SCHEMANAME) AS schema_name,RTRIM(OWNER) AS owner,VARCHAR(CREATE_TIME) AS created,REMARKS AS comment FROM SYSCAT.SCHEMATA s WHERE (SCHEMANAME=CURRENT SCHEMA OR EXISTS (SELECT 1 FROM (` + source + `) o WHERE o.schemaname=s.SCHEMANAME)) AND SCHEMANAME=` + b.bind(r.Schema)
		}
	case d == dialect.ClickHouse:
		q = `SELECT name AS database_name,engine FROM system.databases WHERE name=` + b.bind(r.Database)
	default:
		return catalogQuery{}, ErrUnsupportedDialect
	}
	return catalogQuery{q, b.args}, nil
}

func overviewCatalogValue(d dialect.ID, key string, v sql.NullString, broadSegments bool) OverviewValue {
	state := "unknown"
	if key == "schema_ddl" || key == "definition" || key == "prefix_length" || (key == "included" && d != dialect.SQLServer) {
		state = "unsupported"
	}
	if key == "engine" && d != dialect.ClickHouse || key == "character_set" && d != dialect.DB2 || key == "collation" && d != dialect.SQLServer {
		state = "unsupported"
	}
	if key == "last_analyzed" && (d == dialect.SQLServer || d == dialect.ClickHouse) {
		state = "unsupported"
	}
	source := string(d) + "; authenticated system catalog"
	if d == dialect.SQLServer && strings.HasSuffix(key, "_bytes") {
		source = "sys.partitions/sys.allocation_units reserved rowstore allocation; columnstore excluded; includes LOB/row-overflow once"
	}
	if d.UsesOracleSyntax() && key == "estimated_rows" {
		source = "ALL_TABLES.NUM_ROWS optimizer estimate; may be absent/stale"
	}
	if d == dialect.Oracle && strings.HasSuffix(key, "_bytes") {
		source = "USER_SEGMENTS allocated table/index/LOB bytes; own schema only; other owners unknown"
		if broadSegments {
			source = "Authorized DBA_SEGMENTS allocated table/index/LOB bytes; joined only to objects visible in ALL_*"
		}
	}
	if d == dialect.DB2 && strings.HasSuffix(key, "_bytes") {
		source = "SYSCAT.TABLES.NPAGES and SYSCAT.INDEXES.NLEAF times each tablespace page size; RUNSTATS estimate, excludes internal index/LOB/XML pages"
	}
	if d == dialect.ClickHouse && (key == "total_bytes" || key == "estimated_rows") {
		source = "system.tables local engine totals; NULL when not available; not a cluster/replica sum"
	}
	if d == dialect.ClickHouse && (key == "data_bytes" || key == "index_bytes") {
		state = "unsupported"
		source = "system.tables does not split engine totals into data and index bytes"
	}
	return overviewValue(v, state, source)
}

func overviewCatalogDDL(ctx context.Context, conn *sql.Conn, d dialect.ID, r OverviewRequest) (OverviewValue, error) {
	if d != dialect.ClickHouse {
		return OverviewValue{State: "unsupported", Source: "Schema-wide DDL streaming is not provided by this adapter; native object definitions remain available separately"}, nil
	}
	var text string
	err := conn.QueryRowContext(ctx, "SHOW CREATE DATABASE "+quoteIdentifier(r.Database, d, '`')).Scan(&text)
	if err != nil {
		return OverviewValue{}, err
	}
	if len(text) > 65536 {
		text = text[:65536]
		for !validOverviewText(text) {
			text = text[:len(text)-1]
		}
		return OverviewValue{State: "truncated", Value: &text, Source: "SHOW CREATE DATABASE"}, nil
	}
	if strings.TrimSpace(text) == "" {
		return OverviewValue{State: "unknown", Source: "SHOW CREATE DATABASE"}, nil
	}
	return overviewText(text, "SHOW CREATE DATABASE; native database definition, does not include contained objects"), nil
}
