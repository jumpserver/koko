package telnetlib

import (
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"time"

	log "github.com/jumpserver/koko/pkg/logger"
)

const prefixLen = 2
const defaultTimeout = time.Second * 15

type Client struct {
	conf    *ClientConfig
	sock    net.Conn
	prefix  [prefixLen]byte
	oneByte [1]byte

	enableWindows bool

	autoLogin bool
}

func (c *Client) clientHandshake() error {
	echoPacket := packet{optionCode: DO, commandCode: ECHO}
	SGAPacket := packet{optionCode: DO, commandCode: SGA}
	_ = c.replyOptionPacket(SGAPacket)
	_ = c.replyOptionPacket(echoPacket)
	for {
		p, err := c.readOptionPacket()
		if err != nil {
			log.Error("Telnet read option packet err: ", err)
			return err
		}
		switch p[0] {
		case IAC:
			c.handleOption(p)
		default:
			if c.autoLogin {
				return c.login()
			}
			log.Infof("Telnet client manual login")
			return nil
		}

	}
}

func (c *Client) handleOption(option []byte) {
	var p packet
	log.Debugf("Telnet server %s %s", CodeTOASCII[option[1]], CodeTOASCII[option[2]])
	p.optionCode = option[1]
	cmd := option[2]
	p.commandCode = cmd
	switch option[1] {
	case SB:
		switch cmd {
		case OLD_ENVIRON, NEW_ENVIRON:
			switch option[3] {
			case 1: // send command
				sub := subOption{subCommand: 0, options: make([]byte, 0)}
				sub.options = append(sub.options, 3)
				sub.options = append(sub.options, []byte(c.conf.User)...)
				p.subOption = &sub
			}
			// subCommand 0 is , 1 Send , 2 INFO
			// VALUE     1
			// ESC       2
			// USERVAR   3
		case TTYPE:
			switch option[3] {
			case 1: // send command
				sub := subOption{subCommand: 0, options: make([]byte, 0)}
				sub.options = append(sub.options, []byte(c.conf.TTYOptions.Xterm)...)
				p.subOption = &sub
			}
		case NAWS:
			sub := subOption{subCommand: IAC, options: make([]byte, 0)}
			sub.options = append(sub.options, []byte(fmt.Sprintf("%d%d%d%d",
				0, c.conf.TTYOptions.Wide, 0, c.conf.TTYOptions.High, ))...)
			p.subOption = &sub
		default:
			return

		}
	default:
		switch option[1] {
		case DO:
			switch option[2] {
			case ECHO:
				p.optionCode = WONT
			case TTYPE, NEW_ENVIRON:
				p.optionCode = WILL
			case NAWS:
				p.optionCode = WILL
				c.enableWindows = true
			default:
				p.optionCode = WONT
			}
		case WILL:
			switch option[2] {
			case ECHO:
				p.optionCode = DO
			case SGA:
				p.optionCode = DO
			default:
				p.optionCode = DONT
			}
		case DONT:
			p.optionCode = WONT
		case WONT:
			p.optionCode = DONT
		}
	}
	log.Debugf("Telnet client %s %s", CodeTOASCII[p.optionCode], CodeTOASCII[p.commandCode])
	if err := c.replyOptionPacket(p); err != nil {
		log.Error("Telnet handler option err: ", err)
	}

}

func (c *Client) login() error {
	buf := make([]byte, 1024)
	for {
		nr, err := c.sock.Read(buf)
		if err != nil {
			return err
		}
		result := c.handleLoginData(buf[:nr])
		switch result {
		case AuthSuccess:
			return nil
		case AuthFailed:
			return errors.New("failed login")
		default:
			continue
		}

	}
}

func (c *Client) handleLoginData(data []byte) AuthStatus {
	if incorrectPattern.Match(data) {
		return AuthFailed
	} else if usernamePattern.Match(data) {
		_, _ = c.sock.Write([]byte(c.conf.User + "\r\n"))
		log.Debugf("Username pattern match: %s \n", data)
		return AuthPartial
	} else if passwordPattern.Match(data) {
		_, _ = c.sock.Write([]byte(c.conf.Password + "\r\n"))
		log.Debugf("Password pattern match: %s \n", data)
		return AuthPartial
	} else if successPattern.Match(data) {
		log.Debugf("successPattern match: %s \n", data)
		return AuthSuccess
	}
	if c.conf.CustomString != "" && c.conf.customSuccessPattern != nil {
		if c.conf.customSuccessPattern.Match(data) {
			log.Debugf("CustomString match: %s \n", data)
			return AuthSuccess
		}
	}
	return AuthPartial
}

func (c *Client) readOptionPacket() ([]byte, error) {
	if _, err := io.ReadFull(c.sock, c.oneByte[:]); err != nil {
		return nil, err
	}
	p := make([]byte, 0, 3)
	p = append(p, c.oneByte[0])
	switch c.oneByte[0] {
	case IAC:
		if _, err := io.ReadFull(c.sock, c.prefix[:]); err != nil {
			return nil, err
		}
		p = append(p, c.prefix[:]...)
		switch c.prefix[0] {
		case SB:
			for {
				if _, err := io.ReadFull(c.sock, c.oneByte[:]); err != nil {
					return nil, err
				}
				switch c.oneByte[0] {
				case IAC:
					continue
				case SE:
					return p, nil
				default:
					p = append(p, c.oneByte[0])
				}
			}
		}
	}
	return p, nil
}

func (c *Client) replyOptionPacket(p packet) error {
	_, err := c.sock.Write(p.generatePacket())
	return err
}

func (c *Client) Read(b []byte) (int, error) {
	return c.sock.Read(b)
}

func (c *Client) Write(b []byte) (int, error) {
	return c.sock.Write(b)
}

func (c *Client) Close() error {
	return c.sock.Close()
}

func (c *Client) WindowChange(w, h int) error {
	if !c.enableWindows {
		return nil
	}
	var p packet
	p.optionCode = SB
	p.commandCode = NAWS
	sub := subOption{subCommand: IAC, options: make([]byte, 0)}
	sub.options = append(sub.options, []byte(fmt.Sprintf("%d%d%d%d",
		c.conf.TTYOptions.Wide, w, c.conf.TTYOptions.High, h))...)
	p.subOption = &sub
	if err := c.replyOptionPacket(p); err != nil {
		return err
	}
	c.conf.TTYOptions.Wide = w
	c.conf.TTYOptions.High = h
	return nil

}

type ClientConfig struct {
	User         string
	Password     string
	Timeout      time.Duration
	TTYOptions   *TerminalOptions
	CustomString string

	customSuccessPattern *regexp.Regexp
}

func (conf *ClientConfig) SetDefaults() {
	if conf.Timeout == 0 || conf.Timeout < defaultTimeout {
		conf.Timeout = defaultTimeout
	}
	t := defaultTerminalOptions()
	tops := conf.TTYOptions
	if tops == nil {
		conf.TTYOptions = &t
	} else {
		if tops.Wide == 0 {
			tops.Wide = t.Wide
		}
		if tops.High == 0 {
			tops.High = t.High
		}
		if tops.Xterm == "" {
			tops.Xterm = "xterm"
		}
	}
	if conf.CustomString != "" {
		if cusPattern, err := regexp.Compile(conf.CustomString); err == nil {
			conf.customSuccessPattern = cusPattern
		}
	}
}

func Dial(network, addr string, config *ClientConfig) (*Client, error) {
	conn, err := net.DialTimeout(network, addr, config.Timeout)
	if err != nil {
		return nil, err
	}
	return NewClientConn(conn, config)
}

func NewClientConn(c net.Conn, config *ClientConfig) (*Client, error) {
	fullConf := *config
	fullConf.SetDefaults()
	var autoLogin bool
	if config.User != "" && config.Password != "" {
		autoLogin = true
	}
	conn := &Client{
		sock:      c,
		conf:      config,
		autoLogin: autoLogin,
	}
	if err := conn.clientHandshake(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("telnet: handshake failed: %s", err)
	}
	return conn, nil
}

type TerminalOptions struct {
	Wide  int
	High  int
	Xterm string
}

func defaultTerminalOptions() TerminalOptions {
	return TerminalOptions{
		Wide:  80,
		High:  24,
		Xterm: "xterm",
	}
}
