package sqlservice

import (
	"regexp"
	"strings"

	"github.com/pingcap/tidb/pkg/parser/mysql"
)

var maskNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// ResultMaskColumns recovers names for the JDBC column-name/column-label policy.
// The statement must already have passed execution parsing. Direct projections
// retain their column names even when a predicate prevents query replay. For
// expressions and set results, JDBC exposes the output name/label instead of
// expression lineage; keep that legacy policy rather than masking every field
// when the paging or editability analyzer cannot prove its source.
func ResultMaskColumns(d Dialect, document string, options AnalyzeOptions, labels []string) []string {
	out := append([]string(nil), labels...)
	mode, err := mysql.GetSQLMode(options.MySQLSQLMode)
	if err != nil {
		return out
	}
	tokens := []token{}
	for _, t := range lexMode(document, d, mode.HasANSIQuotesMode(), mode.HasNoBackslashEscapesMode()) {
		if t.kind == 'c' {
			if strings.HasPrefix(t.text, "/*!") || strings.HasPrefix(strings.ToUpper(t.text), "/*M!") || strings.HasPrefix(t.text, "/*") && !t.closed {
				return out
			}
			continue
		}
		if (t.quoted || t.kind == 's') && !t.closed {
			return out
		}
		tokens = append(tokens, t)
	}
	if len(tokens) > 0 && tokens[0].is("WITH") {
		// A CTE can rename its output columns. Recover only the outer
		// projection's own names, never the CTE body's underlying lineage.
		depth := 0
		for i, t := range tokens {
			if t.text == "(" {
				depth++
			} else if t.text == ")" {
				depth--
			} else if depth == 0 && t.is("SELECT") {
				tokens = tokens[i:]
				break
			}
		}
	}
	if len(tokens) == 0 || !tokens[0].is("SELECT") {
		return out
	}
	// JDBC exposes a set result's output names, not one arbitrary arm's base
	// columns. Predicate subqueries do not change the outer projection names.
	depth := 0
	for _, t := range tokens {
		if t.text == "(" {
			depth++
		} else if t.text == ")" {
			depth--
		}
		if depth == 0 && (t.is("UNION") || t.is("INTERSECT") || t.is("EXCEPT") || t.is("MINUS")) {
			return out
		}
	}
	start := 1
	for start < len(tokens) {
		t := tokens[start]
		if t.is("DISTINCT") || t.is("ALL") || IsMySQLDialect(d) && (t.is("DISTINCTROW") || t.is("HIGH_PRIORITY") || t.is("STRAIGHT_JOIN") || t.is("SQL_SMALL_RESULT") || t.is("SQL_BIG_RESULT") || t.is("SQL_BUFFER_RESULT") || t.is("SQL_NO_CACHE") || t.is("SQL_CALC_FOUND_ROWS")) {
			start++
			continue
		}
		break
	}
	if d == SQLServer && start < len(tokens) && tokens[start].is("TOP") {
		start++
		if start < len(tokens) && tokens[start].text == "(" {
			start++
			for start < len(tokens) && tokens[start].kind >= '0' && tokens[start].kind <= '9' {
				start++
			}
			if start >= len(tokens) || tokens[start].text != ")" {
				return out
			}
			start++
		} else {
			for start < len(tokens) && tokens[start].kind >= '0' && tokens[start].kind <= '9' {
				start++
			}
		}
		if start < len(tokens) && tokens[start].is("PERCENT") {
			start++
		}
		if start+1 < len(tokens) && tokens[start].is("WITH") && tokens[start+1].is("TIES") {
			start += 2
		}
	}
	var projections [][]token
	depth = 0
	end := start
	for ; end < len(tokens); end++ {
		t := tokens[end]
		if depth == 0 && (t.is("FROM") || t.is("WHERE") || t.is("GROUP") || t.is("HAVING") || t.is("ORDER") || t.is("LIMIT") || t.is("OFFSET") || t.is("FETCH") || t.is("INTO") || t.text == ";") {
			break
		}
		if depth == 0 && t.text == "," {
			projections = append(projections, tokens[start:end])
			start = end + 1
		}
		if t.text == "(" {
			depth++
		} else if t.text == ")" {
			depth--
		}
	}
	projections = append(projections, tokens[start:end])
	stars := 0
	for i, p := range projections {
		projections[i] = maskProjectionExpression(p)
		if maskProjectionStar(projections[i]) {
			stars++
		}
	}
	if stars == 0 && len(projections) != len(labels) || stars > 0 && len(labels) < len(projections) || stars > 1 && stars != len(projections) {
		return out
	}
	if stars == len(projections) && stars > 0 {
		return out
	}
	col := 0
	for _, p := range projections {
		if maskProjectionStar(p) {
			width := len(labels) - len(projections) + 1
			col += width
			continue
		}
		if len(p) == 0 {
			col++
			continue
		}
		constant := len(p) == 1 && (p[0].kind == 's' || p[0].is("NULL") || p[0].is("TRUE") || p[0].is("FALSE"))
		constant = constant || len(p) == 2 && (p[0].is("N") || d == PostgreSQL && p[0].is("E")) && p[1].kind == 's'
		constant = constant || maskNumber.MatchString(strings.TrimSpace(document[p[0].start:p[len(p)-1].end]))
		count := len(p) == 4 && p[0].is("COUNT") && p[1].text == "(" && p[2].text == "*" && p[3].text == ")"
		if constant || count {
			out[col] = labels[col]
		} else if maskProjectionColumn(p) {
			out[col] = p[len(p)-1].name(d)
		}
		col++
	}
	return out
}

func maskProjectionExpression(p []token) []token {
	if len(p) >= 3 && p[len(p)-2].is("AS") && (p[len(p)-1].ident() || p[len(p)-1].kind == 's') {
		return p[:len(p)-2]
	}
	if len(p) >= 2 && (p[len(p)-1].ident() || p[len(p)-1].kind == 's') && p[len(p)-2].text != "." {
		// Implicit aliases require whitespace; 1e2 is a single numeric token
		// in native parsers but several editor-lexer tokens.
		if p[len(p)-2].end < p[len(p)-1].start {
			return p[:len(p)-1]
		}
	}
	return p
}

func maskProjectionColumn(p []token) bool {
	if len(p)%2 != 1 {
		return false
	}
	for i, t := range p {
		if i%2 == 0 && !t.ident() || i%2 == 1 && t.text != "." {
			return false
		}
	}
	return len(p) > 0
}

func maskProjectionStar(p []token) bool {
	return len(p) == 1 && p[0].text == "*" || len(p) >= 3 && p[len(p)-1].text == "*" && p[len(p)-2].text == "." && maskProjectionColumn(p[:len(p)-2])
}
