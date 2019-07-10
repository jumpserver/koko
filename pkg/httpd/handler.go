package httpd

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/gliderlabs/ssh"
	socketio "github.com/googollee/go-socket.io"
	uuid "github.com/satori/go.uuid"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/service"
)

// OnConnectHandler 当websocket连接后触发
func OnConnectHandler(s socketio.Conn) error {
	// 首次连接 1.获取当前用户的信息
	logger.Debug("Web terminal on connect event trigger")
	cookies := strings.Split(s.RemoteHeader().Get("Cookie"), ";")
	var csrfToken, sessionID, remoteIP string
	for _, line := range cookies {
		if strings.Contains(line, "csrftoken") {
			csrfToken = strings.Split(line, "=")[1]
		}
		if strings.Contains(line, "sessionid") {
			sessionID = strings.Split(line, "=")[1]
		}
	}
	user, err := service.CheckUserCookie(sessionID, csrfToken)
	if err != nil {
		msg := "User is not authenticated"
		logger.Error(msg)
		return errors.New(strings.ToLower(msg))
	}
	remoteAddr := s.RemoteHeader().Get("X-Forwarded-For")
	if remoteAddr == "" {
		remoteIP = s.RemoteAddr().String()
	} else {
		remoteIP = strings.Split(remoteAddr, ",")[0]
	}
	logger.Infof("Accepted %s connect websocket from %s", user.Username, remoteIP)
	conn := newWebConn(s.ID(), s, remoteIP, user)
	ctx := WebContext{User: user, Connection: conn}
	s.SetContext(ctx)
	conns.AddWebConn(s.ID(), conn)
	return nil
}

// OnErrorHandler 当出现错误时触发
func OnErrorHandler(e error) {
	logger.Debug("Web terminal on error trigger: ", e)
}

// OnHostHandler 当用户连接Host时触发
func OnHostHandler(s socketio.Conn, message HostMsg) {
	logger.Debug("Web terminal on host event trigger")
	win := ssh.Window{Height: 24, Width: 80}
	assetID := message.Uuid
	systemUserID := message.UserID
	secret := message.Secret
	width, height := message.Size[0], message.Size[1]
	if width != 0 {
		win.Width = width
	}
	if height != 0 {
		win.Height = height
	}
	clientID := uuid.NewV4().String()
	emitMsg := RoomMsg{clientID, secret}
	s.Emit("room", emitMsg)
	asset := service.GetAsset(assetID)
	systemUser := service.GetSystemUser(systemUserID)

	if asset.ID == "" || systemUser.ID == "" {
		return
	}
	logger.Debug("Web terminal want to connect host: ", asset.Hostname)

	ctx := s.Context().(WebContext)
	userR, userW := io.Pipe()
	conn := conns.GetWebConn(s.ID())
	addr, _, _ := net.SplitHostPort(s.RemoteAddr().String())
	client := &Client{
		Uuid: clientID, Cid: conn.Cid, user: conn.User, addr: addr,
		WinChan: make(chan ssh.Window, 100), Conn: s,
		UserRead: userR, UserWrite: userW, lock: new(sync.RWMutex),
		pty: ssh.Pty{Term: "xterm", Window: win},
	}
	client.WinChan <- win
	conn.AddClient(clientID, client)
	proxySrv := proxy.ProxyServer{
		UserConn: client, User: ctx.User,
		Asset: &asset, SystemUser: &systemUser,
	}
	go func() {
		defer logger.Debug("web proxy end")
		proxySrv.Proxy()
		s.Emit("logout", RoomMsg{Room: clientID})
	}()
}

// OnTokenHandler 当使用token连接时触发
func OnTokenHandler(s socketio.Conn, message TokenMsg) {
	logger.Debug("Web terminal on token event trigger")
	win := ssh.Window{Height: 24, Width: 80}
	token := message.Token
	secret := message.Secret
	width, height := message.Size[0], message.Size[1]
	if width != 0 {
		win.Width = width
	}
	if height != 0 {
		win.Height = height
	}
	clientID := uuid.NewV4().String()
	emitMs := RoomMsg{clientID, secret}
	s.Emit("room", emitMs)

	// check token
	if token == "" || secret == "" {
		msg := fmt.Sprintf("Token or secret is None: %s %s", token, secret)
		dataMsg := EmitDataMsg{Data: msg, Room: clientID}
		s.Emit("data", dataMsg)
		s.Emit("disconnect")
	}
	tokenUser := service.GetTokenAsset(token)
	if tokenUser.UserID == "" {
		msg := "Token info is none, maybe token expired"
		dataMsg := EmitDataMsg{Data: msg, Room: clientID}
		s.Emit("data", dataMsg)
		s.Emit("disconnect")
	}

	currentUser := service.GetUserDetail(tokenUser.UserID)
	asset := service.GetAsset(tokenUser.AssetID)
	systemUser := service.GetSystemUser(tokenUser.SystemUserID)

	if asset.ID == "" || systemUser.ID == "" {
		return
	}

	userR, userW := io.Pipe()
	conn := conns.GetWebConn(s.ID())
	conn.User = currentUser
	client := Client{
		Uuid: clientID, Cid: conn.Cid, user: conn.User,
		WinChan: make(chan ssh.Window, 100), Conn: s,
		UserRead: userR, UserWrite: userW, lock: new(sync.RWMutex),
		pty: ssh.Pty{Term: "xterm", Window: win},
	}
	client.WinChan <- win
	conn.AddClient(clientID, &client)

	proxySrv := proxy.ProxyServer{
		UserConn: &client, User: currentUser,
		Asset: &asset, SystemUser: &systemUser,
	}
	go func() {
		defer logger.Debug("web proxy end")
		proxySrv.Proxy()
		s.Emit("logout", RoomMsg{Room: clientID})
	}()
}

// OnDataHandler 收发数据时触发
func OnDataHandler(s socketio.Conn, message DataMsg) {
	cid := message.Room
	conn := conns.GetWebConn(s.ID())
	client := conn.GetClient(cid)
	if client == nil {
		return
	}
	_, _ = client.UserWrite.Write([]byte(message.Data))
}

// OnResizeHandler 用户窗口改变时触发
func OnResizeHandler(s socketio.Conn, message ResizeMsg) {
	logger.Debugf("Web terminal on resize event trigger: %d*%d", message.Width, message.Height)
	winSize := ssh.Window{Height: message.Height, Width: message.Width}
	conn := conns.GetWebConn(s.ID())
	conn.SetWinSize(winSize)
}

// OnLogoutHandler 用户登出一个会话时触发
func OnLogoutHandler(s socketio.Conn, message string) {
	logger.Debug("Web terminal on logout event trigger")
	conn := conns.GetWebConn(s.ID())
	if conn == nil {
		logger.Error("No conn found")
		return
	}
	client := conn.GetClient(message)
	if client == nil {
		logger.Error("No client found")
		return
	}
	_ = client.Close()
}

// OnDisconnect websocket断开后触发
func OnDisconnect(s socketio.Conn, msg string) {
	logger.Debug("On disconnect event trigger")
	conn := conns.GetWebConn(s.ID())
	conn.Close()
}
