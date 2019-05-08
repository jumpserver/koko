package webssh

import (
	"github.com/gliderlabs/ssh"
	socketio "github.com/googollee/go-socket.io"
)

type Client struct {
	Uuid  string
	Cid   string
	WinCh chan ssh.Window

	Conn socketio.Conn
}
