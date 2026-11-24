package sqlservice

// completionContext describes the position being edited, not a verdict about
// executable SQL. Only tokens at this SELECT's top level establish its clause;
// commas inside function calls and subqueries cannot start a FROM relation.
type completionContext struct {
	clause      string
	relation    bool
	expression  bool
	declaration bool
	using       bool
	usedNames   map[string]bool
}

func (p *scopeParser) completionContext(scope *queryScope, position, pathLength int) completionContext {
	out := completionContext{usedNames: map[string]bool{}}
	if scope == nil {
		return out
	}
	top := []token{}
	stack := []int{}
	usingAt := -1
	for i, t := range p.tokens {
		if t.start < scope.start || t.end > position {
			continue
		}
		if t.kind == '(' {
			if len(stack) == 0 && len(top) > 0 && top[len(top)-1].is("USING") {
				usingAt = i
			}
			stack = append(stack, i)
			continue
		}
		if t.kind == ')' {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if len(stack) == 0 {
				usingAt = -1
			}
			continue
		}
		if len(stack) > 0 {
			if usingAt >= 0 && t.ident() {
				out.usedNames[t.name(p.dialect)] = true
			}
			continue
		}
		top = append(top, t)
		switch {
		case t.is("SELECT"):
			out.clause = "SELECT"
		case t.is("FROM"), t.is("JOIN"):
			out.clause = "FROM"
		case t.is("ON"):
			out.clause = "ON"
		case t.is("WHERE"):
			out.clause = "WHERE"
		case t.is("HAVING"):
			out.clause = "HAVING"
		case t.is("GROUP"):
			out.clause = "GROUP"
		case t.is("ORDER"):
			out.clause = "ORDER"
		case t.is("LIMIT"), t.is("OFFSET"):
			out.clause = "LIMIT"
		case t.is("UNION"), t.is("INTERSECT"), t.is("EXCEPT"):
			out.clause = "SET"
		}
	}
	out.using = usingAt >= 0
	// Qualified table prefixes consist of identifier-dot pairs before the
	// replacement range. Strip them when checking the grammar immediately left.
	base := top
	if n := pathLength * 2; n > 0 && n <= len(base) {
		base = base[:len(base)-n]
	}
	if len(base) > 0 {
		last := base[len(base)-1]
		out.relation = last.is("FROM") || last.is("JOIN") || out.clause == "FROM" && last.kind == ','
		if last.is("ONLY") || last.is("LATERAL") {
			out.relation = out.clause == "FROM"
		}
		out.declaration = last.is("AS")
	}
	switch out.clause {
	case "SELECT", "WHERE", "ON", "HAVING", "GROUP", "ORDER":
		out.expression = true
	}
	return out
}

func (c completionContext) genericKeywords() []string {
	switch c.clause {
	case "FROM":
		return []string{"AS", "JOIN", "LEFT JOIN", "RIGHT JOIN", "FULL JOIN", "INNER JOIN", "CROSS JOIN", "ON", "USING", "WHERE", "GROUP BY", "ORDER BY", "LIMIT"}
	case "SET":
		return []string{"SELECT", "ALL"}
	case "LIMIT":
		return []string{"OFFSET"}
	case "":
		return []string{"SELECT", "WITH", "EXPLAIN"}
	}
	return keywords
}

func (c completionContext) keywords(d Dialect) []string {
	words := c.genericKeywords()
	out := make([]string, 0, len(words)+4)
	for _, word := range words {
		if (d == SQLServer || d == Oracle || d == OceanBaseOracle || d == DB2) && (word == "LIMIT" || word == "USING" && d == SQLServer) {
			continue
		}
		if (d == Oracle || d == OceanBaseOracle || d == SQLServer) && word == "RECURSIVE" {
			continue
		}
		out = append(out, word)
	}
	if c.clause == "SELECT" && d == SQLServer {
		out = append(out, "TOP")
	}
	if c.clause == "FROM" || c.clause == "ORDER" || c.clause == "LIMIT" {
		if d == SQLServer || d == Oracle || d == OceanBaseOracle || d == DB2 {
			out = append(out, "FETCH FIRST", "OFFSET")
		}
	}
	return out
}
