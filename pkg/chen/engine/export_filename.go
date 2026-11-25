package engine

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

// Use the result's frozen source, never the console's current selection. Cached
// results and source-bound exports may be downloaded after switching databases.
func resultExportPrefix(d dialect.ID, result *Result) string {
	return exportSourcePrefix(d, result.browse, result.editSource, result.querySource)
}

func executionExportPrefix(s *session, e *execution) string {
	d := s.grant.Dialect()
	if e.queryPage != nil {
		s.mu.Lock()
		result := s.results[e.queryPage.sourceResultID]
		s.mu.Unlock()
		if result != nil {
			return resultExportPrefix(d, result)
		}
		return exportSourcePrefix(d, e.browse, nil, e.queryPage)
	}
	return exportSourcePrefix(d, e.browse, nil, e.querySource)
}

func exportSourcePrefix(d dialect.ID, browse *editBrowse, source *sqlservice.EditableSource, query *queryPagePlan) string {
	var schema, table string
	if query != nil {
		if source == nil {
			// This existing parser accepts only an unambiguous local table. A
			// join, expression or unknown source keeps the generic query name.
			if parsed, err := sqlservice.AnalyzeEditableQuery(d, query.original, query.options); err == nil {
				source = &parsed
			}
		}
	}
	if browse != nil && browse.plan != nil {
		t := browse.plan.table
		schema, table = t.Schema, t.Table
	} else if source != nil {
		// Do not guess a schema from current_schema: an unqualified table
		// can resolve later in PostgreSQL's search_path or SQL Server's dbo.
		schema, table = source.Schema, source.Table
	}
	if d.UsesMySQLSyntax() || d == dialect.ClickHouse {
		schema = "" // These products use the schema field for the database.
	}
	table = exportFilenamePart(table)
	if table == "" {
		table = "query"
	}
	if schema = exportFilenamePart(schema); schema != "" {
		return schema + "." + table
	}
	return table
}

func exportFilenamePart(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, value)
	value = strings.Trim(value, " .")
	// Bound the complete filename below common 255-byte filesystem limits,
	// retaining valid UTF-8 for quoted and non-ASCII database identifiers.
	for index, r := range value {
		if index+utf8.RuneLen(r) > 64 {
			value = value[:index]
			break
		}
	}
	return strings.TrimRight(value, " .")
}

func exportFilename(prefix, format string, created time.Time) string {
	// A colon in the time is not a valid filename character on Windows.
	return prefix + "_" + created.Format("20060102_1504") + "." + format
}
