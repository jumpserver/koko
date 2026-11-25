package sqlservice

import (
	"fmt"
)

// The extra dialects use the same authorized directory as object navigation.
// Constraints are paged before column pairs, so a composite FK cannot straddle
// pages. Target identities are NULL when the target is outside that directory.
func diagramExtraSQL(d Dialect, r DiagramRequest) (catalogQuery, error) {
	b := &catalogBinder{dialect: d}
	if r.Section == "nodes" {
		source, err := extraObjectSource(d, b, r.Database)
		if err != nil {
			return catalogQuery{}, err
		}
		q := "SELECT schemaname,objectname,objecttype FROM (" + source + ") objects WHERE objectkind='table'"
		if r.Direction == "incoming" {
			q += " AND schemaname=" + b.bind(r.Schema) + " AND objectname=" + b.bind(r.Table)
		} else {
			if r.Schema != "" {
				q += " AND schemaname=" + b.bind(r.Schema)
			}
			if r.AfterSchema != "" || r.AfterName != "" {
				q += " AND (" + extraOrder(d, "schemaname") + ">" + extraOrder(d, b.bind(r.AfterSchema)) + " OR (schemaname=" + b.bind(r.AfterSchema) + " AND " + extraOrder(d, "objectname") + ">" + extraOrder(d, b.bind(r.AfterName)) + "))"
			}
		}
		q += " ORDER BY " + extraOrder(d, "schemaname") + "," + extraOrder(d, "objectname")
		return b.orderedPage(q, r.Limit+1), nil
	}
	var source string
	switch {
	case d == SQLServer:
		source = `SELECT s.name AS source_schema,t.name AS source_table,f.name AS constraint_name,rs.name AS target_schema,rt.name AS target_table,
 CASE WHEN rt.object_id IS NOT NULL THEN 1 ELSE 0 END AS target_visible,k.constraint_column_id AS pair_ordinal,
 (SELECT COUNT(*) FROM sys.foreign_key_columns z WHERE z.constraint_object_id=f.object_id) AS pair_count,sc.name AS source_column,rc.name AS target_column
 FROM sys.foreign_keys f JOIN sys.tables t ON t.object_id=f.parent_object_id JOIN sys.schemas s ON s.schema_id=t.schema_id
 JOIN sys.foreign_key_columns k ON k.constraint_object_id=f.object_id
 LEFT JOIN sys.columns sc ON sc.object_id=t.object_id AND sc.column_id=k.parent_column_id
 LEFT JOIN sys.tables rt ON rt.object_id=f.referenced_object_id LEFT JOIN sys.schemas rs ON rs.schema_id=rt.schema_id
 LEFT JOIN sys.columns rc ON rc.object_id=rt.object_id AND rc.column_id=k.referenced_column_id`
	case IsOracleDialect(d):
		source = `SELECT f.OWNER AS source_schema,f.TABLE_NAME AS source_table,f.CONSTRAINT_NAME AS constraint_name,
 rt.OWNER AS target_schema,rt.TABLE_NAME AS target_table,CASE WHEN rt.OWNER IS NOT NULL THEN 1 ELSE 0 END AS target_visible,
 k.POSITION AS pair_ordinal,(SELECT COUNT(*) FROM ALL_CONS_COLUMNS z WHERE z.OWNER=f.OWNER AND z.TABLE_NAME=f.TABLE_NAME AND z.CONSTRAINT_NAME=f.CONSTRAINT_NAME) AS pair_count,
 k.COLUMN_NAME AS source_column,CASE WHEN rt.OWNER IS NOT NULL THEN rk.COLUMN_NAME END AS target_column
 FROM ALL_CONSTRAINTS f JOIN ALL_TABLES t ON t.OWNER=f.OWNER AND t.TABLE_NAME=f.TABLE_NAME
 LEFT JOIN ALL_CONS_COLUMNS k ON k.OWNER=f.OWNER AND k.TABLE_NAME=f.TABLE_NAME AND k.CONSTRAINT_NAME=f.CONSTRAINT_NAME
 LEFT JOIN ALL_CONSTRAINTS rf ON rf.OWNER=f.R_OWNER AND rf.CONSTRAINT_NAME=f.R_CONSTRAINT_NAME
 LEFT JOIN ALL_TABLES rt ON rt.OWNER=rf.OWNER AND rt.TABLE_NAME=rf.TABLE_NAME
 LEFT JOIN ALL_CONS_COLUMNS rk ON rk.OWNER=rf.OWNER AND rk.TABLE_NAME=rf.TABLE_NAME AND rk.CONSTRAINT_NAME=rf.CONSTRAINT_NAME AND rk.POSITION=k.POSITION
 WHERE f.CONSTRAINT_TYPE='R'`
	case d == DB2:
		targetVisible := "rt.TABNAME IS NOT NULL"
		source = `SELECT RTRIM(f.TABSCHEMA) AS source_schema,RTRIM(f.TABNAME) AS source_table,RTRIM(f.CONSTNAME) AS constraint_name,
 CASE WHEN ` + targetVisible + ` THEN RTRIM(rt.TABSCHEMA) END AS target_schema,CASE WHEN ` + targetVisible + ` THEN RTRIM(rt.TABNAME) END AS target_table,
 CASE WHEN ` + targetVisible + ` THEN 1 ELSE 0 END AS target_visible,k.COLSEQ AS pair_ordinal,f.COLCOUNT AS pair_count,
 RTRIM(k.COLNAME) AS source_column,CASE WHEN ` + targetVisible + ` THEN RTRIM(rk.COLNAME) END AS target_column
 FROM SYSCAT.REFERENCES f JOIN SYSCAT.TABLES t ON t.TABSCHEMA=f.TABSCHEMA AND t.TABNAME=f.TABNAME
 LEFT JOIN SYSCAT.KEYCOLUSE k ON k.TABSCHEMA=f.TABSCHEMA AND k.TABNAME=f.TABNAME AND k.CONSTNAME=f.CONSTNAME
 LEFT JOIN SYSCAT.TABLES rt ON rt.TABSCHEMA=f.REFTABSCHEMA AND rt.TABNAME=f.REFTABNAME
 LEFT JOIN SYSCAT.KEYCOLUSE rk ON rk.TABSCHEMA=f.REFTABSCHEMA AND rk.TABNAME=f.REFTABNAME AND rk.CONSTNAME=f.REFKEYNAME AND rk.COLSEQ=k.COLSEQ`
	default:
		return catalogQuery{}, ErrUnsupportedDialect
	}
	where := "1=1"
	if r.Direction == "incoming" {
		where += " AND target_schema=" + b.bind(r.Schema) + " AND target_table=" + b.bind(r.Table)
	} else if r.Schema != "" {
		where += " AND source_schema=" + b.bind(r.Schema)
	}
	// Empty strings are NULL in Oracle, so omit a nonexistent initial key.
	if r.AfterSchema != "" || r.AfterTable != "" || r.AfterName != "" {
		where += " AND (" + extraOrder(d, "source_schema") + ">" + extraOrder(d, b.bind(r.AfterSchema)) + " OR (source_schema=" + b.bind(r.AfterSchema) + " AND (" + extraOrder(d, "source_table") + ">" + extraOrder(d, b.bind(r.AfterTable)) + " OR (source_table=" + b.bind(r.AfterTable) + " AND " + extraOrder(d, "constraint_name") + ">" + extraOrder(d, b.bind(r.AfterName)) + "))))"
	}
	order := extraOrder(d, "source_schema") + "," + extraOrder(d, "source_table") + "," + extraOrder(d, "constraint_name")
	q := `SELECT source_schema,source_table,constraint_name,target_schema,target_table,target_visible,pair_ordinal,pair_count,source_column,target_column FROM (SELECT k.*,DENSE_RANK() OVER (ORDER BY ` + order + `) AS page_ordinal FROM (` + source + `) k WHERE ` + where + `) page WHERE page_ordinal<=` + fmt.Sprint(r.Limit+1) + ` AND (pair_ordinal<=64 OR pair_ordinal IS NULL) ORDER BY ` + order + `,pair_ordinal`
	return catalogQuery{q, b.args}, nil
}
