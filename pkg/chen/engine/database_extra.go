package engine

import (
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jumpserver/koko/pkg/chen/core"
	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
	ob "github.com/oceanbase/go-oceanbase-driver"
	goora "github.com/sijms/go-ora/v2"
)

func (pool databasePool) extraDatabase(c core.ConnectionConfig, t *tls.Config) (*sql.DB, error) {
	switch c.Dialect {
	case "oceanbase-mysql", "oceanbase-oracle":
		cfg := ob.NewConfig()
		cfg.Net = "tcp"
		cfg.Addr = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
		cfg.User = c.User
		cfg.Passwd = c.Password
		cfg.DBName = c.Database
		cfg.ParseTime = false
		cfg.ClientFoundRows = c.Dialect == "oceanbase-mysql"
		if c.Dialect == "oceanbase-oracle" {
			cfg.Params = map[string]string{"autocommit": "1"}
		}
		cfg.Timeout = 10 * time.Second
		cfg.ReadTimeout = pool.readTimeout(2 * time.Minute)
		cfg.WriteTimeout = 15 * time.Second
		cfg.TLS = t
		connector, err := ob.NewConnector(cfg)
		if err != nil {
			return nil, errors.New("invalid OceanBase connection configuration")
		}
		return pool.openConnector(connector, 2*time.Minute), nil
	case "sqlserver":
		config, err := msdsn.Parse("server=127.0.0.1;user id=chen;database=master;encrypt=disable;connection timeout=10;dial timeout=10")
		if err != nil {
			return nil, errors.New("invalid SQL Server connection defaults")
		}
		config.Host, config.Port, config.User, config.Password, config.Database = c.Host, uint64(c.Port), c.User, c.Password, c.Database
		config.AppName, config.DisableRetry = "Chen Go", true

		if t != nil {
			config.TLSConfig = t
			config.Encryption = msdsn.EncryptionRequired
			config.HostInCertificateProvided = true
		}
		loginTimeout := config.ConnTimeout
		if pool == catalogPool {
			// ConnTimeout otherwise applies the same deadline to reads and writes.
			config.ConnTimeout = 0
		}
		connector := mssql.NewConnectorConfig(config)
		if pool == catalogPool {
			dialer := &net.Dialer{KeepAlive: config.KeepAlive}
			connector.Dialer = catalogDialer{dial: dialer.DialContext, writeTimeout: loginTimeout}
		}
		return pool.openConnector(connector, loginTimeout), nil
	case "clickhouse":
		protocol := clickhouse.Native
		if c.Transport == "http" || c.Transport == "" && (c.Port == 8123 || c.Port == 8443) {
			protocol = clickhouse.HTTP
		}
		database := c.Database
		if database == "" {
			database = "default"
		}
		options := &clickhouse.Options{Addr: []string{net.JoinHostPort(c.Host, strconv.Itoa(c.Port))}, Auth: clickhouse.Auth{Database: database, Username: c.User, Password: c.Password}, TLS: t, Protocol: protocol, DialTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, ClientInfo: clickhouse.ClientInfo{Products: []struct{ Name, Version string }{{Name: "chen", Version: "go"}}}}
		if pool == catalogPool {
			options.TransportFunc = catalogClickHouseHTTPTransport
		}
		if protocol == clickhouse.HTTP {
			return pool.openConnector(&clickHouseHTTPConnector{options: *options}, options.DialTimeout), nil
		}
		if c.Transport == "auto" {
			return pool.openConnector(&clickHouseAutoConnector{options: *options, pool: pool}, options.DialTimeout), nil
		}
		if pool == catalogPool {
			return pool.openConnector(catalogClickHouseNative(*options), options.DialTimeout), nil
		}
		return clickhouse.OpenDB(options), nil
	case "oracle":
		options := oracleConnectionOptions(c)
		if pool == catalogPool {
			options["TIMEOUT"] = "0" // go-ora couples read and write timeouts.
		}
		if t != nil {
			options["SSL"] = "true"
			options["SSL VERIFY"] = strconv.FormatBool(!t.InsecureSkipVerify)
		}
		connector := goora.NewConnector(goora.BuildUrl(c.Host, c.Port, c.Database, c.User, c.Password, options)).(*goora.OracleConnector)
		if t != nil {
			connector.WithTLSConfig(t)
		}
		if pool == catalogPool {
			connector.Dialer(catalogDialer{dial: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, writeTimeout: 2 * time.Minute})
			return pool.openConnector(catalogOracleConnector{connector}, 2*time.Minute), nil
		}
		return sql.OpenDB(connector), nil
	case "dameng":
		if t != nil {
			return nil, errors.New("Dameng TLS requires a configured vendor certificate directory; TLS cannot be silently disabled")
		}
		// Vendor DSN parsing deliberately does not URL-decode userinfo. Using
		// url.UserPassword changes valid punctuation in passwords. Its parser
		// splits the first colon and last @, and the final ? starts options.
		if strings.Contains(c.User, ":") {
			return nil, errors.New("Dameng usernames containing colon are unsupported by the driver")
		}
		dialName := damengDialName
		if pool == catalogPool {
			dialName = damengCatalogDialName
		}
		dsn := "dm://" + c.User + ":" + c.Password + "@" + net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) + "?connectTimeout=10000&socketTimeout=120&doSwitch=0&logLevel=off&lobMode=1&dialName=" + dialName
		return pool.openDameng(dsn)

	case "db2":
		return pool.openDB2(c, t)
	default:
		return nil, errors.New("unsupported database protocol")
	}
}

func supportedProtocols() []string {
	return []string{"mysql", "mariadb", "postgresql", "sqlserver", "oracle", "dameng", "clickhouse", "oceanbase", "oceanbase-mysql", "oceanbase-oracle", "db2"}
}

func quoteDatabaseIdentifier(name, dialect string) string {
	switch {
	case dialect == "clickhouse":
		return "`" + strings.ReplaceAll(strings.ReplaceAll(name, "\\", "\\\\"), "`", "``") + "`"
	case core.IsMySQLDialect(dialect):
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case dialect == "sqlserver":
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	default:
		return "\"" + strings.ReplaceAll(name, "\"", "\"\"") + "\""
	}
}

func unsupportedContext(dialect string) error {
	return fmt.Errorf("changing database context is not supported for %s; open a configured selector", dialect)
}

func oracleConnectionOptions(c core.ConnectionConfig) map[string]string {
	options := map[string]string{"CONNECTION TIMEOUT": "10", "TIMEOUT": "120", "LOB FETCH": "post", "JSON FETCH": "text", "DBA PRIVILEGE": "NONE"}
	if c.OracleSYSDBA {
		options["DBA PRIVILEGE"] = "SYSDBA"
	}
	return options
}
