package proxy

import (
	"cocogo/pkg/logger"
	"cocogo/pkg/service"
	"context"
	"github.com/ibuler/ssh"
	"github.com/satori/go.uuid"
	"time"
)

func NewSwitch(userConn UserConnection, serverConn ServerConnection) (sw *Switch) {
	rules, err := service.GetSystemUserFilterRules("")
	if err != nil {
		logger.Error("Get system user filter rule error: ", err)
	}
	parser := &Parser{
		cmdFilterRules: rules,
	}
	parser.Initial()
	sw = &Switch{userConn: userConn, serverConn: serverConn, parser: parser}
	return sw
}

type Switch struct {
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

	parser     *Parser
	userConn   UserConnection
	serverConn ServerConnection
	userTran   Transport
	serverTran Transport
	cancelFunc context.CancelFunc
}

func (s *Switch) Initial() {
	s.Id = uuid.NewV4().String()
	s.User = s.userConn.User()
	s.Server = s.serverConn.Name()
	s.SystemUser = s.serverConn.User()
	s.LoginFrom = s.userConn.LoginFrom()
	s.RemoteAddr = s.userConn.RemoteAddr()
	s.DateStart = time.Now()
}

func (s *Switch) preBridge() {

}

func (s *Switch) postBridge() {

}

func (s *Switch) watchWindowChange(ctx context.Context, winCh <-chan ssh.Window) {
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
			err := s.serverConn.SetWinSize(win.Height, win.Width)
			if err != nil {
				logger.Error("Change server win size err: ", err)
				return
			}
			logger.Debugf("Window server change: %d*%d", win.Height, win.Width)
		}
	}
}

func (s *Switch) readUserToServer(ctx context.Context) {
	defer func() {
		logger.Debug("Read user to server end")
	}()
	for {
		select {
		case <-ctx.Done():
			_ = s.userTran.Close()
			return
		case p, ok := <-s.userTran.Chan():
			if !ok {
				s.cancelFunc()
			}
			buf2 := s.parser.ParseUserInput(p)
			logger.Debug("Send to server: ", string(buf2))
			_, err := s.serverTran.Write(buf2)
			if err != nil {
				return
			}
		}
	}
}

func (s *Switch) readServerToUser(ctx context.Context) {
	defer func() {
		logger.Debug("Read server to user end")
	}()
	for {
		select {
		case <-ctx.Done():
			_ = s.serverTran.Close()
			return
		case p, ok := <-s.serverTran.Chan():
			if !ok {
				s.cancelFunc()
			}
			buf2 := s.parser.ParseServerOutput(p)
			_, err := s.userTran.Write(buf2)
			if err != nil {
				return
			}
		}
	}
}

func (s *Switch) Bridge() (err error) {
	winCh := s.userConn.WinCh()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFunc = cancel

	s.userTran = NewDirectTransport("", s.userConn)
	s.serverTran = NewDirectTransport("", s.serverConn)
	go s.watchWindowChange(ctx, winCh)
	go s.readServerToUser(ctx)
	s.readUserToServer(ctx)
	logger.Debug("Switch bridge end")
	return
}
