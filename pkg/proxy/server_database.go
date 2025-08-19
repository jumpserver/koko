package proxy

import (
	"errors"
	"net"

	"github.com/jumpserver/koko/pkg/srvconn"
)

var usqlProtocolAlias = map[string]string{
	srvconn.ProtocolMySQL:      "mysql",
	srvconn.ProtocolMariadb:    "maria",
	srvconn.ProtocolPostgresql: "postgres",
	srvconn.ProtocolClickHouse: "clickhouse",
	srvconn.ProtocolSQLServer:  "sqlserver",
	srvconn.ProtocolOracle:     "oracle",
}

var errUnknownProtocol = errors.New("unknown protocol")

func (s *Server) getUSQLConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.USQLConn, err error) {

	platform := s.connOpts.authInfo.Platform
	asset := s.connOpts.authInfo.Asset
	protocol := s.connOpts.authInfo.Protocol
	host := asset.Address
	port := asset.ProtocolPort(protocol)
	if localTunnelAddr != nil {
		host = "127.0.0.1"
		port = localTunnelAddr.Port
	}

	schema, ok := usqlProtocolAlias[protocol]
	if !ok {
		return nil, errUnknownProtocol
	}
	disableSQLServerEncrypt := false
	if platformProtocol, ok1 := platform.GetProtocolSetting(protocol); ok1 {
		protocolSetting := platformProtocol.GetSetting()
		disableSQLServerEncrypt = !protocolSetting.Encrypt
	}

	opts := make([]srvconn.SqlOption, 0, 9)
	opts = append(opts, srvconn.SqlAssetName(asset.Name))
	opts = append(opts, srvconn.SqlSchema(schema))
	opts = append(opts, srvconn.SqlHost(host))
	opts = append(opts, srvconn.SqlPort(port))
	opts = append(opts, srvconn.SqlUsername(s.account.Username))
	opts = append(opts, srvconn.SqlPassword(s.account.Secret))
	opts = append(opts, srvconn.SqlDBName(asset.SpecInfo.DBName))
	opts = append(opts, srvconn.SqlUseSSL(asset.SpecInfo.UseSSL))
	opts = append(opts, srvconn.SqlPGSSLMode(asset.SpecInfo.PgSSLMode))
	opts = append(opts, srvconn.SqlCaCert(asset.SecretInfo.CaCert))
	opts = append(opts, srvconn.SqlClientCert(asset.SecretInfo.ClientCert))
	opts = append(opts, srvconn.SqlCertKey(asset.SecretInfo.ClientKey))
	opts = append(opts, srvconn.SqlAllowInvalidCert(asset.SpecInfo.AllowInvalidCert))
	opts = append(opts, srvconn.SqlDisableSqlServerEncrypt(disableSQLServerEncrypt))
	opts = append(opts, srvconn.SqlPtyWin(srvconn.Windows{
		Width:  s.UserConn.Pty().Window.Width,
		Height: s.UserConn.Pty().Window.Height,
	}))
	srvConn, err = srvconn.NewUSQLConnection(opts...)

	return
}
