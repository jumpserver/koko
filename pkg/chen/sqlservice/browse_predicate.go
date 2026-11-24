package sqlservice

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// This is a complete, closed expression grammar, not a statement classifier.
// It deliberately admits no calls, subqueries, casts, qualified names or tails.
// The server still validates the admitted dialect's types and collation.
var browseExponent = regexp.MustCompile(`^[eE][0-9]*$`)
var browseNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

type browsePredicateParser struct {
	tokens     []token
	pos, depth int
	d          Dialect
	known      func(string) bool
}

func validatePortableBrowseWhere(d Dialect, where string, known func(string) bool) bool {
	if !utf8.ValidString(where) {
		return false
	}
	tokens := []token{}
	for _, t := range lex(where, d) {
		if t.kind == 'c' {
			if strings.HasPrefix(t.text, "--+") {
				return false
			}
			if strings.HasPrefix(t.text, "/*") && (!t.closed || strings.HasPrefix(t.text, "/*+")) {
				return false
			}
			continue
		}
		if t.quoted && !t.closed || t.kind == 's' && !t.closed {
			return false
		}
		tokens = append(tokens, t)
	}
	if len(tokens) == 0 || len(tokens) > 2048 {
		return false
	}
	p := browsePredicateParser{tokens: tokens, d: d, known: known}
	return p.expr() && p.pos == len(tokens)
}
func (p *browsePredicateParser) take(s string) bool {
	if p.pos < len(p.tokens) && p.tokens[p.pos].is(s) {
		p.pos++
		return true
	}
	return false
}
func (p *browsePredicateParser) expr() bool {
	if !p.and() {
		return false
	}
	for p.take("OR") {
		if !p.and() {
			return false
		}
	}
	return true
}
func (p *browsePredicateParser) and() bool {
	if !p.predicate() {
		return false
	}
	for p.take("AND") {
		if !p.predicate() {
			return false
		}
	}
	return true
}
func (p *browsePredicateParser) predicate() bool {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 32 {
		return false
	}
	if p.take("NOT") {
		return p.predicate()
	}
	if p.take("(") {
		return p.expr() && p.take(")")
	}
	if !p.operand(false) {
		return false
	}
	if p.take("IS") {
		p.take("NOT")
		return p.take("NULL")
	}
	negative := p.take("NOT")
	if p.take("IN") {
		if !p.take("(") || !p.operand(true) {
			return false
		}
		n := 1
		for p.take(",") {
			n++
			if n > 100 || !p.operand(true) {
				return false
			}
		}
		return p.take(")")
	}
	if p.take("BETWEEN") {
		return p.operand(true) && p.take("AND") && p.operand(true)
	}
	if p.take("LIKE") {
		return p.operand(true)
	}
	if negative {
		return false
	}
	if p.pos == len(p.tokens) {
		return false
	}
	op := p.tokens[p.pos].text
	p.pos++
	if op == "<" || op == ">" || op == "!" {
		if p.take("=") {
			op += "="
		} else if op == "<" && p.take(">") {
			op = ">"
		}
	}
	if op != "=" && op != "<" && op != ">" && op != "<=" && op != ">=" && op != "!=" {
		return false
	}
	return p.operand(false)
}
func (p *browsePredicateParser) operand(literalOnly bool) bool {
	if p.pos == len(p.tokens) {
		return false
	}
	t := p.tokens[p.pos]
	if t.kind == 's' {
		p.pos++
		return t.closed
	}
	if !t.quoted && strings.EqualFold(t.text, "N") && p.pos+1 < len(p.tokens) && p.tokens[p.pos+1].kind == 's' && t.end == p.tokens[p.pos+1].start && (p.d == SQLServer || IsOracleDialect(p.d)) {
		p.pos += 2
		return true
	}
	if p.take("NULL") {
		return true
	}
	if t.kind >= '0' && t.kind <= '9' || t.kind == '.' || t.kind == '+' || t.kind == '-' {
		start := p.pos
		end := t.end
		raw := t.text
		p.pos++
		for p.pos < len(p.tokens) && p.tokens[p.pos].start == end {
			next := p.tokens[p.pos]
			if next.kind >= '0' && next.kind <= '9' || next.kind == '.' || next.kind == '+' || next.kind == '-' || next.kind == 'i' && (browseExponent.MatchString(next.text)) {
				raw += next.text
				end = next.end
				p.pos++
			} else {
				break
			}
		}
		if browseNumber.MatchString(raw) {
			return true
		}
		p.pos = start
		return false
	}
	if !literalOnly && t.ident() && p.known(t.name(p.d)) {
		p.pos++
		return true
	}
	return false
}
