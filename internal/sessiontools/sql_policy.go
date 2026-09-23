package sessiontools

import (
	"fmt"
	"strings"
	"unicode"
)

type sqlKind uint8

const (
	sqlUnknown sqlKind = iota
	sqlRead
	sqlWrite
	sqlSession
)

type sqlAnalysis struct {
	kind             sqlKind
	keyword          string
	words            []string
	depths           []int
	multi            bool
	incomplete       bool
	semicolon        bool
	lineCommentStart int
}

func (a sqlAnalysis) PTYCommand(statement string) string {
	if a.semicolon {
		return statement
	}
	offset := len(statement)
	if a.lineCommentStart >= 0 {
		// A terminator appended after a line comment would be ignored by usql.
		offset = len(strings.TrimRightFunc(statement[:a.lineCommentStart], unicode.IsSpace))
	}
	return statement[:offset] + ";" + statement[offset:]
}

func (a sqlAnalysis) BackgroundEligible() bool {
	return !a.multi && !a.incomplete && (a.kind == sqlRead || a.kind == sqlWrite)
}

func (a sqlAnalysis) PTYReason() string {
	switch {
	case a.multi:
		return "multiple SQL statements are restricted to the active PTY"
	case a.incomplete:
		return "incomplete SQL is restricted to the active PTY"
	case a.kind == sqlSession:
		return "session-dependent SQL is restricted to the active PTY"
	default:
		return "unclassified SQL is restricted to the active PTY"
	}
}

func analyzeSQL(statement, protocol string) (sqlAnalysis, error) {
	dialect := dialectForSQL(protocol)
	words, depths, semicolon, trailingOnly, incomplete, lineCommentStart, err := scanSQL(statement, dialect)
	if err != nil {
		return sqlAnalysis{}, err
	}
	analysis := sqlAnalysis{
		words: words, depths: depths,
		multi: semicolon && !trailingOnly, incomplete: incomplete,
		semicolon: semicolon, lineCommentStart: lineCommentStart,
	}
	if len(words) == 0 {
		return analysis, fmt.Errorf("model generated an empty SQL statement")
	}
	analysis.keyword = rootSQLKeyword(words, depths)
	analysis.kind = dialect.keywordKind(analysis.keyword)
	if analysis.kind == sqlUnknown {
		return analysis, nil
	}
	if containsSQLSequence(words, "CREATE", "TEMPORARY", "TABLE") ||
		containsSQLSequence(words, "CREATE", "TEMP", "TABLE") ||
		containsSQLSequence(words, "DROP", "TEMPORARY", "TABLE") ||
		(dialect.mysql && (containsSQLWord(words, "GET_LOCK") || containsSQLWord(words, "RELEASE_LOCK"))) ||
		containsSQLSequence(words, "FOR", "UPDATE") ||
		containsSQLSequence(words, "FOR", "SHARE") ||
		containsSQLSequence(words, "LOCK", "IN", "SHARE", "MODE") {
		analysis.kind = sqlSession
	}
	for _, word := range words {
		if (dialect.sqlserver && strings.HasPrefix(word, "#")) ||
			((dialect.mysql || dialect.sqlserver) && strings.HasPrefix(word, "@")) ||
			(dialect.postgres && (strings.HasPrefix(word, "PG_ADVISORY_") || strings.HasPrefix(word, "PG_TRY_ADVISORY_"))) {
			analysis.kind = sqlSession
		}
	}
	if analysis.keyword == "SELECT" && dialect.mysql &&
		(containsSQLSequence(words, "INTO", "OUTFILE") ||
			containsSQLSequence(words, "INTO", "DUMPFILE")) {
		analysis.kind = sqlWrite
	}
	if (analysis.kind == sqlWrite && (containsTopLevelSQLWord(words, depths, "RETURNING") ||
		containsTopLevelSQLWord(words, depths, "OUTPUT"))) ||
		(analysis.keyword == "SELECT" && (dialect.postgres || dialect.sqlserver) &&
			containsTopLevelSQLWord(words, depths, "INTO")) {
		// These statements can produce result sets that ExecContext cannot return.
		analysis.kind = sqlSession
	}
	return analysis, nil
}

func isSchemaChangingSQL(analysis sqlAnalysis) bool {
	switch analysis.keyword {
	case "CREATE", "ALTER", "DROP", "TRUNCATE", "RENAME", "ATTACH", "DETACH":
		return true
	default:
		return false
	}
}

func scanSQL(statement string, dialect sqlDialect) (
	words []string, depths []int, semicolon, trailingOnly, incomplete bool, lineCommentStart int, err error,
) {
	lineCommentStart = -1
	var word strings.Builder
	state := byte(0)
	escapeBackslash := false
	blockDepth := 0
	sqlDepth := 0
	wordDepth := 0
	flush := func() {
		if word.Len() == 0 {
			return
		}
		words = append(words, strings.ToUpper(word.String()))
		depths = append(depths, wordDepth)
		word.Reset()
	}
	trailingOnly = true
	for index := 0; index < len(statement); index++ {
		current := statement[index]
		next := byte(0)
		if index+1 < len(statement) {
			next = statement[index+1]
		}
		switch state {
		case '\'', '"', '`', ']':
			if current == '\\' && escapeBackslash {
				index++
				continue
			}
			if current == state {
				if next == state {
					index++
				} else {
					state = 0
				}
			}
			continue
		case '-':
			if current == '\n' || current == '\r' {
				state = 0
				lineCommentStart = -1
			}
			continue
		case '/':
			if current == '/' && next == '*' && (dialect.postgres || dialect.sqlserver || dialect.clickhouse) {
				blockDepth++
				index++
				continue
			}
			if current == '*' && next == '/' {
				blockDepth--
				index++
				if blockDepth == 0 {
					state = 0
				}
			}
			continue
		}
		if current == '#' && (dialect.mysql || (dialect.clickhouse && (next == '!' || next == ' '))) {
			flush()
			lineCommentStart = index
			state = '-'
			continue
		}
		if (current == '-' && next == '-' && (!dialect.mysql || mysqlDashComment(statement, index))) ||
			(dialect.clickhouse && current == '/' && next == '/') {
			flush()
			lineCommentStart = index
			state = '-'
			index++
			continue
		}
		if current == '/' && next == '*' {
			// Executable comments must not hide statements from the execution policy.
			if dialect.mysql && (strings.HasPrefix(statement[index:], "/*!") || strings.HasPrefix(statement[index:], "/*M!")) {
				err = fmt.Errorf("executable SQL comments are unsupported")
			}
			flush()
			state = '/'
			blockDepth = 1
			index++
			continue
		}
		if current == ';' {
			flush()
			if semicolon {
				trailingOnly = false
			}
			semicolon = true
			continue
		}
		if semicolon && !unicode.IsSpace(rune(current)) {
			trailingOnly = false
		}
		if current == '$' && word.Len() == 0 && (dialect.postgres || dialect.clickhouse) {
			if delimiter := sqlDollarDelimiter(statement, index); delimiter != "" {
				end := strings.Index(statement[index+len(delimiter):], delimiter)
				if end < 0 {
					incomplete = true
					break
				}
				index += len(delimiter)*2 + end - 1
				continue
			}
		}
		if dialect.oracle && word.Len() == 0 && (current == 'q' || current == 'Q') && next == '\'' && index+2 < len(statement) {
			endQuote := statement[index+2]
			switch endQuote {
			case '[':
				endQuote = ']'
			case '{':
				endQuote = '}'
			case '(':
				endQuote = ')'
			case '<':
				endQuote = '>'
			}
			end := strings.Index(statement[index+3:], string(endQuote)+"'")
			if end < 0 {
				incomplete = true
				break
			}
			index += end + 4
			continue
		}
		if current == '\'' || current == '"' || current == '`' || (dialect.sqlserver && current == '[') {
			escapeBackslash = (dialect.mysql && current != '`') || dialect.clickhouse ||
				(dialect.postgres && current == '\'' && strings.EqualFold(word.String(), "E"))
			flush()
			state = current
			if current == '[' {
				state = ']'
			}
			if dialect.sqlserver && (current == '[' || current == '"') && next == '#' {
				words = append(words, "#")
				depths = append(depths, sqlDepth)
			}
			if current == '`' && !dialect.mysql && !dialect.clickhouse {
				incomplete = true
			}
			continue
		}
		if unicode.IsLetter(rune(current)) || unicode.IsDigit(rune(current)) ||
			current == '_' || current == '$' || (dialect.sqlserver && current == '#') ||
			((dialect.mysql || dialect.sqlserver) && current == '@') {
			if word.Len() == 0 {
				wordDepth = sqlDepth
			}
			word.WriteByte(current)
		} else {
			flush()
			switch current {
			case '(':
				sqlDepth++
			case ')':
				if sqlDepth > 0 {
					sqlDepth--
				} else {
					incomplete = true
				}
			}
		}
	}
	flush()
	incomplete = incomplete || (state != 0 && state != '-') || sqlDepth != 0
	return
}

func mysqlDashComment(statement string, index int) bool {
	next := index + 2
	return next >= len(statement) || unicode.IsSpace(rune(statement[next]))
}

func rootSQLKeyword(words []string, depths []int) string {
	if len(words) == 0 {
		return ""
	}
	if words[0] != "WITH" {
		return words[0]
	}
	for index, word := range words[1:] {
		if index+1 >= len(depths) || depths[index+1] != 0 {
			continue
		}
		switch word {
		case "SELECT", "SHOW", "DESC", "DESCRIBE", "EXPLAIN",
			"INSERT", "UPDATE", "DELETE", "REPLACE", "MERGE", "VALUES", "TABLE":
			return word
		}
	}
	return ""
}

func containsSQLWord(words []string, target string) bool {
	for _, word := range words {
		if word == target {
			return true
		}
	}
	return false
}

func containsTopLevelSQLWord(words []string, depths []int, target string) bool {
	for index, word := range words {
		if word == target && index < len(depths) && depths[index] == 0 {
			return true
		}
	}
	return false
}

func containsSQLSequence(words []string, sequence ...string) bool {
	if len(sequence) == 0 || len(words) < len(sequence) {
		return false
	}
	for index := 0; index <= len(words)-len(sequence); index++ {
		matched := true
		for offset := range sequence {
			if words[index+offset] != sequence[offset] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
