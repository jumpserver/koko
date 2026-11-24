package sessiontools

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/jumpserver/koko/internal/dbconn"
	"github.com/jumpserver/koko/pkg/srvconn"
)

// DatabaseDialect is independent of the asset's connection protocol. Both
// OceanBase tenant modes use the MySQL wire protocol.
func DatabaseDialect(config DatabaseConfig) (string, error) {
	if config.PlatformType != "oceanbase" {
		return config.Protocol, nil
	}
	if config.Protocol != srvconn.ProtocolMySQL {
		return "", errors.New("OceanBase requires the mysql connection protocol")
	}
	switch config.TenantMode {
	case "mysql", "oracle":
		return config.TenantMode, nil
	default:
		return "", fmt.Errorf("unsupported OceanBase tenant mode %q", config.TenantMode)
	}
}

func newOceanBaseDatabase(config DatabaseConfig) (*sql.DB, error) {
	if _, err := DatabaseDialect(config); err != nil {
		return nil, err
	}
	options := dbconn.MySQLOptions{ParseTime: true}
	if config.agent {
		options.MaxAllowedPacket = 16 << 20
	}
	if config.UseSSL {
		var err error
		options.TLS, err = buildMySQLTLSConfig(config)
		if err != nil {
			return nil, err
		}
	}
	connector, err := dbconn.OceanBaseConnector(config.connectionConfig(), options)
	if err != nil {
		return nil, fmt.Errorf("create OceanBase connector: %w", err)
	}
	return sql.OpenDB(connector), nil
}
