package sqlservice

// Constraint details retain the v5.1.1 name/type/definition view. The caller has
// already checked catalog visibility; business-table grants are independent.
func inspectionConstraints(d Dialect, schema, table string) (catalogQuery, []string, bool) {
	b := catalogBinder{dialect: d}
	s, t := b.bind(schema), b.bind(table)
	if d == PostgreSQL {
		s, t = "$1", "$2"
	}
	q := ""
	switch {
	case IsMySQLDialect(d):
		q = `SELECT CONSTRAINT_NAME,CONSTRAINT_TYPE,NULL FROM information_schema.TABLE_CONSTRAINTS WHERE TABLE_SCHEMA=` + s + ` AND TABLE_NAME=` + t + ` AND CONSTRAINT_TYPE IN ('PRIMARY KEY','FOREIGN KEY','UNIQUE','CHECK') ORDER BY CONSTRAINT_NAME`
	case d == PostgreSQL:
		q = `SELECT k.conname,CASE k.contype WHEN 'p' THEN 'PRIMARY KEY' WHEN 'f' THEN 'FOREIGN KEY' WHEN 'u' THEN 'UNIQUE' WHEN 'c' THEN 'CHECK' END,pg_catalog.pg_get_constraintdef(k.oid,true)
FROM pg_catalog.pg_constraint k JOIN pg_catalog.pg_class t ON t.oid=k.conrelid JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace
LEFT JOIN pg_catalog.pg_class tr ON tr.oid=k.confrelid LEFT JOIN pg_catalog.pg_namespace nr ON nr.oid=tr.relnamespace
WHERE n.nspname=` + s + ` AND t.relname=` + t + ` AND k.contype IN ('p','f','u','c')
AND (k.contype<>'f' OR nr.nspname IN (SELECT schema_name FROM information_schema.schemata)) ORDER BY k.conname`
	case d == SQLServer:
		q = `WITH target AS (SELECT t.object_id FROM sys.tables t JOIN sys.schemas s ON s.schema_id=t.schema_id WHERE s.name=` + s + ` AND t.name=` + t + `)
SELECT k.name,CASE k.type WHEN 'PK' THEN 'PRIMARY KEY' ELSE 'UNIQUE' END,NULL FROM target t JOIN sys.key_constraints k ON k.parent_object_id=t.object_id
UNION ALL SELECT k.name,'FOREIGN KEY',NULL FROM target t JOIN sys.foreign_keys k ON k.parent_object_id=t.object_id
UNION ALL SELECT k.name,'CHECK',k.definition FROM target t JOIN sys.check_constraints k ON k.parent_object_id=t.object_id ORDER BY 1`
	case IsOracleDialect(d):
		q = `SELECT CONSTRAINT_NAME,CASE CONSTRAINT_TYPE WHEN 'P' THEN 'PRIMARY KEY' WHEN 'R' THEN 'FOREIGN KEY' WHEN 'U' THEN 'UNIQUE' WHEN 'C' THEN 'CHECK' END,SEARCH_CONDITION FROM ALL_CONSTRAINTS WHERE OWNER=` + s + ` AND TABLE_NAME=` + t + ` AND CONSTRAINT_TYPE IN ('P','R','U','C') ORDER BY CONSTRAINT_NAME`
	case d == DB2:
		q = `SELECT RTRIM(k.CONSTNAME),CASE k.TYPE WHEN 'P' THEN 'PRIMARY KEY' WHEN 'F' THEN 'FOREIGN KEY' WHEN 'U' THEN 'UNIQUE' WHEN 'K' THEN 'CHECK' END,c.TEXT FROM SYSCAT.TABCONST k LEFT JOIN SYSCAT.CHECKS c ON c.TABSCHEMA=k.TABSCHEMA AND c.TABNAME=k.TABNAME AND c.CONSTNAME=k.CONSTNAME WHERE k.TABSCHEMA=` + s + ` AND k.TABNAME=` + t + ` AND k.TYPE IN ('P','F','U','K') ORDER BY k.CONSTNAME`
	}
	return catalogQuery{SQL: q, Args: b.args}, []string{"name", "type", "definition"}, q != ""
}
