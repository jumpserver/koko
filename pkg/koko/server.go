package koko

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
)

type Server struct {
	terminalConf atomic.Value
	jmsService   *service.JMService
	sync.Mutex

	ideClients map[string]*IDEClient
}

func (s *Server) run() {
	for {
		time.Sleep(time.Minute)
		conf, err := s.jmsService.GetTerminalConfig()
		if err != nil {
			logger.Errorf("Update terminal config failed: %s", err)
			continue
		}
		s.UpdateTerminalConfig(conf)
	}
}

func (s *Server) UpdateTerminalConfig(conf model.TerminalConfig) {
	s.terminalConf.Store(conf)
}

func (s *Server) GetTerminalConfig() model.TerminalConfig {
	return s.terminalConf.Load().(model.TerminalConfig)
}

func (s *Server) getIDEClient(reqId string) *IDEClient {
	s.Lock()
	defer s.Unlock()
	return s.ideClients[reqId]
}

func (s *Server) addIDEClient(client *IDEClient) {
	s.Lock()
	defer s.Unlock()
	s.ideClients[client.reqId] = client
}

func (s *Server) deleteIDEClient(client *IDEClient) {
	s.Lock()
	defer s.Unlock()
	delete(s.ideClients, client.reqId)
}

type IDEClient struct {
	*srvconn.SSHClient
	reqId      string
	user       *model.User
	session    *model.Session
	tokenInfo  *model.ConnectTokenInfo
	expireInfo *model.ExpireInfo
}
