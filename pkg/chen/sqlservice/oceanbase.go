package sqlservice

import (
	"context"
	"database/sql"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

const mysqlVersionQuery = "SELECT VERSION()"
const mysqlVersionCommentQuery = "SELECT @@version_comment"

// MySQLServerVersion keeps the compatibility banner from VERSION() even when a
// separate product banner supplies the version used by a feature.
type MySQLServerVersion struct {
	Reported string
	Product  string
	Source   string
}

func (v MySQLServerVersion) FromVersionComment() bool {
	return v.Source == mysqlVersionCommentQuery
}

func ReadMySQLServerVersion(ctx context.Context, conn *sql.Conn, d Dialect) (MySQLServerVersion, error) {
	var reported string
	if err := conn.QueryRowContext(ctx, mysqlVersionQuery).Scan(&reported); err != nil {
		return MySQLServerVersion{}, err
	}
	return resolveMySQLServerVersion(ctx, conn, d, reported)
}

// The diagram reads VERSION() alongside other session settings in one query.
// Resolve its product using the same connection without querying VERSION() twice.
func resolveMySQLServerVersion(ctx context.Context, conn *sql.Conn, d Dialect, reported string) (MySQLServerVersion, error) {
	version := MySQLServerVersion{Reported: reported, Product: reported, Source: mysqlVersionQuery}
	if d != OceanBaseMySQL || dialect.HasOceanBaseBrand(reported) {
		return version, nil
	}
	var comment string
	if err := conn.QueryRowContext(ctx, mysqlVersionCommentQuery).Scan(&comment); err != nil {
		// The primary probe succeeded. Callers that accept an unverified
		// product can still use the original banner.
		return version, err
	}
	if dialect.HasOceanBaseBrand(comment) {
		version.Product, version.Source = comment, mysqlVersionCommentQuery
	}
	return version, nil
}

// OceanBaseMySQLVersion returns the product version and the query that supplied
// it. Some OceanBase deployments expose only the MySQL compatibility version
// through VERSION().
func OceanBaseMySQLVersion(ctx context.Context, conn *sql.Conn) (string, string, error) {
	version, err := ReadMySQLServerVersion(ctx, conn, OceanBaseMySQL)
	if version.Source == "" {
		return "", mysqlVersionQuery, err
	}
	return version.Product, version.Source, err
}
