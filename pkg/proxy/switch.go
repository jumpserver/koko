package proxy

import (
	"context"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/satori/go.uuid"

	"cocogo/pkg/logger"
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

	cmdRecordChan chan [2]string
	userConn      UserConnection
	srvConn       ServerConnection
	userChan      Transport
	srvChan       Transport
	cancelFunc    context.CancelFunc
}

func (s *SwitchSession) Initial() {
	s.Id = uuid.NewV4().String()
	s.User = s.userConn.User()
	s.Server = s.srvConn.Name()
	s.SystemUser = s.srvConn.User()
	s.LoginFrom = s.userConn.LoginFrom()
	s.RemoteAddr = s.userConn.RemoteAddr()
	s.DateStart = time.Now()

	s.cmdRecordChan = make(chan [2]string, 1024)
	s.cmdRecorder = NewCommandRecorder(s)
	s.replayRecorder = NewReplyRecord(s)

	parser := &Parser{
		userInputChan:  make(chan []byte, 1024),
		userOutputChan: make(chan []byte, 1024),
		srvInputChan:   make(chan []byte, 1024),
		srvOutputChan:  make(chan []byte, 1024),
		cmdChan:        s.cmdRecordChan,
	}
	parser.Initial()
	s.parser = parser
}

func (s *SwitchSession) watchWindowChange(ctx context.Context, winCh <-chan ssh.Window) {
	defer func() {
		logger.Debug("Watch window change routine end")
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case win, ok := <-winCh:
			if !ok {
				return
			}
			err := s.srvConn.SetWinSize(win.Height, win.Width)
			if err != nil {
				logger.Error("Change server win size err: ", err)
				return
			}
			logger.Debugf("Window server change: %d*%d", win.Height, win.Width)
		}
	}
}

func (s *SwitchSession) readParserToServer(ctx context.Context) {
	defer func() {
		logger.Debug("Read parser to server end")
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-s.parser.userOutputChan:
			if !ok {
				s.cancelFunc()
			}
			_, _ = s.srvChan.Write(p)
		}
	}
}

func (s *SwitchSession) readUserToParser(ctx context.Context) {
	defer func() {
		logger.Debug("Read user to server end")
	}()
	for {
		select {
		case <-ctx.Done():
			_ = s.userChan.Close()
			return
		case p, ok := <-s.userChan.Chan():
			if !ok {
				s.cancelFunc()
			}
			s.parser.userInputChan <- p
		}
	}
}

func (s *SwitchSession) readServerToParser(ctx context.Context) {
	defer func() {
		logger.Debug("Read server to parser end")
	}()
	for {
		select {
		case <-ctx.Done():
			_ = s.srvChan.Close()
			return
		case p, ok := <-s.srvChan.Chan():
			if !ok {
				s.cancelFunc()
			}
			s.parser.srvInputChan <- p
		}
	}
}

func (s *SwitchSession) readParserToUser(ctx context.Context) {
	defer func() {
		logger.Debug("Read parser to user end")
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-s.parser.srvOutputChan:
			if !ok {
				s.cancelFunc()
			}
			s.replayRecorder.Record(p)
			_, _ = s.userChan.Write(p)
		}
	}
}

func (s *SwitchSession) recordCmd() {
	for cmd := range s.cmdRecordChan {
		s.cmdRecorder.Record(cmd)
	}
}

func (s *SwitchSession) postBridge() {
	s.cmdRecorder.End()
	s.replayRecorder.End()
	s.parser.Close()
}

func (s *SwitchSession) Bridge() (err error) {
	winCh := s.userConn.WinCh()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFunc = cancel

	s.userChan = NewDirectTransport("", s.userConn)
	s.srvChan = NewDirectTransport("", s.srvConn)
	go s.parser.Parse()
	go s.watchWindowChange(ctx, winCh)
	go s.readServerToParser(ctx)
	go s.readParserToUser(ctx)
	go s.readParserToServer(ctx)
	go s.recordCmd()
	defer s.postBridge()
	s.readUserToParser(ctx)
	logger.Debug("Session bridge end")
	return
}
