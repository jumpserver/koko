package proxy

import (
	"context"
	"time"

	"github.com/satori/go.uuid"

	"cocogo/pkg/i18n"
	"cocogo/pkg/logger"
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
	DateStart  time.Time `json:"date_start"`
	DateEnd    time.Time `json:"date_end"`
	DateActive time.Time `json:"date_last_active"`
	Finished   bool      `json:"is_finished"`
	Closed     bool

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
	s.DateStart = time.Now()

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

func (s *SwitchSession) postBridge() {
	s.cmdRecorder.End()
	s.replayRecorder.End()
	s.parser.Close()
	_ = s.userTran.Close()
	_ = s.srvTran.Close()
}

func (s *SwitchSession) Bridge() (err error) {
	winCh := s.userConn.WinCh()
	s.userTran = NewDirectTransport("", s.userConn)
	s.srvTran = NewDirectTransport("", s.srvConn)

	defer func() {
		logger.Info("Session bridge done: ", s.Id)
	}()

	go s.parser.Parse()
	defer s.postBridge()
	for {
		select {
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
			_, _ = s.userTran.Write(p)
		// User发来的数据流流入parser
		case p, ok := <-s.userTran.Chan():
			if !ok {
				return
			}
			s.parser.userInputChan <- p
		// User发来的数据经parser初六，发给Server
		case p := <-s.parser.userOutputChan:
			_, _ = s.srvTran.Write(p)
		}
	}
}
