package sessiontools

import (
	"fmt"
	"strconv"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/internal/dbconn"
	"github.com/jumpserver/koko/pkg/srvconn"
)

// DatabaseConfigFromToken maps the authorized asset before any gateway rewrites
// the dial target. Platform execution options remain distinct from TLS policy.
func DatabaseConfigFromToken(token model.ConnectToken) DatabaseConfig {
	c := dbconn.FromToken(token)
	config := DatabaseConfig{
		Protocol: token.Protocol, PlatformType: token.Platform.Type.Value,
		Host: c.Host, Port: c.Port, ServerName: c.TLS.ServerName,
		Username: c.User, Password: c.Password, Database: c.Database,
		UseSSL: c.TLS.Enabled, PGSSLMode: c.TLS.Mode,
		CACert: c.TLS.CACert, ClientCert: c.TLS.ClientCert, ClientKey: c.TLS.ClientKey,
		AllowInvalidCert: c.TLS.InsecureSkipVerify,
		DataMaskingRules: append([]model.DataMaskingRule(nil), token.DataMaskingRules...),
	}
	if setting, ok := token.Platform.GetProtocolSetting(token.Protocol); ok {
		options := setting.GetSetting()
		config.Encrypt = token.Protocol == srvconn.ProtocolSQLServer && options.Encrypt
		config.DisableEncrypt = token.Protocol == srvconn.ProtocolSQLServer && !options.Encrypt
		config.ClusterMode = options.EnableClusterMode
		config.AuthSource, config.ConnectionOpts = options.AuthSource, options.ConnectionOpts
		if config.PlatformType == "oceanbase" {
			config.TenantMode, _ = setting.Setting["tenant_mode"].(string)
		}
		if token.Protocol == srvconn.ProtocolRedis {
			if raw, exists := setting.Setting["enable_cluster_mode"]; exists {
				config.ClusterMode, _ = strconv.ParseBool(fmt.Sprint(raw))
			}
			if token.Account.IsNull() || !options.AuthUsername {
				config.Username = ""
			}
		}
	}
	return config
}

func (c DatabaseConfig) connectionConfig() dbconn.Config {
	return dbconn.Config{
		Dialect: c.Protocol, Host: c.Host, Port: c.Port,
		User: c.Username, Password: c.Password, Database: c.Database,
		DialContext: c.DialContext,
		TLS: dbconn.TLSConfig{
			Enabled: c.UseSSL, ServerName: c.ServerName, Mode: c.PGSSLMode,
			InsecureSkipVerify: c.AllowInvalidCert, CACert: c.CACert,
			ClientCert: c.ClientCert, ClientKey: c.ClientKey,
		},
	}
}
