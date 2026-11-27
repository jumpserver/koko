package handler

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jumpserver/koko/tools/usql/metacmd"
	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
	"github.com/xo/usql/rline"
)

func TestSQLServerSession(t *testing.T) {
	for _, scheme := range []string{"sqlserver", "mssql"} {
		t.Run(scheme, func(t *testing.T) {
			h, connector := openSessionHandler(t, scheme)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var database string
			var spid int64
			if err := h.DB().QueryRowContext(ctx, "SELECT DB_NAME(), @@SPID").Scan(&database, &spid); err != nil {
				t.Fatal(err)
			}
			assertDatabase := func(want string) {
				t.Helper()
				var got string
				var id int64
				if err := h.DB().QueryRowContext(ctx, "SELECT DB_NAME(), @@SPID").Scan(&got, &id); err != nil {
					t.Fatal(err)
				}
				if got != want || id != spid {
					t.Fatalf("database/session = %s/%d, want %s/%d", got, id, want, spid)
				}
			}
			if err := h.Execute(ctx, io.Discard, metacmd.Option{}, "USE", "USE iccache;", false); err != nil {
				t.Fatal(err)
			}
			assertDatabase("iccache")
			for _, finish := range []func() error{h.Commit, h.Rollback} {
				if err := h.BeginTx(ctx, nil); err != nil {
					t.Fatal(err)
				}
				assertDatabase("iccache")
				if err := finish(); err != nil {
					t.Fatal(err)
				}
				assertDatabase("iccache")
			}
			if _, err := h.DB().Exec("USE forbidden;"); err == nil {
				t.Fatal("switch to a forbidden database succeeded")
			}
			assertDatabase("iccache")
			if connector.opened.Load() != 1 {
				t.Fatal("SQL Server session used multiple connections")
			}
			db := h.db
			if err := h.Close(); err != nil {
				t.Fatal(err)
			}
			if db.Stats().OpenConnections != 0 || connector.closed.Load() != 1 {
				t.Fatal("Close did not release the SQL Server connection")
			}
		})
	}
}

func TestSQLServerIncludeSharesSessionAndTransaction(t *testing.T) {
	h, _ := openSessionHandler(t, "sqlserver")
	if err := h.Begin(nil); err != nil {
		t.Fatal(err)
	}
	tx := h.tx
	script := filepath.Join(t.TempDir(), "switch.sql")
	if err := os.WriteFile(script, []byte("USE scriptdb;\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Include(script, false); err != nil {
		t.Fatal(err)
	}
	if h.tx != tx {
		t.Fatal("included script lost the active transaction")
	}
	if err := h.Commit(); err != nil {
		t.Fatal(err)
	}
	var database string
	var spid int64
	if err := h.DB().QueryRow("SELECT DB_NAME(), @@SPID").Scan(&database, &spid); err != nil {
		t.Fatal(err)
	}
	if database != "scriptdb" || spid != 1 {
		t.Fatalf("included script database/session = %s/%d, want scriptdb/1", database, spid)
	}
}

func TestSQLServerReopenClosesPreviousSession(t *testing.T) {
	h, connector := openSessionHandler(t, "sqlserver")
	previous := h.db
	if err := h.Open(context.Background(), "sqlserver://localhost/master"); err != nil {
		t.Fatal(err)
	}
	if previous.Stats().OpenConnections != 0 || connector.closed.Load() != 1 {
		t.Fatal("reopen leaked the previous connection")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.Open(ctx, "sqlserver://localhost/master"); err == nil {
		t.Fatal("open with a canceled context succeeded")
	}
	if h.db != nil || connector.closed.Load() != 2 {
		t.Fatal("failed open left a connection behind")
	}
}

func TestOtherDriversKeepConnectionPool(t *testing.T) {
	h, _ := openSessionHandler(t, "postgres")
	if h.DB() != h.db {
		t.Fatal("non-SQL Server driver no longer uses its connection pool")
	}
}

func TestSQLServerLostSessionRequiresReconnect(t *testing.T) {
	h, connector := openSessionHandler(t, "sqlserver")
	if _, err := h.DB().Exec("USE iccache;"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DB().Exec("BREAK"); !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("connection loss error = %v, want ErrBadConn", err)
	}
	if err := h.DB().QueryRow("SELECT DB_NAME(), @@SPID").Err(); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("lost session query error = %v, want ErrConnDone", err)
	}
	if connector.opened.Load() != 1 {
		t.Fatal("lost session silently reconnected to the login database")
	}
	if err := h.Open(context.Background(), "sqlserver://localhost/master"); err != nil {
		t.Fatalf("explicit reconnect failed: %v", err)
	}
}

func openSessionHandler(t *testing.T, scheme string) (*Handler, *sessionConnector) {
	t.Helper()
	u, err := dburl.Parse(scheme + "://localhost/master")
	if err != nil {
		t.Fatal(err)
	}
	connector := &sessionConnector{}
	available := drivers.Available()
	original, exists := available[u.Driver]
	available[u.Driver] = drivers.Driver{
		Open: func(context.Context, *dburl.URL, func() io.Writer, func() io.Writer) (func(string, string) (*sql.DB, error), error) {
			return func(string, string) (*sql.DB, error) { return sql.OpenDB(connector), nil }, nil
		},
	}
	t.Cleanup(func() {
		if exists {
			available[u.Driver] = original
		} else {
			delete(available, u.Driver)
		}
	})
	h := New(&rline.Rline{Out: io.Discard, Err: io.Discard}, &user.User{HomeDir: t.TempDir()}, t.TempDir(), true)
	t.Cleanup(func() { _ = h.Rollback(); _ = h.Close() })
	if err := h.Open(context.Background(), u.String()); err != nil {
		t.Fatal(err)
	}
	return h, connector
}

// Model SQL Server's pool reset even when database/sql reuses the same physical
// connection: USE must survive a separate query and transaction checkout.
type sessionConnector struct{ opened, closed atomic.Int64 }

func (c *sessionConnector) Connect(context.Context) (driver.Conn, error) {
	return &sessionDriverConn{connector: c, database: "master", spid: c.opened.Add(1)}, nil
}
func (*sessionConnector) Driver() driver.Driver { return sessionDriver{} }

type sessionDriver struct{}

func (sessionDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type sessionDriverConn struct {
	connector *sessionConnector
	database  string
	spid      int64
}

func (*sessionDriverConn) Prepare(string) (driver.Stmt, error)  { return nil, errors.New("unsupported") }
func (c *sessionDriverConn) Close() error                       { c.connector.closed.Add(1); return nil }
func (c *sessionDriverConn) Begin() (driver.Tx, error)          { return c, nil }
func (*sessionDriverConn) Commit() error                        { return nil }
func (*sessionDriverConn) Rollback() error                      { return nil }
func (c *sessionDriverConn) ResetSession(context.Context) error { c.database = "master"; return nil }
func (c *sessionDriverConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if query == "BREAK" {
		return nil, driver.ErrBadConn
	}
	name := strings.TrimSuffix(strings.TrimPrefix(query, "USE "), ";")
	if name == "forbidden" {
		return nil, errors.New("CONNECT permission denied")
	}
	c.database = name
	return driver.RowsAffected(0), nil
}
func (c *sessionDriverConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &sessionRows{values: []driver.Value{c.database, c.spid}}, nil
}

type sessionRows struct{ values []driver.Value }

func (*sessionRows) Columns() []string { return []string{"database", "spid"} }
func (*sessionRows) Close() error      { return nil }
func (r *sessionRows) Next(values []driver.Value) error {
	if r.values == nil {
		return io.EOF
	}
	copy(values, r.values)
	r.values = nil
	return nil
}
