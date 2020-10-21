package httpd

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gliderlabs/ssh"

	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/httpd/ws"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/service"
)

type ttyCon struct {
	Uuid           string
	ctx            *gin.Context
	webSrv         *server
	conn           *ws.Socket
	user           *model.User
	messageChannel chan *Message

	wg sync.WaitGroup

	targetType   string
	targetId     string
	systemUserId string

	systemUser *model.SystemUser
	assetApp   *model.Asset
	k8sApp     *model.K8sCluster
	dbApp      *model.Database

	backendClient *Client
}

func (tc *ttyCon) Run() {
	ctx, cancel := context.WithCancel(tc.ctx.Request.Context())
	defer cancel()
	errorsChan := make(chan error, 1)
	go tc.writeMessageLoop(ctx)
	go func() {
		errorsChan <- tc.readMessageLoop()
	}()
	if !tc.checkTargetType() {
		logger.Errorf("Ws[%s] check target type failed: %s", tc.Uuid,
			tc.ctx.Request.URL.Query().Encode())
		return
	}
	tc.sendConnectMessage()
	var errMsg string
	select {
	case err := <-errorsChan:
		if err != nil {
			errMsg = err.Error()
		}
	case <-ctx.Done():

	}
	tc.cleanUp()
	logger.Infof("Ws[%s] done with exit %s", tc.Uuid, errMsg)
}

func (tc *ttyCon) cleanUp() {
	if tc.backendClient != nil {
		_ = tc.backendClient.Close()
	}
	tc.wg.Wait()
}

func (tc *ttyCon) sendConnectMessage() {
	msg := Message{
		Id:   tc.Uuid,
		Type: CONNECT,
	}
	tc.SendMessage(&msg)
}

func (tc *ttyCon) sendCloseMessage() {
	closedMsg := Message{
		Id:   tc.Uuid,
		Type: CLOSE,
	}
	tc.SendMessage(&closedMsg)
}

func (tc *ttyCon) SendMessage(msg *Message) {
	tc.messageChannel <- msg
}

func (tc *ttyCon) readMessageLoop() error {
	var terminalInitialed bool
	for {
		p, _, err := tc.conn.ReadData(maxReadTimeout)
		if err != nil {
			logger.Errorf("Ws[%s] read data err: %s", tc.Uuid, err)
			return err
		}
		var msg Message
		err = json.Unmarshal(p, &msg)
		if err != nil {
			logger.Errorf("Ws[%s] message data unmarshal err: %s", tc.Uuid, p)
			continue
		}
		switch msg.Type {
		case PING, PONG:
			logger.Debugf("Ws[%s] receive %s message", tc.Uuid, msg.Type)
			continue
		case TERMINALINIT:
			if msg.Id != tc.Uuid {
				logger.Errorf("Ws[%s] terminal initial unknown message id %s", tc.Uuid, msg.Id)
				continue
			}
			if terminalInitialed {
				logger.Errorf("Ws[%s] terminal has been already initialed", tc.Uuid)
				continue
			}

			var size WindowSize
			err := json.Unmarshal([]byte(msg.Data), &size)
			if err != nil {
				logger.Errorf("Ws[%s] terminal initial message data unmarshal err: %s",
					tc.Uuid, msg.Type, msg.Data)
				continue
			}
			terminalInitialed = true
			win := ssh.Window{
				Width:  size.Cols,
				Height: size.Rows,
			}
			userR, userW := io.Pipe()
			tc.backendClient = &Client{
				WinChan: make(chan ssh.Window, 100), Conn: tc,
				UserRead: userR, UserWrite: userW,
				pty: ssh.Pty{Term: "xterm", Window: win},
			}
			tc.wg.Add(1)
			go tc.proxy(&tc.wg)
			continue
		}
		if terminalInitialed {
			tc.handleTerminalMessage(&msg)
		}

	}
}

func (tc *ttyCon) writeMessageLoop(ctx context.Context) {
	active := time.Now()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		var msg *Message
		select {
		case <-ctx.Done():
			logger.Infof("Ws[%s] end send message", tc.Uuid)
			return
		case tickNow := <-t.C:
			if tickNow.Before(active.Add(time.Second * 30)) {
				continue
			}
			if tickNow.After(active.Add(maxWriteTimeOut)) {
				logger.Infof("Ws[%s] inactive more than 5 minutes and close conn", tc.Uuid)
				_ = tc.conn.Close()
				continue
			}
			msg = &Message{
				Id:   tc.Uuid,
				Type: PING,
			}
		case msg = <-tc.messageChannel:

		}
		p, _ := json.Marshal(msg)
		err := tc.conn.WriteText(p, maxWriteTimeOut)
		if err != nil {
			logger.Errorf("Ws[%s] send %s message err: %s", tc.Uuid, msg.Type, err)
			continue
		}
		active = time.Now()
	}
}

func (tc *ttyCon) handleTerminalMessage(msg *Message) {
	switch msg.Type {
	case TERMINALDATA:
		tc.backendClient.WriteData([]byte(msg.Data))
	case TERMINALRESIZE:
		var size WindowSize
		err := json.Unmarshal([]byte(msg.Data), &size)
		if err != nil {
			logger.Errorf("Ws[%s] message(%s) data unmarshal err: %s", tc.Uuid,
				msg.Type, msg.Data)
			return
		}
		tc.backendClient.SetWinSize(ssh.Window{
			Width:  size.Cols,
			Height: size.Rows,
		})
	case CLOSE:
		_ = tc.backendClient.Close()
	default:
		logger.Infof("Ws[%s] handle unknown message(%s) data %s", tc.Uuid,
			msg.Type, msg.Data)
	}
}

func (tc *ttyCon) checkTargetType() bool {
	var ok bool
	switch tc.targetType {
	case TargetTypeDB, TargetTypeK8s, TargetTypeAsset:
		if tc.systemUserId == "" || tc.targetId == "" {
			logger.Errorf("Ws[%s] miss required query params.", tc.Uuid)
			return false
		}
		systemUser := service.GetSystemUser(tc.systemUserId)
		if systemUser.ID == "" {
			return false
		}
		tc.systemUser = &systemUser
		ok = tc.getApp()
	case TargetTypeRoom:
		ok = true
	}
	logger.Infof("Ws[%s] check connect type %s: %t", tc.Uuid, tc.targetType, ok)
	return ok
}

func (tc *ttyCon) getApp() bool {
	switch tc.getAppType() {
	case AppTypeDB:
		databaseAsset := service.GetDatabase(tc.targetId)
		if databaseAsset.ID != "" {
			tc.dbApp = &databaseAsset
			return true
		}
	case AppTypeK8s:
		k8sCluster := service.GetK8sCluster(tc.targetId)
		if k8sCluster.ID != "" {
			tc.k8sApp = &k8sCluster
			return true
		}
	case AppTypeAsset:
		asset := service.GetAsset(tc.targetId)
		if asset.ID != "" {
			tc.assetApp = &asset
			return true
		}
	}
	return false
}

func (tc *ttyCon) getAppType() int {
	appType := AppUnknown
	switch tc.targetType {
	case TargetTypeDB:
		appType = AppTypeDB
	case TargetTypeK8s:
		appType = AppTypeK8s
	case TargetTypeAsset:
		appType = AppTypeAsset
	}
	return appType
}

func (tc *ttyCon) proxy(wg *sync.WaitGroup) {
	defer wg.Done()
	var proxySrv proxyServer
	switch tc.targetType {
	case TargetTypeDB, TargetTypeK8s, TargetTypeAsset:
		switch tc.getAppType() {
		case AppTypeDB:
			proxySrv = &proxy.DBProxyServer{
				UserConn:   tc.backendClient,
				User:       tc.user,
				Database:   tc.dbApp,
				SystemUser: tc.systemUser,
			}
		case AppTypeK8s:
			proxySrv = &proxy.K8sProxyServer{
				UserConn:   tc.backendClient,
				User:       tc.user,
				Cluster:    tc.k8sApp,
				SystemUser: tc.systemUser,
			}
		case AppTypeAsset:
			proxySrv = &proxy.ProxyServer{
				UserConn:   tc.backendClient,
				User:       tc.user,
				Asset:      tc.assetApp,
				SystemUser: tc.systemUser,
			}
		}
	case TargetTypeRoom:
		JoinRoom(tc.backendClient, tc.targetId)
	default:
		return
	}
	if proxySrv != nil {
		proxySrv.Proxy()
	}
	tc.sendCloseMessage()
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
	ex := exchange.GetExchange()
	roomChan := make(chan model.RoomMessage)
	room, err := ex.JoinRoom(roomChan, roomID)
	if err != nil {
		logger.Errorf("Conn[%s] join room %s err: %s", c.ID(), roomID, err)
		return
	}
	defer ex.LeaveRoom(room, roomID)
	user := c.Conn.user
	if !CheckShareRoomReadPerm(user.ID, roomID) {
		logger.Errorf("Conn[%s] has no pem to join room %s", c.ID(), roomID)
		return
	}
	go func() {
		for {
			msg, ok := <-roomChan
			if !ok {
				break
			}
			switch msg.Event {
			case model.DataEvent, model.MaxIdleEvent, model.AdminTerminateEvent:
				_, _ = c.Write(msg.Body)
				continue
			case model.WindowsEvent, model.PingEvent:
				continue
			case model.LogoutEvent, model.ExitEvent:
			default:
				logger.Errorf("Conn[%s] receive unknown room event %s", user.Name, roomID, msg.Event)

			}
			logger.Infof("Conn[%s] stop receive msg from room %s by %s", c.ID(), roomID, msg.Event)
			break

		}
		_ = c.Close()
		logger.Infof("Conn[%s] User %s exit room %s", c.ID(), user.Name, roomID)
	}()

	buf := make([]byte, 1024)
	for {
		nr, err := c.Read(buf)
		if err != nil {
			logger.Errorf("Conn[%s] User %s exit share room %s by %s", user.Name, roomID, err)
			break
		}
		// checkout user write pem
		if !CheckShareRoomWritePerm(c.Conn.user.ID, roomID) {
			continue
		}
		msg := model.RoomMessage{
			Event: model.DataEvent,
			Body:  buf[:nr],
		}
		room.Publish(msg)
		logger.Infof("User %s published DataEvent to room %s", user.Name, roomID)
	}
}
