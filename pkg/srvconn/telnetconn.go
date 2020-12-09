package srvconn

import (
	"io"
	"net"
	"regexp"
	"strconv"
	"time"

	"github.com/LeeEirc/tclientlib"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/text/transform"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

type ServerTelnetConnection struct {
	User                 *model.User
	Asset                *model.Asset
	SystemUser           *model.SystemUser
	Overtime             time.Duration
	CustomString         string
	CustomSuccessPattern *regexp.Regexp
	Charset              string

	conn      *tclientlib.Client
	proxyConn *gossh.Client
	closed    bool

	transformReader io.Reader
	transformWriter io.Writer
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

func (tc *ServerTelnetConnection) KeepAlive() error {
	return nil
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
		logger.Infof("Connect host %s via proxy", tc.Asset.Hostname)
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
	tclientlib.SetMode(tclientlib.DebugMode)
	loginSuccessReg := tclientlib.DefaultLoginSuccessPattern
	if tc.CustomString != "" {
		tc.CustomSuccessPattern, err = regexp.Compile(tc.CustomString)
		if err == nil {
			loginSuccessReg = tc.CustomSuccessPattern
			logger.Infof("Telnet conn use custom pattern %s", tc.CustomString)
		}
	}

	client, err := tclientlib.NewClientConn(conn, &tclientlib.Config{
		Username: tc.SystemUser.Username,
		Password: tc.SystemUser.Password,
		Timeout:  tc.Timeout(),
		TTYOptions: &tclientlib.TerminalOptions{
			Wide:     w,
			High:     h,
			TermType: term,
		},
		LoginSuccessRegex: loginSuccessReg,
	})
	if err != nil {
		return err
	}
	tc.conn = client
	tc.transformReader = client
	tc.transformWriter = client
	if tc.Charset != model.UTF8 {
		if readDecode := model.LookupCharsetDecode(tc.Charset); readDecode != nil {
			tc.transformReader = transform.NewReader(client, readDecode)
		}
		if writerEncode := model.LookupCharsetEncode(tc.Charset); writerEncode != nil {
			tc.transformWriter = transform.NewWriter(client, writerEncode)
		}
	}
	tc.proxyConn = proxyConn
	logger.Infof("Telnet host %s success", asset.Hostname)
	return nil
}

func (tc *ServerTelnetConnection) SetWinSize(w, h int) error {
	return tc.conn.WindowChange(w, h)
}

func (tc *ServerTelnetConnection) Read(p []byte) (n int, err error) {
	return tc.transformReader.Read(p)
}

func (tc *ServerTelnetConnection) Write(p []byte) (n int, err error) {
	return tc.transformWriter.Write(p)
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
