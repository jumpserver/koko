package sqlservice

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlanalysis"

	"github.com/pingcap/tidb/pkg/parser/mysql"
)

var ErrNotEditable = errors.New("query result is not editable")

type EditProjection struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

type EditableSource struct {
	Database string
	Schema   string
	Table    string
	Alias    string
	Star     bool
	Columns  []EditProjection
}

// AnalyzeEditableQuery proves the narrow source shape needed for writing result
// cells back. Syntax analysis alone cannot prove column identity: callers must
// match this projection to LoadEditTable and the actual result columns as well.
// The outer SELECT deliberately excludes joins, CTEs and computed projections;
// predicate subqueries do not change the identity of its direct result columns.
func AnalyzeEditableQuery(d dialect.ID, document string, opts AnalyzeOptions) (EditableSource, error) {
	var out EditableSource
	fail := func(reason string) (EditableSource, error) {
		return EditableSource{}, fmt.Errorf("%w: %s", ErrNotEditable, reason)
	}
	analysis, err := AnalyzeWithOptions(d, document, opts)
	if err != nil {
		return EditableSource{}, fmt.Errorf("%w: %w", ErrNotEditable, err)
	}
	if len(analysis.Statements) == 1 && analysis.Statements[0].Facts != nil && analysis.Statements[0].Facts.SourceCandidate {
		return EditableSourceFromFacts(*analysis.Statements[0].Facts)
	}
	if len(analysis.Statements) != 1 || !analysis.Statements[0].ReturnsRows {
		return fail("one SELECT is required")
	}
	mode, err := mysql.GetSQLMode(opts.MySQLSQLMode)
	if err != nil {
		return fail("unknown SQL mode")
	}
	var ts []token
	for _, t := range lexMode(document, d, mode.HasANSIQuotesMode(), mode.HasNoBackslashEscapesMode()) {
		if t.kind == 'c' {
			if strings.HasPrefix(t.text, "/*!") || strings.HasPrefix(t.text, "/*M!") {
				return fail("executable comment")
			}
			continue
		}
		ts = append(ts, t)
	}
	if len(ts) > 0 && ts[len(ts)-1].kind == ';' {
		ts = ts[:len(ts)-1]
	}
	if len(ts) < 4 || !ts[0].is("SELECT") {
		return fail("simple SELECT is required")
	}
	if source, handled, err := editWhereSubquerySource(d, document, ts, analysis, opts); handled {
		return source, err
	}
	if analysis.Statements[0].Facts != nil {
		return EditableSourceFromFacts(*analysis.Statements[0].Facts)
	}
	if analysis.Statements[0].MayWrite {
		return fail("one read SELECT is required")
	}
	from := -1
	for i, t := range ts {
		if t.kind == ';' {
			return fail("multiple statements")
		}
		if !t.quoted && t.ident() {
			switch strings.ToUpper(t.value) {
			case "DISTINCT", "DISTINCTROW", "UNION", "INTERSECT", "EXCEPT", "MINUS", "GROUP", "HAVING", "WINDOW", "QUALIFY", "CONNECT", "START", "JOIN", "APPLY", "PIVOT", "UNPIVOT", "INTO", "FOR", "LOCK", "WITH":
				return fail("complex SELECT source")
			case "SELECT":
				if i != 0 {
					return fail("subquery")
				}
			case "FROM":
				if from >= 0 {
					return fail("multiple sources")
				}
				from = i
			}
		}
	}
	if from <= 1 || from+1 >= len(ts) {
		return fail("table source is missing")
	}
	projectionStart := 1
	if ts[projectionStart].is("ALL") {
		projectionStart++
	}
	if d == dialect.SQLServer && projectionStart < from && ts[projectionStart].is("TOP") {
		projectionStart++
		parenthesized := projectionStart < from && ts[projectionStart].kind == '('
		if parenthesized {
			projectionStart++
		}
		numberStart := projectionStart
		for projectionStart < from && ts[projectionStart].kind >= '0' && ts[projectionStart].kind <= '9' {
			projectionStart++
		}
		if projectionStart == numberStart {
			return fail("literal TOP row limit is required")
		}
		if parenthesized {
			if projectionStart >= from || ts[projectionStart].kind != ')' {
				return fail("invalid TOP row limit")
			}
			projectionStart++
		}
	}
	if projectionStart >= from {
		return fail("projection is missing")
	}
	parts, next := editQualifiedName(ts, from+1, d)
	if len(parts) == 0 || len(parts) > 2 {
		return fail("one local table is required")
	}
	out.Table = parts[len(parts)-1]
	if len(parts) == 2 {
		out.Schema = parts[0]
	}
	if next < len(ts) && ts[next].is("AS") {
		next++
		if next >= len(ts) || !ts[next].ident() {
			return fail("table alias is missing")
		}
		out.Alias = ts[next].name(d)
		next++
	} else if next < len(ts) && ts[next].ident() && !editTailStart(ts[next]) {
		out.Alias = ts[next].name(d)
		next++
	}
	if next < len(ts) && !editTailStart(ts[next]) {
		return fail("unsupported table source")
	}
	for start := projectionStart; start < from; {
		end := start
		for end < from && ts[end].kind != ',' {
			end++
		}
		item := ts[start:end]
		if len(item) == 0 {
			return fail("empty projection")
		}
		star := len(item) == 1 && item[0].kind == '*'
		if len(item) == 3 && item[0].ident() && item[1].kind == '.' && item[2].kind == '*' {
			if !editOwnerMatches(item[0].name(d), out, d) {
				return fail("unknown projection owner")
			}
			star = true
		}
		if star {
			if start != projectionStart || end != from {
				return fail("mixed wildcard projection")
			}
			out.Star = true
			break
		}
		cols, consumed := editQualifiedName(item, 0, d)
		if len(cols) == 0 || len(cols) > 2 {
			return fail("direct columns are required")
		}
		if len(cols) == 2 && !editOwnerMatches(cols[0], out, d) {
			return fail("unknown projection owner")
		}
		col := EditProjection{Name: cols[len(cols)-1], Label: cols[len(cols)-1]}
		if consumed < len(item) && item[consumed].is("AS") {
			consumed++
		}
		if consumed < len(item) {
			alias := item[consumed]
			// MySQL allows a quoted string as a SELECT label, even without ANSI_QUOTES.
			if alias.ident() || d.UsesMySQLSyntax() && alias.kind == 's' && alias.closed {
				col.Label = alias.name(d)
				consumed++
			}
		}
		if consumed != len(item) {
			return fail("projection expression")
		}
		// Ordinals identify result columns even when labels repeat. Resolve
		// duplicate source columns against canonical table metadata before editing.
		out.Columns = append(out.Columns, col)
		if end+1 == from {
			return fail("empty trailing projection")
		}
		start = end + 1
	}
	return out, nil
}

func editQualifiedName(ts []token, at int, d dialect.ID) ([]string, int) {
	var parts []string
	for at < len(ts) && ts[at].ident() {
		parts = append(parts, ts[at].name(d))
		at++
		if at >= len(ts) || ts[at].kind != '.' {
			break
		}
		at++
		if at >= len(ts) || !ts[at].ident() {
			return nil, at
		}
	}
	return parts, at
}
func editTailStart(t token) bool {
	return t.is("WHERE") || t.is("ORDER") || t.is("LIMIT") || t.is("OFFSET") || t.is("FETCH")
}
func editNameKey(s string, d dialect.ID) string {
	if d.UsesMySQLSyntax() || d == dialect.SQLServer {
		return strings.ToLower(s)
	}
	return s
}
func editOwnerMatches(s string, source EditableSource, d dialect.ID) bool {
	owner := source.Alias
	if owner == "" {
		owner = source.Table
	}
	return editNameKey(s, d) == editNameKey(owner, d)
}

// EditableSourceFromFacts reuses an already parsed statement. Metadata and
// actual result-column confirmation remain mandatory for every candidate.
func EditableSourceFromFacts(f sqlanalysis.Statement) (EditableSource, error) {
	if !f.SourceCandidate || f.Kind != sqlanalysis.Select || f.MayWrite || !f.ReturnsRows || len(f.Tables) != 1 || len(f.Projections) == 0 {
		for _, d := range f.Diagnostics {
			if d.Code == sqlanalysis.Unsupported {
				return EditableSource{}, fmt.Errorf("%w: %w", ErrNotEditable, &sqlanalysis.Error{Diagnostic: d})
			}
		}
		return EditableSource{}, fmt.Errorf("%w: source identity is not proven", ErrNotEditable)
	}
	t := f.Tables[0]
	if t.Name.Name == "" || t.Server.Name != "" {
		return EditableSource{}, fmt.Errorf("%w: one local base table is required", ErrNotEditable)
	}
	out := EditableSource{Database: t.Database.Name, Schema: t.Schema.Name, Table: t.Name.Name, Alias: t.Alias.Name}
	for _, p := range f.Projections {
		if !p.Direct {
			return EditableSource{}, fmt.Errorf("%w: direct columns are required", ErrNotEditable)
		}
		if p.Star {
			if len(f.Projections) != 1 {
				return EditableSource{}, fmt.Errorf("%w: mixed wildcard projection", ErrNotEditable)
			}
			out.Star = true
			continue
		}
		if p.Column.Name == "" || p.Label.Name == "" {
			return EditableSource{}, fmt.Errorf("%w: column identity is unknown", ErrNotEditable)
		}
		out.Columns = append(out.Columns, EditProjection{Name: p.Column.Name, Label: p.Label.Name})
	}
	return out, nil
}
