package sqlservice

import (
	"database/sql"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

// Overview selects column names for one dictionary table. The diagram selects
// table/column pairs. Keep their original keys in each cursor fingerprint.
func readMySQLCatalogFields(rows *sql.Rows, limit int, qualified bool) (map[string]bool, []string, error) {
	defer rows.Close()
	var table, column string
	dest := []any{&column}
	budgetError := "overview capability budget exceeded"
	if qualified {
		dest = []any{&table, &column}
		budgetError = "diagram capability budget exceeded"
	}
	valid := func(name string) bool {
		return utf8.ValidString(name) && utf8.RuneCountInString(name) <= 128
	}
	fields := make(map[string]bool)
	var names []string
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return nil, nil, err
		}
		if len(names) >= limit || !valid(table) || !valid(column) {
			return nil, nil, errors.New(budgetError)
		}
		key := strings.ToUpper(column)
		if qualified {
			key = strings.ToUpper(table) + "." + key
		}
		fields[key] = true
		names = append(names, key)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	sort.Strings(names)
	return fields, names, nil
}
