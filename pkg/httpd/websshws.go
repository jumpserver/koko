package httpd

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"

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
		logger.Errorf("Ws %s check session failed, may use token", ns.Conn.ID())
		return nil
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
func OnHostHandler(ns *neffos.NSConn, msg neffos.Message) (err error) {
	websocketID := ns.Conn.ID()
	logger.Debugf("Ws %s fire host event", websocketID)
	userConn, ok := websocketManager.GetUserCon(websocketID)
	if !ok {
		errMsg := fmt.Sprintf("Websocket %s should fire connected first.", websocketID)
		logger.Errorf(errMsg)
		return errors.New(errMsg)
	}

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
	userR, userW := io.Pipe()
	client := &Client{
		Uuid:    roomID,
		WinChan: make(chan ssh.Window, 100), Conn: userConn,
		UserRead: userR, UserWrite: userW,
		pty: ssh.Pty{Term: "xterm", Window: win},
	}
	client.WinChan <- win
	logger.Infof("Conn[%s] start to connect %s", roomID, connectName)
	var proxySrv proxyServer
	switch strings.ToLower(message.HostType) {
	case "database":
		proxySrv = &proxy.DBProxyServer{
			UserConn:   client,
			User:       userConn.User,
			Database:   &databaseAsset,
			SystemUser: &systemUser,
			Lang:       GetRequestLang(ns.Conn),
		}
	default:
		proxySrv = &proxy.ProxyServer{
			UserConn: client, User: userConn.User,
			Asset: &asset, SystemUser: &systemUser,
			Lang: GetRequestLang(ns.Conn),
		}
	}
	go func() {
		logger.Infof("Ws %s add Conn[%s] to proxy", websocketID, client.Uuid)
		userConn.AddClient(client.Uuid, client)
		proxySrv.Proxy()
		logoutMsg := LogoutMsg{Room: roomID}
		userConn.SendLogoutEvent(neffos.Marshal(logoutMsg))
		userConn.DeleteClient(client.Uuid)
		logger.Infof("Ws %s remove Conn[%s]", websocketID, client.Uuid)
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
		msg := "check token error"
		dataMsg := DataMsg{Data: msg, Room: roomID}
		ns.Emit("data", neffos.Marshal(dataMsg))
		ns.Emit("disconnect", neffos.Marshal([]byte("")))
		logger.Error("Token Event: ", msg)
		return errors.New("user id error")
	}
	logger.Infof("Ws %s accepted user %s connect by token", ns.Conn.ID(), userConn.User.Username)
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
	logger.Infof("Resize Event: ws %s resize windows to %d*%d", c.Conn.ID(), message.Width, message.Height)
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
	logger.Infof("Logout event: ws %s logout Conn[%s]", c.Conn.ID(), clientID)
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
		errMsg := fmt.Sprintf("Ws %s should fire connected first.", websocketID)
		logger.Errorf(errMsg)
		return errors.New(errMsg)
	}

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
	uid := uuid.NewV4().String()
	emitMsg := RoomMsg{uid, shareRoomMsg.Secret}
	userConn.SendRoomEvent(neffos.Marshal(emitMsg))
	win := ssh.Window{Height: 24, Width: 80}
	client := &Client{
		Uuid:    uid,
		WinChan: make(chan ssh.Window, 100), Conn: userConn,
		UserRead: userR, UserWrite: userW,
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

	if err != nil {
		logger.Errorf("Conn[%s] Join Room err: %s", c.ID(), err)
		logoutMsg.Data = fmt.Sprintf("Join Session err: %s", err)
		logoutData, _ := json.Marshal(logoutMsg)
		c.Conn.SendLogoutEvent(logoutData)
		return
	}
	defer ex.LeaveRoom(room, roomID)

	if !c.Conn.CheckShareRoomReadPerm(roomID) {
		logger.Errorf("Conn[%s] has no pem to join room", c.ID())
		logoutMsg.Data = fmt.Sprintf("You have no perm to join room %s", roomID)
		logoutData, _ := json.Marshal(logoutMsg)
		c.Conn.SendLogoutEvent(logoutData)
		return
	}
	go func() {
		for {
			msg, ok := <-roomChan
			if !ok {
				break
			}
			switch msg.Event {
			case model.DataEvent, model.MaxIdleEvent, model.AdminTerminateEvent:
				data := DataMsg{Data: string(msg.Body), Room: c.Uuid}
				dataMsg, _ := json.Marshal(data)
				c.Conn.SendShareRoomDataEvent(dataMsg)
				continue
			case model.LogoutEvent, model.ExitEvent:
				logoutMsg.Data = fmt.Sprintf("Session %s exit.", roomID)
				logoutData, _ := json.Marshal(logoutMsg)
				c.Conn.SendLogoutEvent(logoutData)
			case model.WindowsEvent, model.PingEvent:
				continue
			default:
				logger.Errorf("Conn[%s] receive unknown room event %s", c.Conn.User.Name, roomID, msg.Event)

			}
			logger.Infof("Conn[%s] stop receive msg from room %s by %s", c.ID(), roomID, msg.Event)
			break

		}
		_ = c.Close()
		c.Conn.DeleteClient(c.Uuid)
		logger.Infof("Conn[%s] User %s exit room %s", c.ID(), c.Conn.User.Name, roomID)
	}()

	buf := make([]byte, 1024)
	for {
		nr, err := c.Read(buf)
		if err != nil {
			logger.Errorf("Conn[%s] User %s exit share room %s by %s", c.Conn.User.Name, roomID, err)
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
		logger.Infof("User %s published DataEvent to room %s", c.Conn.User.Name, roomID)
	}
}
