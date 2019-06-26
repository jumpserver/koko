package srvconn

import (
	"errors"
	"io"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jumpserver/koko/pkg/model"
)

type ServerSSHConnection struct {
	User       *model.User
	Asset      *model.Asset
	SystemUser *model.SystemUser
	Overtime   time.Duration

	client    *gossh.Client
	session   *gossh.Session
	stdin     io.WriteCloser
	stdout    io.Reader
	closed    bool
	connected bool
}

func (sc *ServerSSHConnection) Protocol() string {
	return "ssh"
}

func (sc *ServerSSHConnection) invokeShell(h, w int, term string) (err error) {
	sess, err := sc.client.NewSession()
	if err != nil {
		return
	}
	sc.session = sess
	modes := gossh.TerminalModes{
		gossh.ECHO:          1,     // enable echoing
		gossh.TTY_OP_ISPEED: 14400, // input speed = 14.4 kbaud
		gossh.TTY_OP_OSPEED: 14400, // output speed = 14.4 kbaud
	}
	err = sess.RequestPty(term, h, w, modes)
	if err != nil {
		return
	}
	sc.stdin, err = sess.StdinPipe()
	if err != nil {
		return
	}
	sc.stdout, err = sess.StdoutPipe()
	if err != nil {
		return
	}
	err = sess.Shell()
	return err
}

func (sc *ServerSSHConnection) Connect(h, w int, term string) (err error) {
	sc.client, err = NewClient(sc.User, sc.Asset, sc.SystemUser, sc.Timeout())
	if err != nil {
		return
	}

	err = sc.invokeShell(h, w, term)
	if err != nil {
		return
	}
	sc.connected = true
	return nil
}

func (sc *ServerSSHConnection) TryConnectFromCache(h, w int, term string) (err error) {
	sc.client = GetClientFromCache(sc.User, sc.Asset, sc.SystemUser)
	if sc.client == nil {
		return errors.New("no client in cache")
	}
	err = sc.invokeShell(h, w, term)
	if err != nil {
		RecycleClient(sc.client)
		return
	}
	sc.connected = true
	return nil
}

func (sc *ServerSSHConnection) SetWinSize(h, w int) error {
	return sc.session.WindowChange(h, w)
}

func (sc *ServerSSHConnection) Read(p []byte) (n int, err error) {
	return sc.stdout.Read(p)
}

func (sc *ServerSSHConnection) Write(p []byte) (n int, err error) {
	return sc.stdin.Write(p)
}

func (sc *ServerSSHConnection) Timeout() time.Duration {
	if sc.Overtime == 0 {
		sc.Overtime = 30 * time.Second
	}
	return sc.Overtime
}

func (sc *ServerSSHConnection) Close() (err error) {
	RecycleClient(sc.client)
	if sc.closed || !sc.connected {
		return
	}
	err = sc.session.Close()
	if err != nil {
		return
	}
	return
}
