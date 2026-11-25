package sqlservice

import (
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

// These scopes are a deliberately small tolerant SELECT grammar for unfinished
// editor text. They consume lexical tokens and balanced subqueries, not regular
// expressions over the document. Unsupported constructs produce fewer hints,
// never invented catalog fields or an execution decision.
type queryScope struct {
	start, end int
	parent     *queryScope // Correlated subqueries may see this parent's relations.
	ctes       map[string]*binding
	relations  []*binding
	projection []projection
}
type binding struct {
	key, name, schema, table string
	start                    int
	query                    *queryScope
	declared                 []string
}
type projection struct {
	name, qualifier string
	star            bool
	aliased         bool
}
type scopeParser struct {
	dialect dialect.ID
	tokens  []token
	pairs   map[int]int
	parsed  map[int]*queryScope
	scopes  []*queryScope
}

func parseScopes(tokens []token, d dialect.ID, start, end int) (*scopeParser, *queryScope) {
	p := &scopeParser{dialect: d, tokens: tokens, pairs: map[int]int{}, parsed: map[int]*queryScope{}}
	stack := []int{}
	for i, t := range tokens {
		if t.kind == '(' {
			stack = append(stack, i)
		}
		if t.kind == ')' && len(stack) > 0 {
			j := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			p.pairs[j] = i
		}
	}
	for _, i := range stack {
		p.pairs[i] = len(tokens)
	}
	q := p.parse(0, len(tokens), nil, map[string]*binding{}, start, end)
	return p, q
}
func copyBindings(in map[string]*binding) map[string]*binding {
	out := make(map[string]*binding, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func (p *scopeParser) parse(lo, hi int, parent *queryScope, inherited map[string]*binding, start, end int) *queryScope {
	if q := p.parsed[lo]; q != nil {
		return q
	}
	q := &queryScope{start: start, end: end, parent: parent, ctes: copyBindings(inherited)}
	p.parsed[lo] = q
	p.scopes = append(p.scopes, q)
	t := p.tokens
	i := lo
	if i < hi && t[i].is("WITH") {
		i++
		recursive := i < hi && t[i].is("RECURSIVE")
		if recursive {
			i++
		}
		for i < hi && t[i].ident() {
			name := t[i]
			i++
			b := &binding{key: name.key(p.dialect), name: name.name(p.dialect)}
			if i < hi && t[i].kind == '(' {
				close, ok := p.pairs[i]
				if !ok || close >= hi {
					break
				}
				for j := i + 1; j < close; j++ {
					if t[j].ident() {
						b.declared = append(b.declared, t[j].name(p.dialect))
					}
				}
				i = close + 1
			}
			if i >= hi || !t[i].is("AS") {
				break
			}
			i++
			if i < hi && t[i].is("NOT") {
				i++
			}
			if i < hi && t[i].is("MATERIALIZED") {
				i++
			}
			if i >= hi || t[i].kind != '(' {
				break
			}
			close, ok := p.pairs[i]
			if !ok || close > hi {
				break
			}
			if recursive {
				q.ctes[b.key] = b
			}
			childEnd := end
			if close < len(t) {
				childEnd = t[close].start
			}
			// CTE definitions cannot correlate to the enclosing SELECT's FROM.
			b.query = p.parse(i+1, close, nil, q.ctes, t[i].end, childEnd)
			q.ctes[b.key] = b
			i = close + 1
			if i >= hi || t[i].kind != ',' {
				break
			}
			i++
		}
	}
	// Each unparenthesized set-operation branch owns its FROM aliases. The
	// first branch still supplies a derived table/CTE's projected column names.
	sets := []int{}
	for j := i; j < hi; j++ {
		if t[j].kind == '(' {
			if close, ok := p.pairs[j]; ok {
				j = close
			}
			continue
		}
		if t[j].is("UNION") || t[j].is("INTERSECT") || t[j].is("EXCEPT") || p.dialect.UsesOracleSyntax() && t[j].is("MINUS") {
			sets = append(sets, j)
		}
	}
	if len(sets) > 0 {
		originalHi := hi
		for k, setAt := range sets {
			branchLo := setAt + 1
			if branchLo < originalHi && (t[branchLo].is("ALL") || t[branchLo].is("DISTINCT")) {
				branchLo++
			}
			branchHi, branchEnd := originalHi, end
			if k+1 < len(sets) {
				branchHi = sets[k+1]
				branchEnd = t[branchHi].start
			}
			if branchLo < branchHi {
				p.parse(branchLo, branchHi, parent, q.ctes, t[branchLo].start, branchEnd)
			}
		}
		hi = sets[0]
		q.end = t[hi].start
	}
	selectAt := -1
	fromAt := -1
	for j := i; j < hi; j++ {
		if t[j].kind == '(' {
			if c, ok := p.pairs[j]; ok {
				j = c
			}
			continue
		}
		if t[j].is("SELECT") && selectAt < 0 {
			selectAt = j
			continue
		}
		if t[j].is("FROM") && fromAt < 0 {
			fromAt = j
			break
		}
	}
	if selectAt >= 0 {
		projectionEnd := hi
		if fromAt >= 0 {
			projectionEnd = fromAt
		}
		q.projection = p.project(selectAt+1, projectionEnd)
	}
	if fromAt >= 0 {
		expect := true
		lateral := false
		for j := fromAt + 1; j < hi; {
			if t[j].is("WHERE") || t[j].is("GROUP") || t[j].is("ORDER") || t[j].is("HAVING") || t[j].is("LIMIT") || t[j].is("FETCH") || t[j].is("OFFSET") || t[j].is("QUALIFY") || t[j].is("CONNECT") || t[j].is("START") || t[j].is("UNION") || t[j].is("EXCEPT") || t[j].is("INTERSECT") || t[j].is("RETURNING") {
				break
			}
			if t[j].is("JOIN") || p.dialect == dialect.SQLServer && t[j].is("APPLY") || t[j].kind == ',' {
				expect = true
				lateral = t[j].is("APPLY")
				j++
				continue
			}
			if !expect {
				if t[j].kind == '(' {
					if c, ok := p.pairs[j]; ok {
						j = c + 1
						continue
					}
				}
				j++
				continue
			}
			if t[j].is("LATERAL") || t[j].is("ONLY") {
				if t[j].is("LATERAL") {
					lateral = true
				}
				j++
				continue
			}
			b := &binding{start: t[j].start}
			next := j + 1
			if t[j].kind == '(' {
				close, ok := p.pairs[j]
				if !ok || close > hi {
					break
				}
				childEnd := end
				if close < len(t) {
					childEnd = t[close].start
				}
				if j+1 < close && (t[j+1].is("SELECT") || t[j+1].is("WITH")) {
					var correlationParent *queryScope
					if lateral {
						correlationParent = q
					}
					b.query = p.parse(j+1, close, correlationParent, q.ctes, t[j].end, childEnd)
				}
				next = close + 1
			} else if t[j].ident() {
				parts := []token{t[j]}
				for next+1 < hi && t[next].kind == '.' && t[next+1].ident() {
					parts = append(parts, t[next+1])
					next += 2
				}
				last := parts[len(parts)-1]
				b.key = last.key(p.dialect)
				b.name = last.name(p.dialect)
				b.table = last.name(p.dialect)
				if len(parts) > 1 {
					b.schema = parts[len(parts)-2].name(p.dialect)
				} else if cte := q.ctes[b.key]; cte != nil {
					b.query = cte.query
					b.declared = cte.declared
					b.table = ""
				}
				if next < hi && t[next].kind == '(' {
					if c, ok := p.pairs[next]; ok {
						next = c + 1
					}
					b.table = ""
				} // table-valued functions need dialect type analysis.
			} else {
				j++
				expect = false
				continue
			}
			if next < hi && t[next].is("AS") {
				next++
			}
			if next < hi && t[next].ident() && !relationBoundary(t[next]) {
				b.key = t[next].key(p.dialect)
				b.name = t[next].name(p.dialect)
				next++
			}
			if b.key != "" {
				q.relations = append(q.relations, b)
			}
			j = next
			expect = false
		}
	}
	// Correlated scalar/EXISTS subqueries are separate scopes. Quoted strings,
	// comments and sibling SELECTs cannot leak aliases into the current scope.
	for j := lo; j < hi; j++ {
		if t[j].kind != '(' {
			continue
		}
		close, ok := p.pairs[j]
		if !ok || close > hi {
			continue
		}
		if j+1 < close && (t[j+1].is("SELECT") || t[j+1].is("WITH")) {
			childEnd := end
			if close < len(t) {
				childEnd = t[close].start
			}
			p.parse(j+1, close, q, q.ctes, t[j].end, childEnd)
		}
		j = close
	}
	return q
}
func relationBoundary(t token) bool {
	if t.quoted {
		return false
	}
	switch strings.ToUpper(t.value) {
	case "WHERE", "JOIN", "APPLY", "LEFT", "RIGHT", "FULL", "INNER", "OUTER", "CROSS", "ON", "USING", "GROUP", "ORDER", "HAVING", "LIMIT", "OFFSET", "FETCH", "QUALIFY", "CONNECT", "START", "MODEL", "UNION", "EXCEPT", "INTERSECT", "MINUS", "WINDOW", "FOR", "RETURNING", "SET":
		return true
	}
	return false
}
func (p *scopeParser) project(lo, hi int) []projection {
	t := p.tokens
	out := []projection{}
	start := lo
	if start < hi && (t[start].is("DISTINCT") || t[start].is("ALL")) {
		start++
	}
	if p.dialect == dialect.SQLServer && start < hi && t[start].is("TOP") {
		start++
		if start < hi && t[start].kind == '(' {
			if close, ok := p.pairs[start]; ok && close < hi {
				start = close + 1
			}
		} else {
			for start < hi && (t[start].kind >= '0' && t[start].kind <= '9' || t[start].kind == '.') {
				start++
			}
		}
		if start < hi && t[start].is("PERCENT") {
			start++
		}
		if start+1 < hi && t[start].is("WITH") && t[start+1].is("TIES") {
			start += 2
		}
	}
	appendField := func(a, b int) {
		if a >= b {
			return
		}
		if b-a == 1 && t[a].kind == '*' {
			out = append(out, projection{star: true})
			return
		}
		if b-a == 3 && t[a].ident() && t[a+1].kind == '.' && t[a+2].kind == '*' {
			out = append(out, projection{star: true, qualifier: t[a].key(p.dialect)})
			return
		}
		for j := a; j+1 < b; j++ {
			if t[j].is("AS") && t[j+1].ident() && j+2 == b {
				out = append(out, projection{name: t[j+1].name(p.dialect), aliased: true})
				return
			}
			if t[j].kind == '(' {
				if c, ok := p.pairs[j]; ok {
					j = c
				}
			}
		}
		if b-a >= 2 && t[b-1].ident() && implicitAliasPredecessor(t[b-2]) && !t[b-1].is("END") {
			out = append(out, projection{name: t[b-1].name(p.dialect), aliased: true})
			return
		}
		if t[b-1].ident() {
			simple := true
			for j := a; j < b; j++ {
				if !t[j].ident() && t[j].kind != '.' {
					simple = false
				}
			}
			if simple {
				out = append(out, projection{name: t[b-1].name(p.dialect)})
			}
		}
	}
	for j := start; j < hi; j++ {
		if t[j].kind == '(' {
			if c, ok := p.pairs[j]; ok {
				j = c
			}
			continue
		}
		if t[j].kind == ',' {
			appendField(start, j)
			start = j + 1
		}
	}
	appendField(start, hi)
	return out
}
func (p *scopeParser) at(position int, fallback *queryScope) *queryScope {
	q := fallback
	for _, s := range p.scopes {
		if position >= s.start && position <= s.end && (q == nil || s.end-s.start <= q.end-q.start) {
			q = s
		}
	}
	return q
}
func visibleBindings(q *queryScope) []*binding {
	seen := map[string]bool{}
	out := []*binding{}
	for s := q; s != nil; s = s.parent {
		for _, b := range s.relations {
			if !seen[b.key] {
				seen[b.key] = true
				out = append(out, b)
			}
		}
	}
	return out
}

// Operators followed by an identifier are expressions, not implicit aliases.
func implicitAliasPredecessor(t token) bool {
	if t.kind == ')' || t.kind == 's' || t.kind >= '0' && t.kind <= '9' {
		return true
	}
	if !t.ident() {
		return false
	}
	for _, word := range []string{"AND", "OR", "NOT", "IS", "LIKE", "ILIKE", "COLLATE", "AT", "ZONE", "BETWEEN", "IN", "DISTINCT", "FROM", "WHEN", "THEN", "ELSE"} {
		if t.is(word) {
			return false
		}
	}
	return true
}
