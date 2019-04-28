package proxy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ibuler/ssh"

	"cocogo/pkg/logger"
)

type Switch struct {
	Id         string    `json:"id"`
	User       string    `json:"user"`
	Asset      string    `json:"asset"`
	SystemUser string    `json:"system_user"`
	Org        string    `json:"org_id"`
	LoginFrom  string    `json:"login_from"`
	RemoteAddr string    `json:"remote_addr"`
	DateStart  time.Time `json:"date_start"`
	DateEnd    time.Time `json:"date_end"`
	DateActive time.Time `json:"date_last_active"`
	Finished   bool      `json:"is_finished"`
	Closed     bool

	parser      *Parser
	userSession ssh.Session
	serverConn  ServerConnection
	closeChan   chan struct{}
}

func (s *Switch) preBridge() {

}

func (s *Switch) postBridge() {

}

func (s *Switch) watchWindowChange(ctx context.Context, winCh <-chan ssh.Window, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() {
		logger.Debug("Watch window change routine end")
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case win, ok := <-winCh:
			if !ok {
				break
			}
			err := s.serverConn.SetWinSize(win.Height, win.Width)
			if err != nil {
				logger.Error("Change server win size err: ", err)
				break
			}
			logger.Debugf("Window server change: %d*%d", win.Height, win.Width)
		}
	}
}

func (s *Switch) readUserToServer(wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() {
		logger.Debug("Read user to server end")
	}()
	buf := make([]byte, 1024)
	writer := s.serverConn.Writer()
	for {
		nr, err := s.userSession.Read(buf)
		if err != nil {
			return
		}
		buf2 := s.parser.ParseUserInput(buf[:nr])
		_, err = writer.Write(buf2)
		if err != nil {
			return
		}
	}

}

func (s *Switch) readServerToUser(wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() {
		logger.Debug("Read server to user end")
	}()
	buf := make([]byte, 1024)
	reader := s.serverConn.Reader()
	for {
		nr, err := reader.Read(buf)
		if err != nil {
			logger.Errorf("Read from server error: %s", err)
			break
		}
		buf2 := s.parser.ParseServerOutput(buf[:nr])
		_, err = s.userSession.Write(buf2)
		if err != nil {
			break
		}
	}
}

func (s *Switch) Bridge(ctx context.Context) (err error) {
	_, winCh, _ := s.userSession.Pty()
	wg := sync.WaitGroup{}
	wg.Add(3)
	go s.watchWindowChange(ctx, winCh, &wg)
	go s.readUserToServer(&wg)
	go s.readServerToUser(&wg)
	wg.Wait()
	fmt.Println("Bride end")
	return
}
