package proxy

import (
	"net"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/srvconn"
)

func TestNotifyDatabaseConnectionPreservesProtocolSettings(t *testing.T) {
	tests := []struct {
		protocol string
		setting  map[string]any
		check    func(t *testing.T, info DatabaseConnectionInfo)
	}{
		{
			protocol: srvconn.ProtocolMongoDB,
			setting: map[string]any{
				"auth_source": "users", "connection_options": "replicaSet=rs0",
			},
			check: func(t *testing.T, info DatabaseConnectionInfo) {
				if info.Host != "db.internal" || info.Port != 27017 ||
					info.ProxyURL != "http://127.0.0.1:32000" ||
					info.AuthSource != "users" || info.ConnectionOpts != "replicaSet=rs0" {
					t.Fatalf("unexpected MongoDB connection info: %#v", info)
				}
			},
		},
		{
			protocol: srvconn.ProtocolRedis,
			setting: map[string]any{
				"auth_username": false, "enable_cluster_mode": true,
			},
			check: func(t *testing.T, info DatabaseConnectionInfo) {
				if info.Host != "127.0.0.1" || info.Port != 32000 ||
					info.Username != "" || !info.ClusterMode {
					t.Fatalf("unexpected Redis connection info: %#v", info)
				}
			},
		},
		{
			protocol: srvconn.ProtocolSQLServer,
			setting:  map[string]any{"encrypt": true},
			check: func(t *testing.T, info DatabaseConnectionInfo) {
				if !info.Encrypt || info.DisableEncrypt {
					t.Fatalf("unexpected SQL Server encryption settings: %#v", info)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.protocol, func(t *testing.T) {
			token := &model.ConnectToken{
				Protocol: test.protocol,
				Account: model.Account{BaseAccount: model.BaseAccount{
					Username: "database-user", Secret: "secret",
				}},
				Asset: model.Asset{
					Address: "db.internal", SpecInfo: model.SpecInfo{DBName: "app"},
					Protocols: []model.Protocol{{Name: test.protocol, Port: 27017}},
				},
				Platform: model.Platform{Protocols: model.PlatformProtocols{{
					Protocol: model.Protocol{Name: test.protocol}, Setting: test.setting,
				}}},
			}
			result := make(chan DatabaseConnectionInfo, 1)
			server := &Server{
				connOpts: &ConnectionOptions{authInfo: token}, account: &token.Account,
				OnDatabaseConnection: func(info DatabaseConnectionInfo) { result <- info },
			}
			server.notifyDatabaseConnection(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32000})
			select {
			case info := <-result:
				test.check(t, info)
			case <-time.After(time.Second):
				t.Fatal("database connection notification timed out")
			}
		})
	}
}
