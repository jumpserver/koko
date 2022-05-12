package httpd

import (
	"encoding/json"
	"io"
	"net/url"
	"strings"
	"sync"

	"github.com/gliderlabs/ssh"

	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/common"
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
	asset      *model.Asset

	app *model.Application

	backendClient *Client

	jmsService *service.JMService

	shareInfo *ShareInfo

	extraParams url.Values
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
	case TargetTypeMonitor:
		ok = h.CheckShareRoomReadPerm(h.ws.user.ID, h.targetId)
	case TargetTypeShare:
		ok = h.CheckEnableShare()
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

		var connectInfo TerminalConnectData
		err := json.Unmarshal([]byte(msg.Data), &connectInfo)
		if err != nil {
			logger.Errorf("Ws[%s] terminal initial message data unmarshal err: %s",
				h.ws.Uuid, err)
			return
		}
		if h.targetType == TargetTypeShare {
			code := connectInfo.Code
			info, err2 := h.ValidateShareParams(h.targetId, code)
			if err2 != nil {
				logger.Errorf("Ws[%s] terminal initial validate share err: %s",
					h.ws.Uuid, err2)
				h.sendCloseMessage()
				return
			}
			h.shareInfo = &info
			sessionInfo, err3 := h.jmsService.GetSessionById(info.Record.SessionId)
			if err3 != nil {
				logger.Errorf("Ws[%s] terminal get session %s err: %s",
					h.ws.Uuid, info.Record.SessionId, err3)
				h.sendCloseMessage()
				return
			}
			data, _ := json.Marshal(sessionInfo)
			h.sendSessionMessage(string(data))
		}
		h.initialed = true
		win := ssh.Window{
			Width:  connectInfo.Cols,
			Height: connectInfo.Rows,
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

func (h *tty) sendSessionMessage(data string) {
	msg := Message{
		Id:   h.ws.Uuid,
		Type: TERMINALSESSION,
		Data: data,
	}
	h.ws.SendMessage(&msg)
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
	case TERMINALSHARE:
		var shareData ShareRequestParams

		err := json.Unmarshal([]byte(msg.Data), &shareData)
		if err != nil {
			logger.Errorf("Ws[%s] message(%s) data unmarshal err: %s", h.ws.Uuid,
				msg.Type, msg.Data)
			return
		}
		logger.Debugf("Ws[%s] receive share request %s", h.ws.Uuid, msg.Data)
		go h.createShareSession(shareData)
		return
	case TERMINALGETSHAREUSERS:
		var query GetUserParams
		err := json.Unmarshal([]byte(msg.Data), &query)
		if err != nil {
			logger.Errorf("Ws[%s] message(%s) data unmarshal err: %s", h.ws.Uuid,
				msg.Type, msg.Data)
			return
		}
		logger.Debugf("Ws[%s] receive share request %s", h.ws.Uuid, msg.Data)
		go h.getShareUserInfo(query)
		return

	case CLOSE:
		_ = h.backendClient.Close()
	default:
		logger.Infof("Ws[%s] handle unknown message(%s) data %s", h.ws.Uuid,
			msg.Type, msg.Data)
	}
}

func (h *tty) createShareSession(shareData ShareRequestParams) {
	// 创建 共享连接
	res, err := h.handleShareRequest(shareData)
	if err != nil {
		logger.Errorf("Ws[%s] handle share request err: %s", h.ws.Uuid, err)
	}
	data, _ := json.Marshal(res)
	h.ws.SendMessage(&Message{
		Id:   h.ws.Uuid,
		Type: TERMINALSHARE,
		Data: string(data),
	})
}

func (h *tty) getShareUserInfo(query GetUserParams) {
	shareUserResp, err := h.jmsService.GetShareUserInfo(query.Query)
	if err != nil {
		logger.Error(err)
		return
	}
	data, _ := json.Marshal(shareUserResp)
	h.ws.SendMessage(&Message{
		Id:   h.ws.Uuid,
		Type: TERMINALGETSHAREUSERS,
		Data: string(data),
	})
}

func (h *tty) handleShareRequest(data ShareRequestParams) (res ShareResponse, err error) {
	shareResp, err := h.jmsService.CreateShareRoom(data.SessionID, data.ExpireTime, data.Meta)
	if err != nil {
		logger.Error(err)
		return res, err
	}
	res.ShareId = shareResp.ID
	res.Code = shareResp.Code
	return
}

func (h *tty) ValidateShareParams(shareId, code string) (info ShareInfo, err error) {
	data := service.SharePostData{
		ShareId:    shareId,
		Code:       code,
		UserId:     h.ws.user.ID,
		RemoteAddr: h.ws.ClientIP(),
	}

	recordRes, err := h.jmsService.JoinShareRoom(data)
	if err != nil {
		logger.Errorf("Conn[%s] Validate Share err: %s", h.ws.Uuid, err)
		var errMsg string
		switch v := recordRes.Err.(type) {
		case string:
			errMsg = v
		default:
			errBytes, _ := json.Marshal(v)
			errMsg = string(errBytes)
		}
		h.ws.SendMessage(&Message{
			Id:   h.ws.Uuid,
			Type: TERMINALERROR,
			Data: errMsg,
		})
		return
	}
	return ShareInfo{recordRes}, nil
}

func (h *tty) getTargetApp(protocol string) bool {
	switch strings.ToLower(protocol) {
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb,
		srvconn.ProtocolK8s, srvconn.ProtocolSQLServer,
		srvconn.ProtocolRedis, srvconn.ProtocolMongoDB,
		srvconn.ProtocolPostgreSQL:
		appAsset, err := h.jmsService.GetApplicationById(h.targetId)
		if err != nil {
			logger.Errorf("Get %s application failed; %s", protocol, err)
			return false
		}
		if appAsset.ID != "" {
			h.app = &appAsset
			return true
		}
	default:
		asset, err := h.jmsService.GetAssetById(h.targetId)
		if err != nil {
			logger.Errorf("Get asset failed; %s", err)
			return false
		}
		if asset.ID != "" {
			h.asset = &asset
			return true
		}
	}
	return false
}

func (h *tty) getk8sContainerInfo() *proxy.ContainerInfo {
	pod := h.extraParams.Get("pod")
	namespace := h.extraParams.Get("namespace")
	container := h.extraParams.Get("container")
	if pod == "" || namespace == "" || container == "" {
		return nil
	}
	info := proxy.ContainerInfo{
		PodName:   pod,
		Namespace: namespace,
		Container: container,
	}
	return &info
}

func (h *tty) getConnectionParams() *proxy.ConnectionParams {
	disableAutoHash := h.extraParams.Get("disableautohash")
	if disableAutoHash == "" {
		return nil
	}
	params := proxy.ConnectionParams{
		DisableMySQLAutoHash: true,
	}
	return &params
}

func (h *tty) proxy(wg *sync.WaitGroup) {
	defer wg.Done()
	switch h.targetType {
	case TargetTypeMonitor:
		h.Monitor(h.backendClient, h.targetId)
	case TargetTypeShare:
		roomID := h.shareInfo.Record.SessionId
		h.JoinRoom(h.backendClient, roomID)
	default:
		proxyOpts := make([]proxy.ConnectionOption, 0, 4)
		proxyOpts = append(proxyOpts, proxy.ConnectProtocolType(h.systemUser.Protocol))
		proxyOpts = append(proxyOpts, proxy.ConnectSystemUser(h.systemUser))
		proxyOpts = append(proxyOpts, proxy.ConnectUser(h.ws.user))
		if langCode, err := h.ws.ctx.Cookie("django_language"); err == nil {
			proxyOpts = append(proxyOpts, proxy.ConnectI18nLang(langCode))
		}
		if params := h.getConnectionParams(); params != nil {
			proxyOpts = append(proxyOpts, proxy.ConnectParams(params))
		}
		switch h.systemUser.Protocol {
		case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb,
			srvconn.ProtocolSQLServer, srvconn.ProtocolPostgreSQL,
			srvconn.ProtocolRedis, srvconn.ProtocolMongoDB:
			proxyOpts = append(proxyOpts, proxy.ConnectApp(h.app))
		case srvconn.ProtocolK8s:
			proxyOpts = append(proxyOpts, proxy.ConnectApp(h.app))
			if info := h.getk8sContainerInfo(); info != nil {
				proxyOpts = append(proxyOpts, proxy.ConnectContainer(info))
			}
		default:
			proxyOpts = append(proxyOpts, proxy.ConnectAsset(h.asset))
		}
		srv, err := proxy.NewServer(h.backendClient, h.jmsService, proxyOpts...)
		if err != nil {
			logger.Errorf("Create proxy server failed: %s", err)
			h.sendCloseMessage()
			return
		}
		srv.OnSessionInfo = func(info *model.Session) {
			data, _ := json.Marshal(info)
			h.sendSessionMessage(string(data))
		}
		srv.Proxy()
	}
	h.sendCloseMessage()
	logger.Info("Ws tty proxy end")
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

func (h *tty) CheckEnableShare() bool {
	termConf, err := h.jmsService.GetTerminalConfig()
	if err != nil {
		logger.Error(err)
	}
	return termConf.EnableSessionShare
}

/*
	1. ask join room id (session id)
	2. room receive msg send to client
	3. client emit msg to room
*/

func (h *tty) JoinRoom(c *Client, roomID string) {

	user := h.ws.user
	meta := exchange.MetaMessage{
		UserId:     user.ID,
		User:       user.String(),
		Created:    common.NewNowUTCTime().String(),
		RemoteAddr: c.RemoteAddr(),
	}
	if room := exchange.GetRoom(roomID); room != nil {
		conn := exchange.WrapperUserCon(c)
		room.Subscribe(conn)
		defer room.UnSubscribe(conn)
		room.Broadcast(&exchange.RoomMessage{
			Event: exchange.ShareJoin,
			Body:  nil,
			Meta:  meta,
		})
		for {
			buf := make([]byte, 1024)
			nr, err := c.Read(buf)
			if nr > 0 {
				room.Receive(&exchange.RoomMessage{
					Event: exchange.DataEvent, Body: buf[:nr],
					Meta: meta})
			}
			if err != nil {
				logger.Error(err)
				break
			}
		}
		room.Broadcast(&exchange.RoomMessage{
			Event: exchange.ShareLeave,
			Body:  nil,
			Meta:  meta,
		})
		logger.Infof("Conn[%s] user read end", c.ID())
		if err := h.jmsService.FinishShareRoom(h.shareInfo.Record.ID); err != nil {
			logger.Infof("Conn[%s] finish share room err: %s", c.ID(), err)
		}
	}
}

func (h *tty) Monitor(c *Client, roomID string) {
	if room := exchange.GetRoom(roomID); room != nil {
		conn := exchange.WrapperUserCon(c)
		room.Subscribe(conn)
		defer room.UnSubscribe(conn)
		for {
			buf := make([]byte, 1024)
			_, err := c.Read(buf)
			if err != nil {
				logger.Error(err)
				break
			}
			logger.Debugf("Conn[%s] user monitor", c.ID())
		}
		logger.Infof("Conn[%s] user read end", c.ID())
	}
}
