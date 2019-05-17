package proxy

import (
	"context"
	"time"

	uuid "github.com/satori/go.uuid"

	"cocogo/pkg/config"
	"cocogo/pkg/i18n"
	"cocogo/pkg/logger"
	"cocogo/pkg/service"
	"cocogo/pkg/utils"
)

func NewSwitchSession(userConn UserConnection, serverConn ServerConnection) (sw *SwitchSession) {
	sw = &SwitchSession{userConn: userConn, srvConn: serverConn}
	sw.Initial()
	return sw
}

type SwitchSession struct {
	Id         string
	User       string    `json:"user"`
	Server     string    `json:"asset"`
	SystemUser string    `json:"system_user"`
	Org        string    `json:"org_id"`
	LoginFrom  string    `json:"login_from"`
	RemoteAddr string    `json:"remote_addr"`
	DateStart  string    `json:"date_start"`
	DateEnd    string    `json:"date_end"`
	DateActive time.Time `json:"date_last_active"`
	Finished   bool      `json:"is_finished"`
	Closed     bool

	MaxIdleTime int

	cmdRecorder    *CommandRecorder
	replayRecorder *ReplyRecorder
	parser         *Parser

	userConn UserConnection
	srvConn  ServerConnection
	userTran Transport
	srvTran  Transport

	ctx    context.Context
	cancel context.CancelFunc
}

func (s *SwitchSession) Initial() {
	s.Id = uuid.NewV4().String()
	s.User = s.userConn.User()
	s.Server = s.srvConn.Name()
	s.SystemUser = s.srvConn.User()
	s.LoginFrom = s.userConn.LoginFrom()
	s.RemoteAddr = s.userConn.RemoteAddr()
	s.DateStart = time.Now().UTC().Format("2006-01-02 15:04:05 +0000")
	s.MaxIdleTime = config.GetConf().MaxIdleTime
	s.cmdRecorder = NewCommandRecorder(s)
	s.replayRecorder = NewReplyRecord(s)

	s.parser = newParser()

	s.ctx, s.cancel = context.WithCancel(context.Background())
}

func (s *SwitchSession) Terminate() {
	if !s.Closed {
		msg := i18n.T("Terminated by administrator")
		utils.IgnoreErrWriteString(s.userConn, msg)
		s.cancel()
		s.Closed = true
	}
}

func (s *SwitchSession) recordCmd() {
	for cmd := range s.parser.cmdRecordChan {
		s.cmdRecorder.Record(cmd)
	}
}

func (s *SwitchSession) MapData() map[string]interface{} {
	var dataEnd interface{}
	if s.DateEnd == "" {
		dataEnd = nil
	} else {
		dataEnd = s.DateEnd
	}
	return map[string]interface{}{
		"id":          s.Id,
		"user":        s.User,
		"asset":       s.Server,
		"org_id":      s.Org,
		"login_from":  s.LoginFrom,
		"system_user": s.SystemUser,
		"remote_addr": s.RemoteAddr,
		"is_finished": s.Finished,
		"date_start":  s.DateStart,
		"date_end":    dataEnd,
	}
}

func (s *SwitchSession) postBridge() {
	_ = s.userTran.Close()
	_ = s.srvTran.Close()
	s.parser.Close()
	s.replayRecorder.End()
	s.cmdRecorder.End()
	s.finishSession()
}

func (s *SwitchSession) finishSession() {
	s.DateEnd = time.Now().UTC().Format("2006-01-02 15:04:05 +0000")
	service.FinishSession(s.MapData())
	service.FinishReply(s.Id)
	logger.Debugf("finish Session: %s", s.Id)
}

func (s *SwitchSession) creatSession() bool {
	for i := 0; i < 5; i++ {
		if service.CreateSession(s.MapData()) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func (s *SwitchSession) SetFilterRules(systemUserId string) {
	cmdRules, err := service.GetSystemUserFilterRules(systemUserId)
	if err != nil {
		logger.Error("Get system user filter rule error: ", err)
	}
	s.parser.SetCMDFilterRules(cmdRules)
}

func (s *SwitchSession) Bridge() (err error) {
	if !s.creatSession() {
		msg := i18n.T("Connect with api server failed")
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(s.userConn, msg)
		return
	}
	winCh := s.userConn.WinCh()
	s.userTran = NewDirectTransport("", s.userConn)
	s.srvTran = NewDirectTransport("", s.srvConn)

	defer func() {
		logger.Info("Session bridge done: ", s.Id)
	}()

	go s.parser.Parse()
	go s.recordCmd()
	defer s.postBridge()
	for {
		select {
		// 检测是否超过最大空闲时间
		case <-time.After(time.Duration(s.MaxIdleTime) * time.Minute):
			return
		// 手动结束
		case <-s.ctx.Done():
			return
		// 监控窗口大小变化
		case win := <-winCh:
			_ = s.srvConn.SetWinSize(win.Height, win.Width)
			logger.Debugf("Window server change: %d*%d", win.Height, win.Width)
		// Server发来数据流入parser中
		case p, ok := <-s.srvTran.Chan():
			if !ok {
				return
			}
			s.parser.srvInputChan <- p
		// Server流入parser数据，经处理发给用户
		case p := <-s.parser.srvOutputChan:
			nw, err := s.userTran.Write(p)
			if !s.parser.IsRecvState() {
				s.replayRecorder.Record(p[:nw])
			}
			if err != nil {
				return err
			}
		// User发来的数据流流入parser
		case p, ok := <-s.userTran.Chan():
			if !ok {
				return
			}
			s.parser.userInputChan <- p
		// User发来的数据经parser处理，发给Server
		case p := <-s.parser.userOutputChan:
			_, err = s.srvTran.Write(p)
			if err != nil {
				return err
			}
		}
	}
}
