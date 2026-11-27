package nosql

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// splitStatements preserves quoted strings, regular expressions and nested
// MongoDB documents. Redis treats semicolons as ordinary argument bytes.
func splitStatements(input string, mongo bool) ([]string, bool, error) {
	var statements []string
	var current strings.Builder
	var nesting []rune
	var quote rune
	escaped, lineComment, blockComment := false, false, false
	regexClass := false
	runes := []rune(input)
	flush := func() {
		if statement := strings.TrimSpace(current.String()); statement != "" {
			statements = append(statements, statement)
		}
		current.Reset()
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		var next rune
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		if lineComment {
			if r == '\n' {
				lineComment = false
				if len(nesting) == 0 {
					flush()
				} else {
					current.WriteRune(' ')
				}
			}
			continue
		}
		if blockComment {
			if r == '*' && next == '/' {
				blockComment = false
				i++
				current.WriteRune(' ')
			}
			continue
		}
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && quote != 0 {
			current.WriteRune(r)
			escaped = true
			continue
		}
		if quote != 0 {
			current.WriteRune(r)
			if quote == '/' && r == '[' {
				regexClass = true
			}
			if quote == '/' && r == ']' {
				regexClass = false
			}
			if r == quote && !regexClass {
				quote = 0
			}
			continue
		}
		if mongo && r == '/' && next == '/' {
			lineComment = true
			i++
			continue
		}
		if mongo && r == '/' && next == '*' {
			blockComment = true
			i++
			continue
		}
		if r == '\'' || r == '"' || (mongo && r == '/') {
			quote = r
			current.WriteRune(r)
			continue
		}
		if mongo {
			switch r {
			case '(', '[', '{':
				nesting = append(nesting, r)
			case ')', ']', '}':
				if len(nesting) == 0 || !matching(nesting[len(nesting)-1], r) {
					return nil, false, errors.New("unmatched closing delimiter")
				}
				nesting = nesting[:len(nesting)-1]
			}
		}
		if (r == '\n' || (mongo && r == ';')) && len(nesting) == 0 {
			flush()
		} else {
			current.WriteRune(r)
		}
	}
	if quote != 0 || escaped || blockComment || len(nesting) > 0 {
		return nil, true, nil
	}
	flush()
	return statements, false, nil
}

func matching(open, close rune) bool {
	return open == '(' && close == ')' || open == '[' && close == ']' || open == '{' && close == '}'
}

// redisArgs follows redis-cli's quoted argument and binary escape conventions.
func redisArgs(line string) ([]any, error) {
	var args []any
	var word strings.Builder
	var quote byte
	started, closed := false, false
	flush := func() {
		if started {
			args = append(args, word.String())
			word.Reset()
			started = false
			closed = false
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote == 0 && strings.ContainsRune(" \t\r\n\v\f", rune(c)) {
			flush()
			continue
		}
		if closed {
			return nil, errors.New("closing quote must be followed by whitespace")
		}
		started = true
		if quote == 0 && (c == '\'' || c == '"') {
			quote = c
			continue
		}
		if quote != 0 && c == quote {
			quote = 0
			closed = true
			continue
		}
		if quote != 0 && c == '\\' {
			if i+1 >= len(line) {
				return nil, errors.New("unfinished escape")
			}
			next := line[i+1]
			if quote == '\'' && next != '\'' {
				word.WriteByte(c)
				continue
			}
			i++
			switch next {
			case 'n':
				word.WriteByte('\n')
			case 'r':
				word.WriteByte('\r')
			case 't':
				word.WriteByte('\t')
			case 'b':
				word.WriteByte('\b')
			case 'a':
				word.WriteByte('\a')
			case 'x':
				if i+2 >= len(line) {
					return nil, errors.New("hex escape requires two digits")
				}
				value, err := strconv.ParseUint(line[i+1:i+3], 16, 8)
				if err != nil {
					return nil, fmt.Errorf("invalid hex escape")
				}
				word.WriteByte(byte(value))
				i += 2
			default:
				word.WriteByte(next)
			}
			continue
		}
		word.WriteByte(c)
	}
	if quote != 0 {
		return nil, errors.New("unterminated quoted argument")
	}
	flush()
	return args, nil
}
