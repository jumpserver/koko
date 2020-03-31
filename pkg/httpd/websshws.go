package httpd

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gliderlabs/ssh"
	"github.com/gorilla/mux"
	"github.com/kataras/neffos"
	"github.com/satori/go.uuid"

	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/service"
)

type proxyServer interface {
	Proxy()
}

func OnPingHandler(c *neffos.NSConn, msg neffos.Message) error {
	if conn, ok := websocketManager.GetUserCon(c.Conn.ID()); ok {
		conn.SendPongEvent()
	} else {
		c.Emit("pong", []byte(""))
	}
	return nil
}

// OnConnectHandler 当websocket连接后触发
func OnNamespaceConnected(ns *neffos.NSConn, msg neffos.Message) error {
	// 首次连接 1.获取当前用户的信息
	if _, ok := websocketManager.GetUserCon(ns.Conn.ID()); ok {
		logger.Warnf("Namespace Connect Event: ws %s already connected.", ns.Conn.ID())
		return nil
	}
	logger.Debugf("Namespace Connect Event: ws %s first connected", ns.Conn.ID())

	userConn, err := NewUserWebsocketConnWithSession(ns)
	if err != nil {
		ns.Emit("data", neffos.Marshal(err.Error()))
		ns.Emit("disconnect", []byte(""))
		return err
	}
	logger.Infof("Accepted user %s connect ssh ws", userConn.User.Username)
	websocketManager.AddUserCon(ns.Conn.ID(), userConn)
	go userConn.loopHandler()
	return nil
}

// OnDisconnect websocket断开后触发
func OnNamespaceDisconnect(c *neffos.NSConn, msg neffos.Message) (err error) {
	logger.Debugf("SSH Disconnect Event: ws %s fire disconnect event", c.Conn.ID())
	websocketID := c.Conn.ID()
	if conn, ok := websocketManager.GetUserCon(websocketID); ok {
		conn.Close()
		websocketManager.DeleteUserCon(websocketID)
		logger.Infof("SSH Disconnect Event: User %s ws %s disconnect.", conn.User.Username, c.Conn.ID())
		return nil
	}
	errMsg := fmt.Sprintf("websocket %s could not found or alread closed", websocketID)
	logger.Error(errMsg)
	return errors.New(errMsg)
}

// OnHostHandler 当用户连接Host时触发
func OnHostHandler(c *neffos.NSConn, msg neffos.Message) (err error) {
	websocketID := c.Conn.ID()
	logger.Debugf("Ws %s fire host event", websocketID)
	userConn, ok := websocketManager.GetUserCon(websocketID)
	if !ok {
		errMsg := fmt.Sprintf("Websocket %s should fire connected first.", websocketID)
		logger.Errorf(errMsg)
		return errors.New(errMsg)
	}

	cc := c.Conn
	var message HostMsg
	err = msg.Unmarshal(&message)
	if err != nil {
		logger.Errorf("Host Event: ws %s unmarshal msg err: %s", websocketID, err)
		return err
	}
	win := ssh.Window{Height: 24, Width: 80}
	assetID := message.Uuid
	systemUserID := message.UserID
	secret := message.Secret
	var width, height int
	if len(message.Size) >= 2 {
		width, height = message.Size[0], message.Size[1]
	}
	if width != 0 {
		win.Width = width
	}
	if height != 0 {
		win.Height = height
	}
	roomID := uuid.NewV4().String()
	emitMsg := RoomMsg{roomID, secret}
	userConn.SendRoomEvent(neffos.Marshal(emitMsg))
	var databaseAsset model.Database
	var asset model.Asset

	systemUser := service.GetSystemUser(systemUserID)
	var connectName string
	switch strings.ToLower(message.HostType) {
	case "database":
		databaseAsset = service.GetDatabase(assetID)
		if databaseAsset.ID == "" || systemUser.ID == "" {
			msg := "No database id or system user id found, exit"
			logger.Error(msg)
			dataMsg := DataMsg{Room: roomID, Data: msg}
			userConn.SendDataEvent(neffos.Marshal(dataMsg))
			return errors.New("no found database or systemUser")
		}
		connectName = databaseAsset.Name
	default:
		asset = service.GetAsset(assetID)
		if asset.ID == "" || systemUser.ID == "" {
			msg := "No asset id or system user id found, exit"
			logger.Error(msg)
			dataMsg := DataMsg{Room: roomID, Data: msg}
			userConn.SendDataEvent(neffos.Marshal(dataMsg))
			return errors.New("no found asset or systemUser")
		}
		connectName = asset.Hostname
	}
	logger.Infof("Ws %s start to connect %s", websocketID, connectName)
	currentUser, ok := cc.Get("currentUser").(*model.User)
	if !ok {
		err = errors.New("not found current user")
		dataMsg := DataMsg{Room: roomID, Data: err.Error()}
		logger.Errorf("Host Event: ws %s no found user.", websocketID)
		userConn.SendDataEvent(neffos.Marshal(dataMsg))
		return err
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
		WinChan: make(chan ssh.Window, 100), Conn: userConn,
		UserRead: userR, UserWrite: userW, mu: new(sync.RWMutex),
		pty: ssh.Pty{Term: "xterm", Window: win},
	}
	client.WinChan <- win
	var proxySrv proxyServer
	switch strings.ToLower(message.HostType) {
	case "database":
		proxySrv = &proxy.DBProxyServer{
			UserConn:   client,
			User:       currentUser,
			Database:   &databaseAsset,
			SystemUser: &systemUser,
		}
	default:
		proxySrv = &proxy.ProxyServer{
			UserConn: client, User: currentUser,
			Asset: &asset, SystemUser: &systemUser,
		}
	}
	go func() {
		logger.Infof("Ws %s add client %s to proxy", websocketID, client.Uuid)
		userConn.AddClient(client.Uuid, client)
		proxySrv.Proxy()
		logoutMsg := LogoutMsg{Room: roomID}
		userConn.SendLogoutEvent(neffos.Marshal(logoutMsg))
		userConn.DeleteClient(client.Uuid)
		logger.Infof("Ws %s remove client %s to proxy", websocketID, client.Uuid)
	}()
	return nil
}

// OnTokenHandler 当使用token连接时触发
func OnTokenHandler(ns *neffos.NSConn, msg neffos.Message) (err error) {
	logger.Debugf("Token Event: ws %s ", ns.Conn.ID())
	var message TokenMsg
	err = msg.Unmarshal(&message)
	if err != nil {
		logger.Errorf("Token Event: ws %s token event unmarshal msg err: %s", ns.Conn.ID(), err)
		return
	}
	token := message.Token
	secret := message.Secret
	roomID := uuid.NewV4().String()
	roomMsg := RoomMsg{roomID, secret}
	ns.Emit("room", neffos.Marshal(roomMsg))
	// check token
	if token == "" || secret == "" {
		msg := fmt.Sprintf("Token or secret is None: %s %s", token, secret)
		dataMsg := DataMsg{Data: msg, Room: roomID}
		ns.Emit("data", neffos.Marshal(dataMsg))
		ns.Emit("disconnect", neffos.Marshal([]byte("")))
		logger.Error("Token Event: ", msg)
		return errors.New("token or secret is None")
	}
	tokenUser := service.GetTokenAsset(token)
	if tokenUser.UserID == "" {
		msg := "Token info is none, maybe token expired"
		dataMsg := DataMsg{Data: msg, Room: roomID}
		ns.Emit("data", neffos.Marshal(dataMsg))
		ns.Emit("disconnect", neffos.Marshal([]byte("")))
		logger.Error("Token Event: ", msg)
		return errors.New("token info is none, maybe token expired")
	}
	userConn, err := NewUserWebsocketConnWithTokenUser(ns, tokenUser)
	if err != nil {
		msg := "User id error"
		dataMsg := DataMsg{Data: msg, Room: roomID}
		ns.Emit("data", neffos.Marshal(dataMsg))
		ns.Emit("disconnect", neffos.Marshal([]byte("")))
		logger.Error("Token Event: ", msg)
		return errors.New("user id error")
	}
	websocketManager.AddUserCon(ns.Conn.ID(), userConn)
	go userConn.loopHandler()
	hostMsg := HostMsg{
		Uuid: tokenUser.AssetID, UserID: tokenUser.SystemUserID,
		Size: message.Size, Secret: secret,
	}
	hostWsMsg := neffos.Message{
		Body: neffos.Marshal(hostMsg),
	}
	return OnHostHandler(ns, hostWsMsg)
}

// OnDataHandler 收发数据时触发
func OnDataHandler(c *neffos.NSConn, msg neffos.Message) (err error) {
	var message DataMsg
	err = msg.Unmarshal(&message)
	if err != nil {
		logger.Errorf("Data Event: ws %s data event unmarshal msg err: %s", c.Conn.ID(), err)
		return
	}
	if conn, ok := websocketManager.GetUserCon(c.Conn.ID()); ok {
		return conn.ReceiveDataEvent(message)
	}
	errMsg := fmt.Sprintf("Data Event: ws %s could not found or already closed", c.Conn.ID())
	logger.Error(errMsg)
	return errors.New(errMsg)

}

// OnResizeHandler 用户窗口改变时触发
func OnResizeHandler(c *neffos.NSConn, msg neffos.Message) (err error) {
	var message ResizeMsg
	err = msg.Unmarshal(&message)
	if err != nil {
		logger.Errorf("Resize Event: ws %s unmarshal msg err %s", c.Conn.ID(), err)
		return
	}
	logger.Debugf("Resize Event: ws %s resize windows to %d*%d", c.Conn.ID(), message.Width, message.Height)
	winSize := ssh.Window{Height: message.Height, Width: message.Width}
	if conn, ok := websocketManager.GetUserCon(c.Conn.ID()); ok {
		conn.ReceiveResizeEvent(winSize)
		return nil
	}
	errMsg := fmt.Sprintf("Resize Event: ws %s could not found or already closed", c.Conn.ID())
	logger.Error(errMsg)
	return errors.New(errMsg)
}

// OnLogoutHandler 用户登出一个会话时触发, 用户主动退出
func OnLogoutHandler(c *neffos.NSConn, msg neffos.Message) (err error) {
	var clientID string
	err = msg.Unmarshal(&clientID)
	if err != nil {
		logger.Errorf("Logout event: ws %s unmarshal msg err: %s", c.Conn.ID(), err)
		return
	}
	logger.Debugf("Logout event: ws %s logout clientID %s", c.Conn.ID(), clientID)
	if conn, ok := websocketManager.GetUserCon(c.Conn.ID()); ok {
		conn.ReceiveLogoutEvent(clientID)
		return nil
	}
	errMsg := fmt.Sprintf("Logout event: ws %s could not found or already closed", c.Conn.ID())
	logger.Error(errMsg)
	return errors.New(errMsg)
}

func roomHandler(wr http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	tmpl := template.Must(template.ParseFiles("./templates/ssh/index.html"))
	roomID := vars["roomID"]
	_ = tmpl.Execute(wr, roomID)
}

func OnShareRoom(ns *neffos.NSConn, msg neffos.Message) (err error) {
	websocketID := ns.Conn.ID()
	logger.Debugf("Ws %s fire ShareRoom", websocketID)
	userConn, ok := websocketManager.GetUserCon(websocketID)
	if !ok {
		errMsg := fmt.Sprintf("Websocket %s should fire connected first.", websocketID)
		logger.Errorf(errMsg)
		return errors.New(errMsg)
	}

	cc := ns.Conn
	var shareRoomMsg struct {
		ShareRoomID string `json:"shareRoomID"`
		Secret      string `json:"secret"`
	}
	err = msg.Unmarshal(&shareRoomMsg)
	if err != nil {
		logger.Errorf("ShareRoom Event: ws %s unmarshal msg err: %s", websocketID, err)
		return err
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
	uid := uuid.NewV4().String()
	emitMsg := RoomMsg{uid, shareRoomMsg.Secret}
	userConn.SendRoomEvent(neffos.Marshal(emitMsg))
	win := ssh.Window{Height: 24, Width: 80}
	client := &Client{
		Uuid: uid, addr: addr,
		WinChan: make(chan ssh.Window, 100), Conn: userConn,
		UserRead: userR, UserWrite: userW, mu: new(sync.RWMutex),
		pty: ssh.Pty{Term: "xterm", Window: win},
	}
	client.WinChan <- win
	userConn.AddClient(client.Uuid, client)
	go JoinRoom(client, shareRoomMsg.ShareRoomID)
	return nil
}

func JoinRoom(c *Client, roomID string) {
	ex := exchange.GetExchange()
	roomChan := make(chan model.RoomMessage)
	room, err := ex.JoinRoom(roomChan, roomID)
	// checkout user reading pem
	logoutMsg := LogoutMsg{Room: c.Uuid}
	logoutData, _ := json.Marshal(logoutMsg)

	if err != nil {
		logger.Errorf("Ws Join Room err: %s", err)
		c.Conn.SendLogoutEvent(logoutData)
		return
	}
	defer ex.LeaveRoom(room, roomID)

	if !c.Conn.CheckShareRoomReadPerm(roomID) {
		logger.Errorf("Ws has no pem to join room")
		c.Conn.SendLogoutEvent(logoutData)
		return
	}
	go func() {
		for {
			msg, ok := <-roomChan
			if !ok {
				logger.Infof("User %s exit room %s by roomChan closed", c.Conn.User.Name, roomID)
				break
			}
			switch msg.Event {
			case model.DataEvent:
				data := DataMsg{Data: string(msg.Body), Room: c.Uuid}
				dataMsg, _ := json.Marshal(data)
				c.Conn.SendShareRoomDataEvent(dataMsg)
				continue
			case model.LogoutEvent, model.MaxIdleEvent:

			case model.AdminTerminateEvent, model.ExitEvent:
			case model.WindowsEvent, model.PingEvent:
				continue

			}
			logger.Infof("User %s stop receive msg from room %s by %s", c.Conn.User.Name, roomID, msg.Event)
			break

		}
		c.Conn.SendLogoutEvent(logoutData)
		_ = c.Close()

	}()

	buf := make([]byte, 1024)
	msg := model.RoomMessage{
		Event: model.DataEvent,
		Body:  []byte{12},
	}
	room.Publish(msg)
	for {
		nr, err := c.Read(buf)
		if err != nil {
			logger.Errorf("User %s exit share room %s by %s", c.Conn.User.Name, roomID, err)
			break
		}
		// checkout user write pem
		if !c.Conn.CheckShareRoomWritePerm(roomID) {
			continue
		}
		msg := model.RoomMessage{
			Event: model.DataEvent,
			Body:  buf[:nr],
		}
		room.Publish(msg)
	}
}
