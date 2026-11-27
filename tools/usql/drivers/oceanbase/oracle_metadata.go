package oceanbase

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"

	"github.com/xo/usql/drivers"
	"github.com/xo/usql/drivers/metadata"
	orameta "github.com/xo/usql/drivers/metadata/oracle"
)

var oracleBind = regexp.MustCompile(`:[1-9][0-9]*`)

// The Oracle metadata reader uses numbered binds, while OceanBase Connector/Go
// uses MySQL-style positional binds even for Oracle-compatible tenants.
type oracleMetadataDB struct{ drivers.DB }

func (db oracleMetadataDB) Query(query string, args ...interface{}) (*sql.Rows, error) {
	query, args, err := rewriteOracleMetadataBinds(query, args)
	if err != nil {
		return nil, err
	}
	return db.DB.Query(query, args...)
}

func (db oracleMetadataDB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	query, args, err := rewriteOracleMetadataBinds(query, args)
	if err != nil {
		return nil, err
	}
	return db.DB.QueryContext(ctx, query, args...)
}

func newOracleMetadataReader(db drivers.DB, opts ...metadata.ReaderOption) metadata.Reader {
	return orameta.NewReader()(oracleMetadataDB{db}, opts...)
}

func rewriteOracleMetadataBinds(query string, args []interface{}) (string, []interface{}, error) {
	var bindErr error
	ordered := make([]interface{}, 0, len(args))
	rewritten := oracleBind.ReplaceAllStringFunc(query, func(bind string) string {
		index, err := strconv.Atoi(bind[1:])
		if err != nil || index < 1 || index > len(args) {
			bindErr = fmt.Errorf("invalid Oracle metadata bind %s", bind)
			return bind
		}
		ordered = append(ordered, args[index-1])
		return "?"
	})
	if bindErr != nil {
		return "", nil, bindErr
	}
	return rewritten, ordered, nil
}
