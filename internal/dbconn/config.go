// Package dbconn constructs database transports shared by interactive and agent sessions.
package dbconn

import (
	"context"
	"net"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
)

// Config contains server-owned connection information, never browser output.
type Config struct {
	Dialect      string                                                  `json:"-"`
	OracleSYSDBA bool                                                    `json:"-"`
	Host         string                                                  `json:"-"`
	Port         int                                                     `json:"-"`
	User         string                                                  `json:"-"`
	Password     string                                                  `json:"-"`
	Database     string                                                  `json:"-"`
	Schema       string                                                  `json:"-"`
	Transport    string                                                  `json:"-"`
	TLS          TLSConfig                                               `json:"-"`
	DialContext  func(context.Context, string, string) (net.Conn, error) `json:"-"`
}

type TLSConfig struct {
	Enabled            bool   `json:"-"`
	ServerName         string `json:"-"`
	InsecureSkipVerify bool   `json:"-"`
	Mode               string `json:"-"`
	CACert             string `json:"-"`
	ClientCert         string `json:"-"`
	ClientKey          string `json:"-"`
}

// FromToken preserves the asset's certificate identity when callers later replace
// Host and Port with a gateway's local listener.
func FromToken(token model.ConnectToken) Config {
	asset := token.Asset
	return Config{
		Dialect: token.Protocol, Host: asset.Address, Port: asset.ProtocolPort(token.Protocol),
		User: token.Account.Username, Password: token.Account.Secret, Database: asset.SpecInfo.DBName,
		OracleSYSDBA: strings.EqualFold(token.Protocol, "oracle") && token.ConnectOptions.UseSysDBA,
		TLS: TLSConfig{
			Enabled: asset.SpecInfo.UseSSL, ServerName: asset.Address,
			InsecureSkipVerify: asset.SpecInfo.AllowInvalidCert, Mode: asset.SpecInfo.PgSSLMode,
			CACert: asset.SecretInfo.CaCert, ClientCert: asset.SecretInfo.ClientCert, ClientKey: asset.SecretInfo.ClientKey,
		},
	}
}
