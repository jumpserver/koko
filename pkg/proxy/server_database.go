package proxy

import (
	"errors"
	"fmt"
	"net"

	"github.com/jumpserver/koko/internal/sessiontools"
	"github.com/jumpserver/koko/pkg/srvconn"
)

var usqlProtocolAlias = map[string]string{
	srvconn.ProtocolMySQL:      "mysql",
	srvconn.ProtocolMariadb:    "maria",
	srvconn.ProtocolPostgresql: "postgres",
	srvconn.ProtocolClickHouse: "clickhouse",
	srvconn.ProtocolSQLServer:  "sqlserver",
	srvconn.ProtocolOracle:     "oracle",
	srvconn.ProtocolDameng:     "dameng",
	srvconn.ProtocolDB2:        "db2",
}

var errUnknownProtocol = errors.New("unknown protocol")

type DatabaseConnectionInfo = sessiontools.DatabaseConfig

func (s *Server) notifyDatabaseConnection(localTunnelAddr *net.TCPAddr) {
	if s.OnDatabaseConnection == nil || !s.SupportsBackgroundExecution() {
		return
	}
	protocol := s.connOpts.authInfo.Protocol
	if !isDatabaseProtocol(protocol) {
		return
	}
	info := s.databaseConnectionInfo(localTunnelAddr)
	go s.OnDatabaseConnection(info)
}

func (s *Server) databaseConnectionInfo(localTunnelAddr *net.TCPAddr) DatabaseConnectionInfo {
	token := *s.connOpts.authInfo
	token.Account = *s.account
	info := sessiontools.DatabaseConfigFromToken(token)
	if token.Protocol == srvconn.ProtocolRedis {
		// Older Redis platforms omit protocol settings and use password-only auth.
		if _, configured := token.Platform.GetProtocolSetting(token.Protocol); !configured {
			info.Username = ""
		}
	}
	if localTunnelAddr != nil && info.Protocol == srvconn.ProtocolMongoDB {
		info.ProxyURL = fmt.Sprintf("http://%s", localTunnelAddr.String())
	} else if localTunnelAddr != nil {
		info.Host, info.Port = localIP, localTunnelAddr.Port
	}
	return info
}

func (s *Server) databaseCLIOptions(info DatabaseConnectionInfo) []srvconn.SqlOption {
	win := s.UserConn.Pty().Window
	return []srvconn.SqlOption{
		srvconn.SqlAssetName(s.connOpts.authInfo.Asset.Name),
		srvconn.SqlHost(info.Host),
		srvconn.SqlServerName(info.ServerName),
		srvconn.SqlPort(info.Port),
		srvconn.SqlUsername(info.Username),
		srvconn.SqlPassword(info.Password),
		srvconn.SqlDBName(info.Database),
		srvconn.SqlUseSSL(info.UseSSL),
		srvconn.SqlPGSSLMode(info.PGSSLMode),
		srvconn.SqlCaCert(info.CACert),
		srvconn.SqlClientCert(info.ClientCert),
		srvconn.SqlCertKey(info.ClientKey),
		srvconn.SqlPtyWin(srvconn.Windows{Width: win.Width, Height: win.Height}),
	}
}

func isDatabaseProtocol(protocol string) bool {
	switch protocol {
	case srvconn.ProtocolRedis, srvconn.ProtocolMongoDB,
		srvconn.ProtocolMySQL, srvconn.ProtocolMariadb,
		srvconn.ProtocolPostgresql, srvconn.ProtocolSQLServer,
		srvconn.ProtocolClickHouse, srvconn.ProtocolOracle,
		srvconn.ProtocolDameng, srvconn.ProtocolDB2:
		return true
	default:
		return false
	}
}

func (s *Server) getUSQLConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.USQLConn, err error) {
	info := s.databaseConnectionInfo(localTunnelAddr)
	platform := s.connOpts.authInfo.Platform
	protocol := info.Protocol
	schema, ok := usqlProtocolAlias[protocol]
	if !ok {
		return nil, errUnknownProtocol
	}
	if info.PlatformType == "oceanbase" {
		if protocol != srvconn.ProtocolMySQL {
			return nil, fmt.Errorf("OceanBase requires the mysql wire protocol, got %s", protocol)
		}
		if _, found := platform.GetProtocolSetting(protocol); !found {
			return nil, errors.New("OceanBase tenant mode is not configured")
		}
		switch info.TenantMode {
		case "mysql":
			schema = "oceanbase"
		case "oracle":
			schema = "oboracle"
		default:
			return nil, fmt.Errorf("unsupported OceanBase tenant mode %q", info.TenantMode)
		}
		if err := srvconn.CheckUSQLDriver(schema); err != nil {
			return nil, err
		}
	}
	opts := s.databaseCLIOptions(info)
	opts = append(opts, srvconn.SqlSchema(schema), srvconn.SqlAllowInvalidCert(info.AllowInvalidCert))
	opts = append(opts, srvconn.SqlDisableSqlServerEncrypt(info.DisableEncrypt))
	opts = append(opts, srvconn.SqlUseOracleSYSDBA(
		protocol == srvconn.ProtocolOracle && s.connOpts.authInfo.ConnectOptions.UseSysDBA,
	))
	opts = append(opts, srvconn.SqlMaskingRules(info.DataMaskingRules))
	srvConn, err = srvconn.NewUSQLConnection(opts...)

	return
}
