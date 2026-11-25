package sqlservice

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	pg "github.com/pganalyze/pg_query_go/v6"
	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/mysql"
)

var keywords = []string{"SELECT", "FROM", "WHERE", "JOIN", "LEFT JOIN", "INNER JOIN", "ON", "AS", "WITH", "RECURSIVE", "GROUP BY", "ORDER BY", "HAVING", "LIMIT", "OFFSET", "DISTINCT", "UNION ALL", "EXISTS", "CASE", "WHEN", "THEN", "ELSE", "END", "AND", "OR", "NOT", "NULL", "IS NULL", "IN", "BETWEEN", "LIKE", "COUNT", "SUM", "AVG", "MIN", "MAX", "COALESCE", "CAST", "OVER", "PARTITION BY", "EXPLAIN"}
var functionSignatures = map[string]string{
	"COUNT":    "COUNT(expression | *) → bigint",
	"SUM":      "SUM(numeric_expression)",
	"AVG":      "AVG(numeric_expression)",
	"MIN":      "MIN(expression)",
	"MAX":      "MAX(expression)",
	"COALESCE": "COALESCE(value, ...)",
	"CAST":     "CAST(expression AS type)",
}
var mysqlReserved = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range parser.Keywords {
		if k.Reserved {
			m[k.Word] = true
		}
	}
	return m
}()

func Completion(ctx context.Context, conn *sql.Conn, d dialect.ID, req CompletionRequest) (CompletionResponse, error) {
	return CompletionWithCatalog(ctx, d, req, connectionCatalog{conn: conn, dialect: d})
}

// CompletionWithCatalog shares the exact implementation with the live endpoint
// and permits deterministic catalog fixtures. The tolerant scope grammar supports
// SELECT, JOIN aliases, CTE names/projections, derived tables and correlated
// subqueries. It intentionally omits inferred expression types, dynamic SQL,
// stored-program locals and table-valued function output types.
func CompletionWithCatalog(ctx context.Context, d dialect.ID, req CompletionRequest, catalog Catalog) (CompletionResponse, error) {
	out := CompletionResponse{Version: req.Version, From: req.UTF16Offset, To: req.UTF16Offset, Items: []CompletionItem{}}
	if !d.Supported() {
		return out, ErrUnsupportedDialect
	}
	if DocumentExceedsLimit(req.Document) {
		return out, ErrDocumentTooLarge
	}
	if !utf8.ValidString(req.Document) {
		return out, fmt.Errorf("SQL document is not valid UTF-8")
	}
	position, err := offsetToByte(req.Document, req.UTF16Offset)
	if err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	mode, err := mysql.GetSQLMode(req.Options.MySQLSQLMode)
	if err != nil {
		return out, fmt.Errorf("invalid MySQL SQL mode: %w", err)
	}
	all := lexMode(req.Document, d, mode.HasANSIQuotesMode(), mode.HasNoBackslashEscapesMode())
	prefix := ""
	quote := byte(0)
	from, to := position, position
	active := -1
	for i, t := range all {
		if position < t.start || position > t.end {
			continue
		}
		if t.kind == 'c' || t.kind == 's' {
			if position > t.start && position < t.end || position == t.end && !t.closed {
				return out, nil
			}
			continue
		}
		if t.ident() {
			active = i
			from = t.start
			to = t.end
			prefix = req.Document[t.start:position]
			if t.quoted {
				quote = t.quote
				prefix = strings.TrimPrefix(prefix, string(quote))
				prefix = strings.ReplaceAll(prefix, strings.Repeat(string(quote), 2), string(quote))
				closing := quote
				if quote == '[' {
					closing = ']'
				}
				prefix = strings.ReplaceAll(prefix, strings.Repeat(string(closing), 2), string(closing))
				prefix = strings.TrimSuffix(prefix, string(closing))
			}
			break
		}
	}
	out.From = byteToOffset(req.Document, from)
	out.To = byteToOffset(req.Document, to)
	// Select only the current lexical statement. A sibling statement cannot
	// contribute aliases or CTEs to completion.
	lo, hi := 0, len(all)
	depth := 0
	for i, t := range all {
		if t.kind == '(' {
			depth++
			if depth > 128 {
				return out, fmt.Errorf("SQL completion nesting exceeds 128 levels")
			}
		}
		if t.kind == ')' {
			depth--
		}
		if t.kind == ';' && depth == 0 {
			if t.end <= position {
				lo = i + 1
			} else {
				hi = i
				break
			}
		}
	}
	tokens := []token{}
	before := []token{}
	for i := lo; i < hi; i++ {
		t := all[i]
		if t.kind == 'c' {
			continue
		}
		tokens = append(tokens, t)
		if t.end <= from && i != active {
			before = append(before, t)
		}
	}
	parser, root := parseScopes(tokens, d, 0, len(req.Document))
	scope := parser.at(position, root)
	path := []token{}
	j := len(before) - 1
	if j >= 0 && before[j].kind == '.' {
		j--
		for j >= 0 && before[j].ident() {
			path = append([]token{before[j]}, path...)
			j--
			if j >= 0 && before[j].kind == '.' {
				j--
				continue
			}
			break
		}
	}
	context := parser.completionContext(scope, from, len(path))
	memo := &completionCatalog{catalog: catalog, columns: map[string][]Column{}}
	seen := map[string]bool{}
	addItem := func(name, insert, filter, kind, detail, doc string) {
		if !strings.HasPrefix(strings.ToLower(filter), strings.ToLower(prefix)) {
			return
		}
		key := kind + "\x00" + insert
		if seen[key] {
			return
		}
		seen[key] = true
		out.Items = append(out.Items, CompletionItem{Label: name, InsertText: insert, FilterText: filter, Kind: kind, Detail: detail, Documentation: doc})
	}
	add := func(name, kind, detail, doc string) {
		insert := name
		if kind != "keyword" && kind != "function" {
			insert = quoteIdentifier(name, d, quote)
		}
		addItem(name, insert, name, kind, detail, doc)
	}
	if context.declaration {
		return out, nil
	}
	bindings := visibleBindings(scope)
	if context.using && scope != nil {
		bindings = scope.relations
	}
	if context.clause == "ON" || context.using {
		// A JOIN condition cannot reference relations introduced to its right.
		bindings = bindingsBefore(bindings, from)
	}
	if len(path) > 0 && !context.relation {
		key := path[len(path)-1].key(d)
		for _, b := range bindings {
			if b.key != key || len(path) > 1 && (len(path) != 2 || b.schema != path[0].name(d) || b.name != b.table) {
				continue
			}
			columns, err := memo.bindingColumns(ctx, b, req.Context, 0, map[*binding]bool{})
			if err != nil {
				return out, err
			}
			for _, c := range columns {
				add(c.Name, "column", columnDetail(b, c), c.Comment)
			}
			return sortedCompletion(out), nil
		}
		// Fully qualified relation access (schema.table.column) is unambiguous.
		if len(path) == 2 {
			columns, err := memo.load(ctx, ColumnRequest{Context: req.Context, Schema: path[0].name(d), Table: path[1].name(d)})
			if err != nil {
				return out, err
			}
			for _, c := range columns {
				add(c.Name, "column", c.DataType, c.Comment)
			}
		}
		// An unresolved alias is not a schema lookup in an expression.
		return sortedCompletion(out), nil
	}
	if context.using {
		if len(bindings) < 2 || len(path) != 0 {
			return out, nil
		}
		right := bindings[len(bindings)-1]
		columns, err := memo.bindingColumns(ctx, right, req.Context, 0, map[*binding]bool{})
		if err != nil {
			return out, err
		}
		leftNames := map[string]bool{}
		for _, b := range bindings[:len(bindings)-1] {
			cols, err := memo.bindingColumns(ctx, b, req.Context, 0, map[*binding]bool{})
			if err != nil {
				return out, err
			}
			for _, col := range cols {
				leftNames[col.Name] = true
			}
		}
		for _, col := range columns {
			if leftNames[col.Name] && !context.usedNames[col.Name] {
				add(col.Name, "column", col.DataType+" · USING", col.Comment)
			}
		}
		return sortedCompletion(out), nil
	}
	if len(path) == 0 && context.expression {
		type sourceColumn struct {
			binding *binding
			column  Column
		}
		columns := []sourceColumn{}
		counts := map[string]int{}
		for _, b := range bindings {
			cols, err := memo.bindingColumns(ctx, b, req.Context, 0, map[*binding]bool{})
			if err != nil {
				return out, err
			}
			for _, col := range cols {
				columns = append(columns, sourceColumn{b, col})
				counts[col.Name]++
			}
		}
		if scope != nil && (context.clause == "ORDER" || context.clause == "GROUP" || d.UsesMySQLSyntax() && context.clause == "HAVING") {
			for _, field := range scope.projection {
				if field.aliased {
					add(field.name, "column", "result column alias", "")
				}
			}
		}
		for _, source := range columns {
			b, col := source.binding, source.column
			if counts[col.Name] > 1 || context.clause == "ON" && len(bindings) > 1 {
				insert := quoteIdentifier(b.name, d, 0) + "." + quoteIdentifier(col.Name, d, quote)
				addItem(b.name+"."+col.Name, insert, col.Name, "column", columnDetail(b, col), col.Comment)
			} else {
				add(col.Name, "column", columnDetail(b, col), col.Comment)
			}
		}
		for _, b := range bindings {
			add(b.name, "alias", "relation · "+bindingSource(b), "")
		}
	}
	if !context.relation && len(path) == 0 {
		for _, word := range context.keywords(d) {
			if signature, ok := functionSignatures[word]; ok {
				add(word, "function", signature, "Built-in SQL function; database versions may provide additional overloads.")
			} else {
				add(word, "keyword", "SQL keyword", "")
			}
		}
	}
	if context.relation && len(path) <= 1 {
		catalogContext := req.Context
		if d == dialect.PostgreSQL {
			catalogContext.Schema = ""
		} // unqualified names follow the full live search_path.
		if len(path) == 1 {
			catalogContext.Schema = path[0].name(d)
		}
		if len(path) == 0 && scope != nil {
			for _, b := range scope.ctes {
				add(b.name, "table", "common table expression", "")
			}
		}
		if catalog != nil {
			relations, err := catalog.Relations(ctx, MetadataRequest{Context: catalogContext, Prefix: prefix, Limit: MaxCompletionItems})
			if err != nil {
				return out, err
			}
			for _, r := range relations {
				kind := "table"
				if strings.Contains(strings.ToUpper(r.Kind), "VIEW") {
					kind = "view"
				}
				add(r.Name, kind, r.Schema, r.Comment)
			}
		}
	}
	return sortedCompletion(out), nil
}

func bindingSource(b *binding) string {
	if b.table == "" {
		return b.name
	}
	if b.schema != "" {
		return b.schema + "." + b.table
	}
	return b.table
}
func columnDetail(b *binding, c Column) string {
	source := bindingSource(b)
	if b.name != source {
		source = b.name + " · " + source
	}
	if c.DataType == "" {
		return source
	}
	return c.DataType + " · " + source
}
func bindingsBefore(bindings []*binding, position int) []*binding {
	out := []*binding{}
	for _, b := range bindings {
		if b.start <= position {
			out = append(out, b)
		}
	}
	return out
}

func sortedCompletion(out CompletionResponse) CompletionResponse {
	rank := func(kind string) int {
		switch kind {
		case "column":
			return 0
		case "variable", "alias":
			return 1
		case "table", "view":
			return 2
		}
		return 3
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		if rank(a.Kind) != rank(b.Kind) {
			return rank(a.Kind) < rank(b.Kind)
		}
		if order := strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label)); order != 0 {
			return order < 0
		}
		return a.InsertText < b.InsertText
	})
	if len(out.Items) > MaxCompletionItems {
		out.Items = out.Items[:MaxCompletionItems]
	}
	for i := range out.Items {
		out.Items[i].SortText = fmt.Sprintf("%04d", i)
	}
	return out
}

type completionCatalog struct {
	catalog Catalog
	columns map[string][]Column
}

func (c *completionCatalog) load(ctx context.Context, req ColumnRequest) ([]Column, error) {
	key := req.Context.Database + "\x00" + req.Context.Schema + "\x00" + req.Schema + "\x00" + req.Table
	if cols, ok := c.columns[key]; ok {
		return cols, nil
	}
	if c.catalog == nil {
		return nil, nil
	}
	cols, err := c.catalog.Columns(ctx, req)
	if err != nil {
		return nil, err
	}
	c.columns[key] = cols
	return cols, nil
}
func (c *completionCatalog) bindingColumns(ctx context.Context, b *binding, current Context, depth int, seen map[*binding]bool) ([]Column, error) {
	if depth > 16 || seen[b] {
		return nil, nil
	}
	seen[b] = true
	defer delete(seen, b)
	if len(b.declared) > 0 {
		cols := []Column{}
		for _, n := range b.declared {
			cols = append(cols, Column{Name: n})
		}
		return cols, nil
	}
	if b.query == nil {
		if b.table == "" {
			return nil, nil
		}
		return c.load(ctx, ColumnRequest{Context: current, Schema: b.schema, Table: b.table})
	}
	out := []Column{}
	names := map[string]bool{}
	for _, field := range b.query.projection {
		if !field.star {
			if !names[field.name] {
				names[field.name] = true
				out = append(out, Column{Name: field.name})
			}
			continue
		}
		for _, source := range b.query.relations {
			if field.qualifier != "" && source.key != field.qualifier {
				continue
			}
			cols, err := c.bindingColumns(ctx, source, current, depth+1, seen)
			if err != nil {
				return nil, err
			}
			for _, col := range cols {
				if !names[col.Name] {
					names[col.Name] = true
					out = append(out, col)
				}
			}
		}
	}
	return out, nil
}

func quoteIdentifier(name string, d dialect.ID, quote byte) string {
	if quote == 0 {
		needs := name == ""
		for i, r := range name {
			if !(unicode.IsLetter(r) || r == '_' || i > 0 && (unicode.IsDigit(r) || r == '$')) {
				needs = true
			}
			if d == dialect.PostgreSQL && unicode.IsUpper(r) || upperCaseIdentifiers(d) && unicode.IsLower(r) {
				needs = true
			}
		}
		if d.UsesMySQLSyntax() || d == dialect.ClickHouse {
			needs = needs || mysqlReserved[strings.ToUpper(name)]
		} else if d != dialect.PostgreSQL {
			needs = needs || commonReserved[strings.ToUpper(name)]
		} else if !needs {
			scan, err := pg.Scan(name)
			needs = err != nil || len(scan.GetTokens()) != 1
			if !needs {
				kind := scan.Tokens[0].KeywordKind
				needs = kind == pg.KeywordKind_RESERVED_KEYWORD || kind == pg.KeywordKind_TYPE_FUNC_NAME_KEYWORD
			}
		}
		if !needs {
			return name
		}
		quote = '"'
		if d.UsesMySQLSyntax() || d == dialect.ClickHouse {
			quote = '`'
		}
		if d == dialect.SQLServer {
			quote = '['
		}
	}
	if quote == '[' {
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	}
	q := string(quote)
	return q + strings.ReplaceAll(name, q, q+q) + q
}

// Quote conservatively across the additional dialects without feeding their
// identifiers to a PostgreSQL parser. Always quoting is also valid for callers
// that explicitly request an identifier delimiter.
var commonReserved = func() map[string]bool {
	out := map[string]bool{}
	for k, v := range mysqlReserved {
		out[k] = v
	}
	for _, k := range strings.Fields("TOP FETCH ROWS ROW ROWNUM CONNECT START PRIOR LEVEL PIVOT UNPIVOT APPLY MINUS SAMPLE MODEL QUALIFY CURRENT_USER SESSION_USER USER CURRENT SCHEMA DATABASE VALUE VALUES") {
		out[k] = true
	}
	return out
}()
