package proxy

import (
	"net"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
)

func (s *Server) getMySQLConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.MySQLConn, err error) {
	asset := s.connOpts.authInfo.Asset
	protocol := s.connOpts.authInfo.Protocol
	host := asset.Address
	port := asset.ProtocolPort(protocol)
	if localTunnelAddr != nil {
		host = "127.0.0.1"
		port = localTunnelAddr.Port
	}
	mysqlOpts := make([]srvconn.SqlOption, 0, 7)
	mysqlOpts = append(mysqlOpts, srvconn.SqlHost(host))
	mysqlOpts = append(mysqlOpts, srvconn.SqlPort(port))
	mysqlOpts = append(mysqlOpts, srvconn.SqlUsername(s.account.Username))
	mysqlOpts = append(mysqlOpts, srvconn.SqlPassword(s.account.Secret))
	mysqlOpts = append(mysqlOpts, srvconn.SqlDBName(asset.SpecInfo.DBName))
	mysqlOpts = append(mysqlOpts, srvconn.SqlPtyWin(srvconn.Windows{
		Width:  s.UserConn.Pty().Window.Width,
		Height: s.UserConn.Pty().Window.Height,
	}))
	tokenConnOpts := s.connOpts.authInfo.ConnectOptions
	switch {
	case tokenConnOpts.DisableAutoHash != nil:
		if *tokenConnOpts.DisableAutoHash {
			mysqlOpts = append(mysqlOpts, srvconn.MySQLDisableAutoReHash())
		}
		logger.Debugf("Connection token set disableAutoHash: %v", *tokenConnOpts.DisableAutoHash)
	case s.connOpts.params != nil:
		if s.connOpts.params.DisableMySQLAutoHash {
			mysqlOpts = append(mysqlOpts, srvconn.MySQLDisableAutoReHash())
			logger.Debugf("Connection params set disableAutoHash: true")
		}

	}
	srvConn, err = srvconn.NewMySQLConnection(mysqlOpts...)
	return
}

func (s *Server) getSQLServerConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.SQLServerConn, err error) {
	asset := s.connOpts.authInfo.Asset
	protocol := s.connOpts.authInfo.Protocol
	host := asset.Address
	port := asset.ProtocolPort(protocol)
	if localTunnelAddr != nil {
		host = "127.0.0.1"
		port = localTunnelAddr.Port
	}
	srvConn, err = srvconn.NewSQLServerConnection(
		srvconn.SqlHost(host),
		srvconn.SqlPort(port),
		srvconn.SqlUsername(s.account.Username),
		srvconn.SqlPassword(s.account.Secret),
		srvconn.SqlDBName(asset.SpecInfo.DBName),
		srvconn.SqlPtyWin(srvconn.Windows{
			Width:  s.UserConn.Pty().Window.Width,
			Height: s.UserConn.Pty().Window.Height,
		}),
	)
	return
}

func (s *Server) getPostgreSQLConn(localTunnelAddr *net.TCPAddr) (srvConn *srvconn.PostgreSQLConn, err error) {
	asset := s.connOpts.authInfo.Asset
	protocol := s.connOpts.authInfo.Protocol
	host := asset.Address
	port := asset.ProtocolPort(protocol)
	if localTunnelAddr != nil {
		host = "127.0.0.1"
		port = localTunnelAddr.Port
	}
	srvConn, err = srvconn.NewPostgreSQLConnection(
		srvconn.SqlHost(host),
		srvconn.SqlPort(port),
		srvconn.SqlUsername(s.account.Username),
		srvconn.SqlPassword(s.account.Secret),
		srvconn.SqlDBName(asset.SpecInfo.DBName),
		srvconn.SqlPtyWin(srvconn.Windows{
			Width:  s.UserConn.Pty().Window.Width,
			Height: s.UserConn.Pty().Window.Height,
		}),
	)
	return
}
