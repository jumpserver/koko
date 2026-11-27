package main

import (
	"bytes"
	"errors"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/xo/usql/drivers"
)

func TestWriteAvailableDrivers(t *testing.T) {
	var output bytes.Buffer
	if err := writeAvailableDrivers(&output); err != nil {
		t.Fatalf("writeAvailableDrivers() error = %v", err)
	}

	names := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if !sort.StringsAreSorted(names) {
		t.Errorf("available drivers are not sorted: %q", names)
	}
	seen := make(map[string]bool)
	for _, name := range names {
		if name == "" || len(strings.Fields(name)) != 1 {
			t.Errorf("expected one driver name per line, got %q", name)
		}
		if seen[name] {
			t.Errorf("duplicate driver name %q", name)
		}
		seen[name] = true
	}
	for _, name := range []string{
		"mysql", "mariadb", "maria", "my", "postgres", "postgresql", "pgsql", "pg",
		"dameng", "dm", "db2", "db2s", "sqlserver", "mssql", "oracle", "clickhouse", "oceanbase", "oboracle",
		"mongodb", "mongodb+srv", "redis", "rediss",
	} {
		if !seen[name] {
			t.Errorf("available drivers %q do not include %q", names, name)
		}
	}
	for name := range drivers.Available() {
		if !seen[name] {
			t.Errorf("registered driver %q is missing", name)
		}
	}
	// dburl knows these schemes, but this binary does not include their drivers.
	for _, name := range []string{"sqlite3", "sqlite", "moderncsqlite", "pgx", "godror"} {
		if seen[name] {
			t.Errorf("unavailable driver %q was advertised", name)
		}
	}
}

func TestWriteAvailableDriversWriteError(t *testing.T) {
	if err := writeAvailableDrivers(failingDriverWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("writeAvailableDrivers() error = %v, want %v", err, io.ErrClosedPipe)
	}
}

type failingDriverWriter struct{}

func (failingDriverWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}
