package dbconn

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	ob "github.com/oceanbase/go-oceanbase-driver"
)

// MySQLOptions keeps execution and value semantics with the caller. Both drivers
// reject multi-statements; the workbench executes its parsed statements separately.
type MySQLOptions struct {
	TLS                                *tls.Config
	ParseTime                          bool
	ClientFoundRows                    bool
	MaxAllowedPacket                   int
	Autocommit                         string
	Timeout, ReadTimeout, WriteTimeout time.Duration
}

func MySQLConnector(c Config, options MySQLOptions) (driver.Connector, error) {
	cfg := mysql.NewConfig()
	cfg.Net, cfg.Addr = "tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	cfg.User, cfg.Passwd, cfg.DBName = c.User, c.Password, c.Database
	cfg.TLS, cfg.DialFunc = options.TLS, c.DialContext
	cfg.ParseTime, cfg.ClientFoundRows = options.ParseTime, options.ClientFoundRows
	cfg.Loc = time.UTC
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = options.Timeout, options.ReadTimeout, options.WriteTimeout
	if options.MaxAllowedPacket > 0 {
		cfg.MaxAllowedPacket = options.MaxAllowedPacket
	}
	if options.Autocommit != "" {
		cfg.Params = map[string]string{"autocommit": options.Autocommit}
	}
	return mysql.NewConnector(cfg)
}

type oceanBaseDialKey struct{}

var oceanBaseDialOnce sync.Once

type oceanBaseConnector struct {
	driver.Connector
	dial func(context.Context, string, string) (net.Conn, error)
}

func (c oceanBaseConnector) Connect(ctx context.Context) (driver.Conn, error) {
	return c.Connector.Connect(context.WithValue(ctx, oceanBaseDialKey{}, c.dial))
}

func OceanBaseConnector(c Config, options MySQLOptions) (driver.Connector, error) {
	cfg := ob.NewConfig()
	cfg.Net, cfg.Addr = "tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	cfg.User, cfg.Passwd, cfg.DBName = c.User, c.Password, c.Database
	cfg.TLS = options.TLS
	cfg.ParseTime, cfg.ClientFoundRows = options.ParseTime, options.ClientFoundRows
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = options.Timeout, options.ReadTimeout, options.WriteTimeout
	if options.MaxAllowedPacket > 0 {
		cfg.MaxAllowedPacket = options.MaxAllowedPacket
	}
	if options.Autocommit != "" {
		cfg.Params = map[string]string{"autocommit": options.Autocommit}
	}
	if c.DialContext != nil {
		// One registry entry; each Connect carries its own authorized dialer.
		oceanBaseDialOnce.Do(func() {
			ob.RegisterDialContext("jumpserver", func(ctx context.Context, address string) (net.Conn, error) {
				dial, ok := ctx.Value(oceanBaseDialKey{}).(func(context.Context, string, string) (net.Conn, error))
				if !ok || dial == nil {
					return nil, errors.New("OceanBase connection has no controlled dialer")
				}
				return dial(ctx, "tcp", address)
			})
		})
		cfg.Net = "jumpserver"
	}
	connector, err := ob.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	if c.DialContext != nil {
		return oceanBaseConnector{Connector: connector, dial: c.DialContext}, nil
	}
	return connector, nil
}
