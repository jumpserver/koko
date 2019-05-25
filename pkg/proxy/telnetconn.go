package proxy

import (
	"bytes"
	"net"
	"regexp"
	"time"

	"github.com/pkg/errors"

	"cocogo/pkg/logger"
)

const (
	IAC  = 255
	DONT = 254
	DO   = 253
	WONT = 252
	WILL = 251
	SB   = 250

	TTYPE = 24
	SAG   = 3
	ECHO  = 1

	loginRegs          = "(?i)login:?\\s*$|username:?\\s*$|name:?\\s*$|用户名:?\\s*$|账\\s*号:?\\s*$"
	passwordRegs       = "(?i)Password:?\\s*$|ssword:?\\s*$|passwd:?\\s*$|密\\s*码:?\\s*$"
	FailedRegs         = "(?i)incorrect|failed|失败|错误"
	DefaultSuccessRegs = "(?i)Last\\s*login|success|成功|#|>|\\$"
)

var (
	incorrectPattern, _ = regexp.Compile(FailedRegs)
	usernamePattern, _  = regexp.Compile(loginRegs)
	passwordPattern, _  = regexp.Compile(passwordRegs)
	successPattern, _   = regexp.Compile(DefaultSuccessRegs)
)

type AuthStatus int

const (
	AuthSuccess AuthStatus = iota
	AuthPartial
	AuthFailed
)

type ServerTelnetConnection struct {
	name                 string
	host                 string
	port                 string
	user                 string
	password             string
	timeout              int
	customString         string
	customSuccessPattern *regexp.Regexp

	conn net.Conn

	closed bool
}

func (tc *ServerTelnetConnection) Name() string {
	return tc.name
}

func (tc *ServerTelnetConnection) Host() string {
	return tc.host
}

func (tc *ServerTelnetConnection) Port() string {
	return tc.port
}

func (tc *ServerTelnetConnection) User() string {
	return tc.user
}

func (tc *ServerTelnetConnection) Timeout() time.Duration {
	return time.Duration(tc.timeout) * time.Second
}

func (tc *ServerTelnetConnection) Protocol() string {
	return "telnet"
}

func (tc *ServerTelnetConnection) optionNegotiate(data []byte) []byte {
	var buf bytes.Buffer
	optionData := bytes.Split(data, []byte{IAC})
	for _, item := range optionData {
		if len(item) == 0 {
			continue
		}
		buf.Write([]byte{IAC})
		switch item[0] {
		case DO:
			switch item[1] {
			case ECHO:
				buf.Write([]byte{WONT, ECHO})
			case TTYPE:
				buf.Write([]byte{WILL, TTYPE})
			default:
				buf.Write(bytes.ReplaceAll(item, []byte{DO}, []byte{WONT}))

			}
		case WILL:
			switch item[1] {
			case ECHO:
				buf.Write([]byte{DO, ECHO})
			case SAG:
				buf.Write([]byte{DO, ECHO})
			default:
				buf.Write(bytes.ReplaceAll(item, []byte{WILL}, []byte{DONT}))
			}
		case DONT:
			buf.Write(bytes.ReplaceAll(item, []byte{DONT}, []byte{WONT}))
		case WONT:
			buf.Write(bytes.ReplaceAll(item, []byte{WONT}, []byte{DONT}))
		case SB:
			switch item[1] {
			case TTYPE:
				if item[2] == 1 {
					buf.Write([]byte{SB, TTYPE, 0})
					buf.Write([]byte("XTERM-256COLOR"))
				}
			}
		default:
			buf.Write(item)
		}
	}
	return buf.Bytes()
}

func (tc *ServerTelnetConnection) login(data []byte) AuthStatus {
	if incorrectPattern.Match(data) {
		return AuthFailed
	} else if usernamePattern.Match(data) {
		_, _ = tc.conn.Write([]byte(tc.user + "\r\n"))
		logger.Debug("usernamePattern ", tc.user)
		return AuthPartial
	} else if passwordPattern.Match(data) {
		_, _ = tc.conn.Write([]byte(tc.password + "\r\n"))
		logger.Debug("passwordPattern ", tc.password)
		return AuthPartial
	} else if successPattern.Match(data) {
		return AuthSuccess
	}
	if tc.customString != "" {
		if tc.customSuccessPattern.Match(data) {
			return AuthSuccess
		}
	}
	return AuthPartial
}

func (tc *ServerTelnetConnection) Connect(h, w int, term string) (err error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(tc.host, tc.port), tc.Timeout())
	if err != nil {
		return
	}
	buf := make([]byte, 1024)
	tc.conn = conn
	var nr int
	for {
		nr, err = conn.Read(buf)
		if err != nil {
			return
		}
		if bytes.IndexByte(buf[:nr], IAC) == 0 {
			replayData := tc.optionNegotiate(buf[:nr])
			_, _ = conn.Write(replayData)
			continue
		} else {
			result := tc.login(buf[:nr])
			switch result {
			case AuthSuccess:
				return nil
			case AuthFailed:
				return errors.New("Failed login")
			default:
				continue
			}
		}

	}
}

func (tc *ServerTelnetConnection) SetWinSize(w, h int) error {
	return nil
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
	err = tc.conn.Close()
	tc.closed = true
	return
}
