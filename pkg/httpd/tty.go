package httpd

import (
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/gliderlabs/ssh"

	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/srvconn"
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

	jmsService *service.JMService
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
	case TargetTypeRoom:
		ok = h.CheckShareRoomReadPerm(h.ws.user.ID, h.targetId)
	default:
		if h.systemUserId == "" || h.targetId == "" {
			logger.Errorf("Ws[%s] miss required query params.", h.ws.Uuid)
			return false
		}
		systemUser, err := h.jmsService.GetSystemUserById(h.systemUserId)
		if err != nil {
			logger.Errorf("Ws[%s] get system user err: %s", h.ws.Uuid, err)
			return false
		}
		if systemUser.ID == "" {
			logger.Errorf("Ws[%s] get invalid system user", h.ws.Uuid)
			return false
		}
		h.systemUser = &systemUser

		ok = h.getTargetApp(systemUser.Protocol)
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
	case TERMINALBINARY:
		h.backendClient.WriteData(msg.Raw)
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

func (h *tty) getTargetApp(protocol string) bool {
	switch strings.ToLower(protocol) {
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
		databaseAsset, err := h.jmsService.GetMySQLOrMariadbApplicationById(h.targetId)
		if err != nil {
			logger.Errorf("Get MySQL App failed; %s", err)
			return false
		}
		if databaseAsset.ID != "" {
			h.dbApp = &databaseAsset
			return true
		}
	case srvconn.ProtocolK8s:
		k8sCluster, err := h.jmsService.GetK8sApplicationById(h.targetId)
		if err != nil {
			logger.Errorf("Get K8s App failed; %s", err)
			return false
		}
		if k8sCluster.ID != "" {
			h.k8sApp = &k8sCluster
			return true
		}
	default:
		asset, err := h.jmsService.GetAssetById(h.targetId)
		if err != nil {
			logger.Errorf("Get asset failed; %s", err)
			return false
		}
		if asset.ID != "" {
			h.assetApp = &asset
			return true
		}
	}
	return false
}

func (h *tty) proxy(wg *sync.WaitGroup) {
	defer wg.Done()
	switch h.targetType {
	case TargetTypeRoom:
		h.JoinRoom(h.backendClient, h.targetId)
	default:
		proxyOpts := make([]proxy.ConnectionOption, 0, 4)
		proxyOpts = append(proxyOpts, proxy.ConnectProtocolType(h.systemUser.Protocol))
		proxyOpts = append(proxyOpts, proxy.ConnectSystemUser(h.systemUser))
		proxyOpts = append(proxyOpts, proxy.ConnectUser(h.ws.user))
		switch h.systemUser.Protocol {
		case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb:
			proxyOpts = append(proxyOpts, proxy.ConnectDBApp(h.dbApp))
		case srvconn.ProtocolK8s:
			proxyOpts = append(proxyOpts, proxy.ConnectK8sApp(h.k8sApp))
		default:
			proxyOpts = append(proxyOpts, proxy.ConnectAsset(h.assetApp))
		}
		srv, err := proxy.NewServer(h.backendClient, h.jmsService, proxyOpts...)
		if err != nil {
			logger.Errorf("Create proxy server failed: %s", err)
			return
		}
		srv.Proxy()
	}
	h.sendCloseMessage()
}

func (h *tty) CheckShareRoomReadPerm(uerId, roomId string) bool {
	ret, err := h.jmsService.ValidateJoinSessionPermission(uerId, roomId)
	if err != nil {
		logger.Errorf("Create share room %s failed: %s", roomId, err)
		return false
	}
	if !ret.Ok {
		return false
	}
	return true
}

func (h *tty) CheckShareRoomWritePerm(uid, roomId string) bool {
	// todo: check current user has pem to write
	return false
}

func (h *tty) JoinRoom(c *Client, roomID string) {
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
			if nr > 0 && h.CheckShareRoomWritePerm(c.Conn.user.ID, roomID) {
				room.Receive(&exchange.RoomMessage{
					Event: exchange.DataEvent, Body: buf[:nr]})
			}
			if err != nil {
				logger.Error(err)
				break
			}
		}
		logger.Infof("Conn[%s] user read end", c.ID())
	}
}
