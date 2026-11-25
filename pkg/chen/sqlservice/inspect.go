package sqlservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	goora "github.com/sijms/go-ora/v2"
)

type MetadataSection struct {
	State     CapabilityState  `json:"state,omitempty"`
	Deferred  bool             `json:"deferred,omitempty"`
	Supported bool             `json:"supported"`
	Items     []map[string]any `json:"items,omitempty"`
	Text      string           `json:"text,omitempty"`
	Source    string           `json:"source,omitempty"`
	Error     string           `json:"error,omitempty"`
	Truncated bool             `json:"truncated,omitempty"`
}
type TableInspection struct {
	ColumnOrdinals    map[string]int             `json:"column_ordinals"`
	ColumnsRestricted bool                       `json:"columns_restricted,omitempty"`
	Database          string                     `json:"database"`
	Schema            string                     `json:"schema"`
	Table             string                     `json:"table"`
	Columns           []CatalogColumn            `json:"columns"`
	Sections          map[string]MetadataSection `json:"sections"`
	Truncated         bool                       `json:"truncated"`
}

// InspectTable only reads system catalogs visible to the authenticated account.
// Each section declares support separately. Missing privileges never turn a
// partial/synthetic definition into a native DDL claim.
func InspectTable(ctx context.Context, conn *sql.Conn, d dialect.ID, req CatalogColumnRequest) (TableInspection, error) {
	return inspectTable(ctx, conn, d, req, false)
}
func InspectTableSummary(ctx context.Context, conn *sql.Conn, d dialect.ID, req CatalogColumnRequest) (TableInspection, error) {
	return inspectTable(ctx, conn, d, req, false)
}
func inspectTable(ctx context.Context, conn *sql.Conn, d dialect.ID, req CatalogColumnRequest, includeDDL bool) (TableInspection, error) {
	result := TableInspection{Database: req.Database, Schema: req.Schema, Table: req.Table, Sections: map[string]MetadataSection{}}
	req.Limit = 200
	req.AfterOrdinal = 0
	columns, err := ColumnRows(ctx, conn, d, req)
	if err != nil {
		return result, err
	}
	for page := columns; len(page) > MaxCatalogPageSize; {
		req.AfterOrdinal = page[len(page)-1].Ordinal
		page, err = ColumnRows(ctx, conn, d, req)
		if err != nil {
			return result, err
		}
		columns = append(columns, page...)
	}
	result.Columns = columns
	result.ColumnOrdinals = make(map[string]int, len(columns))
	for _, column := range columns {
		result.ColumnOrdinals[column.Name] = column.Ordinal
	}
	if len(columns) == 0 {
		return result, ErrCatalogNotFound
	}
	ddl := MetadataSection{Supported: ddlSupported(d), Source: ddlSource(d), Deferred: true}
	if d == dialect.PostgreSQL {
		var relationKind string
		err := conn.QueryRowContext(ctx, `SELECT t.relkind::text
FROM pg_catalog.pg_class t JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace
WHERE n.nspname=$1 AND t.relname=$2`, req.Schema, req.Table).Scan(&relationKind)
		if err != nil {
			return result, fmt.Errorf("check definition visibility: %w", err)
		}
		if relationKind == "v" || relationKind == "m" {
			ddl.Source = "native_view_query"
		}
	}
	result.Sections["ddl"] = ddl
	if d == dialect.ClickHouse && includeDDL {
		result.Sections["ddl"] = inspectionDDL(ctx, conn, d, req, result)
		return result, ctx.Err()
	}
	for _, kind := range []string{"primary_key", "foreign_keys", "indexes", "defaults", "constraints"} {
		q, names, ok := inspectionQuery(d, req, kind, true)
		clickHouseIndexes := d == dialect.ClickHouse && kind == "indexes"
		if clickHouseIndexes {
			capabilities, err := probeClickHouseCapabilities(ctx, conn, "indexes")
			if err != nil {
				return result, err
			}
			if capabilities.skippingIndexes != CapabilitySupported {
				result.Sections[kind] = MetadataSection{Supported: false, State: capabilities.skippingIndexes, Source: "system.data_skipping_indices; capability=" + string(capabilities.skippingIndexes)}
				continue
			}
			q, names, ok = capabilities.indexesQuery(req)
		}
		if !ok {
			result.Sections[kind] = MetadataSection{Supported: false}
			continue
		}
		section := MetadataSection{Supported: true, Items: []map[string]any{}}
		if clickHouseIndexes {
			section.State = CapabilitySupported
		}
		rows, err := conn.QueryContext(CatalogQueryContext(ctx), q.SQL, q.Args...)
		if err != nil {
			if clickHouseIndexes {
				if !clickHousePermissionDenied(err) {
					return result, err
				}
				section.State, section.Supported = CapabilityRestricted, false
			}
			section.Error = "Metadata is unavailable for this account or server version"
			result.Sections[kind] = section
			continue
		}
		for rows.Next() {
			vals := make([]any, len(names))
			dest := make([]any, len(names))
			for i := range vals {
				dest[i] = &vals[i]
			}
			if err = rows.Scan(dest...); err != nil {
				break
			}
			item := map[string]any{}
			for i, n := range names {
				switch value := vals[i].(type) {
				case []byte:
					item[n] = string(value)
				case goora.Clob, goora.NClob, interface {
					GetLength() (int64, error)
					ReadString(int, int) (string, error)
				}:
					item[n], err = metadataText(value)
				default:
					item[n] = value
				}
				if err != nil {
					break
				}
			}
			if err != nil {
				break
			}
			section.Items = append(section.Items, item)
		}
		if err == nil {
			err = rows.Err()
		}
		_ = rows.Close()
		if err != nil {
			if clickHouseIndexes {
				return result, err
			}
			section.Error = "Unable to read complete metadata"
		}
		result.Sections[kind] = section
	}
	if includeDDL && ddl.Supported {
		result.Sections["ddl"] = inspectionDDL(ctx, conn, d, req, result)
	}
	return result, ctx.Err()
}
func inspectionQuery(d dialect.ID, r CatalogColumnRequest, kind string, keySize ...bool) (catalogQuery, []string, bool) {
	if kind == "constraints" {
		return inspectionConstraints(d, r.Schema, r.Table)
	}
	b := catalogBinder{dialect: d}
	a, c := b.bind(r.Schema), b.bind(r.Table)
	if d == dialect.PostgreSQL {
		a, c = "$1", "$2"
	}
	q := ""
	names := []string{}
	switch kind {
	case "defaults":
		names = []string{"column", "expression"}
		switch {
		case d.UsesMySQLSyntax():
			q = `SELECT COLUMN_NAME, COLUMN_DEFAULT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=` + a + ` AND TABLE_NAME=` + c + ` ORDER BY ORDINAL_POSITION`
		case d == dialect.PostgreSQL:
			names = []string{"column", "expression", "generated", "identity"}
			q = `SELECT a.attname, pg_catalog.pg_get_expr(ad.adbin,ad.adrelid), COALESCE(to_jsonb(a)->>'attgenerated',''), COALESCE(to_jsonb(a)->>'attidentity','') FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class t ON t.oid=a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace LEFT JOIN pg_catalog.pg_attrdef ad ON ad.adrelid=t.oid AND ad.adnum=a.attnum WHERE n.nspname=` + a + ` AND t.relname=` + c + ` AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum`
		case d == dialect.SQLServer:
			q = `SELECT col.name, dc.definition FROM sys.columns col JOIN sys.objects t ON t.object_id=col.object_id JOIN sys.schemas s ON s.schema_id=t.schema_id LEFT JOIN sys.default_constraints dc ON dc.object_id=col.default_object_id WHERE s.name=` + a + ` AND t.name=` + c + ` ORDER BY col.column_id`
		case d.UsesOracleSyntax():
			q = `SELECT COLUMN_NAME, DATA_DEFAULT FROM ALL_TAB_COLUMNS WHERE OWNER=` + a + ` AND TABLE_NAME=` + c + ` ORDER BY COLUMN_ID`
		case d == dialect.DB2:
			q = `SELECT COLNAME, DEFAULT FROM SYSCAT.COLUMNS WHERE TABSCHEMA=` + a + ` AND TABNAME=` + c + ` ORDER BY COLNO`
		case d == dialect.ClickHouse:
			q = `SELECT name, default_expression FROM system.columns WHERE database=` + a + ` AND table=` + c + ` ORDER BY position`
		}
	case "primary_key":
		names = []string{"name", "column", "ordinal"}
		switch {
		case d == dialect.MariaDB:
			// MariaDB's metadata collation can equate distinct table names.
			// Use the same exact lookup for the key columns and their count.
			const source = ` FROM information_schema.KEY_COLUMN_USAGE
WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
  AND BINARY TABLE_SCHEMA = BINARY ? AND BINARY TABLE_NAME = BINARY ?
  AND BINARY CONSTRAINT_NAME = BINARY 'PRIMARY'`
			args := []any{r.Schema, r.Table, r.Schema, r.Table}
			q = "SELECT CONSTRAINT_NAME, COLUMN_NAME, ORDINAL_POSITION"
			if len(keySize) > 0 && keySize[0] {
				q += ", (SELECT COUNT(*)" + source + ")"
				args = append(args, args...)
				names = append(names, "column_count")
			}
			return catalogQuery{q + source + " ORDER BY ORDINAL_POSITION", args}, names, true
		case d.UsesMySQLSyntax():
			q = `SELECT CONSTRAINT_NAME, COLUMN_NAME, ORDINAL_POSITION FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=` + a + ` AND TABLE_NAME=` + c + ` AND CONSTRAINT_NAME='PRIMARY' ORDER BY ORDINAL_POSITION`
		case d == dialect.PostgreSQL:
			q = `SELECT k.conname,a.attname,u.ord FROM pg_catalog.pg_constraint k JOIN pg_catalog.pg_class t ON t.oid=k.conrelid JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace CROSS JOIN LATERAL unnest(k.conkey) WITH ORDINALITY AS u(attnum,ord) JOIN pg_catalog.pg_attribute a ON a.attrelid=t.oid AND a.attnum=u.attnum WHERE n.nspname=` + a + ` AND t.relname=` + c + ` AND k.contype='p' ORDER BY u.ord`
		case d == dialect.SQLServer:
			q = `SELECT i.name,c.name,ic.key_ordinal FROM sys.indexes i JOIN sys.tables t ON t.object_id=i.object_id JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.index_columns ic ON ic.object_id=t.object_id AND ic.index_id=i.index_id JOIN sys.columns c ON c.object_id=t.object_id AND c.column_id=ic.column_id WHERE s.name=` + a + ` AND t.name=` + c + ` AND i.is_primary_key=1 ORDER BY ic.key_ordinal`
		case d.UsesOracleSyntax():
			q = `SELECT c.CONSTRAINT_NAME,k.COLUMN_NAME,k.POSITION FROM ALL_CONSTRAINTS c JOIN ALL_CONS_COLUMNS k ON k.OWNER=c.OWNER AND k.CONSTRAINT_NAME=c.CONSTRAINT_NAME WHERE c.OWNER=` + a + ` AND c.TABLE_NAME=` + c + ` AND c.CONSTRAINT_TYPE='P' ORDER BY k.POSITION`
		case d == dialect.DB2:
			q = `SELECT k.CONSTNAME,k.COLNAME,k.COLSEQ FROM SYSCAT.KEYCOLUSE k JOIN SYSCAT.TABCONST t ON t.TABSCHEMA=k.TABSCHEMA AND t.TABNAME=k.TABNAME AND t.CONSTNAME=k.CONSTNAME WHERE k.TABSCHEMA=` + a + ` AND k.TABNAME=` + c + ` AND t.TYPE='P' ORDER BY k.COLSEQ`
		case d == dialect.ClickHouse:
			q = `SELECT 'PRIMARY', name, position FROM system.columns WHERE database=` + a + ` AND table=` + c + ` AND is_in_primary_key=1 ORDER BY position`
		}
	case "indexes":
		names = []string{"name", "column", "ordinal", "unique", "method", "sort_order", "included", "protected"}
		switch {
		case d.UsesMySQLSyntax():
			q = `SELECT i.INDEX_NAME,i.COLUMN_NAME,i.SEQ_IN_INDEX,CASE WHEN i.NON_UNIQUE=0 THEN 1 ELSE 0 END,i.INDEX_TYPE,CASE i.COLLATION WHEN 'A' THEN 'ASC' WHEN 'D' THEN 'DESC' END,0,CASE WHEN EXISTS (SELECT 1 FROM information_schema.TABLE_CONSTRAINTS k WHERE k.TABLE_SCHEMA=i.TABLE_SCHEMA AND k.TABLE_NAME=i.TABLE_NAME AND k.CONSTRAINT_NAME=i.INDEX_NAME) THEN 1 ELSE 0 END FROM information_schema.STATISTICS i WHERE i.TABLE_SCHEMA=` + a + ` AND i.TABLE_NAME=` + c + ` ORDER BY i.INDEX_NAME,i.SEQ_IN_INDEX`
		case d == dialect.PostgreSQL:
			names = append(names, "definition")
			q = `SELECT ci.relname,pg_catalog.pg_get_indexdef(i.indexrelid,u.ord::integer,true),u.ord,CASE WHEN i.indisunique THEN 1 ELSE 0 END,am.amname,CASE WHEN (i.indoption[(u.ord-1)::integer] & 1)=1 THEN 'DESC' ELSE 'ASC' END,CASE WHEN u.ord>COALESCE((to_jsonb(i)->>'indnkeyatts')::integer,i.indnatts) THEN 1 ELSE 0 END,CASE WHEN EXISTS (SELECT 1 FROM pg_catalog.pg_constraint k WHERE k.conindid=i.indexrelid AND k.contype IN ('p','u','x')) THEN 1 ELSE 0 END,pg_catalog.pg_get_indexdef(i.indexrelid) FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class t ON t.oid=i.indrelid JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace JOIN pg_catalog.pg_class ci ON ci.oid=i.indexrelid JOIN pg_catalog.pg_am am ON am.oid=ci.relam CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY u(attnum,ord) WHERE n.nspname=` + a + ` AND t.relname=` + c + ` ORDER BY ci.relname,u.ord`
		case d == dialect.SQLServer:
			q = `SELECT i.name,c.name,ic.key_ordinal,CONVERT(int,i.is_unique),i.type_desc,CASE WHEN ic.is_included_column=1 THEN NULL WHEN ic.is_descending_key=1 THEN 'DESC' ELSE 'ASC' END,CONVERT(int,ic.is_included_column),CASE WHEN i.is_primary_key=1 OR i.is_unique_constraint=1 THEN 1 ELSE 0 END FROM sys.indexes i JOIN sys.objects t ON t.object_id=i.object_id JOIN sys.schemas s ON s.schema_id=t.schema_id LEFT JOIN sys.index_columns ic ON ic.object_id=t.object_id AND ic.index_id=i.index_id LEFT JOIN sys.columns c ON c.object_id=t.object_id AND c.column_id=ic.column_id WHERE s.name=` + a + ` AND t.name=` + c + ` AND i.index_id>0 AND i.is_hypothetical=0 ORDER BY i.name,ic.index_column_id`
		case d.UsesOracleSyntax():
			names = append(names, "expression")
			q = `SELECT i.INDEX_NAME,k.COLUMN_NAME,k.COLUMN_POSITION,CASE WHEN i.UNIQUENESS='UNIQUE' THEN 1 ELSE 0 END,i.INDEX_TYPE,k.DESCEND,0,CASE WHEN EXISTS (SELECT 1 FROM ALL_CONSTRAINTS z WHERE z.OWNER=i.TABLE_OWNER AND z.TABLE_NAME=i.TABLE_NAME AND z.INDEX_NAME=i.INDEX_NAME AND z.CONSTRAINT_TYPE IN ('P','U')) THEN 1 ELSE 0 END,e.COLUMN_EXPRESSION FROM ALL_INDEXES i JOIN ALL_IND_COLUMNS k ON k.INDEX_OWNER=i.OWNER AND k.INDEX_NAME=i.INDEX_NAME AND k.TABLE_OWNER=i.TABLE_OWNER LEFT JOIN ALL_IND_EXPRESSIONS e ON e.INDEX_OWNER=k.INDEX_OWNER AND e.INDEX_NAME=k.INDEX_NAME AND e.TABLE_OWNER=k.TABLE_OWNER AND e.COLUMN_POSITION=k.COLUMN_POSITION WHERE i.TABLE_OWNER=` + a + ` AND i.TABLE_NAME=` + c + ` ORDER BY i.INDEX_NAME,k.COLUMN_POSITION`
		case d == dialect.DB2:
			q = `SELECT RTRIM(i.INDNAME),RTRIM(k.COLNAME),k.COLSEQ,CASE WHEN i.UNIQUERULE IN ('P','U') THEN 1 ELSE 0 END,RTRIM(i.INDEXTYPE),CASE k.COLORDER WHEN 'A' THEN 'ASC' WHEN 'D' THEN 'DESC' END,CASE WHEN k.COLORDER='I' THEN 1 ELSE 0 END,CASE WHEN i.UNIQUERULE='P' OR EXISTS (SELECT 1 FROM SYSCAT.CONSTDEP z WHERE z.TABSCHEMA=i.TABSCHEMA AND z.TABNAME=i.TABNAME AND z.BSCHEMA=i.INDSCHEMA AND z.BNAME=i.INDNAME AND z.BTYPE='I') THEN 1 ELSE 0 END FROM SYSCAT.INDEXES i JOIN SYSCAT.INDEXCOLUSE k ON k.INDSCHEMA=i.INDSCHEMA AND k.INDNAME=i.INDNAME WHERE i.TABSCHEMA=` + a + ` AND i.TABNAME=` + c + ` ORDER BY i.INDNAME,k.COLSEQ`
		}
	case "foreign_keys":
		names = []string{"name", "column", "referenced_schema", "referenced_table", "referenced_column", "ordinal", "column_count"}
		switch {
		case d.UsesMySQLSyntax():
			q = `SELECT k.CONSTRAINT_NAME,k.COLUMN_NAME,k.REFERENCED_TABLE_SCHEMA,k.REFERENCED_TABLE_NAME,k.REFERENCED_COLUMN_NAME,k.ORDINAL_POSITION,(SELECT COUNT(*) FROM information_schema.KEY_COLUMN_USAGE z WHERE z.TABLE_SCHEMA=k.TABLE_SCHEMA AND z.TABLE_NAME=k.TABLE_NAME AND z.CONSTRAINT_NAME=k.CONSTRAINT_NAME) FROM information_schema.KEY_COLUMN_USAGE k WHERE TABLE_SCHEMA=` + a + ` AND TABLE_NAME=` + c + ` AND REFERENCED_TABLE_NAME IS NOT NULL ORDER BY CONSTRAINT_NAME,ORDINAL_POSITION`
		case d == dialect.PostgreSQL:
			q = `SELECT k.conname,ac.attname,nr.nspname,tr.relname,ar.attname,u.ord,cardinality(k.conkey) FROM pg_catalog.pg_constraint k JOIN pg_catalog.pg_class t ON t.oid=k.conrelid JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace JOIN pg_catalog.pg_class tr ON tr.oid=k.confrelid JOIN pg_catalog.pg_namespace nr ON nr.oid=tr.relnamespace CROSS JOIN LATERAL unnest(k.conkey,k.confkey) WITH ORDINALITY AS u(attnum,refnum,ord) JOIN pg_catalog.pg_attribute ac ON ac.attrelid=t.oid AND ac.attnum=u.attnum JOIN pg_catalog.pg_attribute ar ON ar.attrelid=tr.oid AND ar.attnum=u.refnum WHERE n.nspname=` + a + ` AND t.relname=` + c + ` AND k.contype='f' ORDER BY k.conname,u.ord`
		case d == dialect.SQLServer:
			q = `SELECT fk.name,c.name,rs.name,rt.name,rc.name,k.constraint_column_id,(SELECT COUNT(*) FROM sys.foreign_key_columns z WHERE z.constraint_object_id=fk.object_id) FROM sys.foreign_keys fk JOIN sys.foreign_key_columns k ON k.constraint_object_id=fk.object_id JOIN sys.tables t ON t.object_id=k.parent_object_id JOIN sys.schemas s ON s.schema_id=t.schema_id JOIN sys.columns c ON c.object_id=t.object_id AND c.column_id=k.parent_column_id JOIN sys.tables rt ON rt.object_id=k.referenced_object_id JOIN sys.schemas rs ON rs.schema_id=rt.schema_id JOIN sys.columns rc ON rc.object_id=rt.object_id AND rc.column_id=k.referenced_column_id WHERE s.name=` + a + ` AND t.name=` + c + ` ORDER BY fk.name,k.constraint_column_id`
		case d.UsesOracleSyntax():
			q = `SELECT f.CONSTRAINT_NAME,k.COLUMN_NAME,r.OWNER,r.TABLE_NAME,rk.COLUMN_NAME,k.POSITION,(SELECT COUNT(*) FROM ALL_CONS_COLUMNS z WHERE z.OWNER=f.OWNER AND z.CONSTRAINT_NAME=f.CONSTRAINT_NAME) FROM ALL_CONSTRAINTS f JOIN ALL_CONS_COLUMNS k ON k.OWNER=f.OWNER AND k.CONSTRAINT_NAME=f.CONSTRAINT_NAME JOIN ALL_CONSTRAINTS r ON r.OWNER=f.R_OWNER AND r.CONSTRAINT_NAME=f.R_CONSTRAINT_NAME JOIN ALL_CONS_COLUMNS rk ON rk.OWNER=r.OWNER AND rk.CONSTRAINT_NAME=r.CONSTRAINT_NAME AND rk.POSITION=k.POSITION WHERE f.OWNER=` + a + ` AND f.TABLE_NAME=` + c + ` AND f.CONSTRAINT_TYPE='R' ORDER BY f.CONSTRAINT_NAME,k.POSITION`
		case d == dialect.DB2:
			q = `SELECT f.CONSTNAME,k.COLNAME,f.REFTABSCHEMA,f.REFTABNAME,rk.COLNAME,k.COLSEQ,(SELECT COUNT(*) FROM SYSCAT.KEYCOLUSE z WHERE z.TABSCHEMA=f.TABSCHEMA AND z.TABNAME=f.TABNAME AND z.CONSTNAME=f.CONSTNAME) FROM SYSCAT.REFERENCES f JOIN SYSCAT.KEYCOLUSE k ON k.TABSCHEMA=f.TABSCHEMA AND k.TABNAME=f.TABNAME AND k.CONSTNAME=f.CONSTNAME JOIN SYSCAT.KEYCOLUSE rk ON rk.TABSCHEMA=f.REFTABSCHEMA AND rk.TABNAME=f.REFTABNAME AND rk.CONSTNAME=f.REFKEYNAME AND rk.COLSEQ=k.COLSEQ WHERE f.TABSCHEMA=` + a + ` AND f.TABNAME=` + c + ` ORDER BY f.CONSTNAME,k.COLSEQ`
		}
	}
	if q == "" {
		return catalogQuery{}, nil, false
	}
	// Callers can request the source key size to detect incomplete metadata.
	if kind == "primary_key" && len(keySize) > 0 && keySize[0] {
		count := ""
		switch {
		case d.UsesMySQLSyntax():
			q = strings.Replace(q, "FROM information_schema.KEY_COLUMN_USAGE", "FROM information_schema.KEY_COLUMN_USAGE k", 1)
			count = "(SELECT COUNT(*) FROM information_schema.KEY_COLUMN_USAGE z WHERE z.TABLE_SCHEMA=k.TABLE_SCHEMA AND z.TABLE_NAME=k.TABLE_NAME AND z.CONSTRAINT_NAME=k.CONSTRAINT_NAME)"
		case d == dialect.PostgreSQL:
			count = "cardinality(k.conkey)"
		case d == dialect.SQLServer:
			count = "(SELECT COUNT(*) FROM sys.index_columns z WHERE z.object_id=i.object_id AND z.index_id=i.index_id AND z.key_ordinal>0)"
		case d.UsesOracleSyntax():
			count = "(SELECT COUNT(*) FROM ALL_CONS_COLUMNS z WHERE z.OWNER=c.OWNER AND z.CONSTRAINT_NAME=c.CONSTRAINT_NAME)"
		case d == dialect.DB2:
			count = "(SELECT COUNT(*) FROM SYSCAT.KEYCOLUSE z WHERE z.TABSCHEMA=k.TABSCHEMA AND z.TABNAME=k.TABNAME AND z.CONSTNAME=k.CONSTNAME)"
		}
		if count != "" {
			pos := strings.Index(q, " FROM ")
			q = q[:pos] + "," + count + q[pos:]
			names = append(names, "column_count")
		}
	}

	if d == dialect.PostgreSQL && len(keySize) > 1 && keySize[1] {
		// Editing retains its column-grant boundary. Read-only inspection uses
		// native catalogs independently of data privileges.
		guard := postgresInspectionGuard(kind)
		if guard != "" {
			if pos := strings.LastIndex(q, " ORDER BY "); pos >= 0 {
				q = q[:pos] + " AND (" + guard + ")" + q[pos:]
			}
		}
	} else if d == dialect.PostgreSQL && kind == "foreign_keys" {
		if pos := strings.LastIndex(q, " ORDER BY "); pos >= 0 {
			q = q[:pos] + " AND nr.nspname IN (SELECT schema_name FROM information_schema.schemata)" + q[pos:]
		}
	}
	return catalogQuery{q, b.args}, names, true
}

const postgresColumnPrivileges = "'SELECT,INSERT,UPDATE,REFERENCES'"

func postgresInspectionGuard(kind string) string {
	full := "pg_catalog.has_table_privilege(t.oid, " + postgresColumnPrivileges + ")"
	sourceKeys := "NOT EXISTS (SELECT 1 FROM unnest(k.conkey) scoped(attnum) WHERE NOT pg_catalog.has_column_privilege(t.oid, scoped.attnum, " + postgresColumnPrivileges + "))"
	switch kind {
	case "defaults":
		return "pg_catalog.has_column_privilege(t.oid, a.attnum, " + postgresColumnPrivileges + ") AND (" + full + ` OR NOT EXISTS (
SELECT 1 FROM pg_catalog.pg_depend dep
WHERE dep.classid='pg_catalog.pg_attrdef'::regclass AND dep.objid=ad.oid
AND dep.refclassid='pg_catalog.pg_class'::regclass AND dep.refobjid=t.oid AND dep.refobjsubid>0
AND NOT pg_catalog.has_column_privilege(t.oid, dep.refobjsubid::smallint, ` + postgresColumnPrivileges + ")))"
	case "primary_key":
		return sourceKeys
	case "foreign_keys":
		return sourceKeys + " AND pg_catalog.has_schema_privilege(nr.oid, 'USAGE') AND NOT EXISTS (SELECT 1 FROM unnest(k.confkey) scoped(attnum) WHERE NOT pg_catalog.has_column_privilege(tr.oid, scoped.attnum, " + postgresColumnPrivileges + "))"
	case "indexes":
		// A rendered expression or partial-index predicate cannot safely be
		// inferred from its label. Hide the complete index for a column grant.
		return full + " OR (i.indexprs IS NULL AND i.indpred IS NULL AND NOT EXISTS (SELECT 1 FROM unnest(i.indkey) scoped(attnum) WHERE scoped.attnum<=0 OR NOT pg_catalog.has_column_privilege(t.oid, scoped.attnum, " + postgresColumnPrivileges + ")))"
	}
	return ""
}

func inspectionDDL(ctx context.Context, conn *sql.Conn, d dialect.ID, r CatalogColumnRequest, info TableInspection) MetadataSection {
	section := MetadataSection{Supported: true, Source: "native"}
	q := ""
	var args []any
	switch {
	case d.UsesMySQLSyntax(), d == dialect.ClickHouse:
		quote := byte('`')
		q = "SHOW CREATE TABLE " + quoteIdentifier(r.Schema, d, quote) + "." + quoteIdentifier(r.Table, d, quote)
		rows, err := conn.QueryContext(ctx, q)
		if err != nil {
			section.Error = "DDL is unavailable for this account or object"
			return section
		}
		defer rows.Close()
		names, _ := rows.Columns()
		vals := make([]any, len(names))
		ptrs := make([]any, len(vals))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if rows.Next() && rows.Scan(ptrs...) == nil {
			index := 1
			if d == dialect.ClickHouse {
				index = 0
			}
			if len(vals) > index {
				switch value := vals[index].(type) {
				case string:
					section.Text = value
				case []byte:
					section.Text = string(value)
				}
			}
		}
		if section.Text == "" {
			section.Error = "DDL is unavailable for this object"
		}
		return section
	case d == dialect.Oracle:
		var views int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM ALL_VIEWS WHERE OWNER=:1 AND VIEW_NAME=:2", r.Schema, r.Table).Scan(&views); err != nil {
			section.Error = "DDL object type is unavailable"
			return section
		}
		kind := "TABLE"
		if views > 0 {
			kind = "VIEW"
		}
		q = "SELECT DBMS_METADATA.GET_DDL(:1,:2,:3) FROM DUAL"
		args = []any{kind, r.Table, r.Schema}
	case d == dialect.Dameng:
		q = `SELECT TABLEDEF(?,?) FROM DUAL`
		args = []any{r.Schema, r.Table}
	case d == dialect.PostgreSQL:
		if info.Sections["ddl"].Source == "native_view_query" {
			section.Source = "native_view_query"
			q = `SELECT pg_catalog.pg_get_viewdef(t.oid,true)
FROM pg_catalog.pg_class t JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace
WHERE n.nspname=$1 AND t.relname=$2 AND t.relkind IN ('v','m')`
			args = []any{r.Schema, r.Table}
			break
		}
		// PostgreSQL has no built-in CREATE TABLE serializer. This explicit,
		// conservative reconstruction omits storage/partition/identity details.
		section.Source = "reconstructed_columns"
		lines := []string{}
		defaults := map[string]string{}
		for _, row := range info.Sections["defaults"].Items {
			if val, ok := row["expression"].(string); ok && row["generated"] == "" && row["identity"] == "" {
				defaults[fmt.Sprint(row["column"])] = val
			}
		}
		for _, col := range info.Columns {
			line := quoteIdentifier(col.Name, d, '"') + " " + col.DataType
			if val := defaults[col.Name]; val != "" {
				line += " DEFAULT " + val
			}
			if !col.Nullable {
				line += " NOT NULL"
			}
			lines = append(lines, "  "+line)
		}
		section.Text = "-- Column definition preview; constraints, indexes, identity/generated-column, storage and partition options are not included.\nCREATE TABLE " + quoteIdentifier(r.Schema, d, '"') + "." + quoteIdentifier(r.Table, d, '"') + " (\n" + strings.Join(lines, ",\n") + "\n);"
		return section
	default:
		return MetadataSection{Supported: false}
	}
	rows, err := conn.QueryContext(ctx, q, args...)
	if err != nil {
		section.Error = "DDL is unavailable for this account or object"
		return section
	}
	defer rows.Close()
	for rows.Next() {
		var value any
		if err := rows.Scan(&value); err != nil {
			section.Text = ""
			section.Error = "Unable to read DDL"
			break
		}
		text, err := metadataText(value)
		if err != nil {
			section.Text = ""
			section.Error = "Unable to read complete DDL"
			break
		}
		section.Text += text
	}
	if rows.Err() != nil {
		section.Text = ""
		section.Error = "Unable to read complete DDL"
	}
	if section.Source == "native_view_query" && section.Text != "" {
		section.Text = "-- Native view SELECT definition; this is not a CREATE statement.\n" + section.Text
	}
	return section
}

// Driver LOB locators must be consumed while their connection is still pinned.
func metadataText(value any) (string, error) {
	switch x := value.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case []byte:
		return string(x), nil
	case goora.Clob:
		return x.String, nil
	case goora.NClob:
		return x.String, nil
	case interface {
		GetLength() (int64, error)
		ReadString(int, int) (string, error)
	}:
		n, err := x.GetLength()
		if err != nil {
			return "", err
		}
		if n < 0 || uint64(n) > uint64(^uint(0)>>1) {
			return "", errors.New("Invalid DDL length")
		}
		if n == 0 {
			return "", nil
		}
		text, err := x.ReadString(1, int(n))
		if errors.Is(err, io.EOF) {
			err = nil
		}
		return text, err
	default:
		return "", errors.New("Unsupported DDL value")
	}
}

func ddlSupported(d dialect.ID) bool {
	return d.UsesMySQLSyntax() || d == dialect.ClickHouse || d == dialect.Oracle || d == dialect.Dameng || d == dialect.PostgreSQL
}
func ddlSource(d dialect.ID) string {
	if d == dialect.PostgreSQL {
		return "reconstructed_columns"
	}
	if ddlSupported(d) {
		return "native"
	}
	return ""
}
func TableDefinition(ctx context.Context, conn *sql.Conn, d dialect.ID, req CatalogColumnRequest) (MetadataSection, error) {
	info, err := inspectTable(ctx, conn, d, req, true)
	if err != nil {
		return MetadataSection{}, err
	}
	return info.Sections["ddl"], nil
}
