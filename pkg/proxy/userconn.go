package proxy

import (
	"io"

	"github.com/gliderlabs/ssh"
)

type UserConnection interface {
	io.ReadWriteCloser
	WinCh() <-chan ssh.Window
	LoginFrom() string
	RemoteAddr() string
	Pty() ssh.Pty
}
