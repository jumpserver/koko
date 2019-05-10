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
	if p.SystemUser.LoginMode == model.LoginModeManual ||
		(p.SystemUser.Password == "" && p.SystemUser.PrivateKey == "") {
		logger.Info("Get password fom user input")
	}
	p.SystemUser.Password = info.Password
	p.SystemUser.PrivateKey = info.PrivateKey
}

func (p *ProxyServer) getSystemUserUsernameIfNeed() {

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
		host:     p.Asset.Ip,
		port:     strconv.Itoa(p.Asset.Port),
		user:     p.SystemUser.Username,
		password: p.SystemUser.Password,
		timeout:  config.Conf.SSHTimeout,
	}
	pty := p.UserConn.Pty()
	done := make(chan struct{})
	go p.sendConnectingMsg(done)
	err = srvConn.Connect(pty.Window.Height, pty.Window.Width, pty.Term)
	utils.IgnoreErrWriteString(p.UserConn, "\r\n")
	close(done)
	return
}

func (p *ProxyServer) getTelnetConn() (srvConn *ServerSSHConnection, err error) {
	return
}

func (p *ProxyServer) getServerConn() (srvConn ServerConnection, err error) {
	if p.Asset.Protocol == "telnet" {
		return p.getTelnetConn()
	} else {
		return p.getSSHConn()
	}
}

func (p *ProxyServer) sendConnectingMsg(done chan struct{}) {
	delay := 0.0
	msg := fmt.Sprintf(i18n.T("Connecting to %s@%s  %.1f"), p.SystemUser.Username, p.Asset.Ip, delay)
	utils.IgnoreErrWriteString(p.UserConn, msg)
	for int(delay) < config.Conf.SSHTimeout {
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
		msg := fmt.Sprintf("Connect asset %s error: %s\n", p.Asset.Hostname, err)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Errorf(msg)
		return
	}

	sw := NewSwitchSession(p.UserConn, srvConn)
	cmdRules, err := service.GetSystemUserFilterRules(p.SystemUser.Id)
	if err != nil {
		logger.Error("Get system user filter rule error: ", err)
	}
	sw.parser.SetCMDFilterRules(cmdRules)
	replayRecorder := NewReplyRecord(sw.Id)
	sw.parser.SetReplayRecorder(replayRecorder)
	_ = sw.Bridge()
	_ = srvConn.Close()
}
