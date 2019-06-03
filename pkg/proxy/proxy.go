package proxy

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"cocogo/pkg/config"
	"cocogo/pkg/i18n"
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
	"cocogo/pkg/service"
	"cocogo/pkg/srvconn"
	"cocogo/pkg/utils"
)

type ProxyServer struct {
	UserConn   UserConnection
	User       *model.User
	Asset      *model.Asset
	SystemUser *model.SystemUser
}

// getSystemUserAuthOrManualSet 获取系统用户的认证信息或手动设置
func (p *ProxyServer) getSystemUserAuthOrManualSet() {
	if p.SystemUser.LoginMode == model.LoginModeManual ||
		(p.SystemUser.Password == "" && p.SystemUser.PrivateKey == "") {
		term := utils.NewTerminal(p.UserConn, "password: ")
		line, err := term.ReadPassword(fmt.Sprintf("%s's password: ", p.SystemUser.Username))
		if err != nil {
			logger.Errorf("Get password from user err %s", err.Error())
		}
		p.SystemUser.Password = line
		logger.Debug("Get password from user input: ", line)
	} else {
		info := service.GetSystemUserAssetAuthInfo(p.SystemUser.Id, p.Asset.Id)
		p.SystemUser.Password = info.Password
		p.SystemUser.PrivateKey = info.PrivateKey
	}
}

// getSystemUserUsernameIfNeed 获取系统用户用户名，或手动设置
func (p *ProxyServer) getSystemUserUsernameIfNeed() {
	if p.SystemUser.Username == "" {
		var username string
		term := utils.NewTerminal(p.UserConn, "username: ")
		for {
			username, _ = term.ReadLine()
			username = strings.TrimSpace(username)
			if username != "" {
				break
			}
		}
		p.SystemUser.Username = username
		logger.Debug("Get username from user input: ", username)
	}
}

// checkProtocolMatch 检查协议是否匹配
func (p *ProxyServer) checkProtocolMatch() bool {
	return p.SystemUser.Protocol == p.Asset.Protocol
}

// checkProtocolIsGraph 检查协议是否是图形化的
func (p *ProxyServer) checkProtocolIsGraph() bool {
	switch p.Asset.Protocol {
	case "ssh", "telnet":
		return false
	default:
		return true
	}
}

// validatePermission 检查是否有权限连接
func (p *ProxyServer) validatePermission() bool {
	return service.ValidateUserAssetPermission(
		p.User.ID, p.Asset.Id, p.SystemUser.Id, "connect",
	)
}

// getSSHConn 获取ssh连接
func (p *ProxyServer) getSSHConn(fromCache ...bool) (srvConn *srvconn.ServerSSHConnection, err error) {
	pty := p.UserConn.Pty()
	srvConn = &srvconn.ServerSSHConnection{
		User:       p.User,
		Asset:      p.Asset,
		SystemUser: p.SystemUser,
		Overtime:   time.Duration(config.GetConf().SSHTimeout) * time.Second,
	}
	if len(fromCache) > 0 && fromCache[0] {
		err = srvConn.TryConnectFromCache(pty.Window.Height, pty.Window.Width, pty.Term)
	} else {
		err = srvConn.Connect(pty.Window.Height, pty.Window.Width, pty.Term)
	}
	return
}

// getTelnetConn 获取telnet连接
func (p *ProxyServer) getTelnetConn() (srvConn *srvconn.ServerTelnetConnection, err error) {
	conf := config.GetConf()
	cusString := conf.TelnetRegex
	pattern, _ := regexp.Compile(cusString)
	srvConn = &srvconn.ServerTelnetConnection{
		User:                 p.User,
		Asset:                p.Asset,
		SystemUser:           p.SystemUser,
		CustomString:         cusString,
		CustomSuccessPattern: pattern,
		Overtime:             time.Duration(conf.SSHTimeout) * time.Second,
	}
	err = srvConn.Connect(0, 0, "")
	utils.IgnoreErrWriteString(p.UserConn, "\r\n")
	return
}

// getServerConnFromCache 从cache中获取ssh server连接
func (p *ProxyServer) getServerConnFromCache() (srvConn srvconn.ServerConnection, err error) {
	if p.SystemUser.Protocol == "ssh" {
		srvConn, err = p.getSSHConn(true)
	}
	return
}

// getServerConn 获取获取server连接
func (p *ProxyServer) getServerConn() (srvConn srvconn.ServerConnection, err error) {
	p.getSystemUserUsernameIfNeed()
	p.getSystemUserAuthOrManualSet()
	done := make(chan struct{})
	defer func() {
		utils.IgnoreErrWriteString(p.UserConn, "\r\n")
		close(done)
	}()
	go p.sendConnectingMsg(done, config.GetConf().SSHTimeout*time.Second)
	if p.Asset.Protocol == "telnet" {
		return p.getTelnetConn()
	} else {
		return p.getSSHConn(false)
	}
}

// sendConnectingMsg 发送连接信息
func (p *ProxyServer) sendConnectingMsg(done chan struct{}, delayDuration time.Duration) {
	delay := 0.0
	msg := fmt.Sprintf(i18n.T("Connecting to %s@%s  %.1f"), p.SystemUser.Username, p.Asset.Ip, delay)
	utils.IgnoreErrWriteString(p.UserConn, msg)
	for int(delay) < int(delayDuration/time.Second) {
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

// preCheckRequisite 检查是否满足条件
func (p *ProxyServer) preCheckRequisite() (ok bool) {
	if !p.checkProtocolMatch() {
		msg := utils.WrapperWarn(i18n.T("System user <%s> and asset <%s> protocol are inconsistent."))
		msg = fmt.Sprintf(msg, p.SystemUser.Username, p.Asset.Hostname)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		return
	}
	if p.checkProtocolIsGraph() {
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

// Proxy 代理
func (p *ProxyServer) Proxy() {
	if !p.preCheckRequisite() {
		return
	}
	// 先从cache中获取srv连接, 如果没有获得，则连接
	srvConn, err := p.getServerConnFromCache()
	if err != nil || srvConn == nil {
		srvConn, err = p.getServerConn()
	}

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
	AddSession(sw)
	_ = sw.Bridge(p.UserConn, srvConn)
	defer func() {
		_ = srvConn.Close()
		p.finishSession(sw)
		RemoveSession(sw)
	}()
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
	logger.Debugf("Finish session: %s", s.Id)
}

func (p *ProxyServer) GetFilterRules() []model.SystemUserFilterRule {
	cmdRules, err := service.GetSystemUserFilterRules(p.SystemUser.Id)
	if err != nil {
		logger.Error("Get system user filter rule error: ", err)
	}
	return cmdRules
}
