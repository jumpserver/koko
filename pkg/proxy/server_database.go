package proxy

import (
	"errors"
	"fmt"
	"net"

	"github.com/jumpserver-dev/sdk-go/model"
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

type DatabaseConnectionInfo struct {
	Protocol         string
	Host             string
	Port             int
	ServerName       string
	Username         string
	Password         string
	Database         string
	UseSSL           bool
	PGSSLMode        string
	CACert           string
	ClientCert       string
	ClientKey        string
	AllowInvalidCert bool
	Encrypt          bool
	DisableEncrypt   bool
	ClusterMode      bool
	AuthSource       string
	ConnectionOpts   string
	ProxyURL         string
	DataMaskingRules []model.DataMaskingRule
}

func (s *Server) notifyDatabaseConnection(localTunnelAddr *net.TCPAddr) {
	if s.OnDatabaseConnection == nil || !s.SupportsBackgroundExecution() {
		return
	}
	protocol := s.connOpts.authInfo.Protocol
	if !isDatabaseProtocol(protocol) {
		return
	}
	asset := s.connOpts.authInfo.Asset
	host := asset.Address
	port := asset.ProtocolPort(protocol)
	proxyURL := ""
	if localTunnelAddr != nil && protocol == srvconn.ProtocolMongoDB {
		proxyURL = fmt.Sprintf("http://%s", localTunnelAddr.String())
	} else if localTunnelAddr != nil {
		host = "127.0.0.1"
		port = localTunnelAddr.Port
	}
	username := s.account.Username
	var (
		encrypt        bool
		disableEncrypt bool
		clusterMode    bool
		authSource     string
		connectionOpts string
	)
	if platformProtocol, ok := s.connOpts.authInfo.Platform.GetProtocolSetting(protocol); ok {
		setting := platformProtocol.GetSetting()
		encrypt = protocol == srvconn.ProtocolSQLServer && setting.Encrypt
		disableEncrypt = protocol == srvconn.ProtocolSQLServer && !setting.Encrypt
		clusterMode = setting.EnableClusterMode
		authSource = setting.AuthSource
		connectionOpts = setting.ConnectionOpts
		if protocol == srvconn.ProtocolRedis {
			if raw, exists := platformProtocol.Setting["enable_cluster_mode"]; exists {
				clusterMode = parseBoolValue(raw)
			}
			if s.account.IsNull() || !setting.AuthUsername {
				username = ""
			}
		}
	}
	info := DatabaseConnectionInfo{
		Protocol: protocol, Host: host, Port: port, ServerName: asset.Address,
		Username: username, Password: s.account.Secret,
		Database: asset.SpecInfo.DBName,
		UseSSL:   asset.SpecInfo.UseSSL, PGSSLMode: asset.SpecInfo.PgSSLMode,
		CACert:     asset.SecretInfo.CaCert,
		ClientCert: asset.SecretInfo.ClientCert, ClientKey: asset.SecretInfo.ClientKey,
		AllowInvalidCert: asset.SpecInfo.AllowInvalidCert,
		Encrypt:          encrypt, DisableEncrypt: disableEncrypt,
		ClusterMode: clusterMode,
		AuthSource:  authSource, ConnectionOpts: connectionOpts, ProxyURL: proxyURL,
		DataMaskingRules: append(
			[]model.DataMaskingRule(nil), s.connOpts.authInfo.DataMaskingRules...,
		),
	}
	go s.OnDatabaseConnection(info)
}

func isDatabaseProtocol(protocol string) bool {
	switch protocol {
	case srvconn.ProtocolRedis, srvconn.ProtocolMongoDB,
		srvconn.ProtocolMySQL, srvconn.ProtocolMariadb,
		srvconn.ProtocolPostgresql, srvconn.ProtocolSQLServer,
		srvconn.ProtocolClickHouse, srvconn.ProtocolOracle:
		return true
	default:
		return false
	}
}

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
	opts = append(opts, srvconn.SqlMaskingRules(s.connOpts.authInfo.DataMaskingRules))
	srvConn, err = srvconn.NewUSQLConnection(opts...)

	return
}
