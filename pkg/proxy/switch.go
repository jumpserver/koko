package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
	"github.com/jumpserver/koko/pkg/zmodem"
)

type SwitchSession struct {
	ID string

	MaxIdleTime   int
	keepAliveTime int

	ctx    context.Context
	cancel context.CancelFunc

	p *Server

	currentOperator atomic.Value // 终断会话的管理员名称

	pausedStatus atomic.Bool // 暂停状态

	notifyMsgChan chan *exchange.RoomMessage

	MaxSessionTime time.Time

	invalidPerm     atomic.Bool
	invalidPermData []byte
	invalidPermTime time.Time
}

func (s *SwitchSession) Terminate(username string) {
	select {
	case <-s.ctx.Done():
		return
	default:
		s.setOperator(username)
	}
	s.cancel()
	logger.Infof("Session[%s] receive terminate task from %s", s.ID, username)
}

func (s *SwitchSession) PauseOperation(username string) {
	s.pausedStatus.Store(true)
	s.setOperator(username)
	logger.Infof("Session[%s] receive pause task from %s", s.ID, username)
	p, _ := json.Marshal(map[string]string{"user": username})
	s.notifyMsgChan <- &exchange.RoomMessage{
		Event: exchange.PauseEvent,
		Body:  p,
	}
}

func (s *SwitchSession) ResumeOperation(username string) {
	s.pausedStatus.Store(false)
	s.setOperator(username)
	logger.Infof("Session[%s] receive resume task from %s", s.ID, username)
	p, _ := json.Marshal(map[string]string{"user": username})
	s.notifyMsgChan <- &exchange.RoomMessage{
		Event: exchange.ResumeEvent,
		Body:  p,
	}
}

func (s *SwitchSession) PermBecomeExpired(code, detail string) {
	if s.invalidPerm.Load() {
		return
	}
	s.invalidPerm.Store(true)
	p, _ := json.Marshal(map[string]string{"code": code, "detail": detail})
	s.invalidPermData = p
	s.invalidPermTime = time.Now()
	s.notifyMsgChan <- &exchange.RoomMessage{
		Event: exchange.PermExpiredEvent, Body: p}
}

func (s *SwitchSession) PermBecomeValid(code, detail string) {
	if !s.invalidPerm.Load() {
		return
	}
	s.invalidPerm.Store(false)
	s.invalidPermTime = s.MaxSessionTime
	p, _ := json.Marshal(map[string]string{"code": code, "detail": detail})
	s.invalidPermData = p
	s.notifyMsgChan <- &exchange.RoomMessage{
		Event: exchange.PermValidEvent, Body: p}
}

func (s *SwitchSession) CheckPermissionExpired(now time.Time) bool {
	if s.p.CheckPermissionExpired(now) {
		return true
	}
	if s.invalidPerm.Load() {
		if now.After(s.invalidPermTime.Add(10 * time.Minute)) {
			return true
		}
	}
	return false
}

func (s *SwitchSession) setOperator(username string) {
	s.currentOperator.Store(username)
}

func (s *SwitchSession) loadOperator() string {
	return s.currentOperator.Load().(string)
}

func (s *SwitchSession) filterUserInput(p []byte) []byte {
	if s.pausedStatus.Load() {
		return nil
	}
	return p
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
		input  string
		output string
		user   string
	)
	user = item.User.User
	if len(item.Command) > maxBufSize {
		input = item.Command[:maxBufSize]
	} else {
		input = item.Command
	}
	output = item.Output
	if len(output) > maxBufSize {
		output = item.Output[:maxBufSize]
	}

	return s.p.GenerateCommandItem(user, input, output, item)
}

// Bridge 桥接两个链接
func (s *SwitchSession) Bridge(userConn UserConnection, srvConn srvconn.ServerConnection) (err error) {

	parser := s.p.GetFilterParser()
	logger.Infof("Conn[%s] create ParseEngine success", userConn.ID())
	replayRecorder := s.p.GetReplayRecorder()
	logger.Infof("Conn[%s] create replay success", userConn.ID())
	srvInChan := make(chan []byte, 1)
	done := make(chan struct{})
	userInputMessageChan := make(chan *exchange.RoomMessage, 1)
	// 处理数据流
	userOutChan, srvOutChan := parser.ParseStream(userInputMessageChan, srvInChan)
	parser.SetUserInputFilter(s.filterUserInput)

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
		)
		buffer := bytes.NewBuffer(make([]byte, 0, 1024*2))
		/*
		 这里使用了一个buffer，将用户输入的数据进行了分包，分包的依据是utf8编码的字符。
		*/
		maxLen := 1024
		for {
			buf := make([]byte, maxLen)
			nr, err2 := srvConn.Read(buf)
			validBytes := buf[:nr]
			if nr > 0 {
				isZmodem := parser.zmodemParser.IsStartSession()
				if !isZmodem {
					bufferLen := buffer.Len()
					if bufferLen > 0 || nr == maxLen {
						buffer.Write(buf[:nr])
						validBytes = validBytes[:0]
					}
					remainBytes := buffer.Bytes()
					for len(remainBytes) > 0 {
						r, size := utf8.DecodeRune(remainBytes)
						if r == utf8.RuneError {
							// utf8 max 4 bytes
							if len(remainBytes) <= 3 {
								break
							}
						}
						validBytes = append(validBytes, remainBytes[:size]...)
						remainBytes = remainBytes[size:]
					}
					buffer.Reset()
					if len(remainBytes) > 0 {
						buffer.Write(remainBytes)
					}
				}
				select {
				case srvInChan <- validBytes:
				case <-done:
					exitFlag = true
					logger.Infof("Session[%s] done", s.ID)
				}
				if exitFlag {
					break
				}
			}
			if err2 != nil {
				logger.Errorf("Session[%s] srv read err: %s", s.ID, err2)
				break
			}
		}
		logger.Infof("Session[%s] srv read end", s.ID)
		exitSignal <- struct{}{}
		close(srvInChan)
	}()
	user := s.p.connOpts.authInfo.User
	meta := exchange.MetaMessage{
		UserId:     user.ID,
		User:       user.String(),
		Created:    common.NewNowUTCTime().String(),
		RemoteAddr: userConn.RemoteAddr(),
		TerminalId: userConn.ID(),
		Primary:    true,
		Writable:   true,
	}
	room.Broadcast(&exchange.RoomMessage{
		Event: exchange.ShareJoin,
		Meta:  meta,
	})
	if parser.zmodemParser != nil {
		parser.zmodemParser.FireStatusEvent = func(event zmodem.StatusEvent) {
			msg := exchange.RoomMessage{Event: exchange.ActionEvent}
			switch event {
			case zmodem.StartEvent:
				msg.Body = []byte(exchange.ZmodemStartEvent)
			case zmodem.EndEvent:
				msg.Body = []byte(exchange.ZmodemEndEvent)
			default:
				msg.Body = []byte(event)
			}
			room.Broadcast(&msg)
		}
	}
	go func() {
		for {
			buf := make([]byte, 1024)
			nr, err1 := userConn.Read(buf)
			if nr > 0 {
				room.Receive(&exchange.RoomMessage{
					Event: exchange.DataEvent, Body: buf[:nr],
					Meta: meta})
			}
			if err1 != nil {
				logger.Errorf("Session[%s] user read err: %s", s.ID, err1)
				break
			}
		}
		logger.Infof("Session[%s] user read end", s.ID)
		exitSignal <- struct{}{}
	}()
	keepAliveTime := time.Duration(s.keepAliveTime) * time.Second
	keepAliveTick := time.NewTicker(keepAliveTime)
	defer keepAliveTick.Stop()
	lang := s.p.connOpts.getLang()
	for {
		select {
		// 检测是否超过最大空闲时间
		case now := <-tick.C:
			if s.MaxSessionTime.Before(now) {
				msg := lang.T("Session max time reached, disconnect")
				logger.Infof("Session[%s] max session time reached, disconnect", s.ID)
				s.disconnection(room, parser, replayRecorder, msg)
				s.recordSessionFinished(model.ReasonErrMaxSessionTimeout)
				return
			}

			outTime := lastActiveTime.Add(maxIdleTime)
			if now.After(outTime) {
				msg := fmt.Sprintf(lang.T("Connect idle more than %d minutes, disconnect"), s.MaxIdleTime)
				logger.Infof("Session[%s] idle more than %d minutes, disconnect", s.ID, s.MaxIdleTime)
				s.disconnection(room, parser, replayRecorder, msg)
				s.recordSessionFinished(model.ReasonErrIdleDisconnect)
				return
			}
			if s.CheckPermissionExpired(now) {
				msg := lang.T("Permission has expired, disconnect")
				logger.Infof("Session[%s] permission has expired, disconnect", s.ID)
				s.disconnection(room, parser, replayRecorder, msg)
				s.recordSessionFinished(model.ReasonErrPermissionExpired)
				return
			}
			continue
			// 手动结束
		case <-s.ctx.Done():
			adminUser := s.loadOperator()
			msg := fmt.Sprintf(lang.T("Terminated by admin %s"), adminUser)
			logger.Infof("Session[%s]: %s", s.ID, msg)
			s.disconnection(room, parser, replayRecorder, msg)
			s.recordSessionFinished(model.ReasonErrAdminTerminate)
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
				s.recordSessionFinished(model.ReasonErrConnectDisconnect)
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
				s.recordSessionFinished(model.ReasonErrUserClose)
				return
			}
			if _, err1 := srvConn.Write(p); err1 != nil {
				logger.Errorf("Session[%s] srvConn write err: %s", s.ID, err1)
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
			s.recordSessionFinished(model.ReasonErrUserClose)
			return nil
		case <-exitSignal:
			logger.Debugf("Session[%s] end by exit signal", s.ID)
			s.recordSessionFinished(model.ReasonErrConnectDisconnect)
			return
		case notifyMsg := <-s.notifyMsgChan:
			logger.Infof("Session[%s] notify event: %s", s.ID, notifyMsg.Event)
			room.Broadcast(notifyMsg)
			continue
		}
		lastActiveTime = time.Now()
	}
}
func (s *SwitchSession) disconnection(room *exchange.Room, parser *Parser, replayRecorder *ReplyRecorder, msg string) {
	msg = utils.WrapperWarn(msg)
	replayRecorder.Record([]byte(msg))

	roomMessage := &exchange.RoomMessage{Event: exchange.DataEvent, Body: []byte("\n\r" + msg)}
	if parser.zmodemParser.IsStartSession() {
		expectedSize := len(zmodem.SkipSequence) + len(zmodem.CancelSequence)
		roomMessage.Body = make([]byte, 0, expectedSize)
		roomMessage.Body = append(roomMessage.Body, zmodem.SkipSequence...)
		roomMessage.Body = append(roomMessage.Body, zmodem.CancelSequence...)
	}

	room.Broadcast(roomMessage)
}

func (s *SwitchSession) recordSessionFinished(reason model.SessionLifecycleReasonErr) {
	logObj := model.SessionLifecycleLog{Reason: string(reason)}
	if err := s.p.jmsService.RecordSessionLifecycleLog(s.ID, model.AssetConnectFinished, logObj); err != nil {
		logger.Errorf("Session[%s] record session asset_connect_finished failed: %s", s.ID, err)
	}
}
