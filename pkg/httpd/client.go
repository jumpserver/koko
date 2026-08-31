package httpd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/jumpserver/koko/internal/sessiontools"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
)

const sessionReadyIdle = 500 * time.Millisecond

type Client struct {
	WinChan   chan ssh.Window
	UserRead  io.ReadCloser
	UserWrite io.WriteCloser
	Conn      *UserWebsocket
	pty       ssh.Pty

	sync.Mutex
	closeOnce sync.Once
	sessionMu sync.RWMutex

	// 用于防抖处理
	buffer      bytes.Buffer
	bufferMutex sync.Mutex
	timer       *time.Timer

	readyMu    sync.Mutex
	readyArmed bool
	readySent  bool
	readyTimer *time.Timer

	KubernetesId string
	TerminalId   uint32
	Namespace    string
	Pod          string
	Container    string
	SessionInfo  *proxy.SessionInfo
	mcpMu        sync.RWMutex
	mcp          *sessiontools.MCPDispatcher
	mcpClosed    bool
	inputMu      sync.Mutex
	inputLocked  bool
	metrics      clientMetrics
	observerMu   sync.RWMutex
	observer     *sessiontools.TerminalObserver
}

func (c *Client) SetSessionInfo(info *proxy.SessionInfo) {
	c.sessionMu.Lock()
	c.SessionInfo = info
	c.sessionMu.Unlock()
	c.armSessionReady()
}

func (c *Client) armSessionReady() {
	c.readyMu.Lock()
	defer c.readyMu.Unlock()
	if c.readySent {
		return
	}
	c.readyArmed = true
	c.resetSessionReadyTimerLocked()
}

func (c *Client) noteSessionOutput() {
	c.readyMu.Lock()
	defer c.readyMu.Unlock()
	if !c.readyArmed || c.readySent {
		return
	}
	c.resetSessionReadyTimerLocked()
}

func (c *Client) resetSessionReadyTimerLocked() {
	if c.readyTimer != nil {
		c.readyTimer.Stop()
	}
	c.readyTimer = time.AfterFunc(sessionReadyIdle, c.fireSessionReady)
}

func (c *Client) fireSessionReady() {
	c.readyMu.Lock()
	if !c.readyArmed || c.readySent {
		c.readyMu.Unlock()
		return
	}
	c.readySent = true
	c.readyArmed = false
	if c.readyTimer != nil {
		c.readyTimer.Stop()
		c.readyTimer = nil
	}
	c.readyMu.Unlock()
	c.Conn.SendMessage(&Message{
		Id:           c.Conn.Uuid,
		Type:         TerminalReady,
		TerminalId:   c.TerminalId,
		KubernetesId: c.KubernetesId,
	})
}

func (c *Client) stopSessionReady() {
	c.readyMu.Lock()
	defer c.readyMu.Unlock()
	c.readyArmed = false
	if c.readyTimer != nil {
		c.readyTimer.Stop()
		c.readyTimer = nil
	}
}

func (c *Client) GetSessionInfo() *proxy.SessionInfo {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	return c.SessionInfo
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
	c.observerMu.RLock()
	observer := c.observer
	c.observerMu.RUnlock()
	if observer != nil {
		observer.Feed(p)
	}
	category := ""
	connectToken := c.Conn.ConnectToken
	if connectToken != nil {
		category = connectToken.Platform.Category.Value
	}

	if category == "database" {
		c.bufferMutex.Lock()
		c.buffer.Write(p)
		c.bufferMutex.Unlock()

		if c.timer == nil {
			c.timer = time.AfterFunc(time.Millisecond, c.flushBuffer)
		}
		return len(p), nil

	}

	messageType := TerminalBinary
	if c.KubernetesId != "" {
		messageType = TerminalK8SBinary
	}

	msg := Message{
		Id:           c.Conn.Uuid,
		Type:         messageType,
		Raw:          p,
		TerminalId:   c.TerminalId,
		KubernetesId: c.KubernetesId,
	}
	c.Conn.SendMessage(&msg)
	c.noteSessionOutput()
	return len(p), nil
}

func (c *Client) flushBuffer() {
	c.bufferMutex.Lock()
	defer c.bufferMutex.Unlock()

	if c.buffer.Len() > 0 {
		msg := Message{
			Id:         c.Conn.Uuid,
			Type:       TerminalBinary,
			Raw:        c.buffer.Bytes(),
			TerminalId: c.TerminalId,
		}
		c.Conn.SendMessage(&msg)
		c.buffer.Reset()
		c.noteSessionOutput()
	}

	if c.buffer.Len() == 0 && c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}

func (c *Client) Pty() ssh.Pty {
	return c.pty
}

func (c *Client) Close() (err error) {
	c.closeOnce.Do(func() {
		c.stopSessionReady()
		_ = c.UserRead.Close()
		_ = c.UserWrite.Close()
		c.closeClientMCP()
		c.closeTerminalObserver()
		c.stopMetrics()
		c.initPipe()
	})
	return err
}

func (c *Client) initPipe() {
	c.Lock()
	defer c.Unlock()
	c.UserRead, c.UserWrite = io.Pipe()
}

func (c *Client) SetWinSize(size ssh.Window) {
	c.observerMu.RLock()
	observer := c.observer
	c.observerMu.RUnlock()
	if observer != nil {
		observer.Resize(int(size.Width), int(size.Height))
	}
	select {
	case c.WinChan <- size:
	default:
	}
}

func (c *Client) setMCP(dispatcher *sessiontools.MCPDispatcher) bool {
	c.mcpMu.Lock()
	defer c.mcpMu.Unlock()
	if c.mcpClosed || c.mcp != nil || dispatcher == nil {
		return false
	}
	c.mcp = dispatcher
	return true
}

func (c *Client) getMCP() *sessiontools.MCPDispatcher {
	c.mcpMu.RLock()
	defer c.mcpMu.RUnlock()
	return c.mcp
}

func (c *Client) closeMCP() {
	c.mcpMu.Lock()
	dispatcher := c.mcp
	c.mcp = nil
	c.mcpMu.Unlock()
	if dispatcher != nil {
		dispatcher.Close()
	}
}

func (c *Client) closeClientMCP() {
	c.mcpMu.Lock()
	c.mcpClosed = true
	dispatcher := c.mcp
	c.mcp = nil
	c.mcpMu.Unlock()
	if dispatcher != nil {
		dispatcher.Close()
	}
}

func (c *Client) ID() string {
	return c.Conn.Uuid
}

func (c *Client) WriteData(p []byte) {
	c.inputMu.Lock()
	defer c.inputMu.Unlock()
	if c.inputLocked {
		return
	}
	_, _ = c.UserWrite.Write(p)
}

func (c *Client) WriteAgentToolData(p []byte) error {
	c.inputMu.Lock()
	defer c.inputMu.Unlock()
	_, err := c.UserWrite.Write(p)
	return err
}

func (c *Client) SetInputLocked(locked bool) {
	c.inputMu.Lock()
	c.inputLocked = locked
	c.inputMu.Unlock()
}

func (c *Client) setTerminalObserver(observer *sessiontools.TerminalObserver) bool {
	c.observerMu.Lock()
	defer c.observerMu.Unlock()
	if c.observer != nil || observer == nil {
		return false
	}
	c.observer = observer
	return true
}

func (c *Client) getTerminalObserver() *sessiontools.TerminalObserver {
	c.observerMu.RLock()
	defer c.observerMu.RUnlock()
	return c.observer
}

func (c *Client) closeTerminalObserver() {
	c.observerMu.Lock()
	observer := c.observer
	c.observer = nil
	c.observerMu.Unlock()
	if observer != nil {
		_ = observer.Close()
	}
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
	case exchange.PermValidEvent:
		msgType = TerminalPermValid
		msgData = string(roomMsg.Body)
		logger.Debugf("Terminal perm is valid : %+v", roomMsg)
	case exchange.PermExpiredEvent:
		msgType = TerminalPermExpired
		msgData = string(roomMsg.Body)
		logger.Debugf("Terminal perm is expired : %+v", roomMsg)
	default:
		logger.Infof("unsupported room msg %+v", roomMsg)
		return
	}
	var msg = Message{
		Id:           c.Conn.Uuid,
		Type:         msgType,
		Data:         msgData,
		TerminalId:   c.TerminalId,
		KubernetesId: c.KubernetesId,
	}
	c.Conn.SendMessage(&msg)
}
