package webssh

import (
	socketio "github.com/googollee/go-socket.io"
	"github.com/ibuler/ssh"
)

type Client struct {
	Uuid  string
	Cid   string
	WinCh chan ssh.Window

	Conn socketio.Conn
}
