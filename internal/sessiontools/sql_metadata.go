package sessiontools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/jumpserver/koko/pkg/srvconn"
)

const (
	maxSQLMetadataMatches = 20
	maxSQLMetadataTables  = 5
	maxSQLMetadataQuery   = 128
	maxSQLMetadataColumns = 1000
	maxSQLMetadataBytes   = 64 * 1024
	maxSQLMetadataValue   = 4 * 1024
	maxSQLMetadataCache   = 32
)

type sqlMetadataTool struct {
	db       *sql.DB
	protocol string
	database string
	mu       sync.RWMutex
	cache    map[string]SQLSchemaLookupResult
}

type sqlTableRef struct {
	schema string
	table  string
}

func newSQLMetadataTool(db *sql.DB, protocol, database string) *sqlMetadataTool {
	return &sqlMetadataTool{
		db: db, protocol: protocol, database: strings.TrimSpace(database),
		cache: make(map[string]SQLSchemaLookupResult),
	}
}

func (t *sqlMetadataTool) Scope() string {
	return t.database
}

func (t *sqlMetadataTool) Invalidate() {
	t.mu.Lock()
	clear(t.cache)
	t.mu.Unlock()
}

func (t *sqlMetadataTool) Lookup(
	ctx context.Context, request SQLSchemaLookupRequest,
) (SQLSchemaLookupResult, error) {
	request, err := normalizeSQLSchemaLookupRequest(request)
	if err != nil {
		return SQLSchemaLookupResult{}, err
	}
	cacheValue, _ := json.Marshal(request)
	cacheKey := string(cacheValue)
	t.mu.RLock()
	cached, ok := t.cache[cacheKey]
	t.mu.RUnlock()
	if ok {
		return cached, nil
	}

	refs := make([]sqlTableRef, 0, maxSQLMetadataTables)
	seen := make(map[string]struct{})
	for _, table := range request.Tables {
		ref, parseErr := t.parseTableRef(table)
		if parseErr != nil {
			return SQLSchemaLookupResult{}, parseErr
		}
		appendSQLTableRef(&refs, seen, ref)
	}
	result := SQLSchemaLookupResult{Database: t.database, Tables: []SQLTableSchema{}}
	if request.Query != "" && len(refs) < maxSQLMetadataTables {
		matches, searchErr := t.search(ctx, request.Query)
		if searchErr != nil {
			return result, searchErr
		}
		for _, match := range matches {
			result.Matches = append(result.Matches, qualifiedSQLTableName(match))
			if len(refs) < maxSQLMetadataTables {
				appendSQLTableRef(&refs, seen, match)
			}
		}
	}
	if len(refs) > maxSQLMetadataTables {
		refs = refs[:maxSQLMetadataTables]
	}
	result.Tables, result.Truncated, err = t.describe(ctx, refs)
	if err != nil {
		return result, err
	}
	t.mu.Lock()
	if len(t.cache) >= maxSQLMetadataCache {
		clear(t.cache)
	}
	t.cache[cacheKey] = result
	t.mu.Unlock()
	return result, nil
}

func normalizeSQLSchemaLookupRequest(
	request SQLSchemaLookupRequest,
) (SQLSchemaLookupRequest, error) {
	request.Query = strings.TrimSpace(request.Query)
	if len(request.Query) > maxSQLMetadataQuery {
		return request, fmt.Errorf("SQL metadata search is too long")
	}
	result := SQLSchemaLookupRequest{Query: request.Query}
	seen := make(map[string]struct{})
	for _, table := range request.Tables {
		table = strings.TrimSpace(table)
		if table == "" {
			continue
		}
		if len(table) > 256 || strings.IndexFunc(table, unicode.IsControl) >= 0 {
			return request, fmt.Errorf("SQL metadata table name is invalid")
		}
		key := strings.ToLower(table)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result.Tables = append(result.Tables, table)
	}
	if len(result.Tables) > maxSQLMetadataTables {
		return request, fmt.Errorf("SQL metadata lookup supports at most %d tables", maxSQLMetadataTables)
	}
	if len(result.Tables) == 0 && result.Query == "" {
		return request, fmt.Errorf("SQL metadata lookup requires a table or search query")
	}
	return result, nil
}

func (t *sqlMetadataTool) parseTableRef(value string) (sqlTableRef, error) {
	parts := strings.Split(value, ".")
	if len(parts) > 3 {
		return sqlTableRef{}, fmt.Errorf("SQL metadata table name %q is invalid", value)
	}
	for index := range parts {
		parts[index] = trimSQLIdentifier(parts[index])
		if parts[index] == "" {
			return sqlTableRef{}, fmt.Errorf("SQL metadata table name %q is invalid", value)
		}
	}
	ref := sqlTableRef{table: parts[len(parts)-1]}
	if len(parts) == 3 {
		if t.database != "" && !strings.EqualFold(parts[0], t.database) {
			return ref, fmt.Errorf("SQL metadata lookup is restricted to database %q", t.database)
		}
		ref.schema = parts[1]
	} else if len(parts) == 2 {
		switch t.protocol {
		case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb, srvconn.ProtocolClickHouse:
			if t.database != "" && !strings.EqualFold(parts[0], t.database) {
				return ref, fmt.Errorf("SQL metadata lookup is restricted to database %q", t.database)
			}
		default:
			ref.schema = parts[0]
		}
	}
	return ref, nil
}

func trimSQLIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		pairs := map[byte]byte{'`': '`', '"': '"', '[': ']'}
		if end, ok := pairs[value[0]]; ok && value[len(value)-1] == end {
			value = value[1 : len(value)-1]
		}
	}
	return strings.TrimSpace(value)
}

func appendSQLTableRef(refs *[]sqlTableRef, seen map[string]struct{}, ref sqlTableRef) {
	key := strings.ToLower(ref.schema + "\x00" + ref.table)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*refs = append(*refs, ref)
}

func qualifiedSQLTableName(ref sqlTableRef) string {
	if ref.schema == "" {
		return ref.table
	}
	return ref.schema + "." + ref.table
}

func (t *sqlMetadataTool) search(ctx context.Context, query string) ([]sqlTableRef, error) {
	pattern := "%" + escapeSQLLike(query) + "%"
	prefix := escapeSQLLike(query) + "%"
	var statement string
	var args []any
	switch t.protocol {
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
		statement = `SELECT TABLE_SCHEMA, TABLE_NAME FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE IN ('BASE TABLE', 'VIEW')
AND TABLE_NAME LIKE ? ESCAPE '='
ORDER BY CASE WHEN LOWER(TABLE_NAME) = LOWER(?) THEN 0 WHEN LOWER(TABLE_NAME) LIKE LOWER(?) ESCAPE '=' THEN 1 ELSE 2 END,
TABLE_NAME LIMIT 20`
		args = []any{pattern, query, prefix}
	case srvconn.ProtocolPostgresql:
		statement = `SELECT table_schema, table_name FROM information_schema.tables
WHERE table_catalog = current_database() AND table_schema NOT IN ('pg_catalog', 'information_schema')
AND lower(table_name) LIKE lower($1) ESCAPE '='
ORDER BY CASE WHEN lower(table_name) = lower($2) THEN 0 WHEN lower(table_name) LIKE lower($3) ESCAPE '=' THEN 1 ELSE 2 END,
table_name LIMIT 20`
		args = []any{pattern, query, prefix}
	case srvconn.ProtocolSQLServer:
		statement = `SELECT TOP (20) TABLE_SCHEMA, TABLE_NAME FROM INFORMATION_SCHEMA.TABLES
WHERE TABLE_CATALOG = DB_NAME() AND TABLE_SCHEMA <> 'INFORMATION_SCHEMA'
AND LOWER(TABLE_NAME) LIKE LOWER(@p1) ESCAPE '='
ORDER BY CASE WHEN LOWER(TABLE_NAME) = LOWER(@p2) THEN 0 WHEN LOWER(TABLE_NAME) LIKE LOWER(@p3) ESCAPE '=' THEN 1 ELSE 2 END,
TABLE_NAME`
		args = []any{pattern, query, prefix}
	case srvconn.ProtocolOracle:
		statement = `SELECT OWNER, TABLE_NAME FROM (
SELECT OWNER, TABLE_NAME FROM ALL_TABLES WHERE UPPER(TABLE_NAME) LIKE UPPER(:1) ESCAPE '='
ORDER BY CASE WHEN UPPER(TABLE_NAME) = UPPER(:2) THEN 0 WHEN UPPER(TABLE_NAME) LIKE UPPER(:3) ESCAPE '=' THEN 1 ELSE 2 END,
TABLE_NAME) WHERE ROWNUM <= 20`
		args = []any{pattern, query, prefix}
	case srvconn.ProtocolClickHouse:
		statement = `SELECT database, name FROM system.tables
WHERE database = currentDatabase() AND positionCaseInsensitive(name, ?) > 0
ORDER BY if(lower(name) = lower(?), 0, if(startsWith(lower(name), lower(?)), 1, 2)), name LIMIT 20`
		args = []any{query, query, query}
	default:
		return nil, fmt.Errorf("SQL metadata is unsupported for protocol %s", t.protocol)
	}
	rows, err := t.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]sqlTableRef, 0, maxSQLMetadataMatches)
	for rows.Next() && len(result) < maxSQLMetadataMatches {
		var ref sqlTableRef
		if err = rows.Scan(&ref.schema, &ref.table); err != nil {
			return result, err
		}
		result = append(result, ref)
	}
	return result, rows.Err()
}

func escapeSQLLike(value string) string {
	value = strings.ReplaceAll(value, "=", "==")
	value = strings.ReplaceAll(value, "%", "=%")
	return strings.ReplaceAll(value, "_", "=_")
}

func (t *sqlMetadataTool) describe(
	ctx context.Context, refs []sqlTableRef,
) ([]SQLTableSchema, bool, error) {
	if len(refs) == 0 {
		return []SQLTableSchema{}, false, nil
	}
	statement, args, err := t.describeQuery(refs)
	if err != nil {
		return nil, false, err
	}
	rows, err := t.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	tables := make(map[string]*SQLTableSchema)
	order := make([]string, 0, len(refs))
	columnCount := 0
	resultBytes := 0
	truncated := false
	for rows.Next() {
		if columnCount >= maxSQLMetadataColumns {
			truncated = true
			break
		}
		var schema, table, name, columnType, nullable string
		var defaultValue any
		var ordinal int
		if err = rows.Scan(
			&schema, &table, &name, &columnType, &nullable, &defaultValue, &ordinal,
		); err != nil {
			return nil, false, err
		}
		key := strings.ToLower(schema + "\x00" + table)
		current := tables[key]
		if current == nil {
			current = &SQLTableSchema{
				Database: t.database, Schema: schema, Table: table,
				Columns: []SQLSchemaColumn{},
			}
			tables[key] = current
			order = append(order, key)
		}
		defaultText := nullableSQLMetadataDefault(defaultValue)
		if defaultText != nil {
			bounded := headTailPrompt(*defaultText, maxSQLMetadataValue)
			defaultText = &bounded
		}
		rowBytes := len(schema) + len(table) + len(name) + len(columnType) + len(nullable)
		if defaultText != nil {
			rowBytes += len(*defaultText)
		}
		if resultBytes+rowBytes > maxSQLMetadataBytes {
			truncated = true
			break
		}
		current.Columns = append(current.Columns, SQLSchemaColumn{
			Name: name, Type: columnType,
			Nullable: strings.EqualFold(nullable, "YES") || strings.EqualFold(nullable, "Y") || nullable == "1",
			Default:  defaultText, Ordinal: ordinal,
		})
		columnCount++
		resultBytes += rowBytes
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	result := make([]SQLTableSchema, 0, len(order))
	for _, key := range order {
		table := tables[key]
		sort.SliceStable(table.Columns, func(left, right int) bool {
			return table.Columns[left].Ordinal < table.Columns[right].Ordinal
		})
		result = append(result, *table)
	}
	return result, truncated, nil
}

func nullableSQLMetadataDefault(value any) *string {
	if value == nil {
		return nil
	}
	var result string
	switch typed := value.(type) {
	case []byte:
		result = string(typed)
	default:
		result = fmt.Sprint(typed)
	}
	result = strings.TrimSpace(strings.ToValidUTF8(result, "\uFFFD"))
	return &result
}

func (t *sqlMetadataTool) describeQuery(refs []sqlTableRef) (string, []any, error) {
	binder := sqlMetadataBinder{protocol: t.protocol}
	conditions := make([]string, 0, len(refs))
	for _, ref := range refs {
		name := binder.bind(ref.table)
		schema := ""
		if ref.schema != "" {
			schema = binder.bind(ref.schema)
		}
		switch t.protocol {
		case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
			conditions = append(conditions, "(TABLE_SCHEMA = DATABASE() AND TABLE_NAME = "+name+")")
		case srvconn.ProtocolPostgresql:
			if schema == "" {
				conditions = append(conditions, "(table_schema = current_schema() AND table_name = "+name+")")
			} else {
				conditions = append(conditions, "(table_schema = "+schema+" AND table_name = "+name+")")
			}
		case srvconn.ProtocolSQLServer:
			if schema == "" {
				conditions = append(conditions, "(TABLE_SCHEMA = SCHEMA_NAME() AND TABLE_NAME = "+name+")")
			} else {
				conditions = append(conditions, "(TABLE_SCHEMA = "+schema+" AND TABLE_NAME = "+name+")")
			}
		case srvconn.ProtocolOracle:
			if schema == "" {
				conditions = append(conditions, "(OWNER = SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA') AND TABLE_NAME = UPPER("+name+"))")
			} else {
				conditions = append(conditions, "(OWNER = UPPER("+schema+") AND TABLE_NAME = UPPER("+name+"))")
			}
		case srvconn.ProtocolClickHouse:
			conditions = append(conditions, "(database = currentDatabase() AND table = "+name+")")
		default:
			return "", nil, fmt.Errorf("SQL metadata is unsupported for protocol %s", t.protocol)
		}
	}
	where := strings.Join(conditions, " OR ")
	var statement string
	switch t.protocol {
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
		statement = `SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, ORDINAL_POSITION
FROM information_schema.COLUMNS WHERE ` + where + ` ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION`
	case srvconn.ProtocolPostgresql:
		statement = `SELECT table_schema, table_name, column_name, data_type, is_nullable, column_default, ordinal_position
FROM information_schema.columns WHERE table_catalog = current_database() AND (` + where + `)
ORDER BY table_schema, table_name, ordinal_position`
	case srvconn.ProtocolSQLServer:
		statement = `SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT, ORDINAL_POSITION
FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_CATALOG = DB_NAME() AND (` + where + `)
ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION`
	case srvconn.ProtocolOracle:
		statement = `SELECT OWNER, TABLE_NAME, COLUMN_NAME, DATA_TYPE,
CASE NULLABLE WHEN 'Y' THEN 'YES' ELSE 'NO' END, DATA_DEFAULT, COLUMN_ID
FROM ALL_TAB_COLUMNS WHERE ` + where + ` ORDER BY OWNER, TABLE_NAME, COLUMN_ID`
	case srvconn.ProtocolClickHouse:
		statement = `SELECT database, table, name, type,
if(startsWith(type, 'Nullable('), 'YES', 'NO'), nullIf(default_expression, ''), position
FROM system.columns WHERE ` + where + ` ORDER BY database, table, position`
	}
	return statement, binder.args, nil
}

type sqlMetadataBinder struct {
	protocol string
	args     []any
}

func (b *sqlMetadataBinder) bind(value any) string {
	b.args = append(b.args, value)
	index := len(b.args)
	switch b.protocol {
	case srvconn.ProtocolPostgresql:
		return fmt.Sprintf("$%d", index)
	case srvconn.ProtocolSQLServer:
		return fmt.Sprintf("@p%d", index)
	case srvconn.ProtocolOracle:
		return fmt.Sprintf(":%d", index)
	default:
		return "?"
	}
}
