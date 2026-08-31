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
	kind       sqlKind
	keyword    string
	words      []string
	depths     []int
	multi      bool
	incomplete bool
}

func (a sqlAnalysis) BackgroundEligible() bool {
	return !a.multi && !a.incomplete && (a.kind == sqlRead || a.kind == sqlWrite)
}

func (a sqlAnalysis) RequiresApproval() bool {
	if a.kind == sqlWrite || a.kind == sqlUnknown {
		return true
	}
	if a.kind != sqlRead || a.keyword != "EXPLAIN" ||
		!containsSQLWord(a.words, "ANALYZE") {
		return false
	}
	for _, keyword := range []string{
		"INSERT", "UPDATE", "DELETE", "REPLACE", "CREATE", "ALTER",
		"DROP", "TRUNCATE", "GRANT", "REVOKE",
	} {
		if containsSQLWord(a.words, keyword) {
			return true
		}
	}
	return false
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

func analyzeSQL(statement string) (sqlAnalysis, error) {
	words, depths, semicolon, trailingOnly, incomplete := scanSQL(statement)
	analysis := sqlAnalysis{
		words: words, depths: depths,
		multi: semicolon && !trailingOnly, incomplete: incomplete,
	}
	if len(words) == 0 {
		return analysis, fmt.Errorf("model generated an empty SQL statement")
	}
	analysis.keyword = rootSQLKeyword(words, depths)
	switch analysis.keyword {
	case "SELECT", "SHOW", "DESC", "DESCRIBE", "EXPLAIN":
		analysis.kind = sqlRead
	case "INSERT", "UPDATE", "DELETE", "REPLACE", "CREATE", "ALTER",
		"DROP", "TRUNCATE", "RENAME", "GRANT", "REVOKE", "ANALYZE",
		"OPTIMIZE", "REPAIR", "LOAD", "KILL", "SHUTDOWN", "RESET",
		"PURGE", "FLUSH", "INSTALL", "UNINSTALL":
		analysis.kind = sqlWrite
	case "USE", "SET", "BEGIN", "START", "COMMIT", "ROLLBACK",
		"SAVEPOINT", "RELEASE", "LOCK", "UNLOCK":
		analysis.kind = sqlSession
	default:
		analysis.kind = sqlUnknown
	}
	if containsSQLSequence(words, "CREATE", "TEMPORARY", "TABLE") ||
		containsSQLSequence(words, "DROP", "TEMPORARY", "TABLE") ||
		containsSQLWord(words, "GET_LOCK") ||
		containsSQLWord(words, "RELEASE_LOCK") ||
		containsSQLSequence(words, "FOR", "UPDATE") ||
		containsSQLSequence(words, "LOCK", "IN", "SHARE", "MODE") {
		analysis.kind = sqlSession
	}
	if analysis.keyword == "SELECT" &&
		(containsSQLSequence(words, "INTO", "OUTFILE") ||
			containsSQLSequence(words, "INTO", "DUMPFILE")) {
		analysis.kind = sqlWrite
	}
	return analysis, nil
}

func isSchemaChangingSQL(analysis sqlAnalysis) bool {
	switch analysis.keyword {
	case "CREATE", "ALTER", "DROP", "TRUNCATE", "RENAME":
		return true
	default:
		return false
	}
}

func scanSQL(statement string) (
	words []string, depths []int, semicolon, trailingOnly, incomplete bool,
) {
	var word strings.Builder
	state := byte(0)
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
		case '\'':
			if current == '\\' {
				index++
				continue
			}
			if current == '\'' {
				if next == '\'' {
					index++
				} else {
					state = 0
				}
			}
			continue
		case '"':
			if current == '\\' {
				index++
				continue
			}
			if current == '"' {
				if next == '"' {
					index++
				} else {
					state = 0
				}
			}
			continue
		case '`':
			if current == '`' {
				if next == '`' {
					index++
				} else {
					state = 0
				}
			}
			continue
		case '#':
			if current == '\n' {
				state = 0
			}
			continue
		case '-':
			if current == '\n' {
				state = 0
			}
			continue
		case '/':
			if current == '/' && next == '*' {
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
		if current == '\'' || current == '"' || current == '`' {
			flush()
			state = current
			continue
		}
		if current == '#' {
			flush()
			state = '#'
			continue
		}
		if current == '-' && next == '-' && mysqlDashComment(statement, index) {
			flush()
			state = '-'
			index++
			continue
		}
		if current == '/' && next == '*' {
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
		if unicode.IsLetter(rune(current)) || unicode.IsDigit(rune(current)) ||
			current == '_' || current == '$' {
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
				}
			}
		}
	}
	flush()
	incomplete = state == '\'' || state == '"' || state == '`' || state == '/'
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
			"INSERT", "UPDATE", "DELETE", "REPLACE":
			return word
		}
	}
	return ""
}

func classifySQLRisk(analysis sqlAnalysis, level int, reason string) (int, string) {
	level, reason = normalizeRisk(level, reason)
	raise := func(minimum int, cause string) {
		if level < minimum {
			level, reason = minimum, cause
		}
	}
	switch analysis.kind {
	case sqlRead:
		if containsSQLWord(analysis.words, "LOAD_FILE") {
			raise(4, "backend rule detected SQL reading a server-side file")
		} else if level < 1 {
			level = 1
		}
	case sqlSession:
		switch {
		case containsSQLWord(analysis.words, "PASSWORD"):
			raise(4, "backend rule detected security-sensitive password SQL")
		case containsSQLWord(analysis.words, "GLOBAL"),
			containsSQLWord(analysis.words, "PERSIST"),
			containsSQLWord(analysis.words, "PERSIST_ONLY"):
			raise(3, "backend rule detected global database configuration SQL")
		case containsSQLWord(analysis.words, "LOCK"),
			containsSQLWord(analysis.words, "REPLICA"),
			containsSQLWord(analysis.words, "SLAVE"):
			raise(3, "backend rule detected locking or replication SQL")
		default:
			raise(2, "backend rule detected session-dependent SQL")
		}
	case sqlUnknown:
		raise(2, "backend could not safely classify the SQL statement")
	case sqlWrite:
		switch analysis.keyword {
		case "DROP", "TRUNCATE", "GRANT", "REVOKE", "KILL", "SHUTDOWN",
			"RESET", "PURGE":
			raise(4, "backend rule detected destructive or security-sensitive SQL")
		case "SELECT":
			raise(4, "backend rule detected SQL writing query results to a server file")
		case "UPDATE", "DELETE":
			if !containsTopLevelSQLWord(analysis.words, analysis.depths, "WHERE") {
				raise(4, "backend rule detected UPDATE or DELETE without a WHERE clause")
			} else {
				raise(2, "backend rule detected data-changing SQL")
			}
		case "CREATE", "ALTER", "RENAME":
			if containsSQLWord(analysis.words, "USER") ||
				containsSQLWord(analysis.words, "ROLE") ||
				(containsSQLWord(analysis.words, "FUNCTION") &&
					containsSQLWord(analysis.words, "SONAME")) {
				raise(4, "backend rule detected security-sensitive SQL")
			} else {
				raise(3, "backend rule detected schema-changing SQL")
			}
		case "INSTALL", "UNINSTALL":
			raise(4, "backend rule detected security-sensitive plugin SQL")
		case "LOAD", "FLUSH":
			raise(3, "backend rule detected administrative or material-impact SQL")
		default:
			raise(2, "backend rule detected data-changing SQL")
		}
	}
	return level, reason
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
