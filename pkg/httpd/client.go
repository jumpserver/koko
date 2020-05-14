package httpd

import (
	"io"

	"github.com/gliderlabs/ssh"
	"github.com/kataras/neffos"
)

type Client struct {
	Uuid      string
	WinChan   chan ssh.Window
	UserRead  io.Reader
	UserWrite io.WriteCloser
	Conn      *UserWebsocketConn
	pty       ssh.Pty
}

func (c *Client) WinCh() <-chan ssh.Window {
	return c.WinChan
}

func (c *Client) LoginFrom() string {
	return "WT"
}

func (c *Client) RemoteAddr() string {
	return c.Conn.Addr
}

func (c *Client) Read(p []byte) (n int, err error) {
	return c.UserRead.Read(p)
}

func (c *Client) Write(p []byte) (n int, err error) {
	if _, ok := c.Conn.GetClient(c.Uuid); ok {
		data := DataMsg{Data: string(p), Room: c.Uuid}
		c.Conn.SendDataEvent(neffos.Marshal(data))
		return len(p), nil
	}
	return 0, io.EOF
}

func (c *Client) Pty() ssh.Pty {
	return c.pty
}

func (c *Client) Close() (err error) {
	return c.UserWrite.Close()
}

func (c *Client) SetWinSize(size ssh.Window) {
	select {
	case c.WinChan <- size:
	default:
	}
}

func (c *Client) ID() string {
	return c.Uuid
}
