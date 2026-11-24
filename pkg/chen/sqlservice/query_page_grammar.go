package sqlservice

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// repeatableQueryParser is a complete closed grammar for the portable SQL
// subset, not a SELECT keyword classifier. It consumes joins, read CTEs,
// derived/scalar subqueries, sets, aggregates and windows while collecting every
// base relation/function. Unknown product syntax stays ordinary execution.
// Native Oracle/T-SQL parsing is required as well for those two products.
type repeatableQueryParser struct {
	reexecute                    bool
	tokens                       []token
	pos, depth                   int
	query                        *QueryPageSQL
	document                     string
	cteNames                     map[string]bool
	cteTargets                   map[string]*QueryPageBinding
	currentScope, lastQueryScope *QueryPageScope
}

func analyzeVendorQueryPage(d Dialect, document string, options AnalyzeOptions, replay ...bool) (*QueryPageSQL, error) {
	bad := errors.New("Database paging requires one repeatable SELECT with verified relations and built-ins; writes, locks, parameters, hints, sequence references and unverified vendor syntax keep cached results")
	switch d {
	case Oracle, SQLServer, Dameng, DB2, ClickHouse, OceanBaseOracle:
	default:
		return nil, ErrUnsupportedDialect
	}
	if DocumentExceedsLimit(document) || strings.ContainsRune(document, 0) || !utf8.ValidString(document) {
		return nil, bad
	}
	tokens := []token{}
	for _, t := range lex(document, d) {
		if t.kind == 'c' {
			if strings.HasPrefix(t.text, "--+") && !(len(replay) > 0 && replay[0]) {
				return nil, bad
			}
			if strings.HasPrefix(t.text, "/*") && (!t.closed || strings.HasPrefix(t.text, "/*+") && !(len(replay) > 0 && replay[0]) || strings.HasPrefix(t.text, "/*!") || strings.HasPrefix(strings.ToUpper(t.text), "/*M!")) {
				return nil, bad
			}
			continue
		}
		if t.kind == 's' && !t.closed || t.quoted && !t.closed {
			return nil, bad
		}
		tokens = append(tokens, t)
	}
	if len(tokens) > 0 && tokens[len(tokens)-1].text == ";" {
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) == 0 || len(tokens) > 8192 {
		return nil, bad
	}
	q := &QueryPageSQL{Dialect: d, CTEs: map[string]bool{}}
	p := repeatableQueryParser{tokens: tokens, query: q, document: document, reexecute: len(replay) > 0 && replay[0]}
	if !p.selectQuery(true) || p.pos != len(tokens) {
		return nil, bad
	}
	q.Original = strings.TrimSpace(document[:tokens[len(tokens)-1].end])
	if q.Body == "" {
		q.Body = q.Original
	} else {
		q.Body = strings.TrimSpace(document[p.bodyStart():tokens[len(tokens)-1].end])
	}
	if d == Oracle || d == SQLServer {
		a, err := AnalyzeWithOptions(d, q.Original, options)
		if err != nil || len(a.Statements) != 1 || !a.Statements[0].ReturnsRows {
			return nil, bad
		}
	}
	return q, nil
}
func (p *repeatableQueryParser) bodyStart() int { return len(p.query.Prefix) }
func (p *repeatableQueryParser) take(s string) bool {
	if p.pos < len(p.tokens) && p.tokens[p.pos].is(s) {
		p.pos++
		return true
	}
	return false
}
func (p *repeatableQueryParser) peek(s string) bool {
	return p.pos < len(p.tokens) && p.tokens[p.pos].is(s)
}
func (p *repeatableQueryParser) name() (string, bool) {
	if p.pos >= len(p.tokens) || !p.tokens[p.pos].ident() {
		return "", false
	}
	n := p.tokens[p.pos].name(p.query.Dialect)
	p.pos++
	return n, true
}
func (p *repeatableQueryParser) integer() bool {
	start := p.pos
	end := -1
	for p.pos < len(p.tokens) {
		t := p.tokens[p.pos]
		if t.kind < '0' || t.kind > '9' || end >= 0 && t.start != end {
			break
		}
		end = t.end
		p.pos++
	}
	return p.pos > start
}
func (p *repeatableQueryParser) readAlias() (string, bool) {
	if p.take("AS") {
		return p.name()
	}
	if p.pos < len(p.tokens) && p.tokens[p.pos].ident() && !p.boundary() {
		return p.name()
	}
	return "", true
}
func (p *repeatableQueryParser) boundary() bool {
	for _, s := range []string{"FROM", "WHERE", "GROUP", "HAVING", "ORDER", "LIMIT", "OFFSET", "FETCH", "UNION", "INTERSECT", "EXCEPT", "MINUS", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "CROSS", "OUTER", "ON", "USING", "AND", "OR", "WHEN", "THEN", "ELSE", "END", "ROWS", "RANGE", "PRECEDING", "FOLLOWING", "ASC", "DESC", "NULLS"} {
		if p.peek(s) {
			return true
		}
	}
	return false
}
func (p *repeatableQueryParser) selectQuery(top bool) bool {
	parentScope := p.currentScope
	defer func() { p.currentScope = parentScope }()
	parentTargets := p.cteTargets
	p.cteTargets = map[string]*QueryPageBinding{}
	for name, target := range parentTargets {
		p.cteTargets[name] = target
	}
	defer func() { p.cteTargets = parentTargets }()
	parentNames := p.cteNames
	p.cteNames = map[string]bool{}
	for name, value := range parentNames {
		p.cteNames[name] = value
	}
	defer func() { p.cteNames = parentNames }()
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 32 {
		return false
	}
	if p.take("WITH") {
		if p.take("RECURSIVE") && p.query.Dialect == SQLServer {
			return false
		}
		for {
			name, ok := p.name()
			if !ok {
				return false
			}
			if p.query.Dialect == SQLServer {
				name = strings.ToLower(name)
			}
			p.query.CTEs[name] = true
			p.cteNames[name] = true
			target := &QueryPageBinding{Alias: name}
			p.cteTargets[name] = target
			if p.take("(") {
				column, ok := p.name()
				if !ok {
					return false
				}
				target.Columns = append(target.Columns, column)
				for p.take(",") {
					column, ok = p.name()
					if !ok {
						return false
					}
					target.Columns = append(target.Columns, column)
				}
				if !p.take(")") {
					return false
				}
			}
			if !p.take("AS") || !p.take("(") {
				return false
			}
			outer := p.currentScope
			p.currentScope = nil
			ok = p.selectQuery(false)
			p.currentScope = outer
			if !ok || !p.take(")") {
				return false
			}
			target.Query = p.lastQueryScope
			if !p.take(",") {
				break
			}
		}
		if top {
			if p.pos == len(p.tokens) {
				return false
			}
			start := p.tokens[p.pos].start
			p.query.Prefix = p.document[:start]
			p.query.Body = p.document[start:]
			p.query.HasCTE = true
		}
	}
	if !p.selectBlock(top) {
		return false
	}
	firstScope := p.currentScope
	for p.peek("UNION") || p.peek("INTERSECT") || p.peek("EXCEPT") || p.peek("MINUS") {
		p.pos++
		if !p.take("ALL") {
			p.take("DISTINCT")
		}
		if top {
			p.query.HasWindow = false
		}
		p.currentScope = parentScope
		if !p.selectBlock(false) {
			return false
		}
	}
	p.currentScope = firstScope
	ordered := false
	window := false
	if p.peek("ORDER") {
		orderStart := p.tokens[p.pos].start
		p.take("ORDER")
		if !p.take("BY") || !p.orderList(top, true) {
			return false
		}
		if top {
			p.query.OrderStart = orderStart
			p.query.OrderEnd = p.tokens[p.pos-1].end
		}
		ordered = true
	}
	if p.take("LIMIT") {
		if p.query.Dialect != ClickHouse && p.query.Dialect != Dameng {
			return false
		}
		if !p.integer() {
			return false
		}
		if p.take(",") {
			if !p.integer() {
				return false
			}
		} else if p.take("OFFSET") {
			if !p.integer() {
				return false
			}
		}
		window = true
	}
	if p.take("OFFSET") {
		if !p.integer() || !(p.take("ROWS") || p.take("ROW")) {
			return false
		}
		window = true
	}
	if p.take("FETCH") {
		if !(p.take("FIRST") || p.take("NEXT")) || !p.integer() || !(p.take("ROWS") || p.take("ROW")) || !p.take("ONLY") {
			return false
		}
		window = true
	}
	if top {
		p.query.Ordered = ordered
		p.query.HasWindow = p.query.HasWindow || window
	}
	p.lastQueryScope = firstScope
	return true
}
func (p *repeatableQueryParser) selectBlock(top bool) bool {
	if p.take("(") {
		ok := p.selectQuery(false) && p.take(")")
		if ok {
			p.currentScope = p.lastQueryScope
		}
		return ok
	}
	if !p.take("SELECT") {
		return false
	}
	scope := &QueryPageScope{Parent: p.currentScope}
	p.currentScope = scope
	p.query.Scopes = append(p.query.Scopes, scope)
	distinct := p.take("DISTINCT")
	direct := p.query.Dialect == Dameng && top && p.pos == 1 && !distinct
	if !distinct {
		p.take("ALL")
	}
	if p.take("TOP") {
		if p.query.Dialect != SQLServer {
			return false
		}
		paren := p.take("(")
		if !p.integer() {
			return false
		}
		if paren && !p.take(")") {
			return false
		}
		if top {
			p.query.HasWindow = true
		}
	}
	n := 0
	for {
		start := p.pos
		references := len(scope.References)
		if !p.expression() {
			return false
		}
		end := p.pos
		if top {
			p.query.ProjectionKeys = append(p.query.ProjectionKeys, p.expressionKey(start, end))
		}
		alias, ok := p.readAlias()
		if !ok {
			return false
		}
		projection := p.projection(start, end)
		direct = direct && (projection.Star || projection.Name != "" && len(scope.References) == references+1)
		if alias != "" {
			projection.Name = alias
		}
		scope.Projections = append(scope.Projections, projection)
		n++
		if n > MaxCatalogPageSize {
			return false
		}
		if !p.take(",") {
			break
		}
	}
	if p.take("FROM") {
		if !p.table() {
			return false
		}
		if direct && p.pos == len(p.tokens) && len(scope.Sources) == 1 && scope.Sources[0].Relation != nil {
			p.query.DirectTableProjection = true
		}
		for {
			if p.take(",") {
				if !p.table() {
					return false
				}
				continue
			}
			join := false
			cross := false
			if p.take("JOIN") {
				join = true
			} else if p.take("INNER") {
				join = p.take("JOIN")
				if !join {
					return false
				}
			} else if p.take("LEFT") || p.take("RIGHT") || p.take("FULL") {
				p.take("OUTER")
				join = p.take("JOIN")
				if !join {
					return false
				}
			} else if p.take("CROSS") {
				cross = true
				join = p.take("JOIN")
				if !join {
					return false
				}
			}
			if !join {
				break
			}
			if !p.table() {
				return false
			}
			if cross {
				continue
			}
			if p.take("ON") {
				if !p.expression() {
					return false
				}
			} else if p.take("USING") {
				if !p.take("(") {
					return false
				}
				if _, ok := p.name(); !ok {
					return false
				}
				for p.take(",") {
					if _, ok := p.name(); !ok {
						return false
					}
				}
				if !p.take(")") {
					return false
				}
			} else {
				return false
			}
		}
	}
	if p.take("WHERE") && !p.expression() {
		return false
	}
	if p.take("GROUP") {
		if !p.take("BY") || !p.expression() {
			return false
		}
		for p.take(",") {
			if !p.expression() {
				return false
			}
		}
	}
	if p.take("HAVING") && !p.expression() {
		return false
	}
	return true
}
func (p *repeatableQueryParser) table() bool {
	scope := p.currentScope
	if p.take("(") {
		p.currentScope = nil
		ok := p.selectQuery(false)
		p.currentScope = scope
		if !ok || !p.take(")") {
			return false
		}
		child := p.lastQueryScope
		alias, ok := p.readAlias()
		if !ok {
			return false
		}
		scope.Sources = append(scope.Sources, QueryPageBinding{Alias: alias, Query: child})
		return true
	}
	name, ok := p.name()
	if !ok {
		return false
	}
	r := QueryPageRelation{Name: name}
	if p.take(".") {
		r.Schema = r.Name
		r.Name, ok = p.name()
		if !ok {
			return false
		}
	}
	if p.peek(".") || p.peek("(") || strings.HasPrefix(r.Name, "#") || strings.HasPrefix(r.Name, "@") {
		return false
	}
	alias, ok := p.readAlias()
	if !ok {
		return false
	}
	if alias == "" {
		alias = r.Name
	}
	cteName := r.Name
	if p.query.Dialect == SQLServer {
		cteName = strings.ToLower(cteName)
	}
	if r.Schema == "" && p.cteNames[cteName] {
		target := p.cteTargets[cteName]
		if target == nil {
			return false
		}
		binding := *target
		binding.Alias = alias
		scope.Sources = append(scope.Sources, binding)
	} else {
		p.query.Relations = append(p.query.Relations, r)
		scope.Sources = append(scope.Sources, QueryPageBinding{Alias: alias, Relation: &r})
	}
	return true
}

func (p *repeatableQueryParser) orderList(top, outputAliases bool) bool {
	for {
		start := p.pos
		refCount := 0
		if p.currentScope != nil {
			refCount = len(p.currentScope.References)
		}
		if !p.expression() {
			return false
		}
		end := p.pos
		if outputAliases && end-start == 1 && p.tokens[start].ident() && p.currentScope != nil && len(p.currentScope.References) == refCount+1 {
			p.currentScope.References[refCount].OutputAlias = true
		}
		order := QueryPageOrder{Key: p.expressionKey(start, end)}
		if end-start == 1 && p.tokens[start].ident() {
			order.Name = p.tokens[start].name(p.query.Dialect)
		}
		raw := strings.TrimSpace(p.document[p.tokens[start].start:p.tokens[end-1].end])
		if n, err := strconv.Atoi(raw); err == nil {
			order.Ordinal = n
		}
		if !p.take("ASC") {
			order.Desc = p.take("DESC")
		}
		if p.take("NULLS") {
			if p.take("FIRST") {
				order.Nulls = "FIRST"
			} else if p.take("LAST") {
				order.Nulls = "LAST"
			} else {
				return false
			}
		}
		if top {
			p.query.Order = append(p.query.Order, order)
		}
		if !p.take(",") {
			break
		}
	}
	return true
}
func (p *repeatableQueryParser) expression() bool {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 64 {
		return false
	}
	if !p.value() {
		return false
	}
	for p.pos < len(p.tokens) {
		if p.take("IS") {
			p.take("NOT")
			if !(p.take("NULL") || p.take("TRUE") || p.take("FALSE")) {
				return false
			}
			continue
		}
		negative := p.take("NOT")
		if p.take("IN") {
			if !p.take("(") {
				return false
			}
			if p.peek("SELECT") || p.peek("WITH") {
				if !p.selectQuery(false) {
					return false
				}
			} else {
				if !p.expression() {
					return false
				}
				n := 1
				for p.take(",") {
					n++
					if n > 1000 || !p.expression() {
						return false
					}
				}
			}
			if !p.take(")") {
				return false
			}
			continue
		}
		if p.take("BETWEEN") {
			if !p.value() || !p.take("AND") || !p.value() {
				return false
			}
			continue
		}
		if p.take("LIKE") {
			if !p.value() {
				return false
			}
			if p.take("ESCAPE") && !p.value() {
				return false
			}
			continue
		}
		if negative {
			return false
		}
		binary := false
		for _, op := range []string{"AND", "OR", "=", "<", ">", "!", "+", "-", "*", "/", "%", "|"} {
			if p.take(op) {
				binary = true
				if op == "<" {
					if !p.take("=") {
						p.take(">")
					}
				} else if op == ">" || op == "!" {
					if op == "!" && !p.take("=") {
						return false
					}
					if op == ">" {
						p.take("=")
					}
				} else if op == "|" && !p.take("|") {
					return false
				}
				break
			}
		}
		if !binary {
			break
		}
		if !p.value() {
			return false
		}
	}
	return true
}
func (p *repeatableQueryParser) value() bool {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 64 {
		return false
	}
	if p.take("NOT") || p.take("+") || p.take("-") {
		return p.value()
	}
	if p.take("EXISTS") {
		return p.take("(") && p.selectQuery(false) && p.take(")")
	}
	if p.take("(") {
		if p.peek("SELECT") || p.peek("WITH") {
			return p.selectQuery(false) && p.take(")")
		}
		return p.expression() && p.take(")")
	}
	if p.take("CASE") {
		if !p.peek("WHEN") && !p.expression() {
			return false
		}
		n := 0
		for p.take("WHEN") {
			n++
			if n > 100 || !p.expression() || !p.take("THEN") || !p.expression() {
				return false
			}
		}
		if n == 0 {
			return false
		}
		if p.take("ELSE") && !p.expression() {
			return false
		}
		return p.take("END")
	}
	if p.take("NULL") || p.take("TRUE") || p.take("FALSE") || p.take("*") {
		return true
	}
	if p.pos == len(p.tokens) {
		return false
	}
	t := p.tokens[p.pos]
	if t.kind == 's' {
		p.pos++
		return true
	}
	if !t.quoted && strings.EqualFold(t.text, "N") && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].kind == 's' && t.end == p.tokens[p.pos+1].start {
		p.pos += 2
		return true
	}
	if t.kind >= '0' && t.kind <= '9' || t.kind == '.' {
		literal := browsePredicateParser{tokens: p.tokens, pos: p.pos, d: p.query.Dialect}
		if !literal.operand(true) {
			return false
		}
		p.pos = literal.pos
		return true
	}
	quotedName := p.tokens[p.pos].quoted
	name, ok := p.name()
	if !ok {
		return false
	}
	parts := []string{name}
	for i := 0; p.take("."); i++ {
		if i > 1 {
			return false
		}
		if p.take("*") {
			return true
		}
		quotedName = quotedName || p.pos < len(p.tokens) && p.tokens[p.pos].quoted
		name, ok = p.name()
		if !ok || !p.reexecute && (strings.EqualFold(name, "NEXTVAL") || strings.EqualFold(name, "CURRVAL")) {
			return false
		}
		parts = append(parts, name)
	}
	if p.take("(") {
		if (quotedName || len(parts) != 1) && !p.reexecute {
			return false
		}
		if !p.reexecute && !queryPageVendorFunction(p.query.Dialect, name) {
			return false
		}
		p.query.Functions = append(p.query.Functions, strings.Join(parts, "."))
		p.take("DISTINCT")
		if !p.take(")") {
			if !p.expression() {
				return false
			}
			for p.take(",") {
				if !p.expression() {
					return false
				}
			}
			if !p.take(")") {
				return false
			}
		}
		if p.take("OVER") {
			if !p.take("(") {
				return false
			}
			if p.take("PARTITION") {
				if !p.take("BY") || !p.expression() {
					return false
				}
				for p.take(",") {
					if !p.expression() {
						return false
					}
				}
			}
			if p.take("ORDER") {
				if !p.take("BY") || !p.orderList(false, false) {
					return false
				}
			}
			if p.take("ROWS") || p.take("RANGE") {
				if p.take("BETWEEN") {
					if !p.frame() || !p.take("AND") || !p.frame() {
						return false
					}
				} else if !p.frame() {
					return false
				}
			}
			if !p.take(")") {
				return false
			}
		}
		return true
	}
	if p.currentScope != nil {
		p.currentScope.References = append(p.currentScope.References, QueryPageReference{Parts: parts})
	}
	return true
}
func (p *repeatableQueryParser) frame() bool {
	if p.take("CURRENT") {
		return p.take("ROW")
	}
	if !p.take("UNBOUNDED") && !p.integer() {
		return false
	}
	return p.take("PRECEDING") || p.take("FOLLOWING")
}

func (p *repeatableQueryParser) expressionKey(start, end int) string {
	var b strings.Builder
	for _, t := range p.tokens[start:end] {
		if t.ident() {
			b.WriteString("i:")
			b.WriteString(t.name(p.query.Dialect))
		} else {
			b.WriteString(t.text)
		}
		b.WriteByte(0)
	}
	return b.String()
}

func (p *repeatableQueryParser) projection(start, end int) QueryPageProjection {
	tokens := p.tokens[start:end]
	if len(tokens) == 1 && tokens[0].text == "*" {
		return QueryPageProjection{Star: true}
	}
	if len(tokens) == 3 && tokens[0].ident() && tokens[1].text == "." && tokens[2].text == "*" {
		return QueryPageProjection{Star: true, Qualifier: tokens[0].name(p.query.Dialect)}
	}
	for i, t := range tokens {
		if i%2 == 0 && !t.ident() || i%2 == 1 && t.text != "." {
			return QueryPageProjection{}
		}
	}
	if len(tokens)%2 == 1 {
		return QueryPageProjection{Name: tokens[len(tokens)-1].name(p.query.Dialect)}
	}
	return QueryPageProjection{}
}

// Product built-ins are admitted under the same relation/type and native
// function-resolution guards as portable functions, never by name alone.
func queryPageVendorFunction(d Dialect, name string) bool {
	if queryPageFunction(name) {
		return true
	}
	switch d {
	case Oracle, Dameng, OceanBaseOracle:
		switch strings.ToLower(name) {
		case "nvl", "nvl2", "decode", "instr", "mod", "trunc", "to_char", "to_number", "to_date", "to_timestamp":
			return true
		}
	case SQLServer:
		switch strings.ToLower(name) {
		case "count_big", "isnull", "len", "datalength", "charindex":
			return true
		}
	case DB2:
		switch strings.ToLower(name) {
		case "count_big", "lcase", "ucase", "locate", "posstr":
			return true
		}
	case ClickHouse:
		switch strings.ToLower(name) {
		case "tostring", "toint32", "toint64", "touint32", "touint64", "ifnull":
			return true
		}
	}
	return false
}
