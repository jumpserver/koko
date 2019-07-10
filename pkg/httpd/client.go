package httpd

import (
	"io"
	"sync"

	"github.com/gliderlabs/ssh"
	socketio "github.com/googollee/go-socket.io"

	"github.com/jumpserver/koko/pkg/model"
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
	pty       ssh.Pty
	lock      *sync.RWMutex
}

func (c *Client) WinCh() <-chan ssh.Window {
	return c.WinChan
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
	c.lock.RLock()
	defer c.lock.RUnlock()
	if c.Closed {
		return
	}
	data := DataMsg{Data: string(p), Room: c.Uuid}
	n = len(p)
	c.Conn.Emit("data", data)
	return
}

func (c *Client) Pty() ssh.Pty {
	return c.pty
}

func (c *Client) Close() (err error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.Closed {
		return
	}
	c.Closed = true
	return c.UserWrite.Close()
}
