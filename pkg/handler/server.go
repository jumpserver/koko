package handler

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
)

func NewServer(termCfg model.TerminalConfig, jmsService *service.JMService) *Server {
	app := Server{
		jmsService:    jmsService,
		vscodeClients: make(map[string]*vscodeReq),
	}
	app.UpdateTerminalConfig(termCfg)
	go app.run()
	return &app
}

type Server struct {
	terminalConf atomic.Value
	jmsService   *service.JMService
	sync.Mutex

	vscodeClients map[string]*vscodeReq
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

func (s *Server) getVSCodeReq(reqId string) *vscodeReq {
	s.Lock()
	defer s.Unlock()
	return s.vscodeClients[reqId]
}

func (s *Server) addVSCodeReq(vsReq *vscodeReq) {
	s.Lock()
	defer s.Unlock()
	s.vscodeClients[vsReq.reqId] = vsReq
}

func (s *Server) deleteVSCodeReq(vsReq *vscodeReq) {
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

	sync.Mutex
	forwards map[string]net.Listener
}

func (s *vscodeReq) GetForward(addr string) net.Listener {
	s.Lock()
	defer s.Unlock()

	return s.forwards[addr]
}

func (s *vscodeReq) AddForward(addr string, ln net.Listener) {
	s.Lock()
	defer s.Unlock()

	s.forwards[addr] = ln
}

func (s *vscodeReq) RemoveForward(addr string) {
	s.Lock()
	defer s.Unlock()
	delete(s.forwards, addr)
}
