package httpd

import (
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/jumpserver/koko/pkg/srvconn"

	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
)

var _ Handler = (*tty)(nil)

type tty struct {
	ws *UserWebsocket

	initialed bool
	wg        sync.WaitGroup

	backendClient *Client

	shareInfo *ShareInfo

	K8sClients map[string]*Client
}

func (h *tty) Name() string {
	return TTYName
}

func (h *tty) CleanUp() {
	if h.backendClient != nil {
		_ = h.backendClient.Close()
	}

	for id, client := range h.K8sClients {
		_ = client.Close()
		delete(h.K8sClients, id)
	}
}

func (h *tty) CheckValidation() error {
	var err error
	params := h.ws.wsParams
	switch params.TargetType {
	case TargetTypeMonitor:
		return h.CheckMonitorReadPerm(h.ws.user.ID, params.TargetId)
	case TargetTypeShare:
		return h.CheckEnableShare()
	default:
		if h.ws.ConnectToken == nil {
			return errors.New("connect token is nil")
		}
	}
	return err
}

func (h *tty) HandleMessage(msg *Message) {
	switch msg.Type {
	case TerminalInit:
		if msg.Id != h.ws.Uuid {
			logger.Errorf("Ws[%s] terminal initial unknown message id %s", h.ws.Uuid, msg.Id)
			return
		}
		if h.initialed {
			logger.Errorf("Ws[%s] terminal has been already initialed", h.ws.Uuid)
			return
		}

		connectInfo, err := h.validateAndInitSession(msg)
		if err != nil {
			return
		}

		h.initialed = true
		h.handleTerminalInit(connectInfo, "", "", "", "")

	case TerminalK8SInit:
		if msg.Id != h.ws.Uuid {
			logger.Errorf("Ws[%s] terminal initial unknown message id %s", h.ws.Uuid, msg.Id)
			return
		}

		connectInfo, err := h.validateAndInitSession(msg)
		if err != nil {
			return
		}

		h.handleTerminalInit(connectInfo, msg.KubernetesId, msg.Namespace, msg.Pod, msg.Container)
	}

	if h.initialed || func() bool { _, ok := h.K8sClients[msg.KubernetesId]; return ok }() {
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

func (h *tty) sendK8SCloseMessage(KubernetesId string) {
	closedMsg := Message{
		Id:           h.ws.Uuid,
		Type:         K8SClose,
		KubernetesId: KubernetesId,
	}
	h.ws.SendMessage(&closedMsg)
}

func (h *tty) sendSessionMessage(data string, KubernetesId string) {
	msg := Message{
		Id:           h.ws.Uuid,
		Type:         TerminalSession,
		Data:         data,
		KubernetesId: KubernetesId,
	}
	h.ws.SendMessage(&msg)
}

func (h *tty) validateAndInitSession(msg *Message) (TerminalConnectData, error) {
	var connectInfo TerminalConnectData
	err := json.Unmarshal([]byte(msg.Data), &connectInfo)
	if err != nil {
		logger.Errorf("Ws[%s] terminal initial message data unmarshal err: %s",
			h.ws.Uuid, err)
		return connectInfo, err
	}

	params := h.ws.wsParams

	if params.TargetType == TargetTypeShare {
		code := connectInfo.Code
		info, err2 := h.ValidateShareParams(params.TargetId, code)
		if err2 != nil {
			logger.Errorf("Ws[%s] terminal initial validate share err: %s",
				h.ws.Uuid, err2)
			h.sendCloseMessage()
			return connectInfo, err2
		}
		h.shareInfo = &info
		sessionDetail, err3 := h.ws.apiClient.GetSessionById(info.Record.Session.ID)
		if err3 != nil {
			logger.Errorf("Ws[%s] terminal get session %s err: %s",
				h.ws.Uuid, info.Record.Session.ID, err3)
			h.sendCloseMessage()
			return connectInfo, err3
		}
		sessionInfo := proxy.SessionInfo{
			Session: &sessionDetail,
		}
		data, _ := json.Marshal(sessionInfo)
		h.sendSessionMessage(string(data), msg.KubernetesId)
	}
	return connectInfo, nil
}

func (h *tty) handleTerminalInit(connectInfo TerminalConnectData, KubernetesId, namespace, pod, container string) {
	win := ssh.Window{
		Width:  connectInfo.Cols,
		Height: connectInfo.Rows,
	}
	userR, userW := io.Pipe()
	client := &Client{
		WinChan: make(chan ssh.Window, 100), Conn: h.ws,
		UserRead: userR, UserWrite: userW,
		pty:          ssh.Pty{Term: "xterm", Window: win},
		KubernetesId: KubernetesId, Namespace: namespace,
		Pod: pod, Container: container,
	}

	if KubernetesId != "" {
		if h.K8sClients == nil {
			h.K8sClients = make(map[string]*Client)
		}
		h.K8sClients[KubernetesId] = client
	} else {
		h.backendClient = client
	}

	h.wg.Add(1)
	go h.proxy(&h.wg, client)
}

func (h *tty) handleTerminalMessage(msg *Message) {
	switch msg.Type {
	case TerminalData, TerminalBinary:
		data := getDataBytes(msg)
		h.backendClient.WriteData(data)
	case TerminalResize, TerminalK8SResize:
		h.handleResize(msg)
	case TerminalK8SData, TerminalK8SBinary:
		h.handleK8SMessage(msg)
	case TerminalShare:
		var shareData ShareRequestParams

		err := json.Unmarshal([]byte(msg.Data), &shareData)
		if err != nil {
			logger.Errorf("Ws[%s] message(%s) data unmarshal err: %s", h.ws.Uuid,
				msg.Type, msg.Data)
			return
		}
		logger.Debugf("Ws[%s] receive share request %s", h.ws.Uuid, msg.Data)
		go h.createShareSession(&shareData)
		return
	case TerminalGetShareUser:
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
	case TerminalShareUserRemove:
		var query RemoveSharingUserParams
		err := json.Unmarshal([]byte(msg.Data), &query)
		if err != nil {
			logger.Errorf("Ws[%s] message(%s) data unmarshal err: %s", h.ws.Uuid,
				msg.Type, msg.Data)
			return
		}
		logger.Debugf("Ws[%s] receive share remove user request %s", h.ws.Uuid, msg.Data)
		go h.removeShareUser(&query)
		return
	case TerminalSyncUserPreference:
		var preference UserKoKoPreferenceParam
		err := json.Unmarshal([]byte(msg.Data), &preference)
		if err != nil {
			logger.Errorf("Ws[%s] message(%s) data unmarshal err: %s", h.ws.Uuid,
				msg.Type, msg.Data)
			return
		}
		logger.Debugf("Ws[%s] receive sync user preference request %s", h.ws.Uuid, msg.Data)
		go h.syncUserPreference(&preference)
		return
	case CLOSE:
		_ = h.backendClient.Close()
	case K8SClose:
		if k8sClient, ok := h.K8sClients[msg.KubernetesId]; ok {
			_ = k8sClient.Close()
			delete(h.K8sClients, msg.KubernetesId)
		}
	default:
		logger.Infof("Ws[%s] handle unknown message(%s) data %s", h.ws.Uuid,
			msg.Type, msg.Data)
	}
}

func getDataBytes(msg *Message) []byte {
	if msg.Type == TerminalData || msg.Type == TerminalK8SData {
		return []byte(msg.Data)
	}
	return msg.Raw
}

func (h *tty) handleK8SMessage(msg *Message) {
	if k8sClient, ok := h.K8sClients[msg.KubernetesId]; ok {
		k8sClient.WriteData(getDataBytes(msg))
	}
}

func (h *tty) handleResize(msg *Message) {
	var size WindowSize
	err := json.Unmarshal([]byte(msg.Data), &size)
	if err != nil {
		logger.Errorf("Ws[%s] message(%s) data unmarshal err: %s", h.ws.Uuid, msg.Type, msg.Data)
		return
	}
	if msg.Type == TerminalResize {
		h.backendClient.SetWinSize(ssh.Window{
			Width:  size.Cols,
			Height: size.Rows,
		})
	} else if msg.Type == TerminalK8SResize {
		if k8sClient, ok := h.K8sClients[msg.KubernetesId]; ok {
			k8sClient.SetWinSize(ssh.Window{Width: size.Cols, Height: size.Rows})
		}
	}
}

func (h *tty) removeShareUser(query *RemoveSharingUserParams) {
	if room := exchange.GetRoom(query.SessionId); room != nil {
		var data = make(map[string]interface{})
		data["primary_user"] = h.ws.user.String()
		data["share_user"] = query.UserMeta.User
		data["terminal_id"] = query.UserMeta.TerminalId
		body, _ := json.Marshal(data)
		room.Broadcast(&exchange.RoomMessage{
			Event: exchange.ShareRemoveUser,
			Body:  body,
			Meta:  query.UserMeta,
		})
	}
}

func (h *tty) syncUserPreference(preference *UserKoKoPreferenceParam) {
	/*
		{"basic":{"file_name_conflict_resolution":"replace","terminal_theme_name":"Flat"}}
	*/
	reqCookies := h.ws.ctx.Request.Cookies()
	var cookies = make(map[string]string)
	for _, cookie := range reqCookies {
		cookies[cookie.Name] = cookie.Value
	}
	data := model.UserKokoPreference{
		Basic: model.KokoBasic{
			ThemeName: preference.ThemeName,
		},
	}
	var msg struct {
		EventName string `json:"event_name"`
	}
	msg.EventName = "sync_user_preference"
	errMsg := ""
	err := h.ws.apiClient.SyncUserKokoPreference(cookies, data)
	if err != nil {
		logger.Errorf("Ws[%s] sync user preference err: %s", h.ws.Uuid, err)
		errMsg = err.Error()
	}
	msgNotify, _ := json.Marshal(msg)

	h.ws.SendMessage(&Message{
		Id:   h.ws.Uuid,
		Type: MessageNotify,
		Data: string(msgNotify),
		Err:  errMsg,
	})

}

func (h *tty) createShareSession(shareData *ShareRequestParams) {
	// 创建 共享连接
	res, err := h.handleShareRequest(shareData)
	if err != nil {
		logger.Errorf("Ws[%s] handle share request err: %s", h.ws.Uuid, err)
	}
	data, _ := json.Marshal(res)
	h.ws.SendMessage(&Message{
		Id:   h.ws.Uuid,
		Type: TerminalShare,
		Data: string(data),
	})
}

func (h *tty) getShareUserInfo(query GetUserParams) {
	shareUserResp, err := h.ws.apiClient.GetShareUserInfo(query.Query)
	if err != nil {
		logger.Error(err)
		return
	}
	data, _ := json.Marshal(shareUserResp)
	h.ws.SendMessage(&Message{
		Id:   h.ws.Uuid,
		Type: TerminalGetShareUser,
		Data: string(data),
	})
}

func (h *tty) handleShareRequest(data *ShareRequestParams) (res ShareResponse, err error) {
	shareResp, err := h.ws.apiClient.CreateShareRoom(data.SharingSessionRequest)
	if err != nil {
		logger.Error(err)
		return res, err
	}
	res.ShareId = shareResp.ID
	res.Code = shareResp.Code
	return
}

func (h *tty) ValidateShareParams(shareId, code string) (info ShareInfo, err error) {
	data := model.SharePostData{
		ShareId:    shareId,
		Code:       code,
		UserId:     h.ws.user.ID,
		RemoteAddr: h.ws.ClientIP(),
	}

	recordRes, err := h.ws.apiClient.JoinShareRoom(data)
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
			Type: TerminalError,
			Err:  errMsg,
		})
		return
	}
	return ShareInfo{recordRes}, nil
}

func (h *tty) getK8sContainerInfo(client *Client) *proxy.ContainerInfo {
	pod := client.Pod
	namespace := client.Namespace
	container := client.Container
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
	wsParams := h.ws.wsParams
	disableAutoHash := wsParams.DisableAutoHash
	if disableAutoHash == "" {
		return nil
	}
	params := proxy.ConnectionParams{
		DisableMySQLAutoHash: true,
	}
	return &params
}

func (h *tty) proxy(wg *sync.WaitGroup, client *Client) {
	defer wg.Done()
	params := h.ws.wsParams
	switch params.TargetType {
	case TargetTypeMonitor:
		h.Monitor(h.backendClient, params.TargetId)
	case TargetTypeShare:
		roomID := h.shareInfo.Record.Session.ID
		h.JoinRoom(h.backendClient, roomID)
	default:
		connectToken := h.ws.ConnectToken
		proxyOpts := make([]proxy.ConnectionOption, 0, 10)
		proxyOpts = append(proxyOpts, proxy.ConnectTokenAuthInfo(connectToken))
		proxyOpts = append(proxyOpts, proxy.ConnectI18nLang(h.ws.langCode))
		proxyOpts = append(proxyOpts, proxy.ConnectParams(h.getConnectionParams()))
		proxyOpts = append(proxyOpts, proxy.ConnectContainer(h.getK8sContainerInfo(client)))
		srv, err := proxy.NewServer(client, h.ws.apiClient, proxyOpts...)
		if err != nil {
			logger.Errorf("Create proxy server failed: %s", err)
			h.sendCloseMessage()
			return
		}
		srv.OnSessionInfo = func(info *proxy.SessionInfo) {
			data, _ := json.Marshal(info)
			h.sendSessionMessage(string(data), client.KubernetesId)
		}
		srv.Proxy()
	}

	if params.TargetType == srvconn.ProtocolK8s {
		delete(h.K8sClients, client.KubernetesId)
		h.sendK8SCloseMessage(client.KubernetesId)
		return
	}
	h.sendCloseMessage()
	logger.Info("Ws tty proxy end")
}

func (h *tty) CheckMonitorReadPerm(uerId, roomId string) error {
	ret, err := h.ws.apiClient.ValidateJoinSessionPermission(uerId, roomId)
	if err != nil {
		logger.Errorf("Create share room %s failed: %s", roomId, err)
		return ErrPermissionDenied
	}
	if !ret.Ok {
		return ErrPermissionDenied
	}
	return nil
}

func (h *tty) CheckEnableShare() error {
	termConf, err := h.ws.apiClient.GetTerminalConfig()
	if err != nil {
		logger.Errorf("Get terminal config failed: %s", err)
		return err
	}
	if !termConf.EnableSessionShare {
		return ErrDisableShare
	}
	return nil
}

/*
	1. ask join room id (session id)
	2. room receive msg send to client
	3. client emit msg to room
*/

func (h *tty) JoinRoom(c *Client, roomID string) {
	user := h.ws.user
	writable := h.shareInfo.Record.Writeable()
	meta := exchange.MetaMessage{
		UserId:     user.ID,
		User:       user.String(),
		Created:    common.NewNowUTCTime().String(),
		RemoteAddr: c.RemoteAddr(),
		TerminalId: h.ws.Uuid,
		Primary:    false,
		Writable:   writable,
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
		logObj := model.SessionLifecycleLog{User: h.ws.user.String()}
		h.ws.RecordLifecycleLog(roomID, model.UserJoinSession, logObj)
		for {
			buf := make([]byte, 1024)
			nr, err := c.Read(buf)
			if nr > 0 && writable {
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
		h.ws.RecordLifecycleLog(roomID, model.UserLeaveSession, logObj)
		logger.Infof("Conn[%s] user read end", c.ID())
		if err := h.ws.apiClient.FinishShareRoom(h.shareInfo.Record.ID); err != nil {
			logger.Infof("Conn[%s] finish share room err: %s", c.ID(), err)
		}
	}
}

func (h *tty) Monitor(c *Client, roomID string) {
	if room := exchange.GetRoom(roomID); room != nil {
		conn := exchange.WrapperUserCon(c)
		room.Subscribe(conn)
		defer room.UnSubscribe(conn)
		logObj := model.SessionLifecycleLog{User: h.ws.user.String()}
		h.ws.RecordLifecycleLog(roomID, model.AdminJoinMonitor, logObj)
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
		h.ws.RecordLifecycleLog(roomID, model.AdminExitMonitor, logObj)
	}
}
