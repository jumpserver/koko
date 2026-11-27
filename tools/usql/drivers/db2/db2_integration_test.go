package db2

import (
	"context"
	"database/sql"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
	"github.com/xo/usql/drivers/metadata"
)

// This test uses only read-only SQL and is opt-in to avoid requiring a live
// server or credentials during normal builds. The private DSN file is external
// to the repository and is never logged.
func TestLiveDB2(t *testing.T) {
	path := os.Getenv("USQL_DB2_TEST_DSN_FILE")
	if path == "" {
		t.Skip("set USQL_DB2_TEST_DSN_FILE to a private file containing a Db2 URL")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	u, err := dburl.Parse(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	discard := func() io.Writer { return io.Discard }
	db, err := drivers.Open(context.Background(), u, discard, discard)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	t.Run("User", func(t *testing.T) {
		var user string
		if err := db.QueryRowContext(ctx, "VALUES CURRENT USER").Scan(&user); err != nil {
			t.Fatal(err)
		}
		if !strings.EqualFold(strings.TrimSpace(user), u.User.Username()) {
			t.Fatal("server returned a different authenticated user")
		}
	})
	t.Run("ScalarTypes", func(t *testing.T) {
		var integer int64
		var large int64
		var decimal string
		var unicode string
		var nullable sql.NullString
		err := db.QueryRowContext(ctx, `VALUES (CAST(42 AS INTEGER), CAST(1234567890123 AS BIGINT), CAST(123.45 AS DECIMAL(9,2)), CAST('中文测试' AS VARCHAR(60)), CAST(NULL AS VARCHAR(20)))`).Scan(&integer, &large, &decimal, &unicode, &nullable)
		if err != nil {
			t.Fatal(err)
		}
		if integer != 42 || large != 1234567890123 || decimal != "123.45" || unicode != "中文测试" || nullable.Valid {
			t.Fatalf("incorrect scalar result: %d, %d, %q, %q, null=%v", integer, large, decimal, unicode, nullable.Valid)
		}
	})
	t.Run("PreparedQuery", func(t *testing.T) {
		stmt, err := db.PrepareContext(ctx, "SELECT CAST(? AS INTEGER) + 1 FROM SYSIBM.SYSDUMMY1")
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close()
		var answer int
		if err := stmt.QueryRowContext(ctx, 41).Scan(&answer); err != nil {
			t.Fatal(err)
		}
		if answer != 42 {
			t.Fatalf("result = %d, want 42", answer)
		}
	})
	t.Run("RollbackReadTransaction", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		var answer int
		if err := tx.QueryRowContext(ctx, "VALUES 7").Scan(&answer); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if answer != 7 {
			t.Fatalf("transaction result = %d", answer)
		}
	})
	t.Run("CatalogMetadata", func(t *testing.T) {
		var literalCount, parameterCount int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM SYSCAT.SCHEMATA WHERE RTRIM(SCHEMANAME) LIKE 'SYSIBM'").Scan(&literalCount); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM SYSCAT.SCHEMATA WHERE RTRIM(SCHEMANAME) LIKE ?", "SYSIBM").Scan(&parameterCount); err != nil {
			t.Fatal(err)
		}
		if literalCount != 1 || literalCount != parameterCount {
			t.Fatalf("string binding mismatch: literal=%d, parameter=%d", literalCount, parameterCount)
		}
		r := newMetadataReader(db, metadata.WithTimeout(5*time.Second)).(*metadataReader)
		schemas, err := r.Schemas(metadata.Filter{Name: "SYSIBM", WithSystem: true})
		if err != nil {
			t.Fatal(err)
		}
		defer schemas.Close()
		if schemas.Len() != 1 {
			t.Fatalf("SYSIBM schema count = %d", schemas.Len())
		}
		tables, err := r.Tables(metadata.Filter{Schema: "SYSIBM", Name: "SYSDUMMY1", Types: []string{"VIEW"}, WithSystem: true})
		if err != nil {
			t.Fatal(err)
		}
		defer tables.Close()
		if tables.Len() != 1 {
			t.Fatalf("SYSDUMMY1 table count = %d", tables.Len())
		}
		columns, err := r.Columns(metadata.Filter{Schema: "SYSIBM", Parent: "SYSDUMMY1", WithSystem: true})
		if err != nil {
			t.Fatal(err)
		}
		defer columns.Close()
		if !columns.Next() || columns.Get().Name != "IBMREQD" {
			t.Fatal("missing SYSDUMMY1.IBMREQD column metadata")
		}
	})
}
