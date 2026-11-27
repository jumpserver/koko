package sqlservice

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

// Values remain decimal strings across JSON and JavaScript. State is never
// inferred from truthiness: a supported "0" differs from unknown and denied.
type OverviewValue struct {
	State  string  `json:"state"`
	Value  *string `json:"value"`
	Source string  `json:"source,omitempty"`
}
type OverviewRow struct {
	Object  CatalogRelation          `json:"object"`
	Name    string                   `json:"name"`
	Ordinal int                      `json:"ordinal,omitempty"`
	Fields  map[string]OverviewValue `json:"fields"`
}
type CatalogOverview struct {
	Database   string                   `json:"database"`
	Schema     string                   `json:"schema"`
	Section    string                   `json:"section"`
	State      string                   `json:"state"`
	ErrorCode  string                   `json:"error_code,omitempty"`
	SampledAt  string                   `json:"sampled_at"`
	Snapshot   bool                     `json:"snapshot"`
	Visibility string                   `json:"visibility"`
	Rows       []OverviewRow            `json:"items"`
	Fields     map[string]OverviewValue `json:"fields"`
	HasMore    bool                     `json:"has_more"`
	NextCursor string                   `json:"next_cursor,omitempty"`
	Contract   string                   `json:"-"`
}
type OverviewRequest struct {
	CatalogObjectRequest
	Section, Sort, AfterTable string
	AfterOrdinal              int
	AfterContract             string
}

func overviewValue(v sql.NullString, state, source string) OverviewValue {
	if v.Valid {
		return OverviewValue{State: "supported", Value: &v.String, Source: source}
	}
	return OverviewValue{State: state, Source: source}
}
func overviewText(value, source string) OverviewValue {
	return overviewValue(sql.NullString{String: value, Valid: true}, "unknown", source)
}

// CatalogOverviewRows performs section-level system catalog queries, never
// business COUNT or per-object inspection. MySQL can inspect any database visible
// to the account; an empty PG schema means all visible schemas.
func CatalogOverviewRows(ctx context.Context, conn *sql.Conn, d dialect.ID, req OverviewRequest) (result CatalogOverview, err error) {
	defer func() {
		if overviewPermissionDenied(err) {
			err = ErrCatalogForbidden
		}
	}()
	result = CatalogOverview{Database: req.Database, Schema: req.Schema, Section: req.Section, State: "supported", SampledAt: time.Now().UTC().Format(time.RFC3339Nano), Visibility: "authorized_catalog", Rows: []OverviewRow{}, Fields: map[string]OverviewValue{}}
	if err := validateCatalogInput(conn, d, req.Database, req.Schema, req.Query, req.AfterSchema, req.AfterName, req.AfterTable); err != nil {
		return result, err
	}
	if req.Sort != "name_asc" && req.Sort != "name_desc" {
		return result, fmt.Errorf("invalid overview sort")
	}
	if err := catalogScopeAccess(ctx, conn, d, req.Database, req.Schema); err != nil {
		return result, err
	}
	result.Contract = string(d)
	var clickHouse *clickHouseCapabilities
	if d == dialect.ClickHouse {
		clickHouse, err = probeClickHouseCapabilities(ctx, conn, req.Section, true)
		if err != nil {
			return result, err
		}
		result.Contract = clickHouse.contract(req.Section)
		if req.AfterName != "" && req.AfterContract != result.Contract {
			return result, ErrOverviewContractChanged
		}
		if req.Section == "indexes" && clickHouse.skippingIndexes != CapabilitySupported {
			result.State = string(clickHouse.skippingIndexes)
			result.Fields["indexes"] = OverviewValue{State: result.State, Source: "system.data_skipping_indices; capability=" + result.State}
			return result, nil
		}
	}
	oracleBroadSegments := false
	if d == dialect.Oracle && (req.Section == "tables" || req.Section == "views" || req.Section == "statistics") {
		oracleBroadSegments, err = overviewOracleSegmentAccess(ctx, conn)
		if err != nil {
			return result, err
		}
		result.Contract += fmt.Sprintf(":segments_dba=%t", oracleBroadSegments)
	}
	if !d.UsesMySQLSyntax() && req.AfterName != "" && req.AfterContract != "" && req.AfterContract != result.Contract {
		return result, ErrOverviewContractChanged
	}
	var capabilities *overviewMyCapabilities
	if d.UsesMySQLSyntax() {
		var err error
		capabilities, err = probeOverviewMyCapabilities(ctx, conn, d, req.Section)
		if err != nil {
			return result, err
		}
		result.Contract = capabilities.contract
		if req.AfterName != "" && req.AfterContract != result.Contract {
			return result, ErrOverviewContractChanged
		}
	}
	query, args := overviewSQL(d, req)
	if usesCatalogObjectSource(d) {
		q, e := overviewCatalogQuery(d, req, clickHouse, oracleBroadSegments)
		if e != nil {
			return result, e
		}
		query, args = q.SQL, q.Args
	}
	if capabilities != nil {
		q := capabilities.adaptQuery(catalogQuery{SQL: query, Args: args}, req.Section)
		query, args = q.SQL, q.Args
	}
	if query == "" {
		result.State = "unsupported"
		return result, nil
	}
	rows, err := queryCatalogRows(ctx, conn, d, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	if req.Section == "statistics" || req.Section == "properties" {
		names, err := rows.Columns()
		if err != nil {
			return result, err
		}
		if rows.Next() {
			values := make([]sql.NullString, len(names))
			dest := make([]any, len(names))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				return result, err
			}
			for i, n := range names {
				n = strings.ToLower(n)
				result.Fields[n] = overviewValue(values[i], "unknown", overviewSource(d, req.Section, n))
				if capabilities != nil {
					result.Fields[n] = capabilities.value(n, values[i], req.Section)
				}
				if usesCatalogObjectSource(d) {
					result.Fields[n] = overviewCatalogValue(d, n, values[i], oracleBroadSegments)
				}
				if clickHouse != nil {
					result.Fields[n] = clickHouse.value(n, result.Fields[n])
				}
			}
			if req.Section == "statistics" {
				for _, key := range []string{"estimated_rows", "data_bytes", "index_bytes", "total_bytes"} {
					unknown := result.Fields[key+"_unknown"].Value
					if unknown != nil && *unknown == "0" && result.Fields[key].Value == nil && result.Fields[key].State == "unknown" && (clickHouse == nil || clickHouse.supportsValue(key)) && (capabilities == nil || capabilities.state(key) == "unknown" && (capabilities.storage || !strings.HasSuffix(key, "_bytes"))) {
						result.Fields[key] = overviewText("0", overviewSource(d, req.Section, key))
					}
					if unknown != nil && *unknown != "0" && result.Fields[key].Value != nil {
						v := result.Fields[key]
						v.State = "partial"
						result.Fields[key] = v
						result.State = "partial"
					}
				}
			}
		}
		if err := rows.Err(); err != nil {
			return result, err
		}
		if err := rows.Close(); err != nil {
			return result, err
		}
		if req.Section == "properties" {
			result.Fields["schema_ddl"] = OverviewValue{State: "unsupported", Source: "No native schema DDL implemented for this product"}
			if usesCatalogObjectSource(d) {
				result.Fields["schema_ddl"], err = overviewCatalogDDL(ctx, conn, d, req)
				if err != nil {
					return result, err
				}
			}
			if capabilities != nil {
				result.Fields["server_version"] = overviewText(capabilities.version.Product, string(capabilities.dialect)+"; "+capabilities.version.Source.Query())
				result.Fields["schema_ddl"], err = overviewDatabaseDDL(ctx, conn, capabilities.dialect, req.Database)
				if err != nil {
					return result, err
				}
			}
		}
		return result, nil
	}
	for rows.Next() {
		var row OverviewRow
		row.Fields = map[string]OverviewValue{}
		if req.Section == "indexes" {
			var name, unique, method, definition, column, expression, position, direction, prefix, included sql.NullString
			dest := []any{&row.Object.Database, &row.Object.Schema, &row.Object.Name, &row.Object.Kind, &row.Object.ObjectType, &name, &unique, &method, &definition, &column, &expression, &position, &direction, &prefix}
			if usesCatalogObjectSource(d) {
				dest = append(dest, &included)
			}
			if err := rows.Scan(dest...); err != nil {
				return result, err
			}
			if d.UsesOracleSyntax() && expression.Valid && strings.TrimSpace(expression.String) != "" {
				column = sql.NullString{}
			}
			row.Name = name.String
			if usesCatalogObjectSource(d) {
				row.Fields["included"] = overviewCatalogValue(d, "included", included, oracleBroadSegments)
			}
			row.Ordinal, err = strconv.Atoi(position.String)
			if d.UsesMySQLSyntax() && (err != nil || row.Ordinal < 1) {
				return result, fmt.Errorf("invalid overview index ordinal")
			}
			for k, v := range map[string]sql.NullString{"unique": unique, "method": method, "definition": definition, "column": column, "expression": expression, "position": position, "direction": direction, "prefix_length": prefix} {
				state := "unknown"
				if d == dialect.PostgreSQL && k == "definition" {
					state = "restricted"
				}
				if (d == dialect.PostgreSQL && k != "definition" && k != "unique" && k != "method") || (d.UsesMySQLSyntax() && k == "definition") {
					state = "unsupported"
				}
				f := overviewValue(v, state, overviewSource(d, req.Section, k))
				if capabilities != nil {
					f = capabilities.value(k, v, req.Section)
				}
				if usesCatalogObjectSource(d) {
					f = overviewCatalogValue(d, k, v, oracleBroadSegments)
				}
				if v.Valid && len(v.String) > 16384 {
					short := v.String[:16384]
					for !validOverviewText(short) {
						short = short[:len(short)-1]
					}
					f.Value = &short
					f.State = "truncated"
				}
				row.Fields[k] = f
			}
		} else {
			var comment sql.NullString
			valueKeys := []string{"estimated_rows", "data_bytes", "index_bytes", "total_bytes", "last_analyzed", "engine", "character_set", "collation"}
			if d.UsesMySQLSyntax() {
				valueKeys = append(valueKeys, "created", "average_row_length")
			}
			if usesCatalogObjectSource(d) {
				valueKeys = append(valueKeys, "tablespace", "status", "average_row_length", "sample_size", "created")
			}
			vals := make([]sql.NullString, len(valueKeys))
			dest := []any{&row.Object.Database, &row.Object.Schema, &row.Object.Name, &row.Object.Kind, &row.Object.ObjectType, &comment}
			for i := range vals {
				dest = append(dest, &vals[i])
			}
			if err := rows.Scan(dest...); err != nil {
				return result, err
			}
			row.Name = row.Object.Name
			row.Object.Comment = comment.String
			row.Fields["comment"] = overviewValue(comment, "unknown", "catalog")
			if clickHouse != nil {
				row.Fields["comment"] = clickHouse.value("comment", row.Fields["comment"])
			}
			if capabilities != nil {
				row.Fields["comment"] = capabilities.value("comment", comment, req.Section)
			}
			if len(comment.String) > 4096 {
				text := comment.String[:4096]
				for !validOverviewText(text) {
					text = text[:len(text)-1]
				}
				row.Object.Comment = text
				row.Fields["comment"] = OverviewValue{State: "truncated", Value: &text, Source: "catalog"}
			}
			storage := row.Object.ObjectType == "table" || row.Object.ObjectType == "materialized_view"
			for i, k := range valueKeys {
				state := "unknown"
				if d == dialect.PostgreSQL && (i >= 5 || !storage && i <= 4) || d.UsesMySQLSyntax() && row.Object.Kind == "view" {
					state = "unsupported"
				}
				row.Fields[k] = overviewValue(vals[i], state, overviewSource(d, req.Section, k))
				if usesCatalogObjectSource(d) {
					row.Fields[k] = overviewCatalogValue(d, k, vals[i], oracleBroadSegments)
					if row.Object.Kind == "view" && k != "engine" {
						row.Fields[k] = OverviewValue{State: "unsupported", Source: "View has no stored table statistics"}
					}
				}
				if capabilities != nil {
					row.Fields[k] = capabilities.value(k, vals[i], req.Section)
					if row.Object.Kind == "view" {
						row.Fields[k] = OverviewValue{State: "unsupported", Source: "View has no stored table statistics or storage properties"}
					}
				}
				if clickHouse != nil {
					row.Fields[k] = clickHouse.value(k, row.Fields[k])
				}
			}
		}
		result.Rows = append(result.Rows, row)
	}
	return result, rows.Err()
}
func validOverviewText(v string) bool { return strings.ToValidUTF8(v, "") == v }
func overviewSource(d dialect.ID, section, key string) string {
	if d == dialect.PostgreSQL {
		switch key {
		case "estimated_rows":
			return "pg_class.reltuples; negative=unknown; partition parents/foreign tables excluded"
		case "data_bytes":
			return "pg_table_size; includes TOAST and auxiliary forks; physical relations only"
		case "index_bytes":
			return "pg_indexes_size; physical relations only"
		case "total_bytes":
			return "pg_total_relation_size; includes TOAST/indexes; physical relations only"
		case "last_analyzed":
			return "pg_stat_all_tables.last_analyze/last_autoanalyze; freshness may be unknown"
		}
		return "pg_catalog; visible objects only"
	}
	if section == "indexes" {
		if d == dialect.OceanBaseMySQL {
			return "OceanBase MySQL information_schema.STATISTICS; ordered catalog column names, including generated columns; EXPRESSION is a NULL compatibility placeholder"
		}
		return string(d) + " information_schema.STATISTICS; one row per ordered index part; fields probed on the authorized catalog connection"
	}
	if key == "last_analyzed" {
		return "unknown; UPDATE_TIME is not ANALYZE freshness"
	}
	if d == dialect.MariaDB {
		return "MariaDB information_schema; TABLE_ROWS may be estimated; InnoDB/XtraDB page allocation, Aria/MyISAM file bytes, MEMORY allocated memory; TABLES partition totals counted once; visible base/system-versioned tables only"
	}
	if d == dialect.OceanBaseMySQL {
		return "OceanBase MySQL information_schema; TABLE_ROWS catalog estimate; DATA_LENGTH table macroblock bytes, INDEX_LENGTH non-primary index macroblock bytes; TABLES logical-table totals counted once, not replica or tenant totals"
	}
	return "information_schema; TABLE_ROWS is engine-dependent estimate; DATA_LENGTH includes clustered data; INDEX_LENGTH secondary indexes; partition totals reported by TABLES; external LOB storage may be excluded"
}

const overviewPGFrom = ` FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 WHERE pg_catalog.current_database()=$1 AND ($2='' OR n.nspname=$2)
 AND n.nspname IN (SELECT schema_name FROM information_schema.schemata)`
const overviewMyFrom = ` FROM information_schema.TABLES t WHERE BINARY t.TABLE_SCHEMA=BINARY ? AND t.TABLE_TYPE IN ('BASE TABLE','VIEW')`

func overviewSQL(d dialect.ID, r OverviewRequest) (string, []any) {
	desc := r.Sort == "name_desc"
	op, order := ">", "ASC"
	if desc {
		op, order = "<", "DESC"
	}
	if r.Section == "properties" {
		if d == dialect.PostgreSQL {
			return `SELECT pg_catalog.pg_encoding_to_char(encoding) AS encoding, datcollate AS collation, datctype AS character_classification FROM pg_catalog.pg_database WHERE datname=$1`, []any{r.Database}
		}
		return `SELECT DEFAULT_CHARACTER_SET_NAME AS character_set, DEFAULT_COLLATION_NAME AS collation FROM information_schema.SCHEMATA WHERE BINARY SCHEMA_NAME=BINARY ?`, []any{r.Database}
	}
	if r.Section == "statistics" {
		if d == dialect.PostgreSQL {
			return `SELECT COUNT(*) FILTER(WHERE c.relkind IN ('r','p','f'))::text AS tables, COUNT(*) FILTER(WHERE c.relkind IN ('v','m'))::text AS views,
 COALESCE(SUM((SELECT COUNT(*) FROM pg_catalog.pg_index i WHERE i.indrelid=c.oid)),0)::text AS indexes,
 SUM(CASE WHEN c.relkind IN ('r','m') AND c.reltuples>=0 THEN FLOOR(c.reltuples)::numeric END)::text AS estimated_rows,
 COUNT(*) FILTER(WHERE c.relkind IN ('p','f') OR c.relkind IN ('r','m') AND c.reltuples<0)::text AS estimated_rows_unknown,
 SUM(CASE WHEN c.relkind IN ('r','m') THEN pg_catalog.pg_table_size(c.oid)::numeric END)::text AS data_bytes,
 SUM(CASE WHEN c.relkind IN ('r','m') THEN pg_catalog.pg_indexes_size(c.oid)::numeric END)::text AS index_bytes,
 SUM(CASE WHEN c.relkind IN ('r','m') THEN pg_catalog.pg_total_relation_size(c.oid)::numeric END)::text AS total_bytes,
 COUNT(*) FILTER(WHERE c.relkind IN ('p','f'))::text AS data_bytes_unknown,
 COUNT(*) FILTER(WHERE c.relkind IN ('p','f'))::text AS index_bytes_unknown,
 COUNT(*) FILTER(WHERE c.relkind IN ('p','f'))::text AS total_bytes_unknown` + overviewPGFrom + ` AND c.relkind IN ('r','p','m','f','v')`, []any{r.Database, r.Schema}
		}
		return `SELECT CAST(COALESCE(SUM(t.TABLE_TYPE='BASE TABLE'),0) AS CHAR) AS tables, CAST(COALESCE(SUM(t.TABLE_TYPE='VIEW'),0) AS CHAR) AS views,
 CAST(COALESCE(SUM((SELECT COUNT(DISTINCT BINARY s.INDEX_NAME) FROM information_schema.STATISTICS s WHERE BINARY s.TABLE_SCHEMA=BINARY t.TABLE_SCHEMA AND BINARY s.TABLE_NAME=BINARY t.TABLE_NAME)),0) AS CHAR) AS indexes,
 CAST(SUM(CASE WHEN t.TABLE_TYPE='BASE TABLE' THEN t.TABLE_ROWS END) AS CHAR) AS estimated_rows, CAST(COALESCE(SUM(t.TABLE_TYPE='BASE TABLE' AND t.TABLE_ROWS IS NULL),0) AS CHAR) AS estimated_rows_unknown,
 CAST(SUM(CASE WHEN t.TABLE_TYPE='BASE TABLE' THEN t.DATA_LENGTH END) AS CHAR) AS data_bytes, CAST(SUM(CASE WHEN t.TABLE_TYPE='BASE TABLE' THEN t.INDEX_LENGTH END) AS CHAR) AS index_bytes, CAST(SUM(CASE WHEN t.TABLE_TYPE='BASE TABLE' THEN t.DATA_LENGTH+t.INDEX_LENGTH END) AS CHAR) AS total_bytes,
 CAST(COALESCE(SUM(t.TABLE_TYPE='BASE TABLE' AND t.DATA_LENGTH IS NULL),0) AS CHAR) AS data_bytes_unknown,
 CAST(COALESCE(SUM(t.TABLE_TYPE='BASE TABLE' AND t.INDEX_LENGTH IS NULL),0) AS CHAR) AS index_bytes_unknown,
 CAST(COALESCE(SUM(t.TABLE_TYPE='BASE TABLE' AND (t.DATA_LENGTH IS NULL OR t.INDEX_LENGTH IS NULL)),0) AS CHAR) AS total_bytes_unknown` + overviewMyFrom, []any{r.Database}
	}
	if r.Section == "indexes" {
		if d == dialect.PostgreSQL {
			return `SELECT pg_catalog.current_database(),n.nspname,c.relname,CASE WHEN c.relkind='m' THEN 'view' ELSE 'table' END,CASE c.relkind WHEN 'm' THEN 'materialized_view' WHEN 'p' THEN 'partitioned_table' ELSE 'table' END,
 ix.relname,i.indisunique::text,am.amname,LEFT(pg_catalog.pg_get_indexdef(ix.oid),16385),
 (SELECT string_agg(pg_catalog.pg_get_indexdef(ix.oid,k,true),', ' ORDER BY k) FROM generate_series(1,i.indnatts) k),NULL::text,'0',NULL::text,NULL::text
 FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class ix ON ix.oid=i.indexrelid JOIN pg_catalog.pg_am am ON am.oid=ix.relam` + strings.Replace(overviewPGFrom, " FROM pg_catalog.pg_class c", " JOIN pg_catalog.pg_class c ON c.oid=i.indrelid", 1) + `
 AND (c.relname ILIKE $3 ESCAPE '!' OR ix.relname ILIKE $3 ESCAPE '!')
 AND ($4='' OR (n.nspname,c.relname,ix.relname) ` + op + ` ($4::pg_catalog.name,$5::pg_catalog.name,$6::pg_catalog.name))
 ORDER BY n.nspname ` + order + `,c.relname ` + order + `,ix.relname ` + order + ` LIMIT $7`, []any{r.Database, r.Schema, catalogSearch(r.Query), r.AfterSchema, r.AfterTable, r.AfterName, catalogLimit(r.Limit) + 1}
		}
		return `SELECT s.TABLE_SCHEMA,s.TABLE_SCHEMA,s.TABLE_NAME,'table','table',s.INDEX_NAME,IF(s.NON_UNIQUE=0,'true','false'),s.INDEX_TYPE,NULL,s.COLUMN_NAME,LEFT(s.EXPRESSION,16385),CAST(s.SEQ_IN_INDEX AS CHAR),s.COLLATION,CAST(s.SUB_PART AS CHAR)
 FROM information_schema.STATISTICS s WHERE BINARY s.TABLE_SCHEMA=BINARY ? AND (s.TABLE_NAME LIKE ? ESCAPE '!' OR s.INDEX_NAME LIKE ? ESCAPE '!')
 AND (?='' OR (BINARY s.TABLE_SCHEMA,BINARY s.TABLE_NAME,BINARY s.INDEX_NAME,s.SEQ_IN_INDEX) ` + op + ` (BINARY ?,BINARY ?,BINARY ?,?))
 ORDER BY BINARY s.TABLE_SCHEMA ` + order + `,BINARY s.TABLE_NAME ` + order + `,BINARY s.INDEX_NAME ` + order + `,s.SEQ_IN_INDEX ` + order + ` LIMIT ?`, []any{r.Database, catalogSearch(r.Query), catalogSearch(r.Query), r.AfterSchema, r.AfterSchema, r.AfterTable, r.AfterName, r.AfterOrdinal, catalogLimit(r.Limit) + 1}
	}
	if r.Section != "tables" && r.Section != "views" {
		return "", nil
	}
	if d == dialect.PostgreSQL {
		kinds := "('r','p','f')"
		if r.Section == "views" {
			kinds = "('v','m')"
		}
		return `SELECT ` + postgresRelationProjection + `,
 LEFT(pg_catalog.obj_description(c.oid,'pg_class'),4097),
 CASE WHEN c.relkind IN ('r','m') AND c.reltuples>=0 THEN FLOOR(c.reltuples)::numeric::text END,
 CASE WHEN c.relkind IN ('r','m') THEN pg_catalog.pg_table_size(c.oid)::text END,
 CASE WHEN c.relkind IN ('r','m') THEN pg_catalog.pg_indexes_size(c.oid)::text END,
 CASE WHEN c.relkind IN ('r','m') THEN pg_catalog.pg_total_relation_size(c.oid)::text END,
 GREATEST(st.last_analyze,st.last_autoanalyze)::text, NULL::text,NULL::text,NULL::text` + strings.Replace(overviewPGFrom, "\n WHERE", " LEFT JOIN pg_catalog.pg_stat_all_tables st ON st.relid=c.oid\n WHERE", 1) + ` AND c.relkind IN ` + kinds + `
 AND c.relname ILIKE $3 ESCAPE '!' AND ($4='' OR (n.nspname,c.relname) ` + op + ` ($4::pg_catalog.name,$5::pg_catalog.name))
 ORDER BY n.nspname ` + order + `,c.relname ` + order + ` LIMIT $6`, []any{r.Database, r.Schema, catalogSearch(r.Query), r.AfterSchema, r.AfterName, catalogLimit(r.Limit) + 1}
	}
	kind := "BASE TABLE"
	if r.Section == "views" {
		kind = "VIEW"
	}
	return `SELECT t.TABLE_SCHEMA,t.TABLE_SCHEMA,t.TABLE_NAME,IF(t.TABLE_TYPE='VIEW','view','table'),IF(t.TABLE_TYPE='VIEW','view','table'),LEFT(t.TABLE_COMMENT,4097),
 CAST(t.TABLE_ROWS AS CHAR),CAST(t.DATA_LENGTH AS CHAR),CAST(t.INDEX_LENGTH AS CHAR),CAST(t.DATA_LENGTH+t.INDEX_LENGTH AS CHAR),NULL,t.ENGINE,c.CHARACTER_SET_NAME,t.TABLE_COLLATION,CAST(t.CREATE_TIME AS CHAR),CAST(t.AVG_ROW_LENGTH AS CHAR)
 FROM information_schema.TABLES t LEFT JOIN information_schema.COLLATION_CHARACTER_SET_APPLICABILITY c ON c.COLLATION_NAME=t.TABLE_COLLATION
 WHERE BINARY t.TABLE_SCHEMA=BINARY ? AND t.TABLE_TYPE=? AND t.TABLE_NAME LIKE ? ESCAPE '!'
 AND (?='' OR (BINARY t.TABLE_SCHEMA,BINARY t.TABLE_NAME) ` + op + ` (BINARY ?,BINARY ?)) ORDER BY BINARY t.TABLE_SCHEMA ` + order + `,BINARY t.TABLE_NAME ` + order + ` LIMIT ?`, []any{r.Database, kind, catalogSearch(r.Query), r.AfterSchema, r.AfterSchema, r.AfterName, catalogLimit(r.Limit) + 1}
}
