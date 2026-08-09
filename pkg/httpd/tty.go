package httpd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/srvconn"

	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/terminalai"
)

var _ Handler = (*tty)(nil)

const maxTerminalsPerWebsocket = 64

type tty struct {
	ws *UserWebsocket

	initialed bool
	wg        sync.WaitGroup

	backendClient *Client

	shareInfo *ShareInfo

	K8sClients map[string]*Client
	clients    map[uint32]*Client
	nextID     uint32
	clientsMu  sync.RWMutex
}

func (h *tty) Name() string {
	return TTYName
}

func (h *tty) CleanUp() {
	h.clientsMu.Lock()
	clients := make([]*Client, 0, len(h.clients))
	for _, client := range h.clients {
		clients = append(clients, client)
	}
	h.clients = nil
	h.K8sClients = nil
	h.backendClient = nil
	h.clientsMu.Unlock()
	for _, client := range clients {
		_ = client.Close()
	}
	h.wg.Wait()
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
	case TerminalCreate:
		h.handleTerminalCreate(msg)
		return
	case ChatMessage:
		h.handleChatMessage(msg)
		return
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
		h.handleTerminalInit(connectInfo, "", "", "", "", h.allocateTerminalID(), "")
		return

	case TerminalK8SInit:
		if msg.Id != h.ws.Uuid {
			logger.Errorf("Ws[%s] terminal initial unknown message id %s", h.ws.Uuid, msg.Id)
			return
		}

		connectInfo, err := h.validateAndInitSession(msg)
		if err != nil {
			return
		}

		h.handleTerminalInit(
			connectInfo, msg.KubernetesId, msg.Namespace, msg.Pod, msg.Container,
			h.allocateTerminalID(), "",
		)
		return
	}

	if h.initialed || h.getClient(msg.TerminalId) != nil ||
		h.getK8sClient(msg.KubernetesId) != nil {
		h.handleTerminalMessage(msg)
	}
}

func (h *tty) handleChatMessage(msg *Message) {
	chatMessage, err := terminalai.DecodeChatMessage(msg.Data)
	if err != nil {
		h.ws.SendMessage(&Message{Type: TerminalError, Err: err.Error()})
		return
	}
	terminalID := chatTerminalID(chatMessage)
	client := h.getClient(terminalID)
	if terminalID == 0 || client == nil || client.Agent == nil {
		h.sendTerminalAIError(terminalID, "terminal AI is unavailable for this terminal")
		return
	}
	if err = client.Agent.Handle(chatMessage); err != nil {
		h.sendTerminalAIError(terminalID, err.Error())
	}
}

func (h *tty) sendTerminalAIError(terminalID uint32, message string) {
	chatMessage := terminalai.ChatMessage{
		ID: common.UUID(), Role: "assistant",
		Metadata: map[string]any{
			"terminalId": terminalID, "stage": "final",
		},
		Parts: []terminalai.ChatPart{{
			Type: "data-error", Data: map[string]any{"message": message},
		}},
	}
	data, err := json.Marshal(chatMessage)
	if err != nil {
		return
	}
	h.ws.SendMessage(&Message{
		Type: ChatMessage, TerminalId: terminalID, Data: string(data),
	})
}

func chatTerminalID(message terminalai.ChatMessage) uint32 {
	value, ok := message.Metadata["terminalId"]
	if !ok {
		return 0
	}
	switch terminalID := value.(type) {
	case float64:
		if terminalID > 0 && terminalID <= 4294967295 && math.Trunc(terminalID) == terminalID {
			return uint32(terminalID)
		}
	case uint32:
		return terminalID
	case int:
		if terminalID > 0 && uint64(terminalID) <= uint64(^uint32(0)) {
			return uint32(terminalID)
		}
	}
	return 0
}

func (h *tty) allocateTerminalID() uint32 {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()
	h.nextID++
	if h.nextID == 0 {
		h.nextID = 1
	}
	return h.nextID
}

func (h *tty) getClient(terminalID uint32) *Client {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	return h.clients[terminalID]
}

func (h *tty) getK8sClient(kubernetesID string) *Client {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	return h.K8sClients[kubernetesID]
}

func (h *tty) removeClient(terminalID uint32) *Client {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()
	client := h.clients[terminalID]
	if client == nil {
		return nil
	}
	delete(h.clients, terminalID)
	if client.KubernetesId != "" && h.K8sClients[client.KubernetesId] == client {
		delete(h.K8sClients, client.KubernetesId)
	}
	if h.backendClient == client {
		h.backendClient = nil
	}
	return client
}

func (h *tty) removeK8sClient(kubernetesID string) *Client {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()
	client := h.K8sClients[kubernetesID]
	if client == nil {
		return nil
	}
	delete(h.K8sClients, kubernetesID)
	delete(h.clients, client.TerminalId)
	return client
}

func (h *tty) handleTerminalCreate(msg *Message) {
	var request terminalCreateEnvelope
	if err := json.Unmarshal([]byte(msg.Data), &request); err != nil {
		h.ws.SendMessage(&Message{
			Type: TerminalError, RequestId: msg.RequestId, Err: err.Error(),
		})
		return
	}
	h.clientsMu.RLock()
	activeTerminals := len(h.clients)
	h.clientsMu.RUnlock()
	if activeTerminals >= maxTerminalsPerWebsocket {
		h.ws.SendMessage(&Message{
			Type: TerminalError, RequestId: request.RequestID,
			Err: "terminal limit reached for this websocket",
		})
		return
	}
	terminalID := h.allocateTerminalID()
	kubernetes := request.Params.Kubernetes
	isKubernetesConnection := h.ws.ConnectToken != nil &&
		h.ws.ConnectToken.Protocol == srvconn.ProtocolK8s
	if (kubernetes.ID != "") != isKubernetesConnection {
		h.ws.SendMessage(&Message{
			Type: TerminalError, RequestId: request.RequestID,
			Err: "terminal type does not match the connection protocol",
		})
		return
	}
	if kubernetes.ID == "" {
		if h.initialed &&
			(h.ws.wsParams.TargetType == TargetTypeMonitor ||
				h.ws.wsParams.TargetType == TargetTypeShare) {
			h.ws.SendMessage(&Message{
				Type: TerminalError, RequestId: request.RequestID,
				Err: "this session type supports only one terminal",
			})
			return
		}
		h.initialed = true
	}
	connectInfo := TerminalConnectData{
		Rows: request.Params.Rows, Cols: request.Params.Cols, Code: request.Params.Code,
	}
	if h.ws.wsParams.TargetType == TargetTypeShare {
		data, _ := json.Marshal(connectInfo)
		validated, err := h.validateAndInitSession(&Message{
			Data: string(data), TerminalId: terminalID,
		})
		if err != nil {
			return
		}
		connectInfo = validated
	}
	h.handleTerminalInit(
		connectInfo, kubernetes.ID, kubernetes.Namespace, kubernetes.Pod,
		kubernetes.Container, terminalID, request.RequestID,
	)
}

func (h *tty) sendCloseMessage(terminalID uint32) {
	closedMsg := Message{
		Id: h.ws.Uuid, Type: CLOSE, TerminalId: terminalID,
	}
	h.ws.SendMessage(&closedMsg)
}

func (h *tty) sendK8SCloseMessage(client *Client) {
	if client == nil {
		return
	}
	closedMsg := Message{
		Id:           h.ws.Uuid,
		Type:         K8SClose,
		TerminalId:   client.TerminalId,
		KubernetesId: client.KubernetesId,
	}
	h.ws.SendMessage(&closedMsg)
}

func (h *tty) sendSessionMessage(data string, KubernetesId string, terminalID uint32) {
	msg := Message{
		Id:           h.ws.Uuid,
		Type:         TerminalSession,
		Data:         data,
		TerminalId:   terminalID,
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
			h.sendCloseMessage(msg.TerminalId)
			return connectInfo, err2
		}
		h.shareInfo = &info
		sessionDetail, err3 := h.ws.apiClient.GetSessionById(info.Record.Session.ID)
		if err3 != nil {
			logger.Errorf("Ws[%s] terminal get session %s err: %s",
				h.ws.Uuid, info.Record.Session.ID, err3)
			h.sendCloseMessage(msg.TerminalId)
			return connectInfo, err3
		}
		sessionInfo := proxy.SessionInfo{
			Session: &sessionDetail,
		}
		data, _ := json.Marshal(sessionInfo)
		h.sendSessionMessage(string(data), msg.KubernetesId, msg.TerminalId)
	}
	return connectInfo, nil
}

func (h *tty) handleTerminalInit(
	connectInfo TerminalConnectData,
	KubernetesId, namespace, pod, container string,
	terminalID uint32, requestID string,
) {
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
		Pod: pod, Container: container, TerminalId: terminalID,
	}
	h.initializeTerminalAI(client, connectInfo)
	h.clientsMu.Lock()
	if h.clients == nil {
		h.clients = make(map[uint32]*Client)
	}
	h.clients[terminalID] = client
	if KubernetesId != "" {
		if h.K8sClients == nil {
			h.K8sClients = make(map[string]*Client)
		}
		h.K8sClients[KubernetesId] = client
	} else {
		h.backendClient = client
	}
	h.clientsMu.Unlock()
	if requestID != "" {
		created, _ := json.Marshal(map[string]any{
			"success": true, "requestId": requestID,
		})
		h.ws.SendMessage(&Message{
			Type: "created", TerminalId: terminalID,
			RequestId: requestID, Data: string(created),
		})
	}

	h.wg.Add(1)
	go h.proxy(&h.wg, client)
}

func (h *tty) initializeTerminalAI(
	client *Client,
	connectInfo TerminalConnectData,
) {
	if client == nil || client.KubernetesId != "" ||
		h.ws.ConnectToken == nil ||
		h.ws.ConnectToken.Protocol == srvconn.ProtocolK8s ||
		h.ws.wsParams.TargetType == TargetTypeMonitor ||
		h.ws.wsParams.TargetType == TargetTypeShare {
		return
	}
	termConfig, err := h.ws.apiClient.GetTerminalConfig()
	if err != nil {
		logger.Errorf("Get terminal AI config failed: %s", err)
		return
	}
	connectToken := h.ws.ConnectToken
	session, err := terminalai.NewSession(terminalai.SessionOptions{
		TerminalID:        client.TerminalId,
		UserID:            connectToken.User.ID,
		Width:             connectInfo.Cols,
		Height:            connectInfo.Rows,
		Config:            terminalai.NewConfig(termConfig),
		Context:           terminalai.NewSessionContext(connectToken),
		Language:          h.ws.langCode,
		WritePTY:          client.WriteAgentData,
		SetInputLocked:    client.SetInputLocked,
		RequireCommandACL: true,
		Emit: func(message terminalai.ChatMessage) {
			data, marshalErr := json.Marshal(message)
			if marshalErr != nil {
				return
			}
			h.ws.SendMessage(&Message{
				Type: ChatMessage, TerminalId: client.TerminalId, Data: string(data),
			})
		},
	})
	if err != nil {
		logger.Infof("Terminal AI disabled for terminal %d: %s", client.TerminalId, err)
		return
	}
	providerInfo := session.ProviderInfo()
	logger.Infof(
		"Terminal AI feature %s provider %s model %s initialized for terminal %d",
		terminalai.FeatureName(),
		providerInfo.Name,
		providerInfo.Model,
		client.TerminalId,
	)
	client.Agent = session
	client.Agent.AnnounceCapability()
}

func (h *tty) handleTerminalMessage(msg *Message) {
	switch msg.Type {
	case TerminalData, TerminalBinary:
		data := getDataBytes(msg)
		if client := h.getClient(msg.TerminalId); client != nil {
			client.WriteData(data)
		}
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
		go h.createShareSession(msg.TerminalId, &shareData)
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
		go h.getShareUserInfo(msg.TerminalId, query)
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
		go h.syncUserPreference(msg.TerminalId, &preference)
		return
	case CLOSE:
		if client := h.removeClient(msg.TerminalId); client != nil {
			_ = client.Close()
		}
	case K8SClose:
		if k8sClient := h.removeK8sClient(msg.KubernetesId); k8sClient != nil {
			_ = k8sClient.Close()
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
	if k8sClient := h.getK8sClient(msg.KubernetesId); k8sClient != nil {
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
		if client := h.getClient(msg.TerminalId); client != nil {
			client.SetWinSize(ssh.Window{Width: size.Cols, Height: size.Rows})
		}
	} else if msg.Type == TerminalK8SResize {
		if k8sClient := h.getK8sClient(msg.KubernetesId); k8sClient != nil {
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

func (h *tty) syncUserPreference(terminalID uint32, preference *UserKoKoPreferenceParam) {
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
		Id: h.ws.Uuid, Type: MessageNotify, Data: string(msgNotify),
		Err: errMsg, TerminalId: terminalID,
	})

}

func (h *tty) createShareSession(terminalID uint32, shareData *ShareRequestParams) {
	// 创建 共享连接
	res, err := h.handleShareRequest(shareData)
	if err != nil {
		logger.Errorf("Ws[%s] handle share request err: %s", h.ws.Uuid, err)
	}
	data, _ := json.Marshal(res)
	h.ws.SendMessage(&Message{
		Id: h.ws.Uuid, Type: TerminalShare, Data: string(data),
		TerminalId: terminalID,
	})
}

func (h *tty) getShareUserInfo(terminalID uint32, query GetUserParams) {
	client := h.getClient(terminalID)
	if client == nil {
		logger.Errorf("Ws[%s] get share User info without sessioninfo", h.ws.Uuid)
		return
	}
	sessionInfo := client.GetSessionInfo()
	if sessionInfo == nil || sessionInfo.Perms == nil {
		logger.Errorf("Ws[%s] get share User info without permissions", h.ws.Uuid)
		return
	}
	if !sessionInfo.Perms.EnableShare() {
		logger.Errorf("Ws[%s] get share User info without permissions", h.ws.Uuid)
		return
	}
	shareUserResp, err := h.ws.apiClient.GetSuggestionUsers(query.Query)
	if err != nil {
		logger.Error(err)
		return
	}
	data, _ := json.Marshal(shareUserResp)
	h.ws.SendMessage(&Message{
		Id: h.ws.Uuid, Type: TerminalGetShareUser, Data: string(data),
		TerminalId: terminalID,
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
			h.sendCloseMessage(client.TerminalId)
			return
		}
		agent := client.Agent
		sessionContext := terminalai.NewSessionContext(connectToken)
		if agent != nil {
			if !srv.SupportsBackgroundExecution() {
				agent.DisableBackground(
					"background execution is unavailable for this connection",
				)
			}
			agent.Bind(terminalai.SessionHooks{
				CommandACLCheck: func(command string) terminalai.CommandACLDecision {
					return terminalAIACLDecision(srv.MatchCommandACL(command))
				},
				CommandACLReview: func(
					ctx context.Context,
					decision terminalai.CommandACLDecision,
					command string,
				) (terminalai.CommandACLDecision, error) {
					reviewed, reviewErr := srv.ReviewCommand(
						ctx,
						proxy.CommandACLDecision{
							Action: model.CommandAction(decision.Action),
							ACLID:  decision.ACLID, ItemID: decision.ItemID,
							Name: decision.Name, Matched: decision.Matched,
							Reviewed: decision.Reviewed,
						},
						command,
					)
					return terminalAIACLDecision(reviewed), reviewErr
				},
				BackgroundRecord: func(
					command, output string,
					exitCode *int,
					decision *terminalai.CommandACLDecision,
				) {
					var aclDecision *proxy.CommandACLDecision
					if decision != nil {
						aclDecision = &proxy.CommandACLDecision{
							Action: model.CommandAction(decision.Action),
							ACLID:  decision.ACLID, ItemID: decision.ItemID,
							Name: decision.Name, Matched: decision.Matched,
							Reviewed: decision.Reviewed,
						}
					}
					srv.RecordBackgroundCommand(command, output, exitCode, aclDecision)
				},
				BackgroundGuard: srv.CheckBackgroundExecution,
				PTYAuthorizer: func(
					command string,
					decision *terminalai.CommandACLDecision,
				) {
					if decision == nil {
						return
					}
					srv.AuthorizeTerminalAICommand(command, &proxy.CommandACLDecision{
						Action: model.CommandAction(decision.Action),
						ACLID:  decision.ACLID, ItemID: decision.ItemID,
						Name: decision.Name, Matched: decision.Matched,
						Reviewed: decision.Reviewed,
					})
				},
			})
		}
		setBackgroundExecutor := func(connection terminalai.BackgroundConnection) {
			if agent == nil || !srv.SupportsBackgroundExecution() {
				return
			}
			ctx, cancel := context.WithTimeout(client.Context(), 30*time.Second)
			defer cancel()
			if initErr := agent.AttachBackground(ctx, connection); initErr != nil {
				logger.Errorf(
					"Terminal AI %s background init failed: %s",
					sessionContext.Protocol, initErr,
				)
			}
		}
		srv.OnSessionInfo = func(info *proxy.SessionInfo) {
			client.SetSessionInfo(info)
			data, _ := json.Marshal(info)
			h.sendSessionMessage(string(data), client.KubernetesId, client.TerminalId)
			if agent != nil && info.Session != nil {
				agent.SetSessionID(info.Session.ID)
			}
		}
		srv.OnSSHClient = func(sshClient *srvconn.SSHClient) {
			setBackgroundExecutor(terminalai.BackgroundConnection{SSHClient: sshClient})
		}
		srv.OnDatabaseConnection = func(info proxy.DatabaseConnectionInfo) {
			setBackgroundExecutor(terminalai.BackgroundConnection{
				Database: &terminalai.DatabaseConfig{
					Protocol: info.Protocol,
					Host:     info.Host, Port: info.Port, ServerName: info.ServerName,
					Username: info.Username, Password: info.Password,
					Database: info.Database, UseSSL: info.UseSSL,
					CACert: info.CACert, ClientCert: info.ClientCert,
					ClientKey: info.ClientKey, AllowInvalidCert: info.AllowInvalidCert,
					DataMaskingRules: info.DataMaskingRules,
				},
			})
		}
		srv.Proxy()
		srv.CloseBackgroundRecorder()
	}

	if params.TargetType == srvconn.ProtocolK8s {
		h.removeClient(client.TerminalId)
		h.sendK8SCloseMessage(client)
		return
	}
	h.removeClient(client.TerminalId)
	h.sendCloseMessage(client.TerminalId)
	logger.Info("Ws tty proxy end")
}

func terminalAIACLDecision(value proxy.CommandACLDecision) terminalai.CommandACLDecision {
	return terminalai.CommandACLDecision{
		Action: string(value.Action), ACLID: value.ACLID,
		ItemID: value.ItemID, Name: value.Name, Matched: value.Matched,
		DetailURL: value.DetailURL, Reviewers: value.Reviewers,
		Processor: value.Processor, Reviewed: value.Reviewed,
	}
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
