package httpd

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver/koko/pkg/httpd/ws"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
)

type elfinderCon struct {
	Uuid           string
	ctx            *gin.Context
	webSrv         *server
	conn           *ws.Socket
	user           *model.User
	messageChannel chan *Message

	done chan struct{}

	targetId string

	volume *UserVolume
}

func (ec *elfinderCon) Run() {
	ctx, cancel := context.WithCancel(ec.ctx.Request.Context())
	defer cancel()
	errorsChan := make(chan error, 1)
	go ec.writeMessageLoop(ctx)
	go func() {
		errorsChan <- ec.readMessageLoop()
	}()

	switch strings.TrimSpace(ec.targetId) {
	case "_":
		ec.volume = NewUserVolume(ec.user, ec.ctx.ClientIP(), "")
	default:
		ec.volume = NewUserVolume(ec.user, ec.ctx.ClientIP(), strings.TrimSpace(ec.targetId))
	}
	ec.sendConnectMessage()
	var errMsg string
	select {
	case err := <-errorsChan:
		if err != nil {
			errMsg = err.Error()
		}
	case <-ctx.Done():
	}
	close(ec.done)
	ec.volume.Close()
	logger.Infof("Ws[%s] eflidner done with exit %s", ec.Uuid, errMsg)
}
func (ec *elfinderCon) GetVolume() *UserVolume {
	select {
	case <-ec.done:
		return nil
	default:
		return ec.volume
	}
}

func (ec *elfinderCon) sendConnectMessage() {
	msg := Message{
		Id:   ec.Uuid,
		Type: CONNECT,
	}
	ec.SendMessage(&msg)
}

func (ec *elfinderCon) SendMessage(msg *Message) {
	ec.messageChannel <- msg
}

func (ec *elfinderCon) writeMessageLoop(ctx context.Context) {
	active := time.Now()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		var msg *Message
		select {
		case <-ctx.Done():
			logger.Infof("Ws[%s] end send message", ec.Uuid)
			return
		case tickNow := <-t.C:
			if tickNow.Before(active.Add(time.Second * 30)) {
				continue
			}
			if tickNow.After(active.Add(maxWriteTimeOut)) {
				logger.Infof("Ws[%s] inactive more than 5 minutes and close conn", ec.Uuid)
				_ = ec.conn.Close()
				continue
			}
			msg = &Message{
				Id:   ec.Uuid,
				Type: PING,
			}
		case msg = <-ec.messageChannel:

		}
		p, _ := json.Marshal(msg)
		err := ec.conn.WriteText(p, maxWriteTimeOut)
		if err != nil {
			logger.Errorf("Ws[%s] send %s message err: %s", ec.Uuid, msg.Type, err)
			continue
		}
		active = time.Now()
	}
}

func (ec *elfinderCon) readMessageLoop() error {
	for {
		p, _, err := ec.conn.ReadData(maxReadTimeout)
		if err != nil {
			logger.Errorf("Ws[%s] read data err: %s", ec.Uuid, err)
			return err
		}
		var msg Message
		err = json.Unmarshal(p, &msg)
		if err != nil {
			logger.Errorf("Ws[%s] elfinder message data unmarshal err: %s", ec.Uuid, p)
			continue
		}
		switch msg.Type {
		case PING, PONG:
			logger.Debugf("Ws[%s] elfinder receive %s message", ec.Uuid, msg.Type)
		default:
			logger.Errorf("Ws[%s] elfinder receive unknown type %s message", ec.Uuid, msg.Type)
		}
	}
}
