package srvconn

import (
	"net"
	"regexp"
	"strconv"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/srvconn/telnetlib"
)

type ServerTelnetConnection struct {
	User                 *model.User
	Asset                *model.Asset
	SystemUser           *model.SystemUser
	Overtime             time.Duration
	CustomString         string
	CustomSuccessPattern *regexp.Regexp

	conn      *telnetlib.Client
	proxyConn *gossh.Client
	closed    bool
}

func (tc *ServerTelnetConnection) Timeout() time.Duration {
	if tc.Overtime == 0 {
		tc.Overtime = 30 * time.Second
	}
	return tc.Overtime
}

func (tc *ServerTelnetConnection) Protocol() string {
	return "telnet"
}

func (tc *ServerTelnetConnection) Connect(h, w int, term string) (err error) {
	var ip = tc.Asset.IP
	var port = strconv.Itoa(tc.Asset.ProtocolPort("telnet"))
	var asset = tc.Asset
	var proxyConn *gossh.Client

	if asset.Domain != "" {
		sshConfig := MakeConfig(tc.Asset, tc.SystemUser, tc.Timeout())
		proxyConn, err = sshConfig.DialProxy()
		if err != nil {
			logger.Errorf("Dial proxy host error: %s", err)
			return
		}
	}

	addr := net.JoinHostPort(ip, port)
	var conn net.Conn
	// 判断是否有合适的proxy连接
	if proxyConn != nil {
		logger.Debugf("Connect host %s via proxy", tc.Asset.Hostname)
		conn, err = proxyConn.Dial("tcp", addr)
	} else {
		logger.Debugf("Direct connect host %s", tc.Asset.Hostname)
		conn, err = net.DialTimeout("tcp", addr, tc.Timeout())
	}
	if err != nil {
		logger.Errorf("Telnet host %s err: %s", asset.Hostname, err)
		if proxyConn != nil {
			_ = proxyConn.Close()
		}
		return
	}

	client, err := telnetlib.NewClientConn(conn, &telnetlib.ClientConfig{
		User:     tc.SystemUser.Username,
		Password: tc.SystemUser.Password,
		Timeout:  tc.Timeout(),
		TTYOptions: &telnetlib.TerminalOptions{
			Wide:  w,
			High:  h,
			Xterm: term,
		},
		CustomString: tc.CustomString,
	})
	if err != nil {
		return err
	}
	tc.conn = client
	tc.proxyConn = proxyConn
	logger.Infof("Telnet host %s success", asset.Hostname)
	return nil
}

func (tc *ServerTelnetConnection) SetWinSize(w, h int) error {
	return tc.conn.WindowChange(w, h)
}

func (tc *ServerTelnetConnection) Read(p []byte) (n int, err error) {
	return tc.conn.Read(p)
}

func (tc *ServerTelnetConnection) Write(p []byte) (n int, err error) {
	return tc.conn.Write(p)
}

func (tc *ServerTelnetConnection) Close() (err error) {
	if tc.closed {
		return
	}
	if tc.proxyConn != nil {
		_ = tc.proxyConn.Close()
		logger.Infof("Asset %s close gateway connection first", tc.Asset.Hostname)
	}
	err = tc.conn.Close()
	tc.closed = true
	return
}
