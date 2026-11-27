package sqlservice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jumpserver/koko/pkg/chen/dialect"
)

// Run against a disposable PostgreSQL database as a role able to create roles
// and foreign-data wrappers. The two physical connections model Catalog and Console.
func TestPostgresCatalogRelations(t *testing.T) {
	dsn := os.Getenv("KOKO_CATALOG_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("set KOKO_CATALOG_PG_TEST_DSN to a disposable PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	open := func() *sql.Conn {
		t.Helper()
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}
	catalog, console := open(), open()
	defer catalog.Close()
	defer console.Close()
	var database, schema string
	if err := catalog.QueryRowContext(ctx, "SELECT current_database(), 'koko_catalog_' || txid_current()").Scan(&database, &schema); err != nil {
		t.Fatal(err)
	}
	exec := func(conn *sql.Conn, stmt string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, strings.ReplaceAll(stmt, "$ns", schema)); err != nil {
			t.Fatal(err)
		}
	}
	exec(catalog, `CREATE SCHEMA $ns;
CREATE ROLE $ns;
CREATE FOREIGN DATA WRAPPER $ns;
CREATE SERVER $ns FOREIGN DATA WRAPPER $ns;
CREATE TABLE $ns."A" (n int);
CREATE TABLE $ns.b_partition (n int) PARTITION BY RANGE (n);
CREATE FOREIGN TABLE $ns.c_foreign (n int) SERVER $ns;
CREATE VIEW $ns.d_view AS SELECT 1 AS n;
CREATE MATERIALIZED VIEW $ns.e_mat AS SELECT 1 AS n;
CREATE SEQUENCE $ns.f_seq;
CREATE FUNCTION $ns.g_fn() RETURNS int LANGUAGE SQL AS 'SELECT 1';
COMMENT ON TABLE $ns."A" IS 'catalog regression'`)
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_, _ = console.ExecContext(cleanup, "ROLLBACK; RESET ROLE")
		_, err := catalog.ExecContext(cleanup, strings.ReplaceAll(
			"DROP SCHEMA $ns CASCADE; DROP SERVER $ns; DROP FOREIGN DATA WRAPPER $ns; DROP ROLE $ns", "$ns", schema))
		if err != nil {
			t.Error(err)
		}
	}()
	check := func(conn *sql.Conn, req CatalogRelationRequest, want ...string) {
		t.Helper()
		req.Database, req.Schema = database, schema
		rows, err := RelationRows(ctx, conn, dialect.PostgreSQL, req)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, row := range rows {
			if row.Database != database || row.Schema != schema {
				t.Fatalf("unexpected scope %+v", row)
			}
			if row.Name == "A" && row.Comment != "catalog regression" {
				t.Fatalf("unexpected comment %q", row.Comment)
			}
			got = append(got, fmt.Sprintf("%s/%s/%s", row.Name, row.Kind, row.ObjectType))
		}
		if !slices.Equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	const table, partition, foreign = "A/table/table", "b_partition/table/partitioned_table", "c_foreign/table/foreign_table"
	const view, materialized = "d_view/view/view", "e_mat/view/materialized_view"
	check(catalog, CatalogRelationRequest{}, table, partition, foreign, view, materialized)
	check(catalog, CatalogRelationRequest{Kind: "table"}, table, partition, foreign)
	check(catalog, CatalogRelationRequest{Kind: "view"}, view, materialized)
	check(catalog, CatalogRelationRequest{Limit: 2}, table, partition, foreign)
	check(catalog, CatalogRelationRequest{Limit: 2, AfterSchema: schema, AfterName: "b_partition"}, foreign, view, materialized)
	check(catalog, CatalogRelationRequest{Query: "_V"}, view)

	exec(console, "SET ROLE $ns")
	req := CatalogRelationRequest{Database: database, Schema: schema}
	if _, err := RelationRows(ctx, console, dialect.PostgreSQL, req); !errors.Is(err, ErrCatalogForbidden) {
		t.Fatalf("hidden schema: got %v, want ErrCatalogForbidden", err)
	}
	exec(catalog, "GRANT USAGE ON SCHEMA $ns TO $ns")
	check(console, CatalogRelationRequest{Query: "foreign"}, foreign)
	if _, err := console.ExecContext(ctx, `SELECT n FROM `+schema+`."A"`); err == nil {
		t.Fatal("role without SELECT could read the table")
	}
	exec(console, "RESET ROLE")
	for _, finish := range []string{"ROLLBACK", "COMMIT"} {
		exec(console, "BEGIN; CREATE TABLE $ns.pending(n int)")
		check(console, CatalogRelationRequest{Query: "pending"}, "pending/table/table")
		check(catalog, CatalogRelationRequest{Query: "pending"})
		exec(console, finish)
		if finish == "ROLLBACK" {
			check(catalog, CatalogRelationRequest{Query: "pending"})
		} else {
			check(catalog, CatalogRelationRequest{Query: "pending"}, "pending/table/table")
		}
	}
}
