package postgres

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/xo/dburl"
)

// Only the disposable loopback tls_test fixture is supported, never production.
func TestLivePostgresTLSModesAndCancellation(t *testing.T) {
	dir, port := os.Getenv("USQL_SQL_TLS_TEST_DIR"), os.Getenv("USQL_SQL_TLS_TEST_POSTGRES_PORT")
	if dir == "" || port == "" {
		t.Skip("set the isolated USQL_SQL_TLS_TEST_DIR/POSTGRES_PORT fixture")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		t.Fatal("invalid fixture port")
	}
	for _, mode := range []string{"verify-full", "verify-ca", "require", "prefer"} {
		t.Run(mode, func(t *testing.T) {
			host := "database.test"
			if mode != "verify-full" {
				host = "different-identity.example"
			}
			q := url.Values{"sslmode": {mode}, "sslrootcert": {filepath.Join(dir, "ca.crt")}, "hostaddr": {"127.0.0.1"}, "connect_timeout": {"5"}}
			u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), Path: "/tls_test",
				User: url.UserPassword("postgres", "FixtureTLS@2026"), RawQuery: q.Encode()}
			parsed, err := dburl.Parse(u.String())
			if err != nil {
				t.Fatal("fixture URL rejected")
			}
			connector, err := managedConnector(parsed)
			if err != nil {
				t.Fatal("TLS connector rejected")
			}
			db := sql.OpenDB(connector)
			defer db.Close()
			ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			var encrypted bool
			if err := db.QueryRowContext(ctx, "SELECT ssl FROM pg_stat_ssl WHERE pid=pg_backend_pid()").Scan(&encrypted); err != nil || !encrypted {
				t.Fatal("database connection is not encrypted")
			}
			wait, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			started := time.Now()
			_, err = db.ExecContext(wait, "SELECT pg_sleep(5)")
			cancel()
			if err == nil || time.Since(started) > 3*time.Second {
				t.Fatal("TLS query cancellation did not return promptly")
			}
			// lib/pq discards a canceled connection; the replacement must retain
			// the original TLS files, identity and tunnel address too.
			if err := db.QueryRowContext(ctx, "SELECT ssl FROM pg_stat_ssl WHERE pid=pg_backend_pid()").Scan(&encrypted); err != nil || !encrypted {
				t.Fatal("TLS reconnect after cancellation failed")
			}
		})
	}
}
