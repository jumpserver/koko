package httpd

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/srvconn"

	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/websocket"

	"github.com/jumpserver/koko/pkg/httpd/ws"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
)

type Handler interface {
	Name() string
	CheckValidation() bool
	HandleMessage(*Message)
	CleanUp()
}

type UserWebsocket struct {
	Uuid           string
	webSrv         *Server
	conn           *ws.Socket
	ctx            *gin.Context
	messageChannel chan *Message

	user    *model.User
	setting *model.PublicSetting
	handler Handler
}

func (userCon *UserWebsocket) Run() {
	if userCon.handler == nil {
		return
	}
	ctx, cancel := context.WithCancel(userCon.ctx.Request.Context())
	defer cancel()
	errorsChan := make(chan error, 1)
	go userCon.writeMessageLoop(ctx)
	go func() {
		select {
		case errorsChan <- userCon.readMessageLoop():
		case <-ctx.Done():
		}
	}()
	if !userCon.handler.CheckValidation() {
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
	defer t.Stop()
	for {
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
			msg = &Message{
				Id:   userCon.Uuid,
				Type: PING,
			}
		case msg = <-userCon.messageChannel:

		}
		switch msg.Type {
		case TERMINALBINARY:
			err := userCon.conn.WriteBinary(msg.Raw, maxWriteTimeOut)
			if err != nil {
				logger.Errorf("Ws[%s] send %s message err: %s", userCon.Uuid, msg.Type, err)
				continue
			}
		default:
			p, _ := json.Marshal(msg)
			err := userCon.conn.WriteText(p, maxWriteTimeOut)
			if err != nil {
				logger.Errorf("Ws[%s] send %s message err: %s", userCon.Uuid, msg.Type, err)
				continue
			}
		}
		active = time.Now()
	}
}

func (userCon *UserWebsocket) SendMessage(msg *Message) {
	userCon.messageChannel <- msg
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
			logger.Errorf("Ws[%s] read data err: %s", userCon.Uuid, err)
			return err
		}
		var msg Message
		switch opCode {
		case gorilla.BinaryMessage:
			msg.Raw = p
			msg.Type = TERMINALBINARY
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
