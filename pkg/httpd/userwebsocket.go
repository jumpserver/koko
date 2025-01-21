package httpd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/srvconn"

	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/websocket"

	"github.com/jumpserver/koko/pkg/httpd/ws"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
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
	langCode     string
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
		connectToken, err := userCon.apiClient.GetConnectTokenInfo(token)
		if err != nil {
			logger.Errorf("Get connect token info %s error: %s", token, err)
			errMsg := "Token invalid"
			if connectToken.Detail != "" {
				errMsg = connectToken.Detail
			}
			userCon.SendErrMessage(errMsg)
			return err
		}
		userCon.ConnectToken = &connectToken
	}
	return nil
}

func (userCon *UserWebsocket) Run() {
	if userCon.handler == nil {
		return
	}
	if err := userCon.initial(); err != nil {
		logger.Errorf("Ws[%s] initial err: %s", userCon.Uuid, err)
		return
	}
	ctx, cancel := context.WithCancel(userCon.ctx.Request.Context())
	defer cancel()
	errorsChan := make(chan error, 1)
	go userCon.writeMessageLoop(ctx)
	go func() {
		if err := userCon.readMessageLoop(); err != io.EOF {
			logger.Errorf("Ws[%s] read message err: %s", userCon.Uuid, err)
			errorsChan <- err
		}
		logger.Infof("Ws[%s] read message done", userCon.Uuid)
	}()
	if err := userCon.handler.CheckValidation(); err != nil {
		logger.Errorf("Ws[%s] check validation err: %s", userCon.Uuid, err)
		userCon.SendErrMessage(err.Error())
		return
	}
	userCon.sendConnectMessage()
	var errMsg string
	select {
	case err := <-errorsChan:
		if err != nil {
			errMsg = err.Error()
		}
	case <-ctx.Done():
	}
	userCon.handler.CleanUp()
	if userCon.k8sClient != nil {
		userCon.k8sClient.Close()
	}

	logger.Infof("Ws[%s] done with exit %s", userCon.Uuid, errMsg)
}

func (userCon *UserWebsocket) writeMessageLoop(ctx context.Context) {
	active := time.Now()
	t := time.NewTicker(time.Minute)
	maxErrCount := 10
	errCount := 0
	defer t.Stop()
	for {
		if errCount >= maxErrCount {
			logger.Errorf("Ws[%s] send message err count more than %d and exit goroutine",
				userCon.Uuid, maxErrCount)
			return
		}
		var msg *Message
		select {
		case <-ctx.Done():
			logger.Infof("Ws[%s] end send message", userCon.Uuid)
			return
		case tickNow := <-t.C:
			if tickNow.Before(active.Add(time.Second * 30)) {
				continue
			}
			if tickNow.After(active.Add(maxWriteTimeOut)) {
				logger.Infof("Ws[%s] inactive more than 5 minutes and close conn", userCon.Uuid)
				_ = userCon.conn.Close()
				continue
			}
			msg = &Message{Id: userCon.Uuid, Type: PING}
		case msg = <-userCon.messageChannel:

		}
		switch msg.Type {
		case TerminalBinary:
			err := userCon.conn.WriteBinary(msg.Raw, maxWriteTimeOut)
			if err != nil {
				logger.Errorf("Ws[%s] send %s message err: %s", userCon.Uuid, msg.Type, err)
				errCount++
				continue
			}
		default:
			p, _ := json.Marshal(msg)
			err := userCon.conn.WriteText(p, maxWriteTimeOut)
			if err != nil {
				logger.Errorf("Ws[%s] send %s message err: %s", userCon.Uuid, msg.Type, err)
				errCount++
				continue
			}
		}
		errCount = 0
		active = time.Now()
	}
}

func (userCon *UserWebsocket) SendMessage(msg *Message) {
	select {
	case userCon.messageChannel <- msg:
	case <-userCon.conn.Request().Context().Done():
		logger.Infof("Ws[%s] ctx done and ignore message type %s",
			userCon.Uuid, msg.Type)
	}
}

func (userCon *UserWebsocket) sendConnectMessage() {
	var connectInfo struct {
		User    *model.User          `json:"user"`
		Setting *model.PublicSetting `json:"setting"`
	}
	connectInfo.User = userCon.user
	connectInfo.Setting = userCon.setting
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
		case PING, PONG:
			logger.Debugf("Ws[%s] receive %s message", userCon.Uuid, msg.Type)
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
