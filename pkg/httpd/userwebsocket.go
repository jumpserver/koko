package httpd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/srvconn"

	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/websocket"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/httpd/ws"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/sshcert"
)

type Handler interface {
	Name() string
	CheckValidation() error
	HandleMessage(*Message)
	CleanUp()
}

type UserWebsocket struct {
	Uuid           string
	conn           *ws.Socket
	ctx            *gin.Context
	messageChannel chan *Message

	user    *model.User
	setting *model.PublicSetting
	handler Handler

	wsParams *WsRequestParams

	ConnectToken *model.ConnectToken
	apiClient    *service.JMService
	k8sClient    *proxy.KubernetesClient
	langCode     string

	envelopeProtocol bool
	done             <-chan struct{}
}

func (userCon *UserWebsocket) initial() error {
	var wsParams WsRequestParams
	if err := userCon.ctx.ShouldBind(&wsParams); err != nil {
		logger.Errorf("Ws miss required ws params (token or target) err: %s", err)
		errMsg := "Miss required ws params (token or target)"
		userCon.SendErrMessage(errMsg)
		return err
	}
	userCon.wsParams = &wsParams
	token := userCon.wsParams.Token
	if token != "" {
		connectToken, err := sshcert.GetConnectTokenInfo(userCon.apiClient, token, true)
		if err != nil {
			logger.Error("Get connect token info failed")
			errMsg := "Token invalid"
			if connectToken.Detail != "" {
				errMsg = connectToken.Detail
			}
			userCon.SendErrMessage(errMsg)
			return errors.New("connect token lookup failed")
		}

		if userCon.user.ID != connectToken.User.ID {
			connectToken.ClearSSHCertificateCredential()
			logger.Errorf("No valid auth user found: %s vs %s",
				userCon.user.String(), connectToken.User.String())
			errMsg := "no valid auth user found"
			return errors.New(errMsg)
		}
		userCon.ConnectToken = &connectToken
	}
	return nil
}

func (userCon *UserWebsocket) Run() {
	ctx, cancel := context.WithCancel(userCon.conn.Request().Context())
	userCon.done = ctx.Done()
	closeReason := "initialization_failed"
	validated := false
	defer func() {
		if userCon.ConnectToken != nil {
			defer userCon.ConnectToken.ClearSSHCertificateCredential()
		}
		// Stop producers and close the transport before waiting for SSH cleanup.
		cancel()
		if closeReason != "" {
			logger.Infof("Ws[%s] close source=koko reason=%s", userCon.Uuid, closeReason)
			// A control frame also works when the terminal writer has stopped.
			if err := userCon.conn.WriteCloseReason(4000, "koko:"+closeReason, 5*time.Second); err != nil {
				logger.Infof("Ws[%s] close notification failed: %s", userCon.Uuid, err)
			}
		}
		_ = userCon.conn.Close()
		if validated {
			userCon.handler.CleanUp()
		}
		if userCon.k8sClient != nil {
			userCon.k8sClient.Close()
		}
		logger.Infof("Ws[%s] done", userCon.Uuid)
	}()
	lang := i18n.NewLang(userCon.langCode)
	if userCon.handler == nil {
		return
	}
	if err := userCon.initial(); err != nil {
		logger.Errorf("Ws[%s] initial err: %s", userCon.Uuid, err)
		return
	}
	type loopResult struct {
		operation string
		err       error
	}
	errorsChan := make(chan loopResult, 2)
	go func() {
		errorsChan <- loopResult{"write", userCon.writeMessageLoop(ctx)}
	}()
	go func() {
		errorsChan <- loopResult{"read", userCon.readMessageLoop()}
	}()
	if err := userCon.handler.CheckValidation(); err != nil {
		logger.Errorf("Ws[%s] check validation err: %s", userCon.Uuid, err)
		userCon.SendErrMessage(err.Error())
		return
	}
	validated = true

	if userCon.ConnectToken != nil && userCon.ConnectToken.Protocol == srvconn.ProtocolK8s {
		var err error
		namespaceValue := ""
		if k8sSettings, ok := userCon.ConnectToken.Platform.GetProtocolSetting(srvconn.ProtocolK8s); ok {
			if v, ok := k8sSettings.Setting["namespace"]; ok {
				if s, ok := v.(string); ok {
					namespaceValue = s
				}
			}
		}
		userCon.k8sClient, err = proxy.NewKubernetesClient(
			userCon.ConnectToken.Asset.Address,
			namespaceValue,
			userCon.ConnectToken.Account.Secret,
			userCon.ConnectToken.Gateway,
		)
		if err != nil {
			logger.Errorf("Ws[%s] create k8s client err: %s", userCon.Uuid, err)
			userCon.SendErrMessage(fmt.Sprintf(lang.T("Create k8s client err: %s"), err))
			return
		}
	}

	userCon.sendConnectMessage()
	closeReason = "request_canceled"
	select {
	case result := <-errorsChan:
		if result.err != nil || ctx.Err() == nil {
			closeReason = websocketErrorReason(result.operation, result.err)
		}
		source := "peer_or_transport"
		if closeReason != "" {
			source = "koko"
		}
		logger.Infof("Ws[%s] %s loop ended source=%s reason=%s err=%v",
			userCon.Uuid, result.operation, source, closeReason, result.err)
	case <-ctx.Done():
	}
}

func websocketErrorReason(operation string, err error) string {
	var closeErr *gorilla.CloseError
	if err == nil || errors.As(err, &closeErr) || errors.Is(err, io.EOF) {
		// A peer close or missing close frame is not evidence of a Koko decision.
		return ""
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return operation + "_timeout"
	}
	if operation == "write" {
		return "write_failed"
	}
	return ""
}

func (userCon *UserWebsocket) writeMessageLoop(ctx context.Context) error {
	t := time.NewTicker(25 * time.Second)
	defer t.Stop()
	for {
		var msg *Message
		select {
		case <-ctx.Done():
			logger.Infof("Ws[%s] end send message", userCon.Uuid)
			return nil
		case <-t.C:
			msg = &Message{Id: userCon.Uuid, Type: PING}
		case msg = <-userCon.messageChannel:
		}
		var err error
		switch {
		case userCon.envelopeProtocol:
			var payload []byte
			payload, err = encodeMessageEnvelope(msg)
			if err != nil {
				logger.Errorf("Ws[%s] encode %s envelope err: %s", userCon.Uuid, msg.Type, err)
				continue
			}
			err = userCon.conn.WriteBinary(payload, maxWriteTimeOut)
		case msg.Type == TerminalBinary:
			err = userCon.conn.WriteBinary(msg.Raw, maxWriteTimeOut)
		default:
			p, _ := json.Marshal(msg)
			err = userCon.conn.WriteText(p, maxWriteTimeOut)
		}
		if err != nil {
			logger.Errorf("Ws[%s] send %s message err: %s", userCon.Uuid, msg.Type, err)
			return err
		}
		if msg.Type == CLOSE {
			logger.Infof("Ws[%s] terminal[%d] sent CLOSE reason=%s", userCon.Uuid, msg.TerminalId, msg.Data)
		}
	}
}

func (userCon *UserWebsocket) SendMessage(msg *Message) {
	select {
	case <-userCon.done:
		return
	default:
	}
	select {
	case userCon.messageChannel <- msg:
	case <-userCon.done:
	case <-userCon.conn.Request().Context().Done():
		logger.Infof("Ws[%s] ctx done and ignore message type %s",
			userCon.Uuid, msg.Type)
	}
}

type websocketConnectInfo struct {
	User            *model.User            `json:"user"`
	Setting         *model.PublicSetting   `json:"setting"`
	Asset           *model.Asset           `json:"asset,omitempty"`
	Permission      *model.Permission      `json:"permission,omitempty"`
	ClipboardPolicy *model.ClipboardPolicy `json:"clipboard_policy,omitempty"`
	Capabilities    map[string]any         `json:"capabilities,omitempty"`
}

type websocketCapabilitiesProvider interface {
	WebsocketCapabilities() map[string]any
}

func (userCon *UserWebsocket) buildConnectInfo() websocketConnectInfo {
	var connectInfo websocketConnectInfo
	connectInfo.User = userCon.user
	connectInfo.Setting = userCon.setting

	if userCon.ConnectToken != nil {
		connectInfo.Asset = &userCon.ConnectToken.Asset
		permission := userCon.ConnectToken.Actions.Permission()
		connectInfo.Permission = &permission
		connectInfo.ClipboardPolicy = userCon.ConnectToken.ClipboardPolicy
	} else {
		connectInfo.Asset = nil
	}
	if provider, ok := userCon.handler.(websocketCapabilitiesProvider); ok {
		connectInfo.Capabilities = provider.WebsocketCapabilities()
	}
	return connectInfo
}

func (userCon *UserWebsocket) sendConnectMessage() {
	connectInfo := userCon.buildConnectInfo()
	info, _ := json.Marshal(connectInfo)
	msg := Message{
		Id:   userCon.Uuid,
		Type: CONNECT,
		Data: string(info),
	}
	userCon.SendMessage(&msg)
}

func (userCon *UserWebsocket) readMessageLoop() error {
	for {
		p, opCode, err := userCon.conn.ReadData(maxReadTimeout)
		if err != nil {
			return err
		}
		if userCon.envelopeProtocol {
			if opCode == gorilla.CloseMessage {
				return nil
			}
			if opCode != gorilla.BinaryMessage {
				logger.Errorf("Ws[%s] receive non-binary terminal envelope", userCon.Uuid)
				continue
			}
			frame, parseErr := parseEnvelope(p)
			if parseErr != nil {
				logger.Errorf("Ws[%s] parse terminal envelope err: %s", userCon.Uuid, parseErr)
				userCon.SendMessage(&Message{Type: TerminalError, Err: parseErr.Error()})
				continue
			}
			msg, decodeErr := decodeEnvelopeMessage(frame)
			if decodeErr != nil {
				logger.Errorf("Ws[%s] decode terminal envelope err: %s", userCon.Uuid, decodeErr)
				userCon.SendMessage(&Message{Type: TerminalError, Err: decodeErr.Error()})
				continue
			}
			switch msg.Type {
			case PING:
				userCon.SendMessage(&Message{Id: userCon.Uuid, Type: PONG, Data: msg.Data})
			case PONG:
			case TerminalK8STree:
				if userCon.k8sClient == nil {
					userCon.SendMessage(&Message{
						Type: TerminalError, TerminalId: msg.TerminalId,
						Err: "kubernetes client is unavailable",
					})
					continue
				}
				data, treeErr := userCon.k8sClient.GetTreeData()
				responseMsg := Message{
					Id: userCon.Uuid, Type: TerminalK8STree, Data: data,
					TerminalId: msg.TerminalId, KubernetesId: msg.KubernetesId,
				}
				if treeErr != nil {
					responseMsg.Err = treeErr.Error()
				}
				userCon.SendMessage(&responseMsg)
			default:
				userCon.handler.HandleMessage(msg)
			}
			continue
		}
		var msg Message
		switch opCode {
		case gorilla.BinaryMessage:
			msg.Raw = p
			msg.Type = TerminalBinary
			userCon.handler.HandleMessage(&msg)
			continue
		case gorilla.CloseMessage:
			logger.Errorf("Ws[%s] receive close opcode %d", userCon.Uuid, opCode)
			return nil
		case gorilla.TextMessage:
		default:
			logger.Errorf("Ws[%s] receive unsupported ws msg type %d", userCon.Uuid, opCode)
			continue
		}
		err = json.Unmarshal(p, &msg)
		if err != nil {
			logger.Errorf("Ws[%s] message data unmarshal err: %s", userCon.Uuid, p)
			continue
		}
		switch msg.Type {
		case PING:
			userCon.SendMessage(&Message{Id: userCon.Uuid, Type: PONG, Data: msg.Data})
			continue
		case PONG:
			logger.Debugf("Ws[%s] receive %s message", userCon.Uuid, msg.Type)
			continue
		case TerminalK8STree:
			data, err := userCon.k8sClient.GetTreeData()
			responseMsg := Message{
				Id:           userCon.Uuid,
				Type:         TerminalK8STree,
				Data:         data,
				KubernetesId: msg.KubernetesId,
			}
			if err != nil {
				responseMsg.Err = err.Error()
			}

			userCon.SendMessage(&responseMsg)
			continue
		default:
			userCon.handler.HandleMessage(&msg)
		}
	}
}

func (userCon *UserWebsocket) GetHandler() Handler {
	return userCon.handler
}

func (userCon *UserWebsocket) ClientIP() string {
	return userCon.ctx.ClientIP()
}

func (userCon *UserWebsocket) CurrentUser() *model.User {
	return userCon.user
}

func (userCon *UserWebsocket) SendErrMessage(errMsg string) {
	msg := Message{Id: userCon.Uuid, Type: ERROR, Err: errMsg}
	if userCon.envelopeProtocol {
		data, err := encodeMessageEnvelope(&msg)
		if err == nil {
			err = userCon.conn.WriteBinary(data, maxWriteTimeOut)
		}
		if err != nil {
			logger.Errorf("Ws[%s] send error envelope err: %s", userCon.Uuid, err)
		}
		return
	}
	data, _ := json.Marshal(msg)
	if err := userCon.conn.WriteText(data, maxWriteTimeOut); err != nil {
		logger.Errorf("Ws[%s] send error message err: %s", userCon.Uuid, err)
	}
}

var (
	ErrAssetIdInvalid   = errors.New("asset id invalid")
	ErrDisableShare     = errors.New("disable share")
	ErrPermissionDenied = errors.New("permission denied")
)

func (userCon *UserWebsocket) RecordLifecycleLog(sid string, event model.LifecycleEvent,
	logObj model.SessionLifecycleLog) {
	if err := userCon.apiClient.RecordSessionLifecycleLog(sid, event, logObj); err != nil {
		logger.Errorf("Record session lifecycle log err: %s", err)
	}
}
