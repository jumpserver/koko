package koko

import (
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
	"sync"
	"time"
)

type server struct {
	terminalConf *model.TerminalConfig
	jmsService   *service.JMService
	sync.Mutex

	vscodeClients map[string]*vscodeReq
}

func (s *server) run() {
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

func (s *server) UpdateTerminalConfig(conf model.TerminalConfig) {
	s.Lock()
	defer s.Unlock()
	s.terminalConf = &conf
}

func (s *server) GetTerminalConfig() model.TerminalConfig {
	s.Lock()
	defer s.Unlock()
	return *s.terminalConf
}

func (s *server) getVSCodeReq(reqId string) *vscodeReq {
	s.Lock()
	defer s.Unlock()
	return s.vscodeClients[reqId]
}

func (s *server) addVSCodeReq(vsReq *vscodeReq) {
	s.Lock()
	defer s.Unlock()
	s.vscodeClients[vsReq.reqId] = vsReq
}

func (s *server) deleteVSCodeReq(vsReq *vscodeReq) {
	s.Lock()
	defer s.Unlock()
	delete(s.vscodeClients, vsReq.reqId)
}

type vscodeReq struct {
	reqId  string
	user   *model.User
	client *srvconn.SSHClient
}
