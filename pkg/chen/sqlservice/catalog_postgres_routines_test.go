package sqlservice

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgreSQLRoutineCatalog(t *testing.T) {
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
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var req RoutineListRequest
	if err := conn.QueryRowContext(ctx, `SELECT current_database(), 'koko_r_' || txid_current(), current_setting('server_version_num')::int`).Scan(&req.Database, &req.Schema, &req.ServerVersion); err != nil {
		t.Fatal(err)
	}
	exec := func(stmt string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, strings.ReplaceAll(stmt, "$ns", req.Schema)); err != nil {
			t.Fatal(err)
		}
	}
	exec("BEGIN")
	defer func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") }()
	exec(`CREATE ROLE $ns;
CREATE SCHEMA $ns; CREATE SCHEMA $ns_types;
GRANT USAGE ON SCHEMA $ns, $ns_types TO $ns;
CREATE DOMAIN $ns.tag AS integer;
CREATE TYPE $ns_types.tag AS ENUM ('value');
CREATE FUNCTION $ns_types.user_window() RETURNS bigint AS 'window_row_number' LANGUAGE internal WINDOW`)
	if req.ServerVersion >= 110000 {
		exec(`CREATE PROCEDURE $ns.overload(a text) LANGUAGE SQL AS 'SELECT 1';
REVOKE ALL ON PROCEDURE $ns.overload(text) FROM PUBLIC`)
	}
	exec(`CREATE FUNCTION $ns.overload() RETURNS int LANGUAGE SQL AS 'SELECT 0';
CREATE FUNCTION $ns.overload(a $ns.tag, OUT result int) LANGUAGE SQL AS 'SELECT 1';
CREATE FUNCTION $ns.overload(a $ns_types.tag[]) RETURNS int LANGUAGE SQL AS 'SELECT 2';
CREATE AGGREGATE $ns.total(integer) (SFUNC = pg_catalog.int4pl, STYPE = integer);
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA $ns FROM PUBLIC`)
	read := func(r RoutineListRequest) []ObjectSummary {
		t.Helper()
		rows, err := PostgreSQLRoutineRows(ctx, conn, r)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	window := req
	window.Schema, window.Query = "pg_catalog", "row_number"
	if got := read(window); len(got) != 0 {
		t.Fatalf("unexpected system window routine: %+v", got)
	}
	window.Schema, window.Query = req.Schema+"_types", "user_window"
	if got := read(window); len(got) != 1 || got[0].Ref.Name != "user_window" || got[0].Ref.Kind != "function" {
		t.Fatalf("expected user-defined window routine, got %+v", got)
	}
	rows := read(req)
	wantCount := 3
	if req.ServerVersion >= 110000 {
		wantCount++
	}
	if len(rows) != wantCount {
		t.Fatalf("got %d routines, want %d", len(rows), wantCount)
	}
	var identifiers []uint32
	for _, row := range rows {
		if row.OID == 0 || slices.Contains(identifiers, row.OID) || row.Ref.Database != req.Database || row.Ref.Schema != req.Schema || row.Ref.Name != "overload" {
			t.Fatalf("invalid or duplicated routine: %+v", row)
		}
		identifiers = append(identifiers, row.OID)
	}
	if !slices.IsSorted(identifiers[:3]) {
		t.Fatalf("same-name overloads out of OID order: %v", identifiers)
	}
	req.Kind = "function"
	functions := read(req)
	if len(functions) != 3 || !reflect.DeepEqual(rows[:3], functions) {
		t.Fatalf("got %d functions, want 3", len(functions))
	}
	for i, want := range [][]ObjectTypeRef{{}, {{Schema: req.Schema, Name: "tag"}}, {{Schema: req.Schema + "_types", Name: "_tag"}}} {
		if functions[i].Ref.Kind != "function" || !reflect.DeepEqual(functions[i].Ref.InputTypes, want) {
			t.Fatalf("overload %d: %+v, want input types %v", i, functions[i].Ref, want)
		}
	}
	if req.ServerVersion >= 110000 {
		req.Kind = "procedure"
		procedures := read(req)
		if len(procedures) != 1 || !reflect.DeepEqual(rows[3:], procedures) || procedures[0].Ref.Kind != "procedure" || !reflect.DeepEqual(procedures[0].Ref.InputTypes, []ObjectTypeRef{{Schema: "pg_catalog", Name: "text"}}) {
			t.Fatalf("unexpected procedure: %+v", procedures)
		}
	}
	req.Kind, req.Limit = "", 1
	page := req
	for i, want := range identifiers {
		got := read(page)
		if len(got) != min(2, len(identifiers)-i) || got[0].OID != want {
			t.Fatalf("page %d: %+v", i, got)
		}
		page.AfterSchema, page.AfterName, page.AfterKind, page.AfterOID = got[0].Ref.Schema, got[0].Ref.Name, got[0].Ref.Kind, got[0].OID
	}
	if got := read(page); len(got) != 0 {
		t.Fatalf("unexpected trailing page: %+v", got)
	}

	exec("SET ROLE $ns")
	if got := read(req); len(got) != 0 {
		t.Fatal("routine without EXECUTE was visible")
	}
	exec("RESET ROLE; GRANT EXECUTE ON FUNCTION $ns.overload() TO $ns; SET ROLE $ns")
	got := read(req)
	if len(got) != 1 || got[0].OID != functions[0].OID {
		t.Fatalf("unexpected EXECUTE-only view: %+v", got)
	}
}
