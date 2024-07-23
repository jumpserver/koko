package httpd

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/gliderlabs/ssh"
	"io"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/logger"
)

type Client struct {
	WinChan   chan ssh.Window
	UserRead  io.ReadCloser
	UserWrite io.WriteCloser
	Conn      *UserWebsocket
	pty       ssh.Pty

	sync.Mutex

	// 用于防抖处理
	buffer      bytes.Buffer
	bufferMutex sync.Mutex
	timer       *time.Timer

	KubernetesId string
	Namespace    string
	Pod          string
	Container    string
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

// 向客户端发送数据进行1毫秒的防抖处理
func (c *Client) Write(p []byte) (n int, err error) {
	c.bufferMutex.Lock()
	defer c.bufferMutex.Unlock()

	c.buffer.Write(p)

	if c.timer == nil {
		c.timer = time.AfterFunc(time.Millisecond, c.flushBuffer)
	}
	return len(p), nil
}

func (c *Client) flushBuffer() {
	c.bufferMutex.Lock()
	defer c.bufferMutex.Unlock()
	messageType := TerminalBinary
	if c.KubernetesId != "" {
		messageType = TerminalK8SBinary
	}

	if c.buffer.Len() > 0 {
		msg := Message{
			Id:           c.Conn.Uuid,
			Type:         messageType,
			Raw:          c.buffer.Bytes(),
			KubernetesId: c.KubernetesId,
		}
		c.Conn.SendMessage(&msg)
		c.buffer.Reset()
	}
	c.timer.Stop()
	c.timer = nil
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

func (c *Client) HandleRoomEvent(event string, roomMsg *exchange.RoomMessage) {
	var (
		msgType string
		msgData string
	)
	switch event {
	case exchange.ShareJoin:
		msgType = TerminalShareJoin
		data, _ := json.Marshal(roomMsg.Meta)
		msgData = string(data)
	case exchange.ShareLeave:
		msgType = TerminalShareLeave
		data, _ := json.Marshal(roomMsg.Meta)
		msgData = string(data)
	case exchange.ShareUsers:
		msgType = TerminalShareUsers
		msgData = string(roomMsg.Body)
	case exchange.WindowsEvent:
		msgType = TerminalResize
		msgData = string(roomMsg.Body)
	case exchange.ActionEvent:
		msgType = TerminalAction
		msgData = string(roomMsg.Body)
	case exchange.ShareRemoveUser:
		msgType = TerminalShareUserRemove
		meta := roomMsg.Meta
		if meta.TerminalId != c.Conn.Uuid {
			logger.Debugf("Remove share user Ignore not self: %+v", meta.User)
			return
		}
		logger.Infof("Remove share user self: %+v", meta.User)
		msgData = string(roomMsg.Body)
	case exchange.PauseEvent:
		msgType = TerminalSessionPause
		msgData = string(roomMsg.Body)
		logger.Debugf("Pause terminal session : %+v", roomMsg)
	case exchange.ResumeEvent:
		msgType = TerminalSessionResume
		msgData = string(roomMsg.Body)
		logger.Debugf("Resume terminal session : %+v", roomMsg)
	default:
		logger.Infof("unsupported room msg %+v", roomMsg)
		return
	}
	var msg = Message{
		Id:           c.Conn.Uuid,
		Type:         msgType,
		Data:         msgData,
		KubernetesId: c.KubernetesId,
	}
	c.Conn.SendMessage(&msg)
}
