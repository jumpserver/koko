package db2

import (
	"database/sql"
	"strings"

	"github.com/xo/usql/drivers"
	"github.com/xo/usql/drivers/metadata"
)

// Db2 LUW exposes SYSCAT rather than information_schema. Implement the basic
// readers used by \dn, \dt, \dv and \d; unsupported metadata stays unsupported.
type metadataReader struct{ metadata.LoggingReader }

func newMetadataReader(db drivers.DB, opts ...metadata.ReaderOption) metadata.Reader {
	return &metadataReader{metadata.NewLoggingReader(db, opts...)}
}

func pattern(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
	}
	return strings.ToUpper(s)
}

func conditions(f metadata.Filter, schema, parent, name string) (string, []interface{}) {
	q := " WHERE 1=1"
	var args []interface{}
	for _, part := range []struct{ column, value string }{
		{"CURRENT SERVER", f.Catalog}, {schema, f.Schema}, {parent, f.Parent}, {name, f.Name},
	} {
		if part.column != "" && part.value != "" {
			// Db2 catalog identifiers are fixed-width CHAR columns; LIKE does
			// not ignore their padding, unlike an equality comparison.
			q += " AND RTRIM(" + part.column + ") LIKE ?"
			args = append(args, pattern(part.value))
		}
	}
	if !f.WithSystem {
		q += " AND " + schema + " NOT LIKE 'SYS%' AND " + schema + " NOT IN ('SQLJ', 'NULLID')"
	}
	if f.OnlyVisible {
		q += " AND " + schema + " = CURRENT SCHEMA"
	}
	return q, args
}

func (r *metadataReader) Schemas(f metadata.Filter) (*metadata.SchemaSet, error) {
	where, args := conditions(f, "SCHEMANAME", "", "SCHEMANAME")
	rows, closeRows, err := r.Query("SELECT SCHEMANAME, CURRENT SERVER FROM SYSCAT.SCHEMATA"+where+" ORDER BY SCHEMANAME", args...)
	if err == sql.ErrNoRows {
		return metadata.NewSchemaSet(nil), nil
	}
	if err != nil {
		return nil, err
	}
	defer closeRows()
	var result []metadata.Schema
	for rows.Next() {
		var s metadata.Schema
		if err := rows.Scan(&s.Schema, &s.Catalog); err != nil {
			return nil, err
		}
		s.Schema, s.Catalog = strings.TrimRight(s.Schema, " "), strings.TrimRight(s.Catalog, " ")
		result = append(result, s)
	}
	return metadata.NewSchemaSet(result), rows.Err()
}

func (r *metadataReader) Tables(f metadata.Filter) (*metadata.TableSet, error) {
	where, args := conditions(f, "TABSCHEMA", "", "TABNAME")
	rows, closeRows, err := r.Query(`SELECT CURRENT SERVER, TABSCHEMA, TABNAME,
CASE TYPE WHEN 'V' THEN 'VIEW' WHEN 'W' THEN 'VIEW' WHEN 'S' THEN 'MATERIALIZED VIEW'
WHEN 'A' THEN 'SYNONYM' ELSE 'TABLE' END,
COALESCE(REMARKS, '') FROM SYSCAT.TABLES`+where+" ORDER BY TABSCHEMA, TABNAME", args...)
	if err == sql.ErrNoRows {
		return metadata.NewTableSet(nil), nil
	}
	if err != nil {
		return nil, err
	}
	defer closeRows()
	var result []metadata.Table
	for rows.Next() {
		var table metadata.Table
		if err := rows.Scan(&table.Catalog, &table.Schema, &table.Name, &table.Type, &table.Comment); err != nil {
			return nil, err
		}
		table.Catalog = strings.TrimRight(table.Catalog, " ")
		table.Schema = strings.TrimRight(table.Schema, " ")
		table.Name = strings.TrimRight(table.Name, " ")
		table.Type = strings.TrimRight(table.Type, " ")
		match := len(f.Types) == 0
		for _, typ := range f.Types {
			match = match || typ == table.Type
		}
		if match {
			result = append(result, table)
		}
	}
	return metadata.NewTableSet(result), rows.Err()
}

func (r *metadataReader) Columns(f metadata.Filter) (*metadata.ColumnSet, error) {
	where, args := conditions(f, "TABSCHEMA", "TABNAME", "COLNAME")
	rows, closeRows, err := r.Query(`SELECT CURRENT SERVER, TABSCHEMA, TABNAME, COLNAME, COLNO + 1,
TYPENAME, LENGTH, SCALE, NULLS, COALESCE(CAST(DEFAULT AS VARCHAR(2048)), '')
FROM SYSCAT.COLUMNS`+where+" ORDER BY TABSCHEMA, TABNAME, COLNO", args...)
	if err == sql.ErrNoRows {
		return metadata.NewColumnSet(nil), nil
	}
	if err != nil {
		return nil, err
	}
	defer closeRows()
	var result []metadata.Column
	for rows.Next() {
		var col metadata.Column
		var nullable string
		if err := rows.Scan(&col.Catalog, &col.Schema, &col.Table, &col.Name, &col.OrdinalPosition,
			&col.DataType, &col.ColumnSize, &col.DecimalDigits, &nullable, &col.Default); err != nil {
			return nil, err
		}
		col.Catalog = strings.TrimRight(col.Catalog, " ")
		col.Schema = strings.TrimRight(col.Schema, " ")
		col.Table = strings.TrimRight(col.Table, " ")
		col.Name = strings.TrimRight(col.Name, " ")
		col.DataType = strings.TrimRight(col.DataType, " ")
		col.IsNullable = metadata.NO
		if nullable == "Y" {
			col.IsNullable = metadata.YES
		}
		result = append(result, col)
	}
	return metadata.NewColumnSet(result), rows.Err()
}
