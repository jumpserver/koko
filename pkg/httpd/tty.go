package httpd

import (
	"encoding/json"
	"io"
	"sync"

	"github.com/gliderlabs/ssh"

	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/service"
)

var _ Handler = (*tty)(nil)

type tty struct {
	ws *UserWebsocket

	targetType   string
	targetId     string
	systemUserId string

	initialed  bool
	wg         sync.WaitGroup
	systemUser *model.SystemUser
	assetApp   *model.Asset
	k8sApp     *model.K8sApplication
	dbApp      *model.DatabaseApplication

	backendClient *Client
}

func (h *tty) Name() string {
	return TTYName
}

func (h *tty) CleanUp() {
	if h.backendClient != nil {
		_ = h.backendClient.Close()
	}
	h.wg.Wait()
}

func (h *tty) CheckValidation() bool {
	var ok bool
	switch h.targetType {
	case TargetTypeDB, TargetTypeK8s, TargetTypeAsset:
		if h.systemUserId == "" || h.targetId == "" {
			logger.Errorf("Ws[%s] miss required query params.", h.ws.Uuid)
			return false
		}
		systemUser := service.GetSystemUser(h.systemUserId)
		if systemUser.ID == "" {
			return false
		}
		h.systemUser = &systemUser
		ok = h.getApp()
	case TargetTypeRoom:
		ok = CheckShareRoomReadPerm(h.ws.user.ID, h.targetId)
	}
	logger.Infof("Ws[%s] check connect type %s: %t", h.ws.Uuid, h.targetType, ok)
	return ok
}

func (h *tty) HandleMessage(msg *Message) {
	switch msg.Type {
	case TERMINALINIT:
		if msg.Id != h.ws.Uuid {
			logger.Errorf("Ws[%s] terminal initial unknown message id %s", h.ws.Uuid, msg.Id)
			return
		}
		if h.initialed {
			logger.Errorf("Ws[%s] terminal has been already initialed", h.ws.Uuid)
			return
		}

		var size WindowSize
		err := json.Unmarshal([]byte(msg.Data), &size)
		if err != nil {
			logger.Errorf("Ws[%s] terminal initial message data unmarshal err: %s",
				h.ws.Uuid, err)
			return
		}
		h.initialed = true
		win := ssh.Window{
			Width:  size.Cols,
			Height: size.Rows,
		}
		userR, userW := io.Pipe()
		h.backendClient = &Client{
			WinChan: make(chan ssh.Window, 100), Conn: h.ws,
			UserRead: userR, UserWrite: userW,
			pty: ssh.Pty{Term: "xterm", Window: win},
		}
		h.wg.Add(1)
		go h.proxy(&h.wg)
		return
	}
	if h.initialed {
		h.handleTerminalMessage(msg)
	}
}

func (h *tty) sendCloseMessage() {
	closedMsg := Message{
		Id:   h.ws.Uuid,
		Type: CLOSE,
	}
	h.ws.SendMessage(&closedMsg)
}

func (h *tty) handleTerminalMessage(msg *Message) {
	switch msg.Type {
	case TERMINALDATA:
		h.backendClient.WriteData([]byte(msg.Data))
	case TERMINALRESIZE:
		var size WindowSize
		err := json.Unmarshal([]byte(msg.Data), &size)
		if err != nil {
			logger.Errorf("Ws[%s] message(%s) data unmarshal err: %s", h.ws.Uuid,
				msg.Type, msg.Data)
			return
		}
		h.backendClient.SetWinSize(ssh.Window{
			Width:  size.Cols,
			Height: size.Rows,
		})
	case CLOSE:
		_ = h.backendClient.Close()
	default:
		logger.Infof("Ws[%s] handle unknown message(%s) data %s", h.ws.Uuid,
			msg.Type, msg.Data)
	}
}

func (h *tty) getApp() bool {
	switch h.getAppType() {
	case AppTypeDB:
		databaseAsset := service.GetMySQLApplication(h.targetId)
		if databaseAsset.Id != "" {
			h.dbApp = &databaseAsset
			return true
		}
	case AppTypeK8s:
		k8sCluster := service.GetK8sApplication(h.targetId)
		if k8sCluster.Id != "" {
			h.k8sApp = &k8sCluster
			return true
		}
	case AppTypeAsset:
		asset := service.GetAsset(h.targetId)
		if asset.ID != "" {
			h.assetApp = &asset
			return true
		}
	}
	return false
}

func (h *tty) getAppType() int {
	appType := AppUnknown
	switch h.targetType {
	case TargetTypeDB:
		appType = AppTypeDB
	case TargetTypeK8s:
		appType = AppTypeK8s
	case TargetTypeAsset:
		appType = AppTypeAsset
	}
	return appType
}

func (h *tty) proxy(wg *sync.WaitGroup) {
	defer wg.Done()
	var proxySrv proxyServer
	switch h.targetType {
	case TargetTypeDB, TargetTypeK8s, TargetTypeAsset:
		switch h.getAppType() {
		case AppTypeDB:
			proxySrv = &proxy.DBProxyServer{
				UserConn:   h.backendClient,
				User:       h.ws.CurrentUser(),
				Database:   h.dbApp,
				SystemUser: h.systemUser,
			}
		case AppTypeK8s:
			proxySrv = &proxy.K8sProxyServer{
				UserConn:   h.backendClient,
				User:       h.ws.CurrentUser(),
				Cluster:    h.k8sApp,
				SystemUser: h.systemUser,
			}
		case AppTypeAsset:
			proxySrv = &proxy.ProxyServer{
				UserConn:   h.backendClient,
				User:       h.ws.CurrentUser(),
				Asset:      h.assetApp,
				SystemUser: h.systemUser,
			}
		}
	case TargetTypeRoom:
		JoinRoom(h.backendClient, h.targetId)
	default:
		return
	}
	if proxySrv != nil {
		proxySrv.Proxy()
	}
	h.sendCloseMessage()
}

type proxyServer interface {
	Proxy()
}

func CheckShareRoomReadPerm(uerId, roomId string) bool {
	return service.JoinRoomValidate(uerId, roomId)
}

func CheckShareRoomWritePerm(uid, roomId string) bool {
	// todo: check current user has pem to write
	return false
}

func JoinRoom(c *Client, roomID string) {
	/*
		1. ask join room id (session id)
		2. room receive msg send to client
		3. client emit msg to room
	*/
	if room := exchange.GetRoom(roomID); room != nil {
		conn := exchange.WrapperUserCon(c)
		room.Subscribe(conn)
		defer room.UnSubscribe(conn)
		for {
			buf := make([]byte, 1024)
			nr, err := c.Read(buf)
			if nr > 0 && CheckShareRoomWritePerm(c.Conn.user.ID, roomID) {
				room.Receive(&model.RoomMessage{
					Event: model.DataEvent, Body: buf[:nr]})
			}
			if err != nil {
				break
			}
		}
		logger.Infof("Conn[%s] user read end", c.ID())
	}
}
