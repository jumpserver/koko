package sqlservice

import (
	"context"
	"database/sql"
	"strings"
)

// OceanBaseMySQLVersion returns the product version and the query that supplied
// it. Some OceanBase deployments expose only the MySQL compatibility version
// through VERSION().
func OceanBaseMySQLVersion(ctx context.Context, conn *sql.Conn) (string, string, error) {
	const versionQuery = "SELECT VERSION()"
	const commentQuery = "SELECT @@version_comment"
	var version string
	if err := conn.QueryRowContext(ctx, versionQuery).Scan(&version); err != nil {
		return "", versionQuery, err
	}
	if strings.Contains(strings.ToLower(version), "oceanbase") {
		return version, versionQuery, nil
	}
	var comment string
	if err := conn.QueryRowContext(ctx, commentQuery).Scan(&comment); err != nil {
		return version, versionQuery, err
	}
	if strings.Contains(strings.ToLower(comment), "oceanbase") {
		return comment, commentQuery, nil
	}
	return version, versionQuery, nil
}
