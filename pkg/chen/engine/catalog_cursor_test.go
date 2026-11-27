package engine

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCatalogRelationCursorV1(t *testing.T) {
	req := &catalogRequest{session: &session{id: "session"}, resource: &catalogResource{}, database: "db"}
	parse := func(query string) catalogScope {
		t.Helper()
		scope, err := parseCatalogScope(httptest.NewRequest("GET", "/?"+query, nil), "objects")
		if err != nil {
			t.Fatal(err)
		}
		return scope
	}
	const filter = "schema=app&kind=view&q=v"
	req.scope = parse(filter)
	// Issued by the original v1 encoder with this session's test key.
	const legacy = "eyJ2IjoxLCJzIjoic2Vzc2lvbiIsImQiOiJkYiIsIm8iOiJvYmplY3RzIiwibiI6ImFwcCIsImsiOiJ2aWV3IiwicSI6InYiLCJhcyI6ImFwcCIsImFuIjoidi4xIn0.ettFLTjGZwc0dAqoinYVfaUuZsnyVJIPdKoqncmAoWU"
	want := catalogPosition{AfterSchema: "app", AfterName: "v.1"}
	for _, token := range []string{legacy, req.nextCursor(want)} {
		req.scope = parse(filter + "&collection=relation&limit=1")
		if got, err := req.decodeCursor(token); err != nil || got != want {
			t.Fatalf("v1 position = %+v, %v", got, err)
		}
	}
	if _, err := req.decodeCursor(strings.Replace(legacy, "ettF", "attF", 1)); err == nil {
		t.Fatal("accepted a changed signature")
	}
	req.scope = parse("schema=app&kind=table&q=v")
	if _, err := req.decodeCursor(legacy); err == nil {
		t.Fatal("accepted a cursor from another relation filter")
	}
	for _, q := range []string{"collection=function", "collection=relation&collection=relation", "kind=function"} {
		if _, err := parseCatalogScope(httptest.NewRequest("GET", "/?"+q, nil), "objects"); err == nil {
			t.Fatalf("accepted %q", q)
		}
	}
}

func TestCatalogProjectionCursor(t *testing.T) {
	for _, op := range []string{"overview", "diagram"} {
		req := &catalogRequest{session: &session{id: "s"}, resource: &catalogResource{}, database: "db"}
		req.scope = catalogScope{operation: op, schema: "public", section: "indexes", sort: "name_asc", limit: 10}
		pos := catalogPosition{AfterSchema: "public", AfterTable: "t", AfterName: "ix", AfterOrdinal: 2, Contract: "overview-v1"}
		if op == "diagram" {
			req.scope.section, req.scope.sort, req.scope.direction = "edges", "", "schema"
			pos.AfterOrdinal, pos.Contract, pos.DiagramContract = 0, "", "diagram-v1"
		}
		token := req.nextCursor(pos)
		if got, err := req.decodeCursor(token); err != nil || got != pos {
			t.Fatalf("%s position = %+v, %v", op, got, err)
		}
		req.scope.limit++
		if _, err := req.decodeCursor(token); err == nil {
			t.Fatalf("%s accepted another page size", op)
		}
	}
}
