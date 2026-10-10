package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
	"time"

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
	s.p.operationPaused.Store(true)
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
	s.p.operationPaused.Store(false)
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
	s.p.permissionInvalid.Store(true)
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
	s.p.permissionInvalid.Store(false)
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

type sessionWriteRequest struct {
	data   []byte
	user   string
	window *srvconn.Windows
}

// Serialize connection writes without blocking output or session shutdown.
// The bridge holds at most one pending request; this channel is unbuffered.
func startSessionWriter(conn srvconn.ServerConnection, done <-chan struct{}, filter func([]byte) []byte) (chan<- sessionWriteRequest, <-chan error) {
	requests := make(chan sessionWriteRequest)
	errors := make(chan error, 1)
	write := func(data []byte, _ string) (int, error) { return conn.Write(data) }
	if input, ok := conn.(interface {
		WriteInput([]byte, string) (int, error)
	}); ok {
		write = input.WriteInput
	}
	go func() {
		defer close(errors)
		for {
			select {
			case <-done:
				return
			case request := <-requests:
				if request.window != nil {
					_ = conn.SetWinSize(request.window.Width, request.window.Height)
					continue
				}
				data := filter(request.data)
				if len(data) == 0 {
					continue
				}
				n, err := write(data, request.user)
				if err == nil && n != len(data) {
					err = io.ErrShortWrite
				}
				if err != nil {
					errors <- err
					return
				}
			}
		}
	}()
	return requests, errors
}

// Bridge 桥接两个链接
func (s *SwitchSession) Bridge(userConn UserConnection, srvConn srvconn.ServerConnection) (err error) {
	var parser *Parser
	winRM, isWinRM := srvConn.(*srvconn.WinRMConnection)
	if !isWinRM {
		parser, err = s.p.GetFilterParser()
		if err != nil {
			return err
		}
		logger.Infof("Conn[%s] create ParseEngine success", userConn.ID())
	}
	replayRecorder := s.p.GetReplayRecorder()
	logger.Infof("Conn[%s] create replay success", userConn.ID())
	srvInChan := make(chan []byte, 1)
	done := make(chan struct{})
	userInputMessageChan := make(chan *exchange.RoomMessage, 1)
	var userOutChan <-chan []byte
	var srvOutChan <-chan []byte = srvInChan
	var directInput <-chan *exchange.RoomMessage
	if isWinRM {
		// WinRM already edits and audits complete commands in its local terminal.
		// Reuse the bridge's output path without the character-stream parser.
		directInput = userInputMessageChan
		winRM.Start(func(ctx context.Context, command, user string, output io.Writer) error {
			return s.p.executeWinRMCommand(winRM, ctx, command, user, output)
		})
	} else {
		parser.SetUserInputFilter(s.filterUserInput)
		userOutChan, srvOutChan = parser.ParseStream(userInputMessageChan, srvInChan)
	}
	writeRequests, writeErrors := startSessionWriter(srvConn, done, s.filterUserInput)

	defer func() {
		close(done)
		_ = userConn.Close()
		_ = srvConn.Close()
		if parser != nil {
			parser.Close()
		}
		// 关闭录像
		replayRecorder.End()
	}()

	// 记录命令
	if parser != nil {
		go s.recordCommand(parser.CommandRecordChan())
	}

	winCh := userConn.WinCh()
	maxIdleTime := time.Duration(s.MaxIdleTime) * time.Minute
	lastActiveTime := time.Now()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()

	room := exchange.CreateRoom(s.ID, userInputMessageChan)
	exchange.Register(room)
	defer exchange.UnRegister(room)
	conn := exchange.WrapperUserCon(userConn)
	conn.Primary = true
	room.Subscribe(conn)
	defer room.UnSubscribe(conn)
	exitSignal := make(chan struct{}, 2)
	go func() {
		var (
			exitFlag bool
		)
		readBuf := make([]byte, 8*1024)
		for {
			nr, err2 := srvConn.Read(readBuf)
			if nr > 0 {
				data := append([]byte(nil), readBuf[:nr]...)
				select {
				case srvInChan <- data:
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
	if parser != nil && parser.zmodemParser != nil {
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
			if err1 != nil {
				logger.Errorf("Session[%s] user read err: %s", s.ID, err1)
				break
			}
			room.Receive(&exchange.RoomMessage{
				Event: exchange.DataEvent, Body: buf[:nr],
				Meta: meta})
		}
		logger.Infof("Session[%s] user read end", s.ID)
		exitSignal <- struct{}{}
	}()
	keepAliveTime := time.Duration(s.keepAliveTime) * time.Second
	keepAliveTick := time.NewTicker(keepAliveTime)
	defer keepAliveTick.Stop()
	lang := s.p.connOpts.getLang()
	var pendingOutput *exchange.RoomMessage
	var pendingWrite *sessionWriteRequest
	for {
		serverOutput := srvOutChan
		userOutput := userOutChan
		userMessages := directInput
		windows := winCh
		notifications := s.notifyMsgChan
		var broadcast chan<- *exchange.RoomMessage
		var writes chan<- sessionWriteRequest
		var nextWrite sessionWriteRequest
		if pendingWrite != nil {
			writes = writeRequests
			nextWrite = *pendingWrite
			userOutput = nil
			userMessages = nil
			windows = nil
		}
		if pendingOutput != nil {
			// Keep only one pending message and continue handling session exit
			// while the primary subscriber is applying backpressure.
			serverOutput = nil
			userOutput = nil
			userMessages = nil
			windows = nil
			notifications = nil
			broadcast = room.BroadcastChan()
		}
		select {
		case writes <- nextWrite:
			pendingWrite = nil
			continue
		case err := <-writeErrors:
			logger.Errorf("Session[%s] connection write err: %s", s.ID, err)
			s.recordSessionFinished(model.ReasonErrConnectDisconnect)
			return err
		case msg, ok := <-userMessages:
			if !ok {
				s.recordSessionFinished(model.ReasonErrUserClose)
				return nil
			}
			if msg.Event == exchange.DataEvent && len(msg.Body) > 0 {
				pendingWrite = &sessionWriteRequest{data: msg.Body, user: msg.Meta.User}
			}
		case broadcast <- pendingOutput:
			pendingOutput = nil
			continue
		case <-room.Done():
			s.recordSessionFinished(model.ReasonErrConnectDisconnect)
			return
		// 检测是否超过最大空闲时间
		case now := <-tick.C:
			if s.MaxSessionTime.Before(now) {
				msg := lang.T("Session max time reached, disconnect")
				logger.Infof("Session[%s] max session time reached, disconnect", s.ID)
				s.disconnection(room, parser, replayRecorder, msg)
				s.recordSessionFinished(model.ReasonErrMaxSessionTimeout)
				return
			}

			if timestamp := s.p.backgroundActiveAt.Load(); timestamp > 0 {
				backgroundActive := time.Unix(0, timestamp)
				if backgroundActive.After(lastActiveTime) {
					lastActiveTime = backgroundActive
				}
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
		case win, ok := <-windows:
			if !ok {
				return
			}
			pendingWrite = &sessionWriteRequest{window: &srvconn.Windows{Width: win.Width, Height: win.Height}}
			if parser != nil {
				if err := parser.TerminalParser.Resize(win.Width, win.Height); err != nil {
					logger.Errorf("Session[%s] resize terminal parser failed: %s", s.ID, err)
				}
			}
			logger.Infof("Session[%s] Window server change: %d*%d",
				s.ID, win.Width, win.Height)
			p, _ := json.Marshal(win)
			msg := exchange.RoomMessage{
				Event: exchange.WindowsEvent,
				Body:  p,
			}
			pendingOutput = &msg
			// 经过parse处理的server数据，发给user
		case p, ok := <-serverOutput:
			if !ok {
				s.recordSessionFinished(model.ReasonErrConnectDisconnect)
				return
			}
			if parser == nil || parser.NeedRecord() {
				replayRecorder.Record(p)
			}
			msg := exchange.RoomMessage{
				Event: exchange.DataEvent,
				Body:  p,
			}
			pendingOutput = &msg
			// 经过parse处理的user数据，发给server
		case p, ok := <-userOutput:
			if !ok {
				s.recordSessionFinished(model.ReasonErrUserClose)
				return
			}
			if len(p) > 0 {
				pendingWrite = &sessionWriteRequest{data: p}
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
		case notifyMsg := <-notifications:
			logger.Infof("Session[%s] notify event: %s", s.ID, notifyMsg.Event)
			pendingOutput = notifyMsg
			continue
		}
		lastActiveTime = time.Now()
	}
}
func (s *SwitchSession) disconnection(room *exchange.Room, parser *Parser, replayRecorder *ReplyRecorder, msg string) {
	msg = utils.WrapperWarn(msg)
	replayRecorder.Record([]byte(msg))

	roomMessage := &exchange.RoomMessage{Event: exchange.DataEvent, Body: []byte("\n\r" + msg)}
	if parser != nil && parser.zmodemParser.IsStartSession() {
		expectedSize := len(zmodem.SkipSequence) + len(zmodem.CancelSequence)
		roomMessage.Body = make([]byte, 0, expectedSize)
		roomMessage.Body = append(roomMessage.Body, zmodem.SkipSequence...)
		roomMessage.Body = append(roomMessage.Body, zmodem.CancelSequence...)
	}

	// A full output queue must not prevent an explicit session shutdown.
	select {
	case room.BroadcastChan() <- roomMessage:
	default:
	}
}

func (s *SwitchSession) recordSessionFinished(reason model.SessionLifecycleReasonErr) {
	s.p.SessionEndReason = reason
	logObj := model.SessionLifecycleLog{Reason: string(reason)}
	if err := s.p.jmsService.RecordSessionLifecycleLog(s.ID, model.AssetConnectFinished, logObj); err != nil {
		logger.Errorf("Session[%s] record session asset_connect_finished failed: %s", s.ID, err)
	}
}
