package webssh

import (
	"io"

	socketio "github.com/googollee/go-socket.io"
	"github.com/ibuler/ssh"

	"cocogo/pkg/model"
)

type Client struct {
	Uuid      string
	Cid       string
	user      *model.User
	addr      string
	WinChan   chan ssh.Window
	UserRead  io.Reader
	UserWrite io.WriteCloser
	Conn      socketio.Conn
	Closed    bool
}

func (c *Client) Protocol() string {
	return "ws"
}

func (c *Client) WinCh() <-chan ssh.Window {
	return c.WinChan
}

func (c *Client) User() string {
	return c.user.Username
}
func (c *Client) LoginFrom() string {
	return "WT"
}
func (c *Client) RemoteAddr() string {
	return c.addr
}

func (c *Client) Read(p []byte) (n int, err error) {
	return c.UserRead.Read(p)
}

func (c *Client) Write(p []byte) (n int, err error) {
	data := DataMsg{Data: string(p), Room: c.Uuid}
	n = len(p)
	c.Conn.Emit("data", data)
	return
}

func (c *Client) Close() (err error) {
	if c.Closed {
		return
	}
	return c.UserWrite.Close()
}
