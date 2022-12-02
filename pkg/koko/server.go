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

type server struct {
	terminalConf atomic.Value
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
	s.terminalConf.Store(conf)
}

func (s *server) GetTerminalConfig() model.TerminalConfig {
	return s.terminalConf.Load().(model.TerminalConfig)
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

	expireInfo model.ExpireInfo
	Actions    model.Actions
}
