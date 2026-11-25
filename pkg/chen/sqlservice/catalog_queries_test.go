package sqlservice

import (
	"testing"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

func TestCatalogObjectNameFilter(t *testing.T) {
	for _, tc := range []struct {
		d            dialect.ID
		query        string
		prefix       bool
		sql, pattern string
	}{
		{dialect.SQLServer, "a!%_", true, " AND LOWER(objectname) LIKE LOWER(@p2) ESCAPE '!'", "a!!!%!_%"},
		{dialect.Oracle, "a_b", false, " AND LOWER(objectname) LIKE LOWER(:2) ESCAPE '!'", "%a!_b%"},
		{dialect.DB2, "a%b", false, " AND LOWER(objectname) LIKE LOWER(CAST(? AS VARCHAR(1024))) ESCAPE '!'", "%a!%b%"},
		{dialect.ClickHouse, `a\%_`, true, " AND lowerUTF8(objectname) LIKE lowerUTF8(?)", `a\\\%\_%`},
		{dialect.ClickHouse, "a_b", false, " AND lowerUTF8(objectname) LIKE lowerUTF8(?)", `%a\_b%`},
		{dialect.Oracle, "", false, "", ""},
	} {
		t.Run(string(tc.d)+"/"+tc.query, func(t *testing.T) {
			b := catalogBinder{dialect: tc.d}
			b.bind("scope")
			if got := b.objectNameFilter(tc.query, tc.prefix); got != tc.sql {
				t.Fatalf("SQL = %q, want %q", got, tc.sql)
			}
			if tc.query == "" {
				if len(b.args) != 1 {
					t.Fatalf("empty query added a binding: %v", b.args)
				}
			} else if len(b.args) != 2 || b.args[1] != tc.pattern {
				t.Fatalf("bound arguments = %v, want [scope %q]", b.args, tc.pattern)
			}
		})
	}
}
