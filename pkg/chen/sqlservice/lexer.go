package sqlservice

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type token struct {
	text       string
	value      string
	start, end int
	quoted     bool
	kind       byte // i identifier, s string, c comment, punctuation otherwise
	quote      byte
	closed     bool // lexical string/comment delimiter was completed
}

func (t token) is(s string) bool { return !t.quoted && strings.EqualFold(t.value, s) }
func (t token) ident() bool      { return t.kind == 'i' }
func (t token) key(d Dialect) string {
	if (d == PostgreSQL || upperCaseIdentifiers(d) || d == ClickHouse) && t.quoted {
		return t.value
	}
	if upperCaseIdentifiers(d) {
		return strings.ToUpper(t.value)
	}
	if d == ClickHouse {
		return t.value
	}
	return strings.ToLower(t.value)
}
func (t token) name(d Dialect) string {
	if d == PostgreSQL || upperCaseIdentifiers(d) {
		return t.key(d)
	}
	return t.value
}

// lex is an error-tolerant editor lexer, not the execution parser. Strings,
// comments and quoted identifiers are kept indivisible, including unfinished
// tokens, so keywords inside them cannot introduce scope or relation bindings.
func lex(document string, d Dialect) []token {
	return lexMode(document, d, false, false)
}
func lexMode(document string, d Dialect, ansiQuotes, noBackslash bool) []token {
	tokens := make([]token, 0, len(document)/5)
	for i := 0; i < len(document); {
		r, n := utf8.DecodeRuneInString(document[i:])
		if unicode.IsSpace(r) {
			i += n
			continue
		}
		start := i
		if strings.HasPrefix(document[i:], "--") && (!IsMySQLDialect(d) || i+2 == len(document) || document[i+2] <= ' ') || (IsMySQLDialect(d) || d == ClickHouse) && document[i] == '#' {
			for i < len(document) && document[i] != '\n' {
				i++
			}
			tokens = append(tokens, token{text: document[start:i], start: start, end: i, kind: 'c'})
			continue
		}
		if strings.HasPrefix(document[i:], "/*") {
			i += 2
			depth := 1
			for i < len(document) && depth > 0 {
				if (d == PostgreSQL || d == SQLServer || d == DB2 || d == ClickHouse) && strings.HasPrefix(document[i:], "/*") {
					depth++
					i += 2
				} else if strings.HasPrefix(document[i:], "*/") {
					depth--
					i += 2
				} else {
					i++
				}
			}
			tokens = append(tokens, token{text: document[start:i], start: start, end: i, kind: 'c', closed: depth == 0})
			continue
		}
		if document[i] == '$' && d == PostgreSQL {
			j := i + 1
			for j < len(document) && (document[j] == '_' || document[j] >= 'a' && document[j] <= 'z' || document[j] >= 'A' && document[j] <= 'Z' || j > i+1 && document[j] >= '0' && document[j] <= '9') {
				j++
			}
			if j < len(document) && document[j] == '$' {
				tag := document[i : j+1]
				i = j + 1
				closed := false
				if k := strings.Index(document[i:], tag); k >= 0 {
					i += k + len(tag)
					closed = true
				} else {
					i = len(document)
				}
				tokens = append(tokens, token{text: document[start:i], start: start, end: i, kind: 's', closed: closed})
				continue
			}
		}
		qPrefix := 0
		if IsOracleDialect(d) {
			if i+2 < len(document) && (document[i] == 'q' || document[i] == 'Q') && document[i+1] == '\'' {
				qPrefix = 2
			}
			if i+3 < len(document) && (document[i] == 'n' || document[i] == 'N') && (document[i+1] == 'q' || document[i+1] == 'Q') && document[i+2] == '\'' {
				qPrefix = 3
			}
		}
		if qPrefix > 0 {
			close, width := utf8.DecodeRuneInString(document[i+qPrefix:])
			switch close {
			case '[':
				close = ']'
			case '(':
				close = ')'
			case '{':
				close = '}'
			case '<':
				close = '>'
			}
			i += qPrefix + width
			ending := string(close) + "'"
			closed := false
			if at := strings.Index(document[i:], ending); at >= 0 {
				i += at + len(ending)
				closed = true
			} else {
				i = len(document)
			}
			tokens = append(tokens, token{text: document[start:i], start: start, end: i, kind: 's', closed: closed})
			continue
		}
		if document[i] == '\'' || document[i] == '"' || document[i] == '`' && (IsMySQLDialect(d) || d == ClickHouse) || document[i] == '[' && d == SQLServer {
			q := document[i]
			endQuote := q
			if q == '[' {
				endQuote = ']'
			}
			i++
			var value strings.Builder
			closed := false
			for i < len(document) {
				if document[i] == endQuote {
					i++
					if i < len(document) && document[i] == endQuote {
						value.WriteByte(endQuote)
						i++
						continue
					}
					closed = true
					break
				}
				if d == ClickHouse && q != '\'' && document[i] == '\\' && i+1 < len(document) {
					escaped, consumed := clickHouseIdentifierEscape(document[i:])
					value.WriteString(escaped)
					i += consumed
					continue
				}
				if document[i] == '\\' && ((IsMySQLDialect(d) || d == ClickHouse) && !noBackslash && (q == '\'' || q == '"' && !ansiQuotes) || d == PostgreSQL && q == '\'' && start > 0 && (document[start-1] == 'e' || document[start-1] == 'E')) && i+1 < len(document) {
					value.WriteByte(document[i+1])
					i += 2
					continue
				}
				value.WriteByte(document[i])
				i++
			}
			kind := byte('i')
			if q == '\'' || IsMySQLDialect(d) && q == '"' && !ansiQuotes {
				kind = 's'
			}
			tokens = append(tokens, token{text: document[start:i], value: value.String(), start: start, end: i, quoted: true, kind: kind, quote: q, closed: closed})
			continue
		}
		if unicode.IsLetter(r) || r == '_' || r >= utf8.RuneSelf {
			i += n
			for i < len(document) {
				r, n = utf8.DecodeRuneInString(document[i:])
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '$' {
					break
				}
				i += n
			}
			tokens = append(tokens, token{text: document[start:i], value: document[start:i], start: start, end: i, kind: 'i'})
			continue
		}
		i += n
		tokens = append(tokens, token{text: document[start:i], value: document[start:i], start: start, end: i, kind: document[start]})
	}
	return tokens
}

// ClickHouse quoted identifiers use the same escapes as its string literals.
// Unknown escapes preserve the backslash; doubled quote delimiters are handled
// by the shared quoted-token scanner above.
func clickHouseIdentifierEscape(text string) (string, int) {
	const escaped = "abefnrtv0\\'\"`/="
	const decoded = "\a\b\x1b\f\n\r\t\v\x00\\'\"`/="
	if at := strings.IndexByte(escaped, text[1]); at >= 0 {
		return string(decoded[at]), 2
	}
	if text[1] == 'N' {
		return "", 2
	}
	if text[1] == 'x' && len(text) >= 4 {
		if value, err := strconv.ParseUint(text[2:4], 16, 8); err == nil {
			return string([]byte{byte(value)}), 4
		}
	}
	if text[1] <= 31 {
		return text[1:2], 2
	}
	return text[:1], 1
}

func offsetToByte(document string, offset int) (int, error) {
	if offset < 0 {
		return 0, fmt.Errorf("position_utf16 must not be negative")
	}
	n := 0
	for i, r := range document {
		if n == offset {
			return i, nil
		}
		if r > 0xffff {
			n += 2
		} else {
			n++
		}
		if n > offset {
			return 0, fmt.Errorf("position_utf16 splits a surrogate pair")
		}
	}
	if n == offset {
		return len(document), nil
	}
	return 0, fmt.Errorf("position_utf16 exceeds document length")
}
func byteToOffset(document string, position int) int {
	n := 0
	for _, r := range document[:position] {
		if r > 0xffff {
			n += 2
		} else {
			n++
		}
	}
	return n
}
