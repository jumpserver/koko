package sqlservice

import (
	"context"
	"database/sql"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

// A collection advertises the operations implemented by this Catalog API.
// Every read still checks visibility on its actual database connection.
type CatalogCollection struct {
	Kinds   []ObjectKind `json:"kinds"`
	Actions []string     `json:"actions"`
}

type CatalogCapabilities struct {
	Version     int                          `json:"version"`
	Collections map[string]CatalogCollection `json:"collections"`
}

func ReadCatalogCapabilities(ctx context.Context, conn *sql.Conn, d dialect.ID, database string) (CatalogCapabilities, error) {
	var result CatalogCapabilities
	if err := validateCatalogInput(conn, d, database); err != nil {
		return result, err
	}
	if err := catalogDatabaseAccess(ctx, conn, d, database); err != nil {
		return result, err
	}
	result = CatalogCapabilities{Version: 2, Collections: map[string]CatalogCollection{
		"relation": {Kinds: []ObjectKind{"table", "view"}, Actions: []string{"list"}},
	}}
	if d == dialect.PostgreSQL {
		version, err := PostgreSQLCatalogVersion(ctx, conn)
		if err != nil {
			return CatalogCapabilities{}, err
		}
		if version >= 100000 {
			kinds := []ObjectKind{FunctionKind}
			if version >= 110000 {
				kinds = append(kinds, ProcedureKind)
			}
			result.Collections["routine"] = CatalogCollection{Kinds: kinds, Actions: []string{"list"}}
		}
	}
	return result, nil
}
