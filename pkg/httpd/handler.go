package httpd

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jumpserver/koko/pkg/model"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/gliderlabs/ssh"
	"github.com/kataras/neffos"

	"github.com/satori/go.uuid"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/service"
)



var sshEvents = neffos.Namespaces{
	"ssh": neffos.Events{
		neffos.OnNamespaceConnected: OnNamespaceConnected,
		neffos.OnNamespaceDisconnect: OnNamespaceDisconnect,
		neffos.OnRoomJoined: func(c *neffos.NSConn, msg neffos.Message) error {
			return nil
		},
		neffos.OnRoomLeft: func(c *neffos.NSConn, msg neffos.Message) error {
			return nil
		},

		"data": OnDataHandler,
		"resize": OnResizeHandler,
		"host": OnHostHandler,
		"logout": OnLogoutHandler,
		"token": OnTokenHandler,
	},
}

// OnConnectHandler 当websocket连接后触发
func OnNamespaceConnected(c *neffos.NSConn, msg neffos.Message) error {
	// 首次连接 1.获取当前用户的信息
	cc := c.Conn
	if cc.WasReconnected() {
		logger.Debugf("Web terminal redirected, with tries: %d", cc.ID(), cc.ReconnectTries)
	} else {
		logger.Debug("Web terminal on connect event trigger")
	}
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
	return nil
}


// OnDisconnect websocket断开后触发
func OnNamespaceDisconnect(c *neffos.NSConn, msg neffos.Message) (err error){
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
	fmt.Println("Host msg: ", message)
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
	joinRoomMsg, _ := json.Marshal(emitMsg)
	c.Emit("room", joinRoomMsg)
	if err != nil {
		logger.Debug("Join room error occur: ", err)
		return
	}
	asset := service.GetAsset(assetID)
	systemUser := service.GetSystemUser(systemUserID)

	if asset.ID == "" || systemUser.ID == "" {
		logger.Debug("No asset id or system user id found, exit")
		return
	}
	logger.Debug("Web terminal want to connect host: ", asset.Hostname)
	currentUser, ok := cc.Get("currentUser").(*model.User)
	if !ok {
		return errors.New("not found current user")
	}

	userR, userW := io.Pipe()
	addr, _, _ := net.SplitHostPort(cc.Socket().Request().RemoteAddr)
	client := &Client{
		Uuid: roomID, user: currentUser, addr: addr,
		WinChan: make(chan ssh.Window, 100), Conn: c,
		UserRead: userR, UserWrite: userW, mu: new(sync.RWMutex),
		pty: ssh.Pty{Term: "xterm", Window: win},
	}
	user := cc.Get("currentUser").(*model.User)
	client.WinChan <- win
	clients.AddClient(roomID, client)
	conns.AddClient(cc.ID(), roomID)
	proxySrv := proxy.ProxyServer{
		UserConn: client, User: user,
		Asset: &asset, SystemUser: &systemUser,
	}
	go func() {
		defer logger.Debug("web proxy end")
		logger.Debug("Start proxy")
		proxySrv.Proxy()
		logoutMsg, _ := json.Marshal(RoomMsg{Room: roomID})
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
	clientID := uuid.NewV4().String()
	roomMsg := RoomMsg{clientID, secret}
	c.Emit("room", neffos.Marshal(roomMsg))

	// check token
	if token == "" || secret == "" {
		msg := fmt.Sprintf("Token or secret is None: %s %s", token, secret)
		dataMsg := EmitDataMsg{Data: msg, Room: clientID}
		c.Emit("data", neffos.Marshal(dataMsg))
		c.Emit("disconnect", nil)
	}
	tokenUser := service.GetTokenAsset(token)
	if tokenUser.UserID == "" {
		msg := "Token info is none, maybe token expired"
		dataMsg := EmitDataMsg{Data: msg, Room: clientID}
		c.Emit("data", neffos.Marshal(dataMsg))
		c.Emit("disconnect", nil)
	}

	currentUser := service.GetUserDetail(tokenUser.UserID)

	if currentUser == nil {
		msg := "User id error"
		dataMsg := EmitDataMsg{Data: msg, Room: clientID}
		c.Emit("data", neffos.Marshal(dataMsg))
		c.Emit("disconnect", nil)
	}

	cc.Set("currentUser", currentUser)
	hostMsg := HostMsg{
		Uuid: tokenUser.AssetID, UserID: tokenUser.SystemUserID,
		Size: message.Size, Secret:secret,
	}
	fmt.Println("Host msg: ", hostMsg)
	hostWsMsg := neffos.Message{
		Body:neffos.Marshal(hostMsg),
	}
	return OnHostHandler(c, hostWsMsg)
}

// OnDataHandler 收发数据时触发
func OnDataHandler(c *neffos.NSConn, msg neffos.Message) (err error) {
	roomID := msg.Room
	client := clients.GetClient(roomID)
	if client == nil {
		return
	}

	var message DataMsg
	err = msg.Unmarshal(&message)
	if err != nil {
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
	for _, room := range c.Rooms() {
		roomID := room.Name
		client := clients.GetClient(roomID)
		if client != nil {
			client.SetWinSize(winSize)
		}
	}
	return nil
}

// OnLogoutHandler 用户登出一个会话时触发
func OnLogoutHandler(c *neffos.NSConn, msg neffos.Message) (err error){
	logger.Debug("Web terminal on logout event trigger: ", msg.Room)
	var message RoomMsg
	err = msg.Unmarshal(&message)
	if err != nil {
		return
	}
	roomID := message.Room
	clients.DeleteClient(roomID)
	return
}

