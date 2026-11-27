// Package dialect preserves Chen's dialect API. Shared identity lives in dbkit.
package dialect

import (
	"errors"
	"slices"
	"strings"

	shared "github.com/jumpserver/koko/pkg/dbkit/dialect"
)

type ID = shared.ID

const (
	MySQL           = shared.MySQL
	MariaDB         = shared.MariaDB
	PostgreSQL      = shared.PostgreSQL
	SQLServer       = shared.SQLServer
	Oracle          = shared.Oracle
	Dameng          = shared.Dameng
	ClickHouse      = shared.ClickHouse
	OceanBaseMySQL  = shared.OceanBaseMySQL
	OceanBaseOracle = shared.OceanBaseOracle
	DB2             = shared.DB2
)

const oceanBaseProtocol = "oceanbase"

var supported = [...]ID{MySQL, MariaDB, PostgreSQL, SQLServer, Oracle, Dameng, ClickHouse, OceanBaseMySQL, OceanBaseOracle, DB2}

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
	if !slices.Contains(supported[:], d) {
		return "", errors.New("unsupported_database_for_go_v1")
	}
	return d, nil
}
