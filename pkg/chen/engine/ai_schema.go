package engine

import (
	"errors"
	"sort"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

func aiSchemaResult(req *catalogRequest, e aiEditorContext, tool, query string, tables []string, limit int, objects []sqlservice.CatalogObject) (any, error) {
	d := e.Dialect
	if tool == "search_schema" {
		if limit == 0 {
			limit = 8
		}
		terms := strings.Fields(strings.ToLower(query))
		if len(terms) > 16 {
			terms = terms[:16]
		}
		type candidate struct {
			Database string   `json:"database"`
			Schema   string   `json:"schema"`
			Name     string   `json:"name"`
			Kind     string   `json:"kind"`
			Score    int      `json:"score"`
			Matches  []string `json:"matches"`
		}
		out := []candidate{}
		objectsTruncated := len(objects) > 1000
		if objectsTruncated {
			objects = objects[:1000]
		}
		const columnObjectLimit, columnsPerObjectLimit = 16, 96
		var columnsByTable map[string][]sqlservice.CatalogColumn
		if pinnedMySQLCatalog(e.Dialect) {
			var err error
			columnsByTable, err = sqlservice.MySQLSearchColumns(req.ctx, req.conn, req.database, objects[:min(len(objects), columnObjectLimit)], columnsPerObjectLimit)
			if err != nil {
				return nil, errors.New("Unable to search authorized columns")
			}
		}
		columnObjects, columnsScanned := 0, 0
		columnsTruncated := false
		for _, o := range objects {
			score := 0
			matches := []string{}
			for _, term := range terms {
				if strings.Contains(strings.ToLower(o.Name), term) {
					score += 10
					if strings.EqualFold(o.Name, term) {
						score += 100
					}
					matches = append(matches, "table_name")
				}
				if strings.Contains(strings.ToLower(o.Comment), term) {
					score += 4
					matches = append(matches, "table_comment")
				}
			}
			// Score every visible name/comment even after the bounded column budget.
			// Metadata fan-out remains bounded independently of name search.
			var columns []sqlservice.CatalogColumn
			if columnObjects < columnObjectLimit {
				columnObjects++
				if columnsByTable != nil {
					columns = columnsByTable[o.Name]
				} else {
					var err error
					columns, err = sqlservice.ColumnRows(req.ctx, req.conn, d, sqlservice.CatalogColumnRequest{Database: req.database, Schema: o.Schema, Table: o.Name, Limit: columnsPerObjectLimit})
					if err != nil {
						return nil, errors.New("Unable to search authorized columns")
					}
				}
				if len(columns) > columnsPerObjectLimit {
					columnsTruncated = true
					columns = columns[:columnsPerObjectLimit]
				}
				columnsScanned += len(columns)
			} else {
				columnsTruncated = true
			}
			for _, c := range columns {
				for _, term := range terms {
					if strings.Contains(strings.ToLower(c.Name), term) {
						score += 2
						matches = append(matches, "column_name")
					}
					if strings.Contains(strings.ToLower(c.Comment), term) {
						score++
						matches = append(matches, "column_comment")
					}
				}
			}
			if score > 0 {
				seen := map[string]bool{}
				unique := []string{}
				for _, match := range matches {
					if !seen[match] {
						seen[match] = true
						unique = append(unique, match)
					}
				}
				out = append(out, candidate{o.Database, o.Schema, o.Name, o.Kind, score, unique})
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
		truncated := objectsTruncated || columnsTruncated || len(out) > limit
		if len(out) > limit {
			out = out[:limit]
		}
		return map[string]any{"kind": "schema_search", "tables": out, "truncated": truncated, "objects_scanned": len(objects), "column_objects_scanned": columnObjects, "columns_scanned": columnsScanned, "object_limit": 1000, "column_object_limit": columnObjectLimit, "columns_per_object_limit": columnsPerObjectLimit, "incomplete_columns": columnsTruncated, "incomplete_objects": objectsTruncated, "not_found_is_conclusive": false, "coverage": "authorized_catalog_only", "snapshot": false, "scope": map[string]string{"database": req.database, "schema": e.Schema}, "business_rows_read": false}, nil
	}
	wanted := map[string]bool{}
	for _, table := range tables {
		wanted[strings.ToLower(table)] = true
	}
	chosen := []sqlservice.CatalogObject{}
	for _, o := range objects {
		if wanted[strings.ToLower(o.Name)] || query != "" && (query == "*" || strings.Contains(strings.ToLower(o.Name), strings.ToLower(query))) {
			chosen = append(chosen, o)
			delete(wanted, strings.ToLower(o.Name))
		}
	}
	for _, table := range tables {
		if !wanted[strings.ToLower(table)] {
			continue
		}
		matches, err := sqlservice.ObjectRows(req.ctx, req.conn, d, sqlservice.CatalogObjectRequest{Database: req.database, Schema: e.Schema, Query: table, Limit: 100})
		if err != nil {
			return nil, errors.New("Unable to inspect authorized schema")
		}
		for _, o := range matches {
			if strings.EqualFold(o.Name, table) {
				chosen = append(chosen, o)
				delete(wanted, strings.ToLower(table))
				break
			}
		}
	}
	missing := []string{}
	for _, table := range tables {
		if wanted[strings.ToLower(table)] {
			missing = append(missing, table)
		}
	}
	// Explicitly requested relations take priority over name-search additions.
	explicit := map[string]bool{}
	for _, table := range tables {
		explicit[strings.ToLower(table)] = true
	}
	sort.SliceStable(chosen, func(i, j int) bool {
		return explicit[strings.ToLower(chosen[i].Name)] && !explicit[strings.ToLower(chosen[j].Name)]
	})
	maximum := 8
	discovery := query == "*"
	if discovery {
		maximum = 100
	}
	truncated := len(chosen) > maximum || len(objects) > 100
	if len(chosen) > maximum {
		chosen = chosen[:maximum]
	}
	if discovery {
		return map[string]any{"kind": "schema_inspection", "tables": chosen, "discovery": true, "truncated": truncated, "scope": map[string]string{"database": req.database, "schema": e.Schema}, "business_rows_read": false}, nil
	}
	inspections := []sqlservice.TableInspection{}
	for _, o := range chosen {
		item, err := sqlservice.InspectTableSummary(req.ctx, req.conn, d, sqlservice.CatalogColumnRequest{Database: req.database, Schema: o.Schema, Table: o.Name})
		if err != nil {
			return nil, errors.New("Unable to inspect authorized table metadata")
		}
		delete(item.Sections, "ddl")
		if len(item.Columns) > 96 {
			item.Columns = item.Columns[:96]
			item.Truncated = true
		}
		for name, section := range item.Sections {
			if len(section.Items) > 48 {
				section.Items = section.Items[:48]
				section.Truncated = true
				item.Sections[name] = section
			}
		}
		inspections = append(inspections, item)
	}
	return map[string]any{"kind": "schema_inspection", "tables": inspections, "missingTables": missing, "truncated": truncated, "scope": map[string]string{"database": req.database, "schema": e.Schema}, "business_rows_read": false}, nil
}

func aiTableName(e aiEditorContext, input string) (string, error) {
	var parts []string
	var part strings.Builder
	var quote rune
	chars := []rune(strings.TrimSpace(input))
	appendPart := func() bool {
		name := strings.TrimSpace(part.String())
		if name == "" {
			return false
		}
		parts = append(parts, name)
		part.Reset()
		return true
	}
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		if quote != 0 {
			if c == quote {
				if i+1 < len(chars) && chars[i+1] == quote {
					part.WriteRune(c)
					i++
				} else {
					quote = 0
				}
			} else {
				part.WriteRune(c)
			}
		} else if c == '"' || c == '`' || c == '[' {
			quote = c
			if c == '[' {
				quote = ']'
			}
		} else if c == '.' {
			if !appendPart() {
				return "", errors.New("Invalid table identifier")
			}
		} else {
			part.WriteRune(c)
		}
	}
	if quote != 0 || !appendPart() || len(parts) > 3 {
		return "", errors.New("Invalid table identifier")
	}
	if len(parts) >= 2 {
		qualifier := parts[len(parts)-2]
		if !strings.EqualFold(qualifier, e.Schema) && !strings.EqualFold(qualifier, e.Database) {
			return "", errors.New("Table inspection must stay in the active schema")
		}
	}
	if len(parts) == 3 && !strings.EqualFold(parts[0], e.Database) {
		return "", errors.New("Table inspection must stay in the active database")
	}
	return parts[len(parts)-1], nil
}

// Keep the Java assistant's metadata scope rules. These govern AI disclosure,
// not which system databases the ordinary resource tree may display.
func validateAISchemaScope(e aiEditorContext) error {
	normalize := func(value string) string {
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '`' && value[len(value)-1] == '`' || value[0] == '[' && value[len(value)-1] == ']') {
			value = value[1 : len(value)-1]
		}
		return strings.ToLower(value)
	}
	database, schema := normalize(e.Database), normalize(e.Schema)
	d := e.Dialect
	if schema == "" && !d.UsesMySQLSyntax() {
		return errors.New("Schema inspection requires an active schema")
	}
	if database == "" && (d.UsesMySQLSyntax() || d == dialect.SQLServer || d == dialect.ClickHouse) {
		return errors.New("Schema inspection requires an active database")
	}
	blocked := database == "information_schema" || schema == "information_schema"
	contains := func(values string, value string) bool { return strings.Contains("|"+values+"|", "|"+value+"|") }
	switch {
	case d.UsesMySQLSyntax():
		blocked = blocked || contains("mysql|performance_schema|sys", database)
	case d == dialect.PostgreSQL:
		blocked = blocked || contains("pg_catalog|pg_toast", schema) || strings.HasPrefix(schema, "pg_temp_") || strings.HasPrefix(schema, "pg_toast_temp_")
	case d == dialect.SQLServer:
		blocked = blocked || contains("master|model|msdb|tempdb", database) || schema == "sys"
	case d == dialect.Dameng:
		blocked = blocked || contains("sys|system|sysauditor", schema)
	case d.UsesOracleSyntax():
		blocked = blocked || contains("sys|system|xdb|mdsys|ctxsys|audsys", schema)
	case d == dialect.DB2:
		blocked = blocked || contains("sysibm|syscat|sysstat|sysfun|sysproc|systools", schema)
	case d == dialect.ClickHouse:
		blocked = blocked || database == "system" || schema == "system"
	}
	if blocked {
		return errors.New("AI table metadata is unavailable in the protected system scope")
	}
	return nil
}
