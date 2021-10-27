package srvconn

import (
	"io"
	"net"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/LeeEirc/tclientlib"
	"golang.org/x/text/transform"

	"github.com/jumpserver/koko/pkg/common"
)

func NewTelnetConnection(opts ...TelnetOption) (*TelnetConnection, error) {
	cfg := &TelnetConfig{
		Host:    "127.0.0.1",
		Port:    "23",
		Term:    "xterm",
		Timeout: 15,
		win: Windows{
			Width:  80,
			Height: 120,
		},
		CustomSuccessPattern: tclientlib.DefaultLoginSuccessPattern,
	}
	for _, setter := range opts {
		setter(cfg)
	}
	var (
		conn        net.Conn
		err         error
		proxyClient *SSHClient
		client      *tclientlib.Client
	)
	dstAddr := net.JoinHostPort(cfg.Host, cfg.Port)
	if cfg.proxySSHClientOptions != nil {
		if proxyClient, err = getAvailableProxyClient(cfg.proxySSHClientOptions...); err != nil {
			return nil, err
		}
		if conn, err = proxyClient.Dial("tcp", dstAddr); err != nil {
			_ = proxyClient.Close()
			return nil, err
		}
	} else {
		if conn, err = net.DialTimeout("tcp", dstAddr, cfg.Timeout); err != nil {
			return nil, err
		}
	}
	client, err = newTelnetClient(conn, cfg)
	if err != nil {
		if proxyClient != nil {
			_ = proxyClient.Close()
		}
		return nil, err
	}
	var (
		transformReader io.Reader
		transformWriter io.WriteCloser
	)

	transformReader = client
	transformWriter = client

	if cfg.Charset != common.UTF8 {
		if readDecode := common.LookupCharsetDecode(cfg.Charset); readDecode != nil {
			transformReader = transform.NewReader(client, readDecode)
		}
		if writerEncode := common.LookupCharsetEncode(cfg.Charset); writerEncode != nil {
			transformWriter = transform.NewWriter(client, writerEncode)
		}
	}
	return &TelnetConnection{
		cfg:             cfg,
		conn:            client,
		proxyConn:       proxyClient,
		transformReader: transformReader,
		transformWriter: transformWriter,
	}, nil
}

func newTelnetClient(conn net.Conn, cfg *TelnetConfig) (*tclientlib.Client, error) {
	// 修复未登录成功，但是telnet连接不断的问题。
	// todo：检测超时，直接断开连接，后续通过更新 telnet 包解决
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(cfg.Timeout)
		defer t.Stop()
		select {
		case <-t.C:
			select {
			case <-done:
			default:
				_ = conn.Close()
			}
		case <-done:
			return
		}
	}()
	client, err := tclientlib.NewClientConn(conn, &tclientlib.Config{
		Username: cfg.Username,
		Password: cfg.Password,
		Timeout:  cfg.Timeout,
		TTYOptions: &tclientlib.TerminalOptions{
			Wide:     cfg.win.Width,
			High:     cfg.win.Height,
			TermType: cfg.Term,
		},
		LoginSuccessRegex: cfg.CustomSuccessPattern,
	})
	close(done)
	if err != nil {
		return nil, err
	}
	return client, nil
}

type TelnetConnection struct {
	cfg       *TelnetConfig
	conn      *tclientlib.Client
	proxyConn *SSHClient

	transformReader io.Reader
	transformWriter io.Writer
	once            sync.Once
}

func (tc *TelnetConnection) Protocol() string {
	return "telnet"
}

func (tc *TelnetConnection) KeepAlive() error {
	return nil
}

func (tc *TelnetConnection) SetWinSize(w, h int) error {
	return tc.conn.WindowChange(w, h)
}

func (tc *TelnetConnection) Read(p []byte) (n int, err error) {
	return tc.transformReader.Read(p)
}

func (tc *TelnetConnection) Write(p []byte) (n int, err error) {
	return tc.transformWriter.Write(p)
}

func (tc *TelnetConnection) Close() (err error) {
	tc.once.Do(func() {
		if tc.proxyConn != nil {
			_ = tc.proxyConn.Close()
		}
		err = tc.conn.Close()
	})
	return
}

type TelnetOption func(*TelnetConfig)

type TelnetConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	Term     string
	Charset  string

	Timeout time.Duration

	win Windows

	CustomSuccessPattern *regexp.Regexp

	proxySSHClientOptions []SSHClientOptions
}

func TelnetHost(host string) TelnetOption {
	return func(opt *TelnetConfig) {
		opt.Host = host
	}
}

func TelnetPort(port int) TelnetOption {
	return func(opt *TelnetConfig) {
		opt.Port = strconv.Itoa(port)
	}
}

func TelnetUsername(username string) TelnetOption {
	return func(opt *TelnetConfig) {
		opt.Username = username
	}
}

func TelnetUPassword(password string) TelnetOption {
	return func(opt *TelnetConfig) {
		opt.Password = password
	}
}

func TelnetUTimeout(timeout int) TelnetOption {
	return func(opt *TelnetConfig) {
		opt.Timeout = time.Duration(timeout) * time.Second
	}
}

func TelnetProxyOptions(proxyOpts []SSHClientOptions) TelnetOption {
	return func(opt *TelnetConfig) {
		opt.proxySSHClientOptions = proxyOpts
	}
}

func TelnetPtyWin(win Windows) TelnetOption {
	return func(opt *TelnetConfig) {
		opt.win = win
	}
}

func TelnetCharset(charset string) TelnetOption {
	return func(opt *TelnetConfig) {
		opt.Charset = charset
	}
}

func TelnetCustomSuccessPattern(successPattern *regexp.Regexp) TelnetOption {
	return func(opt *TelnetConfig) {
		opt.CustomSuccessPattern = successPattern
	}
}
