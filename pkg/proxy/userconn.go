package proxy

import (
	"io"
	"strings"

	"github.com/gliderlabs/ssh"
)

type UserConnection interface {
	io.ReadWriteCloser
	Protocol() string
	WinCh() <-chan ssh.Window
	User() string
	LoginFrom() string
	RemoteAddr() string
	Pty() ssh.Pty
}

type UserSSHConnection struct {
	ssh.Session
}

func (uc *UserSSHConnection) Protocol() string {
	return "ssh"
}

func (uc *UserSSHConnection) User() string {
	return uc.Session.User()
}

func (uc *UserSSHConnection) WinCh() (winch <-chan ssh.Window) {
	_, winch, ok := uc.Session.Pty()
	if ok {
		return
	}
	return nil
}

func (uc *UserSSHConnection) LoginFrom() string {
	return "ST"
}

func (uc *UserSSHConnection) RemoteAddr() string {
	return strings.Split(uc.Session.RemoteAddr().String(), ":")[0]
}

func (uc *UserSSHConnection) Pty() ssh.Pty {
	pty, _, _ := uc.Session.Pty()
	return pty
}
