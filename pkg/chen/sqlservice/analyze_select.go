package sqlservice

import (
	"fmt"
	"strings"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlanalysis"

	"github.com/pingcap/tidb/pkg/parser/mysql"
)

// analyzeSelectDocument is intentionally stricter than interactive execution.
// Export and estimated-plan wrappers must not turn a write into a read query.
func analyzeSelectDocument(d dialect.ID, document string, options AnalyzeOptions) (Analysis, error) {
	return scanCommandDocument(d, document, options, func(statement []token) (string, bool, bool, error) {
		kind, at, err := commandHead(statement)
		if err != nil {
			return "", false, false, err
		}
		if err := validateCommandBoundaries(d, document, statement, kind, at); err != nil {
			return "", false, false, err
		}
		first := statement[0]
		if !first.is("SELECT") && !first.is("WITH") {
			return "", false, false, fmt.Errorf("%s requires SELECT or WITH read queries at byte %d", d, first.start)
		}
		hasSelect := false
		for i, t := range statement {
			if t.kind != 'i' || t.quoted {
				continue
			}
			if clickHouseSystemQualifier(d, statement, i) {
				continue
			}
			if t.is("SELECT") {
				hasSelect = true
			}
			switch strings.ToUpper(t.value) {
			case "INSERT", "UPDATE", "DELETE", "MERGE", "UPSERT", "REPLACE", "CREATE", "ALTER", "DROP", "TRUNCATE", "GRANT", "REVOKE", "CALL", "EXEC", "EXECUTE", "INTO", "OUTFILE", "DUMPFILE", "LOCK", "UNLOCK", "ATTACH", "DETACH", "SYSTEM", "GO", "DELIMITER":
				return "", false, false, fmt.Errorf("%s read queries do not support %s at byte %d", d, t.value, t.start)
			case "NEXTVAL", "SETVAL":
				return "", false, false, fmt.Errorf("sequence mutation is not supported in read queries")
			case "NEXT":
				if i+1 < len(statement) && statement[i+1].is("VALUE") {
					return "", false, false, fmt.Errorf("sequence mutation is not supported in read queries")
				}
			}
		}
		if !hasSelect {
			return "", false, false, fmt.Errorf("WITH query must contain SELECT")
		}
		return "select", true, false, nil
	})
}

// scanCommandDocument validates lexical boundaries without rewriting SQL. The
// callback classifies each whole statement before any SQL reaches a connection.
func scanCommandDocument(d dialect.ID, document string, options AnalyzeOptions, classify func([]token) (string, bool, bool, error)) (Analysis, error) {
	out := Analysis{Statements: []Statement{}, Syntax: sqlanalysis.Unknown, Compatibility: sqlanalysis.Unknown, Diagnostics: []sqlanalysis.Diagnostic{{Code: sqlanalysis.Unsupported, Message: "Only lexical statement boundaries were checked; dialect syntax requires server validation"}}}
	if d == dialect.Dameng || d == dialect.OceanBaseOracle {
		out.Diagnostics = append(out.Diagnostics, sqlanalysis.Diagnostic{Code: sqlanalysis.ProductUnknown, Message: "Product version and Oracle compatibility mode have not been verified"})
	}
	mode, err := mysql.GetSQLMode(options.MySQLSQLMode)
	if err != nil {
		return out, err
	}
	tokens := lexMode(document, d, mode.HasANSIQuotesMode(), mode.HasNoBackslashEscapesMode())
	start, depth := -1, 0
	statement := []token{}
	appendStatement := func(end int) error {
		if len(statement) == 0 {
			return nil
		}
		kind, rows, write, err := classify(statement)
		if err != nil {
			return err
		}
		out.Statements = append(out.Statements, Statement{SQL: document[start:end], Start: start, End: end, Kind: kind, ReturnsRows: rows, MayWrite: write})
		statement = nil
		start = -1
		return nil
	}
	for _, t := range tokens {
		if t.kind == 'c' {
			if strings.HasPrefix(t.text, "/*") && !t.closed {
				return out, fmt.Errorf("unterminated SQL comment at byte %d", t.start)
			}
			// Executable comments are a MySQL/MariaDB server extension. The
			// native MySQL parser handles them; this fallback must not skip SQL.
			if strings.HasPrefix(t.text, "/*!") || strings.HasPrefix(t.text, "/*M!") {
				return out, fmt.Errorf("executable SQL comments require native parser support")
			}
			continue
		}
		if (t.quoted || t.kind == 's') && !t.closed {
			return out, fmt.Errorf("unterminated SQL quote at byte %d", t.start)
		}
		if t.kind == ';' {
			if depth != 0 {
				return out, fmt.Errorf("statement terminator inside parentheses")
			}
			if err := appendStatement(t.start); err != nil {
				return out, err
			}
			continue
		}
		if start < 0 {
			start = t.start
		}
		switch t.kind {
		case '(':
			depth++
			if depth > 128 {
				return out, fmt.Errorf("SQL nesting exceeds 128 levels")
			}
		case ')':
			depth--
			if depth < 0 {
				return out, fmt.Errorf("unmatched closing parenthesis at byte %d", t.start)
			}
		}
		statement = append(statement, t)
	}
	if depth != 0 {
		return out, fmt.Errorf("unclosed SQL parenthesis")
	}
	if err := appendStatement(len(document)); err != nil {
		return out, err
	}
	return out, nil
}
