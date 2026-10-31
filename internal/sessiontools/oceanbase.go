package sessiontools

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"

	"github.com/jumpserver/koko/pkg/srvconn"
	obDriver "github.com/oceanbase/go-oceanbase-driver"
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

type oceanBaseDialKey struct{}

var oceanBaseDialOnce sync.Once

// The installed driver only exposes a global dial registry. Register one
// dispatcher, with the per-connection dialer carried by Connect's context;
// registering a closure for every asset session would retain closed sessions.
type oceanBaseConnector struct {
	driver.Connector
	dial databaseDialer
}

func (c oceanBaseConnector) Connect(ctx context.Context) (driver.Conn, error) {
	return c.Connector.Connect(context.WithValue(ctx, oceanBaseDialKey{}, c.dial))
}

func newOceanBaseDatabase(config DatabaseConfig) (*sql.DB, error) {
	if _, err := DatabaseDialect(config); err != nil {
		return nil, err
	}
	cfg := obDriver.NewConfig()
	cfg.Net, cfg.Addr = "tcp", net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	cfg.User, cfg.Passwd, cfg.DBName = config.Username, config.Password, config.Database
	cfg.ParseTime, cfg.MultiStatements = true, false
	if config.agent {
		cfg.MaxAllowedPacket = 16 << 20
	}
	if config.UseSSL {
		var err error
		cfg.TLS, err = buildMySQLTLSConfig(config)
		if err != nil {
			return nil, err
		}
	}
	if config.DialContext != nil {
		oceanBaseDialOnce.Do(func() {
			obDriver.RegisterDialContext("jumpserver-agent", func(ctx context.Context, address string) (net.Conn, error) {
				dial, ok := ctx.Value(oceanBaseDialKey{}).(databaseDialer)
				if !ok || dial == nil {
					return nil, errors.New("OceanBase connection has no controlled dialer")
				}
				return dial(ctx, "tcp", address)
			})
		})
		cfg.Net = "jumpserver-agent"
	}
	connector, err := obDriver.NewConnector(cfg)
	if err != nil {
		return nil, fmt.Errorf("create OceanBase connector: %w", err)
	}
	if config.DialContext != nil {
		return sql.OpenDB(oceanBaseConnector{Connector: connector, dial: config.DialContext}), nil
	}
	return sql.OpenDB(connector), nil
}
