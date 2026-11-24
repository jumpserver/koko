package proxy

import (
	"net"
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
)

func TestDatabaseConnectionInfoPreservesGatewayAndAccountPolicy(t *testing.T) {
	tunnel := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
	for _, tc := range []struct {
		protocol string
		tunnel   *net.TCPAddr
		host     string
		port     int
		username string
		proxyURL string
	}{
		{"mysql", nil, "database.internal", 1234, "input-user", ""},
		{"mysql", tunnel, "127.0.0.1", 12345, "input-user", ""},
		{"mongodb", tunnel, "database.internal", 1234, "input-user", "http://127.0.0.1:12345"},
		{"redis", tunnel, "127.0.0.1", 12345, "", ""},
	} {
		server := &Server{
			connOpts: &ConnectionOptions{authInfo: &model.ConnectToken{
				Protocol: tc.protocol,
				Asset: model.Asset{Address: "database.internal", SpecInfo: model.SpecInfo{UseSSL: true},
					Protocols: []model.Protocol{{Name: tc.protocol, Port: 1234}}},
			}},
			account: &model.Account{BaseAccount: model.BaseAccount{Username: "input-user", Secret: "input-password"}},
		}
		info := server.databaseConnectionInfo(tc.tunnel)
		if info.Host != tc.host || info.Port != tc.port || info.ProxyURL != tc.proxyURL ||
			info.Username != tc.username || info.Password != "input-password" ||
			!info.UseSSL || info.ServerName != "database.internal" {
			t.Fatalf("%s lost transport identity or account policy", tc.protocol)
		}
	}
}
