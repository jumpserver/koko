package dbconn

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"testing"

	db2 "github.com/go-db2/go-db2"
	"github.com/jumpserver-dev/sdk-go/model"
)

func TestTokenConfigPreservesAssetIdentity(t *testing.T) {
	token := model.ConnectToken{Protocol: "OrAcLe"}
	token.Asset.Address = "asset.internal"
	token.Asset.Protocols = []model.Protocol{{Name: "oracle", Port: 1521}}
	token.Asset.SpecInfo = model.SpecInfo{DBName: "service", UseSSL: true}
	token.Account.Username, token.Account.Secret = "user", "secret"
	token.ConnectOptions.UseSysDBA = true
	c := FromToken(token)
	if c.Host != "asset.internal" || c.Port != 1521 || c.TLS.ServerName != c.Host || !c.TLS.Enabled || !c.OracleSYSDBA || c.User != "user" || c.Password != "secret" || c.Database != "service" {
		t.Fatal("token connection settings were lost")
	}
	encoded, err := json.Marshal(c)
	if err != nil || string(encoded) != "{}" {
		t.Fatal("server connection settings must not be exposed as JSON")
	}
}

func TestConnectorsKeepAuthorizedDialTarget(t *testing.T) {
	for _, protocol := range []string{"mysql", "oceanbase", "oracle", "db2"} {
		t.Run(protocol, func(t *testing.T) {
			denied := errors.New("test dial stopped")
			called := false
			config := Config{Host: "127.0.0.1", Port: 1234, User: "user", Database: "db", TLS: TLSConfig{ServerName: "asset.internal"}}
			if protocol == "db2" {
				config.Host = "::1"
			}
			config.DialContext = func(_ context.Context, _, address string) (net.Conn, error) {
				called = true
				if address != net.JoinHostPort(config.Host, strconv.Itoa(config.Port)) {
					t.Fatalf("dial escaped the gateway: %s", address)
				}
				return nil, denied
			}
			var connector driver.Connector
			var err error
			switch protocol {
			case "mysql":
				connector, err = MySQLConnector(config, MySQLOptions{})
			case "oceanbase":
				connector, err = OceanBaseConnector(config, MySQLOptions{})
			case "oracle":
				connector = OracleConnector(config, nil, nil)
			case "db2":
				var cfg *db2.Config
				cfg, err = DB2Config(config, nil)
				if err == nil {
					connector = db2.NewConnector(cfg)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = connector.Connect(t.Context()); err == nil || !called {
				t.Fatalf("connector did not use its authorized dialer: %v", err)
			}
		})
	}
}

func TestTLSPreservesAssetIdentityAndVerification(t *testing.T) {
	c := Config{Dialect: "postgresql", Host: "127.0.0.1", TLS: TLSConfig{Enabled: true, ServerName: "asset.internal", Mode: "verify-full"}}
	secure, err := MakeTLS(c, false)
	if err != nil || secure.ServerName != "asset.internal" || secure.InsecureSkipVerify || secure.MinVersion != tls.VersionTLS12 {
		t.Fatalf("lost asset TLS policy: %+v, %v", secure, err)
	}
	c.TLS.Mode = "verify-ca"
	secure, err = MakeTLS(c, false)
	if err != nil || secure.VerifyConnection == nil || secure.VerifyConnection(tls.ConnectionState{}) == nil {
		t.Fatal("verify-ca must validate the certificate chain")
	}
	c.TLS.InsecureSkipVerify = true
	if _, err := MakeTLS(c, false); err == nil {
		t.Fatal("conflicting certificate verification accepted")
	}
	c.TLS = TLSConfig{Enabled: true, CACert: "invalid"}
	if _, err := MakeTLS(c, true); err == nil {
		t.Fatal("invalid CA accepted")
	}
}
