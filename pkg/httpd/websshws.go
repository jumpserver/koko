package httpd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/kataras/neffos"
	"github.com/satori/go.uuid"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/service"
)

func OnPingHandler(c *neffos.NSConn, msg neffos.Message) error {
	c.Emit("pong", []byte(""))
	return nil
}

// OnConnectHandler 当websocket连接后触发
func OnNamespaceConnected(c *neffos.NSConn, msg neffos.Message) error {
	// 首次连接 1.获取当前用户的信息
	cc := c.Conn
	logger.Debug("Web terminal on connect event trigger")
	request := cc.Socket().Request()
	header := request.Header
	cookies := strings.Split(header.Get("Cookie"), ";")
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
	cc.Set("currentUser", user)
	remoteAddr := header.Get("X-Forwarded-For")
	if remoteAddr == "" {
		remoteAddr = request.RemoteAddr
	}
	remoteIP = strings.Split(remoteAddr, ",")[0]
	logger.Infof("Accepted %s connect websocket from %s", user.Username, remoteIP)
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			<-tick.C
			if c.Conn.IsClosed() {
				logger.Infof("User %s from %s websocket connect closed", user.Username, remoteIP)
				return
			}
			c.Emit("ping", []byte(""))
		}
	}()
	return nil
}

// OnDisconnect websocket断开后触发
func OnNamespaceDisconnect(c *neffos.NSConn, msg neffos.Message) (err error) {
	logger.Debug("On disconnect event trigger")
	conns.DeleteClients(c.Conn.ID())
	return nil
}

// OnErrorHandler 当出现错误时触发
func OnErrorHandler(e error) {
	logger.Debug("Web terminal on error trigger: ", e)
}

// OnHostHandler 当用户连接Host时触发
func OnHostHandler(c *neffos.NSConn, msg neffos.Message) (err error) {
	logger.Debug("Web terminal on host event trigger")
	cc := c.Conn
	var message HostMsg
	err = msg.Unmarshal(&message)
	if err != nil {
		return
	}
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
	roomID := uuid.NewV4().String()
	emitMsg := RoomMsg{roomID, secret}
	roomMsg, _ := json.Marshal(emitMsg)
	c.Emit("room", roomMsg)

	asset := service.GetAsset(assetID)
	systemUser := service.GetSystemUser(systemUserID)

	if asset.ID == "" || systemUser.ID == "" {
		msg := "No asset id or system user id found, exit"
		logger.Debug(msg)
		dataMsg := DataMsg{Room: roomID, Data: msg}
		c.Emit("data", neffos.Marshal(dataMsg))
		return
	}
	logger.Debug("Web terminal want to connect host: ", asset.Hostname)
	currentUser, ok := cc.Get("currentUser").(*model.User)
	if !ok {
		err = errors.New("not found current user")
		dataMsg := DataMsg{Room: roomID, Data: err.Error()}
		c.Emit("data", neffos.Marshal(dataMsg))
		return
	}
	userR, userW := io.Pipe()
	var addr string
	request := cc.Socket().Request()
	header := request.Header
	remoteAddr := header.Get("X-Forwarded-For")
	if remoteAddr == "" {
		if host, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
			addr = host
		} else {
			addr = request.RemoteAddr
		}
	} else {
		addr = strings.Split(remoteAddr, ",")[0]
	}

	client := &Client{
		Uuid: roomID, addr: addr,
		WinChan: make(chan ssh.Window, 100), Conn: c,
		UserRead: userR, UserWrite: userW, mu: new(sync.RWMutex),
		pty: ssh.Pty{Term: "xterm", Window: win},
	}
	client.WinChan <- win
	clients.AddClient(roomID, client)
	conns.AddClient(cc.ID(), roomID)
	proxySrv := proxy.ProxyServer{
		UserConn: client, User: currentUser,
		Asset: &asset, SystemUser: &systemUser,
	}
	go func() {
		defer logger.Infof("Request %s: Web ssh end proxy process", client.Uuid)
		logger.Infof("Request %s: Web ssh start proxy to host", client.Uuid)
		proxySrv.Proxy()
		logoutMsg, _ := json.Marshal(RoomMsg{Room: roomID})
		// 服务器主动退出
		c.Emit("logout", logoutMsg)
		clients.DeleteClient(roomID)
	}()
	return nil
}

// OnTokenHandler 当使用token连接时触发
func OnTokenHandler(c *neffos.NSConn, msg neffos.Message) (err error) {
	logger.Debug("Web terminal on token event trigger")
	cc := c.Conn
	var message TokenMsg
	err = msg.Unmarshal(&message)
	if err != nil {
		return
	}
	token := message.Token
	secret := message.Secret
	roomID := uuid.NewV4().String()
	roomMsg := RoomMsg{roomID, secret}
	c.Emit("room", neffos.Marshal(roomMsg))

	// check token
	if token == "" || secret == "" {
		msg := fmt.Sprintf("Token or secret is None: %s %s", token, secret)
		dataMsg := DataMsg{Data: msg, Room: roomID}
		c.Emit("data", neffos.Marshal(dataMsg))
		c.Emit("disconnect", nil)
	}
	tokenUser := service.GetTokenAsset(token)
	if tokenUser.UserID == "" {
		msg := "Token info is none, maybe token expired"
		dataMsg := DataMsg{Data: msg, Room: roomID}
		c.Emit("data", neffos.Marshal(dataMsg))
		c.Emit("disconnect", nil)
	}

	currentUser := service.GetUserDetail(tokenUser.UserID)

	if currentUser == nil {
		msg := "User id error"
		dataMsg := DataMsg{Data: msg, Room: roomID}
		c.Emit("data", neffos.Marshal(dataMsg))
		c.Emit("disconnect", nil)
	}

	cc.Set("currentUser", currentUser)
	hostMsg := HostMsg{
		Uuid: tokenUser.AssetID, UserID: tokenUser.SystemUserID,
		Size: message.Size, Secret: secret,
	}
	hostWsMsg := neffos.Message{
		Body: neffos.Marshal(hostMsg),
	}
	return OnHostHandler(c, hostWsMsg)
}

// OnDataHandler 收发数据时触发
func OnDataHandler(c *neffos.NSConn, msg neffos.Message) (err error) {
	var message DataMsg
	err = msg.Unmarshal(&message)
	if err != nil {
		return
	}
	clientID := message.Room
	client := clients.GetClient(clientID)
	if client == nil {
		return
	}

	_, err = client.UserWrite.Write([]byte(message.Data))
	return err
}

// OnResizeHandler 用户窗口改变时触发
func OnResizeHandler(c *neffos.NSConn, msg neffos.Message) (err error) {
	var message ResizeMsg
	err = msg.Unmarshal(&message)
	if err != nil {
		return
	}
	logger.Debugf("Web terminal on resize event trigger: %d*%d", message.Width, message.Height)
	winSize := ssh.Window{Height: message.Height, Width: message.Width}
	for _, clientID := range conns.GetClients(c.Conn.ID()) {
		client := clients.GetClient(clientID)
		if client != nil {
			client.SetWinSize(winSize)
		}
	}
	return nil
}

// OnLogoutHandler 用户登出一个会话时触发, 用户主动退出
func OnLogoutHandler(c *neffos.NSConn, msg neffos.Message) (err error) {
	logger.Debug("Web terminal on logout event trigger: ", msg.Room)
	var clientID string
	err = msg.Unmarshal(&clientID)
	if err != nil {
		return
	}
	clients.DeleteClient(clientID)
	return
}
