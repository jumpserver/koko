package proxy

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

type ProxyServer struct {
	UserConn   UserConnection
	User       *model.User
	Asset      *model.Asset
	SystemUser *model.SystemUser
}

// getSystemUserAuthOrManualSet 获取系统用户的认证信息或手动设置
func (p *ProxyServer) getSystemUserAuthOrManualSet() {
	info := service.GetSystemUserAssetAuthInfo(p.SystemUser.ID, p.Asset.ID)
	p.SystemUser.Password = info.Password
	p.SystemUser.PrivateKey = info.PrivateKey
	needManualSet := false
	if p.SystemUser.LoginMode == model.LoginModeManual {
		needManualSet = true
		logger.Debugf("System user %s login mode is: %s", p.SystemUser.Name, model.LoginModeManual)
	}
	if p.SystemUser.Password == "" && p.SystemUser.PrivateKey == "" {
		needManualSet = true
		logger.Debugf("System user %s neither has password nor private key", p.SystemUser.Name)
	}
	if needManualSet {
		term := utils.NewTerminal(p.UserConn, "password: ")
		line, err := term.ReadPassword(fmt.Sprintf("%s's password: ", p.SystemUser.Username))
		if err != nil {
			logger.Errorf("Get password from user err %s", err.Error())
		}
		p.SystemUser.Password = line
		logger.Debug("Get password from user input: ", line)
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
	return p.Asset.IsSupportProtocol(p.SystemUser.Protocol)
}

// checkProtocolIsGraph 检查协议是否是图形化的
func (p *ProxyServer) checkProtocolIsGraph() bool {
	switch p.SystemUser.Protocol {
	case "ssh", "telnet":
		return false
	default:
		return true
	}
}

// validatePermission 检查是否有权限连接
func (p *ProxyServer) validatePermission() bool {
	return service.ValidateUserAssetPermission(
		p.User.ID, p.Asset.ID, p.SystemUser.ID, "connect",
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
	msg := fmt.Sprintf(i18n.T("Connecting to %s@%s  %.1f"), p.SystemUser.Username, p.Asset.IP, delay)
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

// sendConnectErrorMsg 发送连接错误消息
func (p *ProxyServer) sendConnectErrorMsg(err error) {
	msg := fmt.Sprintf("Connect asset %s error: %s\r\n", p.Asset.Hostname, err)
	utils.IgnoreErrWriteString(p.UserConn, msg)
	logger.Error(msg)
	password := p.SystemUser.Password
	if password != "" {
		passwordLen := len(p.SystemUser.Password)
		showLen := passwordLen / 2
		hiddenLen := passwordLen - showLen
		msg2 := fmt.Sprintf("Try password: %s", password[:showLen]+strings.Repeat("*", hiddenLen))
		logger.Errorf(msg2)
	}
	return
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

	// 连接后端服务器失败
	if err != nil {
		p.sendConnectErrorMsg(err)
		return
	}
	// 创建Session
	sw, err := CreateSession(p)
	if err != nil {
		return
	}
	_ = sw.Bridge(p.UserConn, srvConn)
	defer func() {
		_ = srvConn.Close()
		RemoveSession(sw)
	}()
}
