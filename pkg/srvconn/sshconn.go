package srvconn

import (
	"errors"
	"io"

	gossh "golang.org/x/crypto/ssh"
)

func NewServerSSHConnection(sess *gossh.Session) *ServerSSHConnection {
	return &ServerSSHConnection{
		session: sess,
	}
}

type ServerSSHConnection struct {
	session *gossh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
}

func (sc *ServerSSHConnection) Protocol() string {
	return "ssh"
}

func (sc *ServerSSHConnection) Connect(h, w int, term string) (err error) {
	if sc.session == nil {
		return errors.New("ssh session is nil")
	}

	modes := gossh.TerminalModes{
		gossh.ECHO:          1,     // enable echoing
		gossh.TTY_OP_ISPEED: 14400, // input speed = 14.4 kbaud
		gossh.TTY_OP_OSPEED: 14400, // output speed = 14.4 kbaud
	}
	err = sc.session.RequestPty(term, h, w, modes)
	if err != nil {
		return
	}
	sc.stdin, err = sc.session.StdinPipe()
	if err != nil {
		return
	}
	sc.stdout, err = sc.session.StdoutPipe()
	if err != nil {
		return
	}
	return sc.session.Shell()
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

func (sc *ServerSSHConnection) Close() (err error) {
	return sc.session.Close()
}
