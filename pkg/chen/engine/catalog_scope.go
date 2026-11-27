package engine

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

type catalogScope struct {
	operation, schema, kind, query, table, cursor  string
	section, sort, database, direction, collection string
	limit                                          int
}

func parseCatalogScope(r *http.Request, operation string) (catalogScope, error) {
	s := catalogScope{operation: operation, limit: 100}
	allowed := map[string]bool{"cursor": true, "limit": true}
	if operation == "capabilities" {
		allowed = map[string]bool{}
	}
	if operation == "objects" {
		allowed["collection"] = true
	}
	if operation != "databases" && operation != "ai" {
		allowed["database"] = true
	}
	switch operation {
	case "diagram":
		for _, key := range []string{"database", "schema", "section", "direction", "table"} {
			allowed[key] = true
		}
	case "overview":
		for _, key := range []string{"database", "schema", "section", "q", "sort"} {
			allowed[key] = true
		}
	case "objects", "ai":
		allowed["schema"], allowed["kind"], allowed["q"] = true, true, true
	case "columns", "inspect", "ddl":
		allowed["schema"], allowed["table"] = true, true
	}
	values := r.URL.Query()
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return s, errors.New("invalid_catalog_request")
		}
	}
	s.section, s.sort, s.database = values.Get("section"), values.Get("sort"), values.Get("database")
	if operation == "overview" {
		if s.sort == "" {
			s.sort = "name_asc"
		}
		if s.sort != "name_asc" && s.sort != "name_desc" {
			return s, errors.New("invalid_catalog_request")
		}
		switch s.section {
		case "tables", "views", "indexes", "statistics", "properties":
		default:
			return s, errors.New("invalid_catalog_request")
		}
		if s.database == "" {
			return s, errors.New("invalid_catalog_request")
		}
		if (s.section == "statistics" || s.section == "properties") && (values.Get("q") != "" || values.Get("cursor") != "") {
			return s, errors.New("invalid_catalog_request")
		}
	}
	if operation == "diagram" {
		s.direction = values.Get("direction")
		if s.database == "" || (s.section != "nodes" && s.section != "edges") || (s.direction != "schema" && s.direction != "incoming") || s.direction == "schema" && values.Get("table") != "" || s.direction == "incoming" && (values.Get("table") == "" || values.Get("schema") == "") {
			return s, errors.New("invalid_catalog_request")
		}
	}
	s.schema, s.kind, s.query, s.table, s.cursor = values.Get("schema"), values.Get("kind"), values.Get("q"), values.Get("table"), values.Get("cursor")
	for _, name := range []string{s.schema, s.query, s.table, s.database} {
		if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 256 || strings.ContainsRune(name, 0) {
			return s, errors.New("invalid_catalog_request")
		}
	}
	switch values.Get("collection") {
	case "", "relation":
		// An omitted collection and an explicit relation share v1 cursors.
		if s.kind != "" && s.kind != "table" && s.kind != "view" {
			return s, errors.New("invalid_catalog_request")
		}
	case "routine":
		s.collection = "routine"
		if s.kind != "" && !sqlservice.ObjectKind(s.kind).IsRoutine() {
			return s, errors.New("invalid_catalog_request")
		}
	default:
		return s, errors.New("invalid_catalog_request")
	}
	if (operation == "columns" || operation == "inspect" || operation == "ddl") && s.table == "" {
		return s, errors.New("invalid_catalog_request")
	}
	if len(s.cursor) > 4096 {
		return s, errors.New("invalid_catalog_cursor")
	}
	if raw := values.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return s, errors.New("invalid_catalog_request")
		}
		s.limit = min(n, sqlservice.MaxCatalogPageSize)
	}
	if operation == "diagram" {
		s.limit = min(s.limit, 100)
	}
	return s, nil
}

func (req *catalogRequest) relationRequest() sqlservice.CatalogRelationRequest {
	return sqlservice.CatalogRelationRequest{
		Database: req.database, Schema: req.scope.schema, Kind: req.scope.kind, Query: req.scope.query,
		AfterSchema: req.after.AfterSchema, AfterName: req.after.AfterName, Limit: req.scope.limit,
	}
}
