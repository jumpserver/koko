package sqlservice

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// AnalyzeConsole preserves Java's ConsoleStatementBoundaryScanner contract:
// source boundaries are scanned without requiring the product grammar to know
// every database command. Native analysis only supplies execution capabilities;
// an unrecognized command is never classified as a safe read.
func AnalyzeConsole(d Dialect, document string, options AnalyzeOptions) (Analysis, error) {
	out := Analysis{}
	if DocumentExceedsLimit(document) || !utf8.ValidString(document) || strings.ContainsRune(document, 0) {
		return out, errors.New("Console command is too large or contains invalid text")
	}
	start, blockDepth := 0, 0
	content := false
	var quote byte
	dollarQuote := ""
	appendStatement := func(end int) {
		if !content {
			return
		}
		raw := document[start:end]
		text := strings.TrimSpace(raw)
		left := len(raw) - len(strings.TrimLeftFunc(raw, unicode.IsSpace))
		s := Statement{SQL: text, Start: start + left, End: start + left + len(text), Kind: "raw", ReturnsRows: true, MayWrite: true}
		if native, err := AnalyzeWithOptions(d, text, options); err == nil && len(native.Statements) == 1 {
			s.Kind, s.ReturnsRows, s.MayWrite, s.Facts = native.Statements[0].Kind, native.Statements[0].ReturnsRows, native.Statements[0].MayWrite, native.Statements[0].Facts
		}
		out.Statements = append(out.Statements, s)
	}
	for i := 0; i < len(document); i++ {
		ch := document[i]
		var next byte
		if i+1 < len(document) {
			next = document[i+1]
		}
		if dollarQuote != "" {
			if strings.HasPrefix(document[i:], dollarQuote) {
				i += len(dollarQuote) - 1
				dollarQuote = ""
			}
			continue
		}
		if quote != 0 {
			if ch == '\\' && i+1 < len(document) {
				i++
			} else if ch == quote {
				if next == quote {
					i++
				} else {
					quote = 0
				}
			}
			continue
		}
		if blockDepth > 0 {
			if ch == '/' && next == '*' {
				blockDepth++
				i++
			} else if ch == '*' && next == '/' {
				blockDepth--
				i++
			}
			continue
		}
		if ch == '-' && next == '-' {
			for i < len(document) && document[i] != '\n' && document[i] != '\r' {
				i++
			}
			continue
		}
		if ch == '/' && next == '*' {
			blockDepth = 1
			i++
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			quote, content = ch, true
			continue
		}
		if ch == '$' {
			if delimiter := consoleDollarQuote(document, i); delimiter != "" {
				dollarQuote, content = delimiter, true
				i += len(delimiter) - 1
				continue
			}
		}
		if ch == ';' {
			appendStatement(i + 1)
			start, content = i+1, false
			continue
		}
		r, width := utf8.DecodeRuneInString(document[i:])
		if !unicode.IsSpace(r) {
			content = true
		}
		i += width - 1
	}
	appendStatement(len(document))
	if len(out.Statements) == 0 {
		return out, errors.New("Console raw execution requires a database command")
	}
	return out, nil
}

func consoleDollarQuote(document string, start int) string {
	if start > 0 {
		previous, _ := utf8.DecodeLastRuneInString(document[:start])
		if previous == '$' || previous == '_' || unicode.IsLetter(previous) || unicode.IsDigit(previous) {
			return ""
		}
	}
	end := strings.IndexByte(document[start+1:], '$')
	if end < 0 {
		return ""
	}
	end += start + 1
	for i, r := range document[start+1 : end] {
		if r != '_' && !unicode.IsLetter(r) && (i == 0 || !unicode.IsDigit(r)) {
			return ""
		}
	}
	delimiter := document[start : end+1]
	if !strings.Contains(document[end+1:], delimiter) {
		return ""
	}
	return delimiter
}
