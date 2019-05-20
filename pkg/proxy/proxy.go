package proxy

import (
	"cocogo/pkg/utils"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cocogo/pkg/config"
	"cocogo/pkg/i18n"
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
	"cocogo/pkg/service"
)

type ProxyServer struct {
	UserConn   UserConnection
	User       *model.User
	Asset      *model.Asset
	SystemUser *model.SystemUser
}

func (p *ProxyServer) getSystemUserAuthOrManualSet() {
	info := service.GetSystemUserAssetAuthInfo(p.SystemUser.Id, p.Asset.Id)
	p.SystemUser.Password = info.Password
	p.SystemUser.PrivateKey = info.PrivateKey

	if p.SystemUser.LoginMode == model.LoginModeManual ||
		(p.SystemUser.Password == "" && p.SystemUser.PrivateKey == "") {
		// Todo: terminal
		logger.Info("Get password fom user input")
	}
}

func (p *ProxyServer) getSystemUserUsernameIfNeed() {
	// Todo: terminal

}

func (p *ProxyServer) checkProtocolMatch() bool {
	return p.SystemUser.Protocol == p.Asset.Protocol
}

func (p *ProxyServer) checkProtocolIsGraph() bool {
	switch p.Asset.Protocol {
	case "ssh", "telnet":
		return true
	default:
		return false
	}
}

func (p *ProxyServer) validatePermission() bool {
	return true
}

func (p *ProxyServer) getSSHConn() (srvConn *ServerSSHConnection, err error) {
	srvConn = &ServerSSHConnection{
		name:       p.Asset.Hostname,
		host:       p.Asset.Ip,
		port:       strconv.Itoa(p.Asset.Port),
		user:       p.SystemUser.Username,
		password:   p.SystemUser.Password,
		privateKey: p.SystemUser.PrivateKey,
		timeout:    config.GetConf().SSHTimeout,
	}
	pty := p.UserConn.Pty()
	done := make(chan struct{})
	go p.sendConnectingMsg(done, srvConn.timeout)
	err = srvConn.Connect(pty.Window.Height, pty.Window.Width, pty.Term)
	utils.IgnoreErrWriteString(p.UserConn, "\r\n")
	close(done)
	return
}

func (p *ProxyServer) getTelnetConn() (srvConn *ServerSSHConnection, err error) {
	return
}

func (p *ProxyServer) getServerConn() (srvConn ServerConnection, err error) {
	p.getSystemUserUsernameIfNeed()
	p.getSystemUserAuthOrManualSet()
	if p.Asset.Protocol == "telnet" {
		return p.getTelnetConn()
	} else {
		return p.getSSHConn()
	}
}

func (p *ProxyServer) sendConnectingMsg(done chan struct{}, delaySecond int) {
	delay := 0.0
	msg := fmt.Sprintf(i18n.T("Connecting to %s@%s  %.1f"), p.SystemUser.Username, p.Asset.Ip, delay)
	utils.IgnoreErrWriteString(p.UserConn, msg)
	for int(delay) < delaySecond {
		select {
		case <-done:
			return
		default:
			delayS := fmt.Sprintf("%.1f", delay)
			data := strings.Repeat("\x08", len(delayS)) + delayS
			utils.IgnoreErrWriteString(p.UserConn, data)
			time.Sleep(100 * time.Millisecond)
			delay += 0.1
		}
	}
}

func (p *ProxyServer) preCheckRequisite() (ok bool) {
	if !p.checkProtocolMatch() {
		msg := utils.WrapperWarn(i18n.T("System user <%s> and asset <%s> protocol are inconsistent."))
		msg = fmt.Sprintf(msg, p.SystemUser.Username, p.Asset.Hostname)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		return
	}
	if !p.checkProtocolIsGraph() {
		msg := i18n.T("Terminal only support protocol ssh/telnet, please use web terminal to access")
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		return
	}
	if !p.validatePermission() {
		msg := fmt.Sprintf("You don't have permission login %s@%s", p.SystemUser.Username, p.Asset.Hostname)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		return
	}
	return true
}

func (p *ProxyServer) Proxy() {
	if !p.preCheckRequisite() {
		return
	}
	srvConn, err := p.getServerConn()
	if err != nil {
		msg := fmt.Sprintf("Connect asset %s error: %s\n\r", p.Asset.Hostname, err)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Errorf(msg)
		return
	}
	sw := NewSwitchSession(p)
	ok := p.createSession(sw)
	if !ok {
		msg := i18n.T("Connect with api server failed")
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		return
	}
	cmdRules := p.GetFilterRules()
	sw.SetFilterRules(cmdRules)
	_ = sw.Bridge(p.UserConn, srvConn)
	p.finishSession(sw)
}

func (p *ProxyServer) createSession(s *SwitchSession) bool {
	data := s.MapData()
	for i := 0; i < 5; i++ {
		if service.CreateSession(data) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func (p *ProxyServer) finishSession(s *SwitchSession) {
	data := s.MapData()
	service.FinishSession(data)
	service.FinishReply(s.Id)
	logger.Debugf("finish Session: %s", s.Id)
}

func (p *ProxyServer) GetFilterRules() []model.SystemUserFilterRule {
	cmdRules, err := service.GetSystemUserFilterRules(p.SystemUser.Id)
	if err != nil {
		logger.Error("Get system user filter rule error: ", err)
	}
	return cmdRules
}
