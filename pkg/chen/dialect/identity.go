// Package dialect identifies the database products supported by Chen.
package dialect

import (
	"errors"
	"slices"
	"strings"
)

// ID identifies a SQL dialect, including the tenant mode for OceanBase.
type ID string

const (
	MySQL           ID = "mysql"
	MariaDB         ID = "mariadb"
	PostgreSQL      ID = "postgresql"
	SQLServer       ID = "sqlserver"
	Oracle          ID = "oracle"
	Dameng          ID = "dameng"
	ClickHouse      ID = "clickhouse"
	OceanBaseMySQL  ID = "oceanbase-mysql"
	OceanBaseOracle ID = "oceanbase-oracle"
	DB2             ID = "db2"
)

const oceanBaseProtocol = "oceanbase"

var supported = [...]ID{MySQL, MariaDB, PostgreSQL, SQLServer, Oracle, Dameng, ClickHouse, OceanBaseMySQL, OceanBaseOracle, DB2}

func (d ID) Supported() bool {
	return slices.Contains(supported[:], d)
}

// SupportedProtocols returns the advertised protocol names in API order.
func SupportedProtocols() []string {
	protocols := make([]string, 0, len(supported)+1)
	for _, d := range supported {
		if d == OceanBaseMySQL {
			protocols = append(protocols, oceanBaseProtocol)
		}
		protocols = append(protocols, string(d))
	}
	return protocols
}

// Parse resolves configured protocols and aliases to their canonical dialect.
func Parse(protocol, tenantMode string) (ID, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	tenantMode = strings.ToLower(strings.TrimSpace(tenantMode))
	if protocol == oceanBaseProtocol {
		switch ID(tenantMode) {
		case MySQL:
			return OceanBaseMySQL, nil
		case Oracle:
			return OceanBaseOracle, nil
		default:
			return "", errors.New("oceanbase_tenant_mode_required")
		}
	}
	if tenantMode != "" && protocol != oceanBaseProtocol+"-"+tenantMode {
		return "", errors.New("tenant_mode_requires_oceanbase")
	}
	switch protocol {
	case "postgres", "pg", "pgsql":
		return PostgreSQL, nil
	case "mssql":
		return SQLServer, nil
	case "dm":
		return Dameng, nil
	case "maria":
		return MariaDB, nil
	}
	d := ID(protocol)
	if !d.Supported() {
		return "", errors.New("unsupported_database_for_go_v1")
	}
	return d, nil
}

// UsesMySQLSyntax reports whether the dialect uses MySQL syntax.
func (d ID) UsesMySQLSyntax() bool {
	return d == MySQL || d == MariaDB || d == OceanBaseMySQL
}

// UsesOracleSyntax reports whether the dialect uses Oracle syntax.
func (d ID) UsesOracleSyntax() bool {
	return d == Oracle || d == Dameng || d == OceanBaseOracle
}

// UsesMySQLWire includes the Oracle SQL mode of OceanBase.
func (d ID) UsesMySQLWire() bool {
	return d.UsesMySQLSyntax() || d == OceanBaseOracle
}
