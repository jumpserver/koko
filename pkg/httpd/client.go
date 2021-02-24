package httpd

import (
	"context"
	"io"
	"sync"
	"unicode/utf8"

	"github.com/gliderlabs/ssh"

	"github.com/jumpserver/koko/pkg/common"
)

type Client struct {
	WinChan   chan ssh.Window
	UserRead  io.ReadCloser
	UserWrite io.WriteCloser
	Conn      *UserWebsocket
	pty       ssh.Pty

	remainBuf []byte
	sync.Mutex
}

func (c *Client) WinCh() <-chan ssh.Window {
	return c.WinChan
}

func (c *Client) LoginFrom() string {
	return "WT"
}

func (c *Client) RemoteAddr() string {
	return c.Conn.ClientIP()
}

func (c *Client) Read(p []byte) (n int, err error) {
	c.Lock()
	defer c.Unlock()
	return c.UserRead.Read(p)
}

func (c *Client) Write(p []byte) (n int, err error) {
	n = len(p)
	buf := make([]byte, len(c.remainBuf)+n)
	copy(buf, c.remainBuf)
	copy(buf[len(c.remainBuf):], p)
	c.remainBuf = buf
	for i := len(buf); i > 0; i-- {
		if utf8.Valid(buf[:i]) {
			c.remainBuf = buf[i:]
			break
		}
	}
	validUTF8Index := len(buf) - len(c.remainBuf)
	// 确保是一个完整的utf8字符发送给前端
	msg := Message{
		Id:   c.Conn.Uuid,
		Type: TERMINALDATA,
		Data: common.BytesToString(buf[:validUTF8Index]),
	}
	c.Conn.SendMessage(&msg)
	return len(p), nil
}

func (c *Client) Pty() ssh.Pty {
	return c.pty
}

func (c *Client) Close() (err error) {
	_ = c.UserRead.Close()
	_ = c.UserWrite.Close()
	c.initPipe()
	return err
}

func (c *Client) initPipe() {
	c.Lock()
	defer c.Unlock()
	c.UserRead, c.UserWrite = io.Pipe()
}

func (c *Client) SetWinSize(size ssh.Window) {
	select {
	case c.WinChan <- size:
	default:
	}
}

func (c *Client) ID() string {
	return c.Conn.Uuid
}

func (c *Client) WriteData(p []byte) {
	_, _ = c.UserWrite.Write(p)
}

func (c *Client) Context() context.Context {
	return c.Conn.ctx.Request.Context()
}
