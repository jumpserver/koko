package srvconn

import (
	"bytes"
	"errors"
	"net"
	"regexp"
	"strconv"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
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
	User                 *model.User
	Asset                *model.Asset
	SystemUser           *model.SystemUser
	Overtime             time.Duration
	CustomString         string
	CustomSuccessPattern *regexp.Regexp

	conn   net.Conn
	closed bool
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
		_, _ = tc.conn.Write([]byte(tc.SystemUser.Username + "\r\n"))
		logger.Debug("Username pattern match: ", data)
		return AuthPartial
	} else if passwordPattern.Match(data) {
		_, _ = tc.conn.Write([]byte(tc.SystemUser.Password + "\r\n"))
		logger.Debug("Password pattern ", data)
		return AuthPartial
	} else if successPattern.Match(data) {
		return AuthSuccess
	}
	if tc.CustomString != "" {
		if tc.CustomSuccessPattern.Match(data) {
			return AuthSuccess
		}
	}
	return AuthPartial
}

func (tc *ServerTelnetConnection) Connect(h, w int, term string) (err error) {
	var ip = tc.Asset.IP
	var port = strconv.Itoa(tc.Asset.ProtocolPort("telnet"))
	var asset = tc.Asset
	var proxyConn *gossh.Client

	if asset.Domain != "" {
		sshConfig := MakeConfig(tc.Asset, tc.SystemUser, tc.Timeout())
		proxyConn, err = sshConfig.DialProxy()
		logger.Errorf("Proxy conn: ", proxyConn)
		if err != nil {
			logger.Error("Dial proxy host error")
			return
		}
	}

	addr := net.JoinHostPort(ip, port)
	var conn net.Conn
	// 判断是否有合适的proxy连接
	if proxyConn != nil {
		logger.Debug("Connect host via proxy")
		conn, err = proxyConn.Dial("tcp", addr)
	} else {
		logger.Debug("Direct connect host")
		conn, err = net.DialTimeout("tcp", addr, tc.Timeout())
	}
	if err != nil {
		return
	}

	if tc.SystemUser.Password == ""{
		info := service.GetSystemUserAssetAuthInfo(tc.SystemUser.ID, asset.ID)
		tc.SystemUser.Password = info.Password
		tc.SystemUser.PrivateKey = info.PrivateKey
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
