package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

// Only cache positions cross the trust boundary. Table names, source columns,
// keys and values all come from the authenticated result and live metadata.
type copySQLRequest struct {
	Operation string `json:"operation"`
	Rows      []int  `json:"rows"`
	Columns   []int  `json:"columns"`
}
type copiedSQL struct {
	SQL      string   `json:"sql"`
	RowCount int      `json:"row_count"`
	Notes    []string `json:"notes"`
}

func (v *Server) copyResultSQL(w http.ResponseWriter, req *http.Request) {
	s, c, ctx, done := v.editConsole(w, req)
	if s == nil {
		return
	}
	defer done()
	if !s.grant.Permissions.Copy {
		fail(w, 403, "copy_permission_denied", "Copying is not permitted")
		return
	}
	var input copySQLRequest
	if !decode(w, req, &input) {
		return
	}
	if err := validateCopySelection(input); err != nil {
		fail(w, 400, "invalid_copy_selection", err.Error())
		return
	}
	d := s.grant.Dialect()
	switch d {
	case dialect.PostgreSQL, dialect.MySQL, dialect.MariaDB, dialect.OceanBaseMySQL, dialect.SQLServer, dialect.Oracle, dialect.Dameng, dialect.OceanBaseOracle, dialect.DB2, dialect.ClickHouse:
	default:
		fail(w, 409, "copy_sql_unavailable", "SQL copying is not supported for dialect "+string(d)+"; a separate literal codec is required")
		return
	}
	if v.auditCatalog(ctx, s, c, "copy-"+input.Operation+"-sql") != nil {
		fail(w, 503, "audit_persistence_failed", "SQL copying could not be audited")
		return
	}
	result, err := v.editResult(s, c, req.PathValue("rid"))
	if err != nil {
		fail(w, 409, "copy_sql_unavailable", err.Error())
		return
	}
	x, err := v.resolveResultSource(ctx, s, c, result)
	if err != nil {
		fail(w, 409, "copy_sql_unavailable", err.Error())
		return
	}
	if input.Operation == "insert" {
		if d == dialect.PostgreSQL {
			// Column grants can hide required columns from the visible catalog.
			// Count physical user columns without returning hidden names/values.
			var count int
			err = c.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class t ON t.oid=a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid=t.relnamespace WHERE n.nspname=$1 AND t.relname=$2 AND a.attnum>0 AND NOT a.attisdropped`, x.table.Schema, x.table.Table).Scan(&count)
			if err != nil || count != len(x.table.Columns) {
				fail(w, 409, "copy_sql_unavailable", "INSERT SQL requires complete table column metadata; some columns are hidden or unavailable")
				return
			}
		}
		if (d.UsesMySQLSyntax() || d == dialect.Dameng || d == dialect.OceanBaseOracle) && result.browse == nil && (result.editSource == nil || !result.editSource.Star) {
			if err := v.confirmCopyInsertColumns(ctx, s, c, x.table); err != nil {
				fail(w, 409, "copy_sql_unavailable", err.Error())
				return
			}
		}
	}
	// Recheck both historical and current policy. A permission to copy visible
	// cells never grants access to a masked original, including a masked key.
	checked := map[int]bool{}
	check := func(index, row int) error {
		if index < 0 || index >= len(x.columns) || row < 0 || row >= len(result.Rows) || len(result.original[row]) != len(x.columns) || row >= len(result.masked) || len(result.masked[row]) != len(x.columns) {
			return errors.New("The selected cached cells are unavailable; refresh first")
		}
		if d == dialect.OceanBaseOracle && copyTypeName(x.columns[index].DataType) == "raw" && result.original[row][index] != nil && !binaryResultType(result.Columns[index].DataType) {
			return fmt.Errorf("Column %s lacks a reliable binary wire type for OB Oracle RAW copying", result.Columns[index].Name)
		}
		if row < len(result.copyLossy) && index < len(result.copyLossy[row]) && result.copyLossy[row][index] {
			return fmt.Errorf("Column %s contains a cached value with encoding or temporal information loss", result.Columns[index].Name)
		}
		_, masked := v.provider.Mask(s.grant, x.columns[index].Name, result.Columns[index].Name, result.original[row][index])
		if masked || result.masked[row][index] {
			return fmt.Errorf("Column %s is masked and cannot be copied as writable SQL", result.Columns[index].Name)
		}
		return nil
	}
	for _, index := range input.Columns {
		if index < len(x.columns) && (x.columns[index].Generated || x.columns[index].Identity || input.Operation == "update" && x.columns[index].PrimaryKey) {
			continue
		}
		checked[index] = true
	}
	if input.Operation == "update" {
		for _, name := range x.table.PrimaryKey {
			for i, col := range x.columns {
				if col.Name == name {
					checked[i] = true
				}
			}
		}
	}
	for _, row := range input.Rows {
		for index := range checked {
			if err = check(index, row); err != nil {
				fail(w, 409, "copy_sql_unavailable", err.Error())
				return
			}
		}
	}
	output, err := buildCopiedSQL(d, result, x, input)
	if err != nil {
		fail(w, 409, "copy_sql_unavailable", err.Error())
		return
	}
	if d == dialect.Dameng || d == dialect.OceanBaseOracle {
		if err := v.confirmCopyCharacterLiterals(ctx, s, c, result, x, input, checked); err != nil {
			fail(w, 409, "copy_sql_unavailable", err.Error())
			return
		}
	}
	if ctx.Err() != nil {
		fail(w, 409, "copy_sql_unavailable", "SQL copying timed out; select fewer cells")
		return
	}
	if s.grant.Check() != nil {
		fail(w, 409, "session_unavailable", "Session is unavailable")
		return
	}
	writeJSON(w, 200, output)
}

// MySQL column grants can hide catalog columns. Confirm complete columns for
// direct MySQL/DM/OB Oracle projections using a zero-row result on the pinned console.
// This is verification, not a fallback
// source/metadata discovery path, and never supplies values to the generated SQL.
func (v *Server) confirmCopyInsertColumns(ctx context.Context, s *session, c *console, table sqlservice.EditTable) error {
	query := "SELECT * FROM " + editQualifiedTable(table, s.grant.Dialect()) + " WHERE 1=0"
	if err := v.provider.Authorize(ctx, s.grant, query); err != nil {
		return errors.New("Complete INSERT column metadata cannot be confirmed because the metadata query is not authorized")
	}
	rows, err := c.conn.QueryContext(ctx, query)
	if err != nil {
		return errors.New("Complete INSERT column metadata is unavailable on this connection")
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil || len(columns) != len(table.Columns) {
		return errors.New("Table column metadata changed or is incomplete; refresh before copying INSERT SQL")
	}
	for i, name := range columns {
		if name != table.Columns[i].Name && !(s.grant.Dialect().UsesMySQLSyntax() && strings.EqualFold(name, table.Columns[i].Name)) {
			return errors.New("Table column metadata changed or is incomplete; refresh before copying INSERT SQL")
		}
	}
	if rows.Next() {
		return errors.New("The INSERT metadata query unexpectedly returned rows")
	}
	if rows.Err() != nil {
		return errors.New("Complete INSERT column metadata is unavailable on this connection")
	}
	return nil
}

// Probe only documented, synthetic constants on this console. No user value is
// sent back to the database. Empty-string and Unicode behavior are confirmed
// for DM and OB Oracle independently of the native Oracle codec.
func (v *Server) confirmCopyCharacterLiterals(ctx context.Context, s *session, c *console, r *Result, x *editResolved, req copySQLRequest, checked map[int]bool) error {
	d, product := s.grant.Dialect(), "DM"
	if d == dialect.OceanBaseOracle {
		product = "OB Oracle"
	}
	needed, empty, supplementary := false, false, false
	for _, row := range req.Rows {
		for index := range checked {
			switch copyTypeName(x.columns[index].DataType) {
			case "char", "varchar", "varchar2":
			case "nchar", "nvarchar2":
				if d != dialect.OceanBaseOracle {
					continue
				}
			default:
				continue
			}
			if r.original[row][index] == nil {
				continue
			}
			needed = true
			text, isText := r.original[row][index].(string)
			empty = empty || isText && text == ""
			for _, ch := range text {
				supplementary = supplementary || ch > 0xffff
			}
		}
	}
	if !needed {
		return nil
	}
	sample := "q'\\"
	if supplementary {
		sample += "😀"
	}
	literal, err := copyTextLiteral(d, sample)
	if err != nil {
		return err
	}
	query := "SELECT " + literal + ", CASE WHEN '' IS NULL THEN 1 ELSE 0 END FROM DUAL"
	if err := v.provider.Authorize(ctx, s.grant, query); err != nil {
		return fmt.Errorf("%s literal verification is not authorized on this connection", product)
	}
	var actual string
	var emptyNull int
	if err := c.conn.QueryRowContext(ctx, query).Scan(&actual, &emptyNull); err != nil {
		return fmt.Errorf("%s literal behavior cannot be verified on this connection", product)
	}
	if actual != sample || emptyNull < 0 || emptyNull > 1 {
		return fmt.Errorf("%s Unicode literal conversion is not lossless on this connection", product)
	}
	if empty && emptyNull != 0 {
		return fmt.Errorf("This %s compatibility mode converts empty strings to NULL; the cached empty string cannot be copied losslessly", product)
	}
	return nil
}

func validateCopySelection(req copySQLRequest) error {
	if req.Operation != "insert" && req.Operation != "update" {
		return errors.New("Choose INSERT or UPDATE SQL")
	}
	if len(req.Rows) == 0 || len(req.Rows) > 500 || len(req.Columns) == 0 || len(req.Columns) > sqlservice.MaxCatalogPageSize {
		return errors.New("Select 1 to 500 cached rows and at least one column")
	}
	for _, values := range [][]int{req.Rows, req.Columns} {
		seen := map[int]bool{}
		for _, index := range values {
			if index < 0 || seen[index] {
				return errors.New("Selection positions must be nonnegative and unique")
			}
			seen[index] = true
		}
	}
	return nil
}
func buildCopiedSQL(d dialect.ID, r *Result, x *editResolved, req copySQLRequest) (copiedSQL, error) {
	out := copiedSQL{RowCount: len(req.Rows), Notes: []string{}}
	if err := validateCopySelection(req); err != nil {
		return out, err
	}
	indexes := []int{}
	present := map[string]bool{}
	for _, index := range req.Columns {
		if index >= len(x.columns) {
			return out, errors.New("A selected column is unavailable")
		}
		field := x.columns[index]
		if field.Generated || field.Identity {
			out.Notes = append(out.Notes, fmt.Sprintf("Generated/identity column %s is omitted", field.Name))
			continue
		}
		if req.Operation == "update" && field.PrimaryKey {
			out.Notes = append(out.Notes, fmt.Sprintf("Primary key %s is used only in WHERE", field.Name))
			continue
		}
		if present[field.Name] {
			return out, fmt.Errorf("Select only one result column for source column %s", field.Name)
		}
		indexes = append(indexes, index)
		present[field.Name] = true
	}
	if len(indexes) == 0 {
		return out, errors.New("Select at least one writable, non-generated column")
	}
	keys := []int{}
	if req.Operation == "insert" {
		// Omitted defaults are evaluated by the target database, never guessed from
		// a cached value or copied as the text of a default expression.
		for _, field := range x.table.Columns {
			if present[field.Name] || field.Generated || field.Identity {
				continue
			}
			if !field.Nullable && !field.HasDefault {
				return out, fmt.Errorf("Select required column %s to generate INSERT SQL", field.Name)
			}
			if field.HasDefault {
				out.Notes = append(out.Notes, fmt.Sprintf("Omitted column %s uses its database default", field.Name))
			}
		}
	} else {
		if len(x.table.PrimaryKey) == 0 {
			return out, errors.New("UPDATE SQL requires a complete primary key")
		}
		for _, name := range x.table.PrimaryKey {
			found := -1
			for i, field := range x.columns {
				if field.Name == name {
					if found >= 0 {
						return out, fmt.Errorf("UPDATE SQL requires primary-key column %s exactly once in the result", name)
					}
					found = i
				}
			}
			if found < 0 {
				return out, fmt.Errorf("UPDATE SQL requires primary-key column %s in the result", name)
			}
			keys = append(keys, found)
		}
	}
	target := editQualifiedTable(x.table, d)
	var sql strings.Builder
	for _, row := range req.Rows {
		if row >= len(r.original) || len(r.original[row]) != len(x.columns) {
			return out, errors.New("A selected cached row is unavailable")
		}
		values := []string{}
		names := []string{}
		assignments := []string{}
		literal := func(index int) (string, error) {
			text, err := copySQLLiteral(d, x.columns[index].DataType, r.original[row][index])
			if err != nil {
				return "", fmt.Errorf("Row %d, column %s: %w", row+1, x.columns[index].Name, err)
			}
			return text, nil
		}
		for _, index := range indexes {
			if r.original[row][index] == nil && !x.columns[index].Nullable {
				return out, fmt.Errorf("Column %s no longer permits NULL; refresh the result", x.columns[index].Name)
			}
			value, err := literal(index)
			if err != nil {
				return out, err
			}
			name := quoteDatabaseIdentifier(x.columns[index].Name, d)
			names = append(names, name)
			values = append(values, value)
			assignments = append(assignments, name+" = "+value)
		}
		line := ""
		if req.Operation == "insert" {
			line = "INSERT INTO " + target + " (" + strings.Join(names, ", ") + ") VALUES (" + strings.Join(values, ", ") + ");\n"
		} else {
			where := []string{}
			for _, index := range keys {
				if r.original[row][index] == nil {
					return out, errors.New("A primary key value is NULL or unavailable")
				}
				value, err := literal(index)
				if err != nil {
					return out, err
				}
				where = append(where, quoteDatabaseIdentifier(x.columns[index].Name, d)+" = "+value)
			}
			// Never emit an UPDATE unless every key was found and serialized.
			if len(where) == 0 || len(where) != len(x.table.PrimaryKey) {
				return out, errors.New("UPDATE SQL requires a complete primary key")
			}
			line = "UPDATE " + target + " SET " + strings.Join(assignments, ", ") + " WHERE " + strings.Join(where, " AND ") + ";\n"
		}
		if sql.Len()+len(line) > sqlservice.MaxDocumentBytes {
			return out, errors.New("Copied SQL exceeds the SQL document limit; select fewer cells")
		}
		sql.WriteString(line)
	}
	if sqlservice.DocumentExceedsLimit(sql.String()) {
		return out, errors.New("Copied SQL exceeds the SQL document limit; select fewer cells")
	}
	out.SQL = strings.TrimSuffix(sql.String(), "\n")
	return out, nil
}
