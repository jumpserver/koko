package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

type SwitchSession struct {
	ID string

	MaxIdleTime   int
	keepAliveTime int

	ctx    context.Context
	cancel context.CancelFunc

	p *Server
}

func (s *SwitchSession) Terminate() {
	select {
	case <-s.ctx.Done():
		return
	default:
	}
	s.cancel()
	logger.Infof("Session[%s] receive terminate task from admin", s.ID)
}

func (s *SwitchSession) SessionID() string {
	return s.ID
}

func (s *SwitchSession) recordCommand(cmdRecordChan chan *ExecutedCommand) {
	// 命令记录
	cmdRecorder := s.p.GetCommandRecorder()
	for item := range cmdRecordChan {
		if item.Command == "" {
			continue
		}
		cmd := s.generateCommandResult(item)
		cmdRecorder.Record(cmd)
	}
	// 关闭命令记录
	cmdRecorder.End()
}

// generateCommandResult 生成命令结果
func (s *SwitchSession) generateCommandResult(item *ExecutedCommand) *model.Command {
	var (
		input string
		output string
		riskLevel int64
		user string
	)
	user = item.User.User
	if len(item.Command) > 128 {
		input = item.Command[:128]
	} else {
		input = item.Command
	}
	i := strings.LastIndexByte(item.Output, '\r')
	if i <= 0 {
		output = item.Output
	} else if i > 0 && i < 1024 {
		output = item.Output[:i]
	} else {
		output = item.Output[:1024]
	}

	switch item.RiskLevel {
	case model.HighRiskFlag:
		riskLevel = model.DangerLevel
	default:
		riskLevel = model.NormalLevel
	}
	return s.p.GenerateCommandItem(user, input, output, riskLevel, item.CreatedDate)
}

// Bridge 桥接两个链接
func (s *SwitchSession) Bridge(userConn UserConnection, srvConn srvconn.ServerConnection) (err error) {

	parser := s.p.GetFilterParser()
	logger.Infof("Conn[%s] create ParseEngine success", userConn.ID())
	//replayRecorder = NewReplyRecord(s.ID)
	replayRecorder := s.p.GetReplayRecorder()
	logger.Infof("Conn[%s] create replay success", userConn.ID())
	srvInChan := make(chan []byte, 1)
	done := make(chan struct{})
	userInputMessageChan := make(chan *exchange.RoomMessage, 1)
	// 处理数据流
	userOutChan, srvOutChan := parser.ParseStream(userInputMessageChan, srvInChan)

	defer func() {
		close(done)
		_ = userConn.Close()
		_ = srvConn.Close()
		parser.Close()
		// 关闭录像
		replayRecorder.End()
	}()

	// 记录命令
	cmdChan := parser.CommandRecordChan()
	go s.recordCommand(cmdChan)

	winCh := userConn.WinCh()
	maxIdleTime := time.Duration(s.MaxIdleTime) * time.Minute
	lastActiveTime := time.Now()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()

	room := exchange.CreateRoom(s.ID, userInputMessageChan)
	exchange.Register(room)
	defer exchange.UnRegister(room)
	conn := exchange.WrapperUserCon(userConn)
	room.Subscribe(conn)
	defer room.UnSubscribe(conn)
	exitSignal := make(chan struct{}, 2)
	go func() {
		var (
			exitFlag bool
			err      error
			nr       int
		)
		for {
			buf := make([]byte, 1024)
			nr, err = srvConn.Read(buf)
			if nr > 0 {
				select {
				case srvInChan <- buf[:nr]:
				case <-done:
					exitFlag = true
					logger.Infof("Session[%s] done", s.ID)
				}
				if exitFlag {
					break
				}
			}
			if err != nil {
				logger.Errorf("Session[%s] srv read err: %s", s.ID, err)
				break
			}
		}
		logger.Infof("Session[%s] srv read end", s.ID)
		exitSignal <- struct{}{}
		close(srvInChan)
	}()
	user := s.p.connOpts.user
	meta := exchange.MetaMessage{
		UserId:     user.ID,
		User:       user.String(),
		Created:    common.NewNowUTCTime().String(),
		RemoteAddr: userConn.RemoteAddr(),
	}
	room.Broadcast(&exchange.RoomMessage{
		Event: exchange.ShareJoin,
		Body:  nil,
		Meta: meta,
	})
	go func() {
		for {
			buf := make([]byte, 1024)
			nr, err := userConn.Read(buf)
			if nr > 0 {
				index := bytes.IndexFunc(buf[:nr], func(r rune) bool {
					return r == '\r' || r == '\n'
				})
				if index <= 0 || !parser.NeedRecord() {
					room.Receive(&exchange.RoomMessage{
						Event: exchange.DataEvent, Body: buf[:nr],
						Meta: meta})
				} else {
					room.Receive(&exchange.RoomMessage{
						Event: exchange.DataEvent, Body: buf[:index],
						Meta: meta})
					time.Sleep(time.Millisecond * 100)
					room.Receive(&exchange.RoomMessage{
						Event: exchange.DataEvent, Body: buf[index:nr],
						Meta: meta})
				}
			}
			if err != nil {
				logger.Errorf("Session[%s] user read err: %s", s.ID, err)
				break
			}
		}
		logger.Infof("Session[%s] user read end", s.ID)
		exitSignal <- struct{}{}
	}()
	keepAliveTime := time.Duration(s.keepAliveTime) * time.Second
	keepAliveTick := time.NewTicker(keepAliveTime)
	defer keepAliveTick.Stop()
	for {
		select {
		// 检测是否超过最大空闲时间
		case now := <-tick.C:
			outTime := lastActiveTime.Add(maxIdleTime)
			if now.After(outTime) {
				msg := fmt.Sprintf(i18n.T("Connect idle more than %d minutes, disconnect"), s.MaxIdleTime)
				logger.Infof("Session[%s] idle more than %d minutes, disconnect", s.ID, s.MaxIdleTime)
				msg = utils.WrapperWarn(msg)
				replayRecorder.Record([]byte(msg))
				room.Broadcast(&exchange.RoomMessage{Event: exchange.DataEvent, Body: []byte("\n\r" + msg)})
				return
			}
			if s.p.CheckPermissionExpired(now) {
				msg := i18n.T("Permission has expired, disconnect")
				logger.Infof("Session[%s] permission has expired, disconnect", s.ID)
				msg = utils.WrapperWarn(msg)
				replayRecorder.Record([]byte(msg))
				room.Broadcast(&exchange.RoomMessage{Event: exchange.DataEvent, Body: []byte("\n\r" + msg)})
				return
			}
			continue
			// 手动结束
		case <-s.ctx.Done():
			msg := i18n.T("Terminated by administrator")
			msg = utils.WrapperWarn(msg)
			replayRecorder.Record([]byte(msg))
			logger.Infof("Session[%s]: %s", s.ID, msg)
			room.Broadcast(&exchange.RoomMessage{Event: exchange.DataEvent, Body: []byte("\n\r" + msg)})
			return
			// 监控窗口大小变化
		case win, ok := <-winCh:
			if !ok {
				return
			}
			_ = srvConn.SetWinSize(win.Width, win.Height)
			logger.Infof("Session[%s] Window server change: %d*%d",
				s.ID, win.Width, win.Height)
			p, _ := json.Marshal(win)
			msg := exchange.RoomMessage{
				Event: exchange.WindowsEvent,
				Body:  p,
			}
			room.Broadcast(&msg)
			// 经过parse处理的server数据，发给user
		case p, ok := <-srvOutChan:
			if !ok {
				return
			}
			if parser.NeedRecord() {
				replayRecorder.Record(p)
			}
			msg := exchange.RoomMessage{
				Event: exchange.DataEvent,
				Body:  p,
			}
			room.Broadcast(&msg)
			// 经过parse处理的user数据，发给server
		case p, ok := <-userOutChan:
			if !ok {
				return
			}
			if _, err := srvConn.Write(p); err != nil {
				logger.Errorf("Session[%s] srvConn write err: %s", s.ID, err)
			}

		case now := <-keepAliveTick.C:
			if now.After(lastActiveTime.Add(keepAliveTime)) {
				if err := srvConn.KeepAlive(); err != nil {
					logger.Errorf("Session[%s] srvCon keep alive err: %s", s.ID, err)
				}
			}
			continue
		case <-userConn.Context().Done():
			logger.Infof("Session[%s]: user conn context done", s.ID)
			return nil
		case <-exitSignal:
			logger.Debugf("Session[%s] end by exit signal", s.ID)
			return
		}
		lastActiveTime = time.Now()
	}
}
