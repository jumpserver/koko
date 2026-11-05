package srvconn

import (
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/LeeEirc/tclientlib"
	"golang.org/x/text/transform"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/logger"
)

// TelnetBreakInput is the parser's IAC BRK followed by Ctrl-C and Enter.
const TelnetBreakInput = "\xff\xf3\x03\r"

func NewTelnetConnection(opts ...TelnetOption) (*TelnetConnection, error) {
	cfg := &TelnetConfig{
		Host:    "127.0.0.1",
		Port:    "23",
		Term:    "xterm",
		Timeout: 15 * time.Second,
		win: Windows{
			Width:  80,
			Height: 120,
		},
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
	tCon := &TelnetConnection{
		cfg:       cfg,
		conn:      client,
		proxyConn: proxyClient,
	}
	if cfg.suCfg != nil {
		if err = LoginToTelnetSu(tCon); err != nil {
			_ = tCon.Close()
			logger.Errorf("Telnet Login to su user %s failed: %s",
				cfg.suCfg.SudoUsername, err)
			return nil, err
		}
	}
	_, _ = tCon.Write([]byte("\r\n"))
	return tCon, nil
}

func newTelnetClient(conn net.Conn, cfg *TelnetConfig) (*tclientlib.Client, error) {
	config := &tclientlib.ClientConfig{
		User:           cfg.Username,
		Password:       cfg.Password,
		Timeout:        cfg.Timeout,
		TerminalType:   cfg.Term,
		Width:          cfg.win.Width,
		Height:         cfg.win.Height,
		LineEnding:     tclientlib.LineEndingCRNUL,
		ShellPrompt:    telnetPromptPattern(cfg.CustomSuccessPattern, `success|成功|#|>|\$`, `success|成功|#|>`),
		UsernamePrompt: telnetPromptPattern(cfg.CustomUsernamePattern, `username:|login:`),
		PasswordPrompt: telnetPromptPattern(cfg.CustomPasswordPattern, `password:`),
		WrapStream: func(reader io.Reader, writer io.Writer) (io.Reader, io.Writer) {
			if decoder := common.LookupCharsetDecode(cfg.Charset); decoder != nil {
				reader = transform.NewReader(reader, decoder)
			}
			if encoder := common.LookupCharsetEncode(cfg.Charset); encoder != nil {
				writer = transform.NewWriter(writer, encoder)
			}
			return reader, writer
		},
	}
	for name, pattern := range map[string]*regexp.Regexp{
		"username": config.UsernamePrompt,
		"password": config.PasswordPrompt,
		"success":  config.ShellPrompt,
	} {
		if pattern != nil && pattern.MatchString("") {
			_ = conn.Close()
			return nil, fmt.Errorf("invalid telnet %s prompt: expression matches empty input", name)
		}
	}
	client, err := tclientlib.NewClientConn(conn, config)
	if err != nil {
		_ = conn.Close()
	}
	return client, err
}

func telnetPromptPattern(pattern *regexp.Regexp, legacyDefaults ...string) *regexp.Regexp {
	if pattern == nil || pattern.String() == "" {
		return nil
	}
	// Only Core's exact historical defaults should fall back to built-in matchers.
	for _, legacy := range legacyDefaults {
		if pattern.String() == legacy {
			return nil
		}
	}
	return pattern
}

type TelnetConnection struct {
	cfg       *TelnetConfig
	conn      *tclientlib.Client
	proxyConn *SSHClient

	once sync.Once
}

func (tc *TelnetConnection) KeepAlive() error {
	return nil
}

func (tc *TelnetConnection) SetWinSize(w, h int) error {
	return tc.conn.WindowChange(h, w)
}

func (tc *TelnetConnection) Read(p []byte) (n int, err error) {
	return tc.conn.Read(p)
}

func (tc *TelnetConnection) Write(p []byte) (n int, err error) {
	if string(p) == TelnetBreakInput {
		// Control commands must bypass application-data escaping and charset conversion.
		if err := tc.conn.SendCommand(tclientlib.CommandBreak); err != nil {
			return 0, err
		}
		n, err := tc.conn.Write(p[2:])
		return n + 2, err
	}
	return tc.conn.Write(p)
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

	suCfg *SuConfig

	Timeout time.Duration

	win Windows

	CustomSuccessPattern  *regexp.Regexp
	CustomUsernamePattern *regexp.Regexp
	CustomPasswordPattern *regexp.Regexp

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

func TelnetCustomUsernamePattern(usernamePrompt *regexp.Regexp) TelnetOption {
	return func(opt *TelnetConfig) {
		opt.CustomUsernamePattern = usernamePrompt
	}
}

func TelnetCustomPasswordPattern(passwordPrompt *regexp.Regexp) TelnetOption {
	return func(opt *TelnetConfig) {
		opt.CustomPasswordPattern = passwordPrompt
	}
}

func TelnetSuConfig(cfg *SuConfig) TelnetOption {
	return func(opt *TelnetConfig) {
		opt.suCfg = cfg
	}
}
