// Package oceanbase registers the official OceanBase Go driver for both tenant dialects.
package oceanbase

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"

	obdriver "github.com/oceanbase/go-oceanbase-driver"
	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
	"github.com/xo/usql/drivers/metadata"
	mymeta "github.com/xo/usql/drivers/metadata/mysql"
)

var (
	registeredTLS      = make(map[string]struct{})
	registeredTLSMutex sync.Mutex
)

func init() {
	for _, mode := range []struct {
		name   string
		oracle bool
	}{
		{"oceanbase", false},
		{"oboracle", true},
	} {
		mode := mode
		dburl.Register(dburl.Scheme{
			Driver:    mode.name,
			Transport: dburl.TransportTCP,
			Generator: func(u *dburl.URL) (string, string, error) {
				dsn, _, err := dburl.GenMysql(u)
				return dsn, "oceanbase", err
			},
		})
		d := drivers.Driver{
			AllowMultilineComments: true,
			AllowHashComments:      !mode.oracle,
			LexerName:              "mysql",
			UseColumnTypes:         true,
			Version: func(ctx context.Context, db drivers.DB) (string, error) {
				query := "SELECT VERSION()"
				if mode.oracle {
					query = "SELECT BANNER FROM V$VERSION"
				}
				var version string
				err := db.QueryRowContext(ctx, query).Scan(&version)
				return version, err
			},
			User: func(ctx context.Context, db drivers.DB) (string, error) {
				query := "SELECT CURRENT_USER()"
				if mode.oracle {
					query = "SELECT USER FROM DUAL"
				}
				var username string
				err := db.QueryRowContext(ctx, query).Scan(&username)
				return username, err
			},
			ForceParams: drivers.ForceQueryParameters([]string{
				"parseTime", "true", "loc", "Local",
			}),
			Err: func(err error) (string, string) {
				var databaseError *obdriver.MySQLError
				if errors.As(err, &databaseError) {
					return strconv.Itoa(int(databaseError.Number)), databaseError.Message
				}
				return "", err.Error()
			},
			IsPasswordErr: func(err error) bool {
				var databaseError *obdriver.MySQLError
				return errors.As(err, &databaseError) && databaseError.Number == 1045
			},
			Open: openOceanBase,
			Copy: drivers.CopyWithInsert(func(int) string { return "?" }),
		}
		if mode.oracle {
			d.LexerName = "oracle"
			d.NewMetadataReader = newOracleMetadataReader
			d.NewMetadataWriter = func(db drivers.DB, w io.Writer, opts ...metadata.ReaderOption) metadata.Writer {
				return metadata.NewDefaultWriter(newOracleMetadataReader(db, opts...))(db, w)
			}
		} else {
			d.NewMetadataReader = mymeta.NewReader
			d.NewMetadataWriter = func(db drivers.DB, w io.Writer, opts ...metadata.ReaderOption) metadata.Writer {
				return metadata.NewDefaultWriter(mymeta.NewReader(db, opts...))(db, w)
			}
			d.NewCompleter = mymeta.NewCompleter
		}
		drivers.Register(mode.name, d)
	}
}

func openOceanBase(_ context.Context, u *dburl.URL, _, _ func() io.Writer) (func(string, string) (*sql.DB, error), error) {
	return func(_, _ string) (*sql.DB, error) {
		params := u.Query()
		if params.Get("tls") == "custom" {
			name, err := oceanBaseTLSConfig(u, params.Get("ssl-ca"), params.Get("ssl-cert"), params.Get("ssl-key"))
			if err != nil {
				return nil, err
			}
			params.Set("tls", name)
		}
		params.Del("ssl-ca")
		params.Del("ssl-cert")
		params.Del("ssl-key")
		params.Del("ssl-insecure")
		params.Del("ssl-server-name")
		copyURL := *u
		copyURL.RawQuery = params.Encode()
		parsed, err := dburl.Parse(copyURL.String())
		if err != nil {
			return nil, err
		}
		return sql.Open("oceanbase", parsed.DSN)
	}, nil
}

func oceanBaseTLSConfig(u *dburl.URL, caPath, certPath, keyPath string) (string, error) {
	serverName := u.Query().Get("ssl-server-name")
	if serverName == "" {
		serverName = u.Hostname()
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	var identity []byte
	for _, path := range []string{caPath, certPath, keyPath} {
		if path == "" {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read OceanBase TLS file: %w", err)
		}
		identity = append(identity, content...)
		if path == caPath {
			pool, err := x509.SystemCertPool()
			if err != nil || pool == nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(content) {
				return "", errors.New("invalid OceanBase CA certificate")
			}
			config.RootCAs = pool
		}
	}
	if certPath != "" || keyPath != "" {
		if certPath == "" || keyPath == "" {
			return "", errors.New("OceanBase client certificate and key must be supplied together")
		}
		certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return "", fmt.Errorf("load OceanBase client certificate: %w", err)
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	config.InsecureSkipVerify = u.Query().Get("ssl-insecure") == "true"
	identity = append(identity, serverName...)
	identity = append(identity, []byte(strconv.FormatBool(config.InsecureSkipVerify))...)
	name := fmt.Sprintf("oceanbase-%x", sha256.Sum256(identity))
	registeredTLSMutex.Lock()
	defer registeredTLSMutex.Unlock()
	if _, exists := registeredTLS[name]; !exists {
		if err := obdriver.RegisterTLSConfig(name, config); err != nil {
			return "", fmt.Errorf("register OceanBase TLS configuration: %w", err)
		}
		registeredTLS[name] = struct{}{}
	}
	return name, nil
}
