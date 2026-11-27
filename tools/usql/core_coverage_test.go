package main

import (
	"context"
	"io"
	"testing"

	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
)

// Core DatabaseTypes at 2452f9cdd lists these nine SQL database types.
// OceanBase has two tenant dialects. This checks real driver initialization,
// not just aliases advertised by --drivers, without contacting a database.
func TestCoreSQLDriverConnections(t *testing.T) {
	for _, test := range []struct {
		name, scheme, driver string
	}{
		{"MySQL", "mysql", "mysql"},
		{"MariaDB", "mariadb", "mysql"},
		{"PostgreSQL", "postgresql", "postgres"},
		{"Oracle", "oracle", "oracle"},
		{"SQLServer", "sqlserver", "sqlserver"},
		{"Db2", "db2", "db2"},
		{"Dameng", "dameng", "dameng"},
		{"OceanBaseMySQL", "oceanbase", "oceanbase"},
		{"OceanBaseOracle", "oboracle", "oboracle"},
		{"ClickHouse", "clickhouse", "clickhouse"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dsn := test.scheme + "://probe:test@127.0.0.1:1/test"
			if test.driver == "postgres" {
				// The upstream SSL retry mode pings during Open unless sslmode
				// is explicit. This fixture only checks lazy initialization.
				dsn += "?sslmode=disable"
			}
			u, err := dburl.Parse(dsn)
			if err != nil {
				t.Fatal(err)
			}
			if u.Driver != test.driver || !drivers.Registered(u.Driver) {
				t.Fatalf("driver mapping = %q, want registered %q", u.Driver, test.driver)
			}
			discard := func() io.Writer { return io.Discard }
			db, err := drivers.Open(context.Background(), u, discard, discard)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if db.Stats().OpenConnections != 0 {
				t.Fatal("driver initialization unexpectedly connected to a database")
			}
		})
	}
}
