package core

import (
	"errors"
	"strings"
)

// CanonicalDialect keeps transport identity separate from SQL compatibility.
// OceanBase Oracle tenants still use OceanBase's MySQL-family wire protocol.
func CanonicalDialect(protocol, tenantMode string) (string, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	tenantMode = strings.ToLower(strings.TrimSpace(tenantMode))
	if protocol == "oceanbase" {
		if tenantMode != "mysql" && tenantMode != "oracle" {
			return "", errors.New("oceanbase_tenant_mode_required")
		}
		return "oceanbase-" + tenantMode, nil
	}
	if tenantMode != "" && protocol != "oceanbase-"+tenantMode {
		return "", errors.New("tenant_mode_requires_oceanbase")
	}
	switch protocol {
	case "mysql", "mariadb", "postgresql", "oracle", "sqlserver", "db2", "dameng", "clickhouse", "oceanbase-mysql", "oceanbase-oracle":
		return protocol, nil
	case "postgres", "pg", "pgsql":
		return "postgresql", nil
	case "mssql":
		return "sqlserver", nil
	case "dm":
		return "dameng", nil
	case "maria":
		return "mariadb", nil
	default:
		return "", errors.New("unsupported_database_for_go_v1")
	}
}

func IsMySQLDialect(dialect string) bool {
	return dialect == "mysql" || dialect == "mariadb" || dialect == "oceanbase-mysql"
}
func IsMySQLWire(dialect string) bool {
	return IsMySQLDialect(dialect) || dialect == "oceanbase-oracle"
}
