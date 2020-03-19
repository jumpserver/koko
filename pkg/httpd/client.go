package httpd

import (
	"io"
	"sync"

	"github.com/gliderlabs/ssh"
	"github.com/kataras/neffos"
)

type Client struct {
	Uuid      string
	Cid       string
	addr      string
	WinChan   chan ssh.Window
	UserRead  io.Reader
	UserWrite io.WriteCloser
	Conn      *UserWebsocketConn
	Closed    bool
	pty       ssh.Pty
	mu        *sync.RWMutex
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Closed {
		return
	}
	data := DataMsg{Data: string(p), Room: c.Uuid}
	n = len(p)
	c.Conn.SendDataEvent(neffos.Marshal(data))
	return
}

func (c *Client) Pty() ssh.Pty {
	return c.pty
}

func (c *Client) Close() (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Closed {
		return
	}
	c.Closed = true
	return c.UserWrite.Close()
}

func (c *Client) SetWinSize(size ssh.Window) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	select {
	case c.WinChan <- size:
	default:
	}
}

func (c *Client) ID() string {
	return c.Uuid
}
