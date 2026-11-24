package core

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
)

func managedConnectionConfig(token model.ConnectToken) (ConnectionConfig, error) {
	port := token.Asset.ProtocolPort(token.Protocol)
	if port < 1 || port > 65535 {
		return ConnectionConfig{}, errors.New("invalid_authorized_database_port")
	}
	protocol := token.Protocol
	settings := token.Platform.GetProtocol(protocol).Setting
	tenant := ""
	if strings.EqualFold(token.Platform.Type.Value, "oceanbase") {
		protocol = "oceanbase"
		tenant, _ = settings["tenant_mode"].(string)
	}
	dialect, err := CanonicalDialect(protocol, tenant)
	if err != nil {
		return ConnectionConfig{}, err
	}
	asset := token.Asset
	c := ConnectionConfig{
		Dialect: dialect, Host: asset.Address, Port: port,
		User: token.Account.Username, Password: token.Account.Secret, Database: asset.SpecInfo.DBName,
		OracleSYSDBA: dialect == "oracle" && token.ConnectOptions.UseSysDBA,
		TLS: TLSConfig{
			Enabled: asset.SpecInfo.UseSSL, ServerName: asset.Address,
			InsecureSkipVerify: asset.SpecInfo.AllowInvalidCert, Mode: asset.SpecInfo.PgSSLMode,
			CACert: asset.SecretInfo.CaCert, ClientCert: asset.SecretInfo.ClientCert, ClientKey: asset.SecretInfo.ClientKey,
		},
	}
	switch dialect {
	case "clickhouse":
		if value, configured := settings["transport"]; configured {
			transport, _ := value.(string)
			c.Transport = strings.ToLower(strings.TrimSpace(transport))
			if c.Transport != "http" && c.Transport != "native" {
				return ConnectionConfig{}, errors.New("invalid_clickhouse_transport_option")
			}
		} else {
			switch c.Port {
			case 8123, 8443:
				c.Transport = "http"
			case 9000, 9440:
				c.Transport = "native"
			default:
				c.Transport = "auto"
			}
		}
	case "sqlserver":
		encrypt := true
		if value, ok := settings["encrypt"]; ok {
			encrypt, err = strconv.ParseBool(fmt.Sprint(value))
			if err != nil {
				return ConnectionConfig{}, errors.New("invalid_sqlserver_encrypt_option")
			}
		}
		if encrypt && !c.TLS.Enabled {
			c.TLS.Enabled = true
			c.TLS.InsecureSkipVerify = true
		}
	}
	return c, nil
}
