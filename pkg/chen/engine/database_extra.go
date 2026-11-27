package engine

import (
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jumpserver/koko/internal/dbconn"
	"github.com/jumpserver/koko/pkg/chen/core"
	"github.com/jumpserver/koko/pkg/chen/dialect"
	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
)

func (pool databasePool) extraDatabase(c core.ConnectionConfig, t *tls.Config) (*sql.DB, error) {
	switch d := dialect.ID(c.Dialect); d {
	case dialect.OceanBaseMySQL, dialect.OceanBaseOracle:
		c.DialContext = databaseDialer{dial: c.DialContext}.DialContext
		options := dbconn.MySQLOptions{
			TLS: t, ParseTime: false, ClientFoundRows: d == dialect.OceanBaseMySQL,
			Timeout: databaseDialTimeout, ReadTimeout: pool.readTimeout(), WriteTimeout: databaseWriteTimeout,
		}
		if d == dialect.OceanBaseOracle {
			options.Autocommit = "1"
		}
		connector, err := dbconn.OceanBaseConnector(c, options)
		if err != nil {
			return nil, errors.New("invalid OceanBase connection configuration")
		}
		return openConnector(connector), nil
	case dialect.SQLServer:
		config, err := msdsn.Parse("server=127.0.0.1;user id=chen;database=master;encrypt=disable")
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
		// ConnTimeout applies to both reads and writes in go-mssqldb.
		config.ConnTimeout, config.DialTimeout = 0, databaseDialTimeout
		connector := mssql.NewConnectorConfig(config)
		dialer := &net.Dialer{Timeout: databaseDialTimeout, KeepAlive: config.KeepAlive}
		connector.Dialer = databaseDialer{dial: dialer.DialContext, readTimeout: pool.readTimeout(), writeTimeout: databaseWriteTimeout}
		return openConnector(connector), nil
	case dialect.ClickHouse:
		protocol := clickhouse.Native
		if c.Transport == "http" || c.Transport == "" && (c.Port == 8123 || c.Port == 8443) {
			protocol = clickhouse.HTTP
		}
		database := c.Database
		if database == "" {
			database = "default"
		}
		options := &clickhouse.Options{Addr: []string{net.JoinHostPort(c.Host, strconv.Itoa(c.Port))}, Auth: clickhouse.Auth{Database: database, Username: c.User, Password: c.Password}, TLS: t, Protocol: protocol, DialTimeout: databaseDialTimeout, ClientInfo: clickhouse.ClientInfo{Products: []struct{ Name, Version string }{{Name: "chen", Version: "go"}}}}
		if protocol == clickhouse.HTTP {
			return openConnector(&clickHouseHTTPConnector{options: *options, pool: pool}), nil
		}
		if c.Transport == "auto" {
			return openConnector(&clickHouseAutoConnector{options: *options, pool: pool}), nil
		}
		return openConnector(pool.clickHouseNative(*options)), nil
	case dialect.Oracle:
		options := oracleConnectionOptions(c)
		if pool == catalogPool {
			options["TIMEOUT"] = "0" // go-ora couples read and write timeouts.
		}
		if t != nil {
			options["SSL"] = "true"
			options["SSL VERIFY"] = strconv.FormatBool(!t.InsecureSkipVerify)
		}
		dialer := databaseDialer{dial: c.DialContext}
		if pool == catalogPool {
			dialer.readTimeout, dialer.writeTimeout = pool.readTimeout(), databaseWriteTimeout
		}
		// Regular go-ora uses *net.TCPConn for out-of-band breaks. Keep its
		// native 120s I/O fallback until break and fetch cleanup can be split.
		followRedirects := pool == regularPool && c.DialContext == nil && (c.TLS.ServerName == "" || c.TLS.ServerName == c.Host)
		c.DialContext = dialer.DialContext
		connector := dbconn.OracleConnector(c, t, options)
		if followRedirects {
			// Direct connections can follow the listener to another RAC instance.
			connector.Dialer(dialer)
		}
		return openConnector(oracleConnector{connector, pool}), nil
	case dialect.Dameng:
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
		dsn := "dm://" + c.User + ":" + c.Password + "@" + net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) + "?connectTimeout=10000&socketTimeout=0&doSwitch=0&logLevel=off&lobMode=1&dialName=" + dialName
		return pool.openDameng(dsn)

	case dialect.DB2:
		return pool.openDB2(c, t)
	default:
		return nil, errors.New("unsupported database protocol")
	}
}

func quoteDatabaseIdentifier(name string, d dialect.ID) string {
	switch {
	case d == dialect.ClickHouse:
		return "`" + strings.ReplaceAll(strings.ReplaceAll(name, "\\", "\\\\"), "`", "``") + "`"
	case d.UsesMySQLSyntax():
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case d == dialect.SQLServer:
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	default:
		return "\"" + strings.ReplaceAll(name, "\"", "\"\"") + "\""
	}
}

func unsupportedContext(d dialect.ID) error {
	return fmt.Errorf("changing database context is not supported for %s; open a configured selector", d)
}

func oracleConnectionOptions(c core.ConnectionConfig) map[string]string {
	options := map[string]string{"CONNECTION TIMEOUT": "10", "TIMEOUT": "120", "LOB FETCH": "post", "JSON FETCH": "text", "DBA PRIVILEGE": "NONE"}
	if c.OracleSYSDBA {
		options["DBA PRIVILEGE"] = "SYSDBA"
	}
	return options
}
