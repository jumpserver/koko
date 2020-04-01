package httpd

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/kataras/neffos"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

func NewUserWebsocketConnWithTokenUser(ns *neffos.NSConn, tokenUser model.TokenUser) (*UserWebsocketConn, error) {
	currentUser := service.GetUserDetail(tokenUser.UserID)
	if currentUser == nil {
		logger.Errorf("Ws %s get user id %s err", ns.Conn.ID(), tokenUser.UserID)
		return nil, errors.New("user id error")
	}
	ns.Conn.Set("currentUser", currentUser)
	return &UserWebsocketConn{
		ns:                  ns,
		User:                currentUser,
		clients:             make(map[string]*Client),
		dataEventChan:       make(chan []byte, 1024),
		logoutEventChan:     make(chan []byte, 1024),
		roomEventChan:       make(chan []byte, 1024),
		disconnectEventChan: make(chan struct{}, 1024),
		pongEventChan:       make(chan struct{}, 1024),
		closed:              make(chan struct{}),

		shareDataEventChan: make(chan []byte, 1024),
	}, nil
}

func NewUserWebsocketConnWithSession(ns *neffos.NSConn) (*UserWebsocketConn, error) {
	request := ns.Conn.Socket().Request()
	header := request.Header
	cookies := strings.Split(header.Get("Cookie"), ";")
	var csrfToken, sessionID string
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
		msg := "uer is not authenticated"
		logger.Error(msg)
		return nil, errors.New(msg)
	}
	ns.Conn.Set("currentUser", user)
	return &UserWebsocketConn{
		ns:                  ns,
		User:                user,
		clients:             make(map[string]*Client),
		dataEventChan:       make(chan []byte, 1024),
		logoutEventChan:     make(chan []byte, 1024),
		roomEventChan:       make(chan []byte, 1024),
		disconnectEventChan: make(chan struct{}, 1024),
		pongEventChan:       make(chan struct{}, 1024),
		closed:              make(chan struct{}),

		shareDataEventChan: make(chan []byte, 1024),
	}, nil
}

type UserWebsocketConn struct {
	ns      *neffos.NSConn
	User    *model.User
	clients map[string]*Client

	dataEventChan       chan []byte   // 1024 cap
	logoutEventChan     chan []byte   // 1024
	roomEventChan       chan []byte   // 1024
	disconnectEventChan chan struct{} // 1024
	pongEventChan       chan struct{} // 1024
	closed              chan struct{} // 1024

	shareDataEventChan chan []byte // 1024 cap

	win ssh.Window
	mu  sync.Mutex
}

func (u *UserWebsocketConn) AddClient(id string, client *Client) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.clients[id] = client
}

func (u *UserWebsocketConn) DeleteClient(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.clients, id)
	logger.Infof("User %s remain connected clients: %d",
		u.User.Name, len(u.clients))
}

func (u *UserWebsocketConn) GetClient(id string) (client *Client, ok bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	client, ok = u.clients[id]
	return client, ok
}

func (u *UserWebsocketConn) loopHandler() {
	logger.Infof("User %s start ws %s events handler loop.",
		u.User.Username, u.ns.Conn.ID())
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		var event string
		var data []byte
		select {
		case dataMsg := <-u.dataEventChan:
			event = "data"
			data = dataMsg

		case logoutMsg := <-u.logoutEventChan:
			event = "logout"
			data = logoutMsg

		case roomMsg := <-u.roomEventChan:
			event = "room"
			data = roomMsg
		case shareData := <-u.shareDataEventChan:
			event = "shareRoomData"
			data = shareData

		case <-u.pongEventChan:
			event = "pong"
			data = []byte("")

		case <-u.disconnectEventChan:
			event = "disconnect"
			data = []byte("")
		case <-tick.C:
			event = "ping"
			data = []byte("")
			// send ping event
		case <-u.closed:
			logger.Infof("User %s stop ws %s events handler loop",
				u.User.Username, u.ns.Conn.ID())
			return
		}
		u.ns.Emit(event, data)
	}
}

func (u *UserWebsocketConn) SendDataEvent(data []byte) {
	u.dataEventChan <- data
}

func (u *UserWebsocketConn) SendLogoutEvent(data []byte) {
	u.logoutEventChan <- data
}

func (u *UserWebsocketConn) SendRoomEvent(data []byte) {
	u.roomEventChan <- data
}

func (u *UserWebsocketConn) SendShareRoomDataEvent(data []byte) {
	u.shareDataEventChan <- data
}

func (u *UserWebsocketConn) SendPongEvent() {
	u.pongEventChan <- struct{}{}
}
func (u *UserWebsocketConn) SendDisconnectEvent() {
	u.disconnectEventChan <- struct{}{}
}

func (u *UserWebsocketConn) ReceiveDataEvent(data DataMsg) error {
	u.mu.Lock()
	client, ok := u.clients[data.Room]
	u.mu.Unlock()
	if !ok {
		return fmt.Errorf("not found client id %s", data.Room)
	}
	_, err := client.UserWrite.Write([]byte(data.Data))
	return err
}

func (u *UserWebsocketConn) ReceiveResizeEvent(size ssh.Window) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.win.Width == size.Width && u.win.Height == size.Height {
		return
	}
	for _, client := range u.clients {
		client.SetWinSize(size)
	}
	u.win = size
}

func (u *UserWebsocketConn) ReceiveLogoutEvent(clientID string) {
	if client, ok := u.GetClient(clientID); ok {
		_ = client.Close()
	}
}

func (u *UserWebsocketConn) Close() {
	u.mu.Lock()
	defer u.mu.Unlock()
	select {
	case <-u.closed:
		logger.Warnf("User %s websocket %s already closed",
			u.User.Name, u.ns.Conn.ID())
		return
	default:
		close(u.closed)
	}
	logger.Warnf("User %s websocket %s closed",
		u.User.Name, u.ns.Conn.ID())
	for _, client := range u.clients {
		_ = client.Close()
	}
}

func (u *UserWebsocketConn) CheckShareRoomWritePerm(shareRoomID string) bool {
	// todo: check current user has pem to write
	return false
}

func (u *UserWebsocketConn) CheckShareRoomReadPerm(shareRoomID string) bool {
	// todo: check current user has pem to join room and read
	return true
}

type WebsocketManager struct {
	userCons map[string]*UserWebsocketConn
	mu       sync.Mutex
}

func (m *WebsocketManager) AddUserCon(id string, conn *UserWebsocketConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.userCons[id] = conn
}

func (m *WebsocketManager) DeleteUserCon(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.userCons, id)
}

func (m *WebsocketManager) GetUserCon(id string) (conn *UserWebsocketConn, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, ok = m.userCons[id]
	return conn, ok
}

func (m *WebsocketManager) GetWebsocketData() map[string]interface{} {
	data := make(map[string]interface{})
	m.mu.Lock()
	defer m.mu.Unlock()
	data["connects"] = len(m.userCons)
	return data
}

var websocketManager = &WebsocketManager{
	userCons: make(map[string]*UserWebsocketConn),
}
