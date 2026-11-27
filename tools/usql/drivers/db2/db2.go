// Package db2 registers IBM Db2 LUW support without a native CLI dependency.
package db2

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	db2driver "github.com/go-db2/go-db2"
	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
)

func init() {
	dburl.Register(dburl.Scheme{
		Driver: "db2", Transport: dburl.TransportTCP, Aliases: []string{"db2s"},
		Generator: func(u *dburl.URL) (string, string, error) {
			v := u.URL
			v.Scheme = "db2"
			if strings.SplitN(u.OriginalScheme, "+", 2)[0] == "db2s" {
				q := v.Query()
				if q.Has("ssl") {
					enabled, err := strconv.ParseBool(q.Get("ssl"))
					if err != nil || !enabled {
						return "", "", errors.New("db2s requires ssl=true")
					}
				} else {
					q.Set("ssl", "true")
				}
				v.RawQuery = q.Encode()
			}
			if _, err := parseConfig(v.String()); err != nil {
				return "", "", err
			}
			return v.String(), "", nil
		},
	})
	drivers.Register("db2", drivers.Driver{
		AllowMultilineComments: true,
		UseColumnTypes:         true,
		Open: func(_ context.Context, _ *dburl.URL, _, _ func() io.Writer) (func(string, string) (*sql.DB, error), error) {
			return open, nil
		},
		Version: func(ctx context.Context, db drivers.DB) (string, error) {
			var version string
			err := db.QueryRowContext(ctx, `SELECT SERVICE_LEVEL FROM SYSIBMADM.ENV_INST_INFO`).Scan(&version)
			return version, err
		},
		User: func(ctx context.Context, db drivers.DB) (string, error) {
			var user string
			err := db.QueryRowContext(ctx, `VALUES CURRENT USER`).Scan(&user)
			return user, err
		},
		IsPasswordErr: func(err error) bool {
			if err == nil {
				return false
			}
			var e *db2driver.Db2Error
			return errors.Is(err, db2driver.ErrAuthenticationFailed) ||
				errors.As(err, &e) && e.SQLState == "28000" ||
				strings.HasPrefix(err.Error(), "authentication failed: SECCHKCD=")
		},
		Process: func(_ *dburl.URL, prefix, sqlstr string) (string, string, bool, error) {
			sqlstr = strings.TrimSuffix(strings.TrimSpace(sqlstr), ";")
			typ, query := drivers.QueryExecType(prefix, sqlstr)
			return typ, sqlstr, query, nil
		},
		Copy:              drivers.CopyWithInsert(func(int) string { return "?" }),
		NewMetadataReader: newMetadataReader,
	})
}

// parseConfig keeps endpoint/credentials in URL components, never interpolated
// into a semicolon-separated DSN. Unsupported options fail rather than silently
// ignoring a misspelled TLS setting.
func parseConfig(dsn string) (*db2driver.Config, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, errors.New("invalid Db2 connection URL")
	}
	if u.Scheme != "db2" {
		return nil, errors.New("Db2 requires a db2 connection URL")
	}
	if u.Hostname() == "" || strings.TrimPrefix(u.Path, "/") == "" || u.Fragment != "" {
		return nil, errors.New("Db2 requires a host and database name, without a URL fragment")
	}
	cfg := db2driver.NewConfig()
	cfg.Host, cfg.Database = u.Hostname(), strings.TrimPrefix(u.Path, "/")
	if len(cfg.Database) > 18 {
		return nil, errors.New("Db2 database name exceeds the driver's 18-byte limit")
	}
	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}
	if u.Port() != "" {
		cfg.Port, err = strconv.Atoi(u.Port())
		if err != nil || cfg.Port < 1 || cfg.Port > 65535 {
			return nil, errors.New("invalid Db2 port")
		}
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, errors.New("invalid Db2 connection options")
	}
	for key, values := range q {
		if len(values) != 1 {
			return nil, fmt.Errorf("duplicate Db2 option %q", key)
		}
		value := values[0]
		switch key {
		case "ssl":
			cfg.UseSSL, err = strconv.ParseBool(value)
			if err != nil {
				return nil, errors.New("Db2 ssl must be true or false")
			}
		case "ssl-ca":
			cfg.SSLRootCAPath = value
		case "ssl-cert":
			cfg.SSLClientCertPath = value
		case "ssl-key":
			cfg.SSLClientKeyPath = value
		case "ssl-server-name":
			// Loaded into TLSConfig in open, with certificate verification enabled.
		case "timeout":
			cfg.Timeout, err = time.ParseDuration(value)
			if err != nil || cfg.Timeout <= 0 {
				return nil, errors.New("Db2 timeout must be a positive duration, e.g. 30s")
			}
		case "secmec":
			switch value {
			case "3":
				cfg.SecurityMechanism = 3
			case "9":
				cfg.SecurityMechanism = 9
			default:
				return nil, errors.New("Db2 secmec must be 3 or 9")
			}
		default:
			return nil, fmt.Errorf("unsupported Db2 option %q", key)
		}
	}
	if !cfg.UseSSL && (cfg.SSLRootCAPath != "" || cfg.SSLClientCertPath != "" || cfg.SSLClientKeyPath != "" || q.Get("ssl-server-name") != "") {
		return nil, errors.New("Db2 certificate options require ssl=true")
	}
	if (cfg.SSLClientCertPath == "") != (cfg.SSLClientKeyPath == "") {
		return nil, errors.New("Db2 client certificate and key must be supplied together")
	}
	return cfg, nil
}

func open(_, dsn string) (*sql.DB, error) {
	cfg, err := parseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if cfg.UseSSL {
		u, _ := url.Parse(dsn)
		serverName := u.Query().Get("ssl-server-name")
		if serverName == "" {
			serverName = cfg.Host
		}
		cfg.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
		if cfg.SSLRootCAPath != "" {
			data, err := os.ReadFile(cfg.SSLRootCAPath)
			if err != nil {
				return nil, fmt.Errorf("read Db2 root CA: %w", err)
			}
			cfg.TLSConfig.RootCAs = x509.NewCertPool()
			if !cfg.TLSConfig.RootCAs.AppendCertsFromPEM(data) {
				return nil, errors.New("invalid Db2 root CA")
			}
		}
		if cfg.SSLClientCertPath != "" {
			cert, err := tls.LoadX509KeyPair(cfg.SSLClientCertPath, cfg.SSLClientKeyPath)
			if err != nil {
				return nil, fmt.Errorf("load Db2 client certificate: %w", err)
			}
			cfg.TLSConfig.Certificates = []tls.Certificate{cert}
		}
	}
	// The pinned upstream driver formats Host:Port itself, so bracket IPv6
	// literals here while keeping TLSConfig.ServerName as the original host.
	if strings.Contains(cfg.Host, ":") {
		cfg.Host = "[" + cfg.Host + "]"
	}
	return sql.OpenDB(db2driver.NewConnector(cfg)), nil
}
