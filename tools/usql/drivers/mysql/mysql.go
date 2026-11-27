package mysql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strconv"

	"github.com/go-sql-driver/mysql" // DRIVER
	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
	"github.com/xo/usql/drivers/metadata"
	mymeta "github.com/xo/usql/drivers/metadata/mysql"
)

func init() {
	d := drivers.Driver{
		AllowMultilineComments: true,
		AllowHashComments:      true,
		LexerName:              "mysql",
		UseColumnTypes:         true,
		ForceParams: drivers.ForceQueryParameters([]string{
			"parseTime", "true",
			"loc", "Local",
		}),
		Err: func(err error) (string, string) {
			if e, ok := err.(*mysql.MySQLError); ok {
				return strconv.Itoa(int(e.Number)), e.Message
			}
			return "", err.Error()
		},
		IsPasswordErr: func(err error) bool {
			if e, ok := err.(*mysql.MySQLError); ok {
				return e.Number == 1045
			}
			return false
		},
		NewMetadataReader: mymeta.NewReader,
		NewMetadataWriter: func(db drivers.DB, w io.Writer, opts ...metadata.ReaderOption) metadata.Writer {
			return metadata.NewDefaultWriter(mymeta.NewReader(db, opts...))(db, w)
		},
		Copy:         drivers.CopyWithInsert(func(int) string { return "?" }),
		NewCompleter: mymeta.NewCompleter,
	}

	d.Open = func(ctx context.Context, source *dburl.URL, f func() io.Writer, f2 func() io.Writer) (func(string, string) (*sql.DB, error), error) {
		return func(string, string) (*sql.DB, error) {
			u := *source // Retry/password prompts must not mutate a shared URL.
			u.RawQuery += u.RawFragment
			queryParams := u.Query()
			var customTLS *tls.Config
			if queryParams.Get("tls") == "custom" {
				var err error
				customTLS, err = connectionTLS(queryParams, u.Hostname())
				if err != nil {
					return nil, err
				}
				for _, key := range []string{"tls", "ssl-ca", "ssl-cert", "ssl-key", "ssl-server-name", "ssl-insecure"} {
					queryParams.Del(key)
				}
			}

			u.RawQuery = queryParams.Encode()
			newDSN, err := dburl.Parse(u.String())
			if err != nil {
				return nil, err
			}
			cfg, err := mysql.ParseDSN(newDSN.DSN)
			if err != nil {
				return nil, err
			}
			if hostIP, err := netip.ParseAddr(u.Hostname()); err == nil && hostIP.Is6() {
				// dburl omits IPv6 brackets; repair the final DSN after TLS options
				// have been processed so URL regeneration cannot discard the fix.
				port := u.Port()
				if port == "" {
					port = "3306"
				}
				cfg.Addr = net.JoinHostPort(u.Hostname(), port)
			}
			if customTLS != nil {
				cfg.TLS = customTLS
			}
			// A TLS config belongs to one connector, not the process-wide
			// "custom" registry entry shared by unrelated connections.
			connector, err := mysql.NewConnector(cfg)
			if err != nil {
				return nil, err
			}
			return sql.OpenDB(connector), nil
		}, nil
	}
	drivers.Register("mysql", d, "memsql", "vitess", "tidb")
}

func connectionTLS(params url.Values, host string) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	// Some MySQL/MariaDB servers only offer static RSA key exchange. Keep
	// Go's non-insecure TLS 1.2 suites and add only the two RSA AES-GCM suites;
	// RC4, 3DES and RSA-CBC remain disabled. TLS 1.3 suites are unaffected.
	for _, suite := range tls.CipherSuites() {
		if slices.Contains(suite.SupportedVersions, tls.VersionTLS12) {
			cfg.CipherSuites = append(cfg.CipherSuites, suite.ID)
		}
	}
	cfg.CipherSuites = append(cfg.CipherSuites,
		tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
	)
	if name := params.Get("ssl-server-name"); name != "" {
		cfg.ServerName = name
	}
	if skip := params.Get("ssl-insecure"); skip != "" {
		var err error
		cfg.InsecureSkipVerify, err = strconv.ParseBool(skip)
		if err != nil {
			return nil, fmt.Errorf("invalid ssl-insecure setting")
		}
	}
	if ca := params.Get("ssl-ca"); ca != "" {
		data, err := os.ReadFile(ca)
		if err != nil {
			return nil, fmt.Errorf("cannot read database CA certificate")
		}
		cfg.RootCAs = x509.NewCertPool()
		if !cfg.RootCAs.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("invalid database CA certificate PEM")
		}
	}
	cert, key := params.Get("ssl-cert"), params.Get("ssl-key")
	if (cert == "") != (key == "") {
		return nil, fmt.Errorf("database client certificate and key must be configured together")
	}
	if cert != "" {
		pair, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, fmt.Errorf("invalid database client certificate/key")
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	return cfg, nil
}
