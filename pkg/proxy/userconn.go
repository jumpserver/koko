package proxy

import (
	"io"
	"strings"

	"github.com/ibuler/ssh"
)

type UserConnection interface {
	io.ReadWriteCloser
	Protocol() string
	WinCh() <-chan ssh.Window
	User() string
	Name() string
	LoginFrom() string
	RemoteAddr() string
}

type SSHUserConnection struct {
	ssh.Session
	winch <-chan ssh.Window
}

func (uc *SSHUserConnection) Protocol() string {
	return "ssh"
}

func (uc *SSHUserConnection) User() string {
	return uc.Session.User()
}

func (uc *SSHUserConnection) WinCh() (winch <-chan ssh.Window) {
	_, winch, ok := uc.Pty()
	if ok {
		return
	}
	return nil
}

func (uc *SSHUserConnection) LoginFrom() string {
	return "T"
}

func (uc *SSHUserConnection) RemoteAddr() string {
	return strings.Split(uc.Session.RemoteAddr().String(), ":")[0]
}
