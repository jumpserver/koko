package sqlservice

import (
	"strings"
	"unicode/utf8"
)

// ClickHouseUseDatabase recognizes a complete USE statement for synchronizing
// an HTTP connection's database after the server has successfully executed it.
// It does not validate database permissions or replace server SQL validation.
func ClickHouseUseDatabase(query string) (string, bool) {
	if !utf8.ValidString(query) {
		return "", false
	}
	statement := []token{}
	for _, t := range lex(query, ClickHouse) {
		if t.kind == 'c' {
			if strings.HasPrefix(t.text, "/*") && !t.closed {
				return "", false
			}
			continue
		}
		if t.quoted && !t.closed {
			return "", false
		}
		statement = append(statement, t)
	}
	if len(statement) == 3 && statement[2].kind == ';' {
		statement = statement[:2]
	}
	if len(statement) != 2 || !statement[0].is("USE") || !statement[1].ident() {
		return "", false
	}
	database := statement[1].name(ClickHouse)
	if database == "" || !utf8.ValidString(database) || strings.ContainsRune(database, 0) {
		return "", false
	}
	return database, true
}
