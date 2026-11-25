package sqlservice

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jumpserver/koko/pkg/chen/dialect"
)

type versionReply struct {
	query, value string
	err          error
}

type versionConn struct {
	driver.Conn
	replies []versionReply
}

func (c *versionConn) Connect(context.Context) (driver.Conn, error) { return c, nil }
func (*versionConn) Driver() driver.Driver                          { return nil }
func (*versionConn) Close() error                                   { return nil }
func (c *versionConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if len(c.replies) == 0 || query != c.replies[0].query {
		return nil, fmt.Errorf("unexpected version query %q", query)
	}
	reply := c.replies[0]
	c.replies = c.replies[1:]
	if reply.err != nil {
		return nil, reply.err
	}
	return &versionRow{value: reply.value}, nil
}

type versionRow struct{ value driver.Value }

func (*versionRow) Columns() []string { return []string{"version"} }
func (*versionRow) Close() error      { return nil }
func (r *versionRow) Next(dest []driver.Value) error {
	if r.value == nil {
		return io.EOF
	}
	dest[0], r.value = r.value, nil
	return nil
}

func probeTestVersion(t *testing.T, d dialect.ID, replies ...versionReply) (MySQLServerVersion, error) {
	t.Helper()
	c := &versionConn{replies: replies}
	db := sql.OpenDB(c)
	defer db.Close()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	v, err := ReadMySQLServerVersion(context.Background(), conn, d)
	if len(c.replies) != 0 {
		t.Errorf("unread version query %q", c.replies[0].query)
	}
	return v, err
}

func TestReadMySQLServerVersion(t *testing.T) {
	const reported = "SELECT VERSION()"
	const comment = "SELECT @@version_comment"
	for _, tc := range []struct {
		d                                   dialect.ID
		raw, fallback, product, productFrom string
		mariaDB, oceanBase                  bool
	}{
		{dialect.MySQL, "8.0.31", "", "8.0.31", reported, false, false},
		{dialect.MariaDB, "5.5.5-10.11.2-mariadb", "", "5.5.5-10.11.2-mariadb", reported, true, false},
		{dialect.OceanBaseMySQL, "5.7.25-OceanBase-v4.3.5", "", "5.7.25-OceanBase-v4.3.5", reported, false, true},
		{dialect.OceanBaseMySQL, "5.7.25", "OceanBase 4.3.5", "OceanBase 4.3.5", comment, false, true},
		{dialect.OceanBaseMySQL, "5.7.25", "Community", "5.7.25", reported, false, false},
		{dialect.OceanBaseMySQL, "5.7.25-MariaDB", "OceanBase 4.3.5", "OceanBase 4.3.5", comment, false, true},
		{dialect.OceanBaseMySQL, "MariaDB-OceanBase-v4.2.4", "", "MariaDB-OceanBase-v4.2.4", reported, true, true},
	} {
		replies := []versionReply{{query: reported, value: tc.raw}}
		if tc.fallback != "" {
			replies = append(replies, versionReply{query: comment, value: tc.fallback})
		}
		v, err := probeTestVersion(t, tc.d, replies...)
		if err != nil || v.Reported != tc.raw || v.Product != tc.product || v.Source.Query() != tc.productFrom || v.FromVersionComment() != (tc.productFrom == comment) {
			t.Errorf("%s %q + %q: got %+v, %v", tc.d, tc.raw, tc.fallback, v, err)
		}
		if v.ReportsMariaDB() != tc.mariaDB || v.ReportsOceanBase() != tc.oceanBase {
			t.Errorf("unexpected brand evidence for %q", tc.product)
		}
	}
}

func TestReadMySQLServerVersionErrors(t *testing.T) {
	unsupported := &mysql.MySQLError{Number: 1193, Message: "Unknown system variable"}
	for _, tc := range []struct {
		optional bool
		cause    error
	}{
		{false, driver.ErrBadConn},
		{true, unsupported},
		{true, context.Canceled},
		{true, context.DeadlineExceeded},
		{true, driver.ErrBadConn},
	} {
		const primary = "SELECT VERSION()"
		failure := versionReply{query: primary, err: fmt.Errorf("read version: %w", tc.cause)}
		var replies []versionReply
		var wantRaw, wantSource string
		if tc.optional {
			wantRaw, wantSource = "5.7.25", primary
			replies = append(replies, versionReply{query: primary, value: wantRaw})
			failure.query = "SELECT @@version_comment"
		}
		v, err := probeTestVersion(t, dialect.OceanBaseMySQL, append(replies, failure)...)
		if !errors.Is(err, tc.cause) || v.Source.Query() != wantSource || v.Reported != wantRaw || v.Product != wantRaw {
			t.Errorf("%q / %v: got %+v, %v", failure.query, tc.cause, v, err)
		}
		var probeErr *MySQLVersionProbeError
		if !errors.As(err, &probeErr) || probeErr.Source.Query() != failure.query {
			t.Errorf("cannot recover probe source %q from %v", failure.query, err)
		}
		if tc.cause == unsupported {
			var mysqlErr *mysql.MySQLError
			if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1193 {
				t.Errorf("cannot recover database error from %v", err)
			}
		}
	}
}

func TestRequireReportedVersion(t *testing.T) {
	// Optional probe errors retain the report for cached browse/diagram paths.
	const primary, comment = "SELECT VERSION()", "SELECT @@version_comment"
	ctx := context.Background()
	_, primaryErr := probeTestVersion(t, dialect.OceanBaseMySQL, versionReply{query: primary, err: driver.ErrBadConn})
	_, optionalErr := probeTestVersion(t, dialect.OceanBaseMySQL, versionReply{query: primary, value: "5.7.25"}, versionReply{query: comment, err: driver.ErrBadConn})
	if !errors.Is(RequireReportedVersion(ctx, primaryErr), driver.ErrBadConn) || RequireReportedVersion(ctx, optionalErr) != nil {
		t.Fatal("primary and optional connection errors lost their error policy")
	}
	for _, interrupt := range []error{context.Canceled, context.DeadlineExceeded} {
		_, err := probeTestVersion(t, dialect.OceanBaseMySQL, versionReply{query: primary, value: "5.7.25"}, versionReply{query: comment, err: interrupt})
		if !errors.Is(RequireReportedVersion(ctx, err), interrupt) {
			t.Errorf("lost probe interruption %v", interrupt)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if !errors.Is(RequireReportedVersion(cancelled, optionalErr), context.Canceled) {
		t.Fatal("lost cancellation while handling optional driver error")
	}
}
