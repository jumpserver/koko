package db2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
)

func TestURLCredentialsAndTLS(t *testing.T) {
	for _, scheme := range []string{"db2", "db", "db2s", "db2s+tcp"} {
		t.Run(scheme, func(t *testing.T) {
			u := url.URL{Scheme: scheme, Host: "[::1]:50000", Path: "/sample", User: url.UserPassword("user@realm", "p@ss:;?#+%密码")}
			parsed, err := dburl.Parse(u.String())
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := parseConfig(parsed.DSN)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Driver != "db2" || cfg.Host != "::1" || cfg.Port != 50000 || cfg.Database != "sample" || cfg.User != "user@realm" || cfg.Password != "p@ss:;?#+%密码" {
				t.Fatal("URL components did not round-trip correctly")
			}
			if cfg.UseSSL != strings.HasPrefix(scheme, "db2s") {
				t.Fatal("incorrect TLS selection")
			}
		})
	}
}

func TestInvalidURLs(t *testing.T) {
	for _, dsn := range []string{
		"db2://host", "db2:///sample", "db2://host:65536/sample", "db2://host/sample#fragment",
		"db2://host/abcdefghijklmnopqrs", "db2://host/sample?ssl=typo", "db2s://host/sample?ssl=false",
		"db2://host/sample?ssl=true&ssl=false", "db2s://host/sample?ssl=true&ssl=false",
		"db2://host/sample?ssl-ca=root.pem", "db2://host/sample?ssl=true&ssl-cert=cert.pem",
		"db2://host/sample?timeout=0s", "db2://host/sample?timeout=typo", "db2://host/sample?sslmode=disable",
		"db2://host/sample?secmec=typo", "db2://user:secret%ZZ@host/sample",
	} {
		t.Run(dsn, func(t *testing.T) {
			if _, err := dburl.Parse(dsn); err == nil {
				t.Fatal("invalid URL accepted")
			}
		})
	}
	if _, err := parseConfig("db2://user:password%ZZ@host/sample"); err == nil || strings.Contains(err.Error(), "password") {
		t.Fatal("URL error must not echo credentials")
	}
}

func TestAuthenticationErrorRecognition(t *testing.T) {
	u, _ := dburl.Parse("db2://host/sample")
	if !drivers.IsPasswordErr(u, fmt.Errorf("authentication failed: SECCHKCD=15")) {
		t.Fatal("pinned driver's authentication error was not recognized")
	}
	if drivers.IsPasswordErr(u, errors.New("network timeout")) || drivers.IsPasswordErr(u, nil) {
		t.Fatal("non-authentication error treated as a password failure")
	}
}

func TestConfiguredAddressAndCancellation(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1"} {
		t.Run(host, func(t *testing.T) {
			listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			listener.(*net.TCPListener).SetDeadline(time.Now().Add(3 * time.Second))
			reached := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					reached <- err
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				// Receiving a DRDA DSS header proves the exact configured socket
				// was reached. Then withhold replies to exercise context cancel.
				var header [6]byte
				_, err = io.ReadFull(conn, header[:])
				if err == nil && header[2] != 0xd0 {
					err = errors.New("not a DRDA DSS header")
				}
				reached <- err
				io.Copy(io.Discard, conn)
			}()
			db, err := open("db2", "db2://probe:test@"+listener.Addr().String()+"/sample")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			if err := db.PingContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("PingContext error = %v, want cancellation", err)
			}
			if err := <-reached; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStatementProcessing(t *testing.T) {
	u, _ := dburl.Parse("db2://host/sample")
	for _, prefix := range []string{"SELECT", "VALUES", "WITH", "TABLE"} {
		_, sqlstr, query, err := drivers.Process(u, prefix, "VALUES ';'; ")
		if err != nil || !query || sqlstr != "VALUES ';'" {
			t.Fatalf("incorrect processing for %s: %q, query=%v, error=%v", prefix, sqlstr, query, err)
		}
	}
}
