package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	uuid "github.com/satori/go.uuid"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

func NewCommonSwitch(p proxyEngine) *commonSwitch {
	ctx, cancel := context.WithCancel(context.Background())
	c := commonSwitch{
		ID:            uuid.NewV4().String(),
		DateStart:     common.CurrentUTCTime(),
		MaxIdleTime:   config.GetConf().MaxIdleTime,
		keepAliveTime: time.Second * 60,
		ctx:           ctx,
		cancel:        cancel,
		p:             p,
	}
	return &c
}

type commonSwitch struct {
	ID        string
	DateStart string
	DateEnd   string
	finished  bool

	isConnected bool

	MaxIdleTime time.Duration

	keepAliveTime time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	p proxyEngine
}

func (s *commonSwitch) Terminate() {
	select {
	case <-s.ctx.Done():
		return
	default:
	}
	s.cancel()
	logger.Infof("Session[%s] receive terminate task from admin", s.ID)
}

func (s *commonSwitch) SessionID() string {
	return s.ID
}

func (s *commonSwitch) recordCommand(cmdRecordChan chan [3]string) {
	// 命令记录
	cmdRecorder := NewCommandRecorder(s.ID)
	for command := range cmdRecordChan {
		if command[0] == "" {
			continue
		}
		cmd := s.generateCommandResult(command)
		cmdRecorder.Record(cmd)
	}
	// 关闭命令记录
	cmdRecorder.End()
}

// generateCommandResult 生成命令结果
func (s *commonSwitch) generateCommandResult(command [3]string) *model.Command {
	var input string
	var output string
	var riskLevel int64
	if len(command[0]) > 128 {
		input = command[0][:128]
	} else {
		input = command[0]
	}
	i := strings.LastIndexByte(command[1], '\r')
	if i <= 0 {
		output = command[1]
	} else if i > 0 && i < 1024 {
		output = command[1][:i]
	} else {
		output = command[1][:1024]
	}

	switch command[2] {
	case model.HighRiskFlag:
		riskLevel = model.DangerLevel
	default:
		riskLevel = model.NormalLevel
	}
	return s.p.GenerateRecordCommand(s, input, output, riskLevel)
}

// postBridge 桥接结束以后执行操作
func (s *commonSwitch) postBridge() {
	s.DateEnd = common.CurrentUTCTime()
	s.finished = true
}

func (s *commonSwitch) MapData() map[string]interface{} {
	return s.p.MapData(s)
}

// Bridge 桥接两个链接
func (s *commonSwitch) Bridge(userConn UserConnection, srvConn srvconn.ServerConnection) (err error) {
	var (
		replayRecorder ReplyRecorder
	)
	s.isConnected = true
	parser := s.p.NewParser(s)
	logger.Infof("Conn[%s] create ParseEngine success", userConn.ID())
	replayRecorder = NewReplyRecord(s.ID)
	logger.Infof("Conn[%s] create replay success", userConn.ID())
	srvInChan := make(chan []byte, 1)
	done := make(chan struct{})
	userInputMessageChan := make(chan *model.RoomMessage, 1)
	// 处理数据流
	userOutChan, srvOutChan := parser.ParseStream(userInputMessageChan, srvInChan)

	defer func() {
		close(done)
		_ = userConn.Close()
		_ = srvConn.Close()
		// 关闭parser
		parser.Close()
		// 关闭录像
		replayRecorder.End()
		s.postBridge()
	}()

	// 记录命令
	cmdChan := parser.CommandRecordChan()
	go s.recordCommand(cmdChan)

	winCh := userConn.WinCh()
	maxIdleTime := s.MaxIdleTime * time.Minute
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

	go func() {
		for {
			buf := make([]byte, 1024)
			nr, err := userConn.Read(buf)
			if nr > 0 {
				room.Receive(&model.RoomMessage{
					Event: model.DataEvent, Body: buf[:nr]})
			}
			if err != nil {
				logger.Errorf("Session[%s] user read err: %s", s.ID, err)
				break
			}
		}
		logger.Infof("Session[%s] user read end", s.ID)
		exitSignal <- struct{}{}
	}()

	keepAliveTick := time.NewTicker(s.keepAliveTime)
	defer keepAliveTick.Stop()
	for {
		select {
		// 检测是否超过最大空闲时间
		case now := <-tick.C:
			outTime := lastActiveTime.Add(maxIdleTime)
			if !now.After(outTime) {
				continue
			}
			msg := fmt.Sprintf(i18n.T("Connect idle more than %d minutes, disconnect"), s.MaxIdleTime)
			logger.Infof("Session[%s] idle more than %d minutes, disconnect", s.ID, s.MaxIdleTime)
			msg = utils.WrapperWarn(msg)
			replayRecorder.Record([]byte(msg))
			room.Broadcast(&model.RoomMessage{Event: model.DataEvent, Body: []byte("\n\r" + msg)})
			return
			// 手动结束
		case <-s.ctx.Done():
			msg := i18n.T("Terminated by administrator")
			msg = utils.WrapperWarn(msg)
			replayRecorder.Record([]byte(msg))
			logger.Infof("Session[%s]: %s", s.ID, msg)
			room.Broadcast(&model.RoomMessage{Event: model.DataEvent, Body: []byte("\n\r" + msg)})
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
			msg := model.RoomMessage{
				Event: model.WindowsEvent,
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
			msg := model.RoomMessage{
				Event: model.DataEvent,
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
			if now.After(lastActiveTime.Add(s.keepAliveTime)) {
				if err := srvConn.KeepAlive(); err != nil {
					logger.Errorf("Session[%s] srvCon keep alive err: %s", s.ID, err)
				}
			}
			continue
		case <-exitSignal:
			logger.Debugf("Session[%s] end by exit signal", s.ID)
			return
		}
		lastActiveTime = time.Now()
	}
}
