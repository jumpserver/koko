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

	cacheSSHConnection *srvconn.ServerSSHConnection
}

// getSystemUserAuthOrManualSet 获取系统用户的认证信息或手动设置
func (p *ProxyServer) getSystemUserAuthOrManualSet() error {
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
			return err
		}
		p.SystemUser.Password = line
		logger.Debug("Get password from user input: ", line)
	}
	return nil
}

// getSystemUserUsernameIfNeed 获取系统用户用户名，或手动设置
func (p *ProxyServer) getSystemUserUsernameIfNeed() (err error) {
	if p.SystemUser.Username == "" {
		var username string
		term := utils.NewTerminal(p.UserConn, "username: ")
		for {
			username, err = term.ReadLine()
			if err != nil {
				return err
			}
			username = strings.TrimSpace(username)
			if username != "" {
				break
			}
		}
		p.SystemUser.Username = username
		logger.Debug("Get username from user input: ", username)
	}
	return
}

func (p *ProxyServer) getSystemUserBasicInfo() {
	if p.SystemUser.UsernameSameWithUser {
		p.SystemUser.Username = p.User.Username
		logger.Infof("SystemUser username same with user: %s", p.User.Username)
	}
	var info model.SystemUserAuthInfo
	if p.SystemUser.UsernameSameWithUser {
		info = service.GetUserAssetAuthInfo(p.SystemUser.ID, p.Asset.ID, p.User.ID, p.User.Username)
	} else {
		info = service.GetSystemUserAssetAuthInfo(p.SystemUser.ID, p.Asset.ID)
	}
	p.SystemUser.Password = info.Password
	p.SystemUser.PrivateKey = info.PrivateKey
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
func (p *ProxyServer) getSSHConn() (srvConn *srvconn.ServerSSHConnection, err error) {
	conf := config.GetConf()
	newClient, err := srvconn.NewClient(p.User, p.Asset, p.SystemUser,
		conf.SSHTimeout*time.Second, conf.ReuseConnection)
	if err != nil {
		logger.Errorf("User %s create ssh client (%s@%s) err: %s",
			p.User.Name, p.SystemUser.Name, p.Asset.Hostname, err)
		return nil, err
	}
	sess, err := newClient.NewSession()
	if err != nil {
		logger.Errorf("User %s ssh client (%s@%s) create session closed err: %s",
			p.User.Name, p.SystemUser.Name, p.Asset.Hostname, err)
		return nil, err
	}
	pty := p.UserConn.Pty()
	srvConn = srvconn.NewServerSSHConnection(sess)
	err = srvConn.Connect(pty.Window.Height, pty.Window.Width, pty.Term)
	go func() {
		_ = sess.Wait()
		logger.Infof("User %s ssh client(%s@%s) session closed.",
			p.User.Name, p.SystemUser.Name, p.Asset.Hostname)
		_ = newClient.Close()
		logger.Infof("User %s ssh client(%s@%s) recycled and current ref: %d",
			p.User.Name, p.SystemUser.Name, p.Asset.Hostname, newClient.RefCount())
	}()
	if err != nil {
		logger.Errorf("User %s ssh client(%s@%s) start shell err: %s",
			p.User.Name, p.SystemUser.Name, p.Asset.Hostname, err)
		return nil, err
	}
	logger.Infof("User %s ssh client(%s@%s) start shell success.",
		p.User.Name, p.SystemUser.Name, p.Asset.Hostname)
	return
}

func (p *ProxyServer) getCacheSSHConn() (srvConn *srvconn.ServerSSHConnection, ok bool) {
	key := srvconn.MakeReuseSSHClientKey(p.User, p.Asset, p.SystemUser)
	if cacheSSHClient, ok := srvconn.GetClientFromCache(key); ok {
		logger.Infof("User %s get cache ssh client(%s@%s)",
			p.User.Name, p.SystemUser.Name, p.Asset.Hostname)
		sess, err1 := cacheSSHClient.NewSession()
		if err1 != nil {
			logger.Errorf("User %s cache ssh client(%s@%s) create session err: %s",
				p.User.Name, p.SystemUser.Name, p.Asset.Hostname, err1)
			return nil, false
		}
		pty := p.UserConn.Pty()
		srvConn := srvconn.NewServerSSHConnection(sess)
		err2 := srvConn.Connect(pty.Window.Height, pty.Window.Width, pty.Term)
		go func() {
			_ = sess.Wait()
			logger.Infof("User %s reuse ssh client(%s@%s) created session closed.",
				p.User.Name, p.SystemUser.Name, p.Asset.Hostname)
			_ = cacheSSHClient.Close()
			logger.Infof("User %s reuse ssh client(%s@%s) recycled and current ref: %d",
				p.User.Name, p.SystemUser.Name, p.Asset.Hostname, cacheSSHClient.RefCount())
		}()
		if err2 != nil {
			logger.Errorf("User %s reuse ssh client(%s@%s) start shell err: %s",
				p.User.Name, p.SystemUser.Name, p.Asset.Hostname, err2)
			return nil, false
		}
		logger.Infof("User %s reuse ssh client(%s@%s) start shell success",
			p.User.Name, p.SystemUser.Name, p.Asset.Hostname)

		reuseMsg := fmt.Sprintf(i18n.T("Reuse SSH connections (%s@%s) [Number of connections: %d]"),
			p.SystemUser.Username, p.Asset.Hostname, cacheSSHClient.RefCount())
		utils.IgnoreErrWriteString(p.UserConn, reuseMsg+"\r\n")
		return srvConn, true
	}
	logger.Errorf("User %s did not found cache ssh client(%s@%s)",
		p.User.Name, p.SystemUser.Name, p.Asset.Hostname)
	return nil, false
}

// getTelnetConn 获取telnet连接
func (p *ProxyServer) getTelnetConn() (srvConn *srvconn.ServerTelnetConnection, err error) {
	conf := config.GetConf()
	cusString := conf.TelnetRegex
	pattern, err := regexp.Compile(cusString)
	if err != nil {
		logger.Errorf("telnet custom regex %s compile err: %s", cusString, err)
	}
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

// getServerConn 获取获取server连接
func (p *ProxyServer) getServerConn() (srvConn srvconn.ServerConnection, err error) {
	if p.cacheSSHConnection != nil {
		return p.cacheSSHConnection, nil
	}
	done := make(chan struct{})
	defer func() {
		utils.IgnoreErrWriteString(p.UserConn, "\r\n")
		close(done)
	}()
	go p.sendConnectingMsg(done, config.GetConf().SSHTimeout*time.Second)
	if p.SystemUser.Protocol == "telnet" {
		return p.getTelnetConn()
	} else {
		return p.getSSHConn()
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
	if err := p.checkRequiredSystemUserInfo(); err != nil {
		msg := fmt.Sprintf("You get asset %s systemuser info err: %s", p.Asset.Hostname, err)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		return
	}
	return true
}

func (p *ProxyServer) checkRequiredSystemUserInfo() error {
	p.getSystemUserBasicInfo()
	if err := p.getSystemUserUsernameIfNeed(); err != nil {
		logger.Errorf("Get asset %s systemuser username err: %s", p.Asset.Hostname, err)
		return err
	}
	if p.checkRequireReuseClient() {
		if cacheSSHConnection, ok := p.getCacheSSHConn(); ok {
			p.cacheSSHConnection = cacheSSHConnection
			return nil
		}
	}
	if err := p.getSystemUserAuthOrManualSet(); err != nil {
		logger.Errorf("Get asset %s systemuser password/PrivateKey err: %s", p.Asset.Hostname, err)
		return err
	}
	return nil
}

func (p *ProxyServer) checkRequireReuseClient() bool {
	if config.GetConf().ReuseConnection && p.SystemUser.Protocol == "ssh" {
		return true
	}
	return false
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
}

// Proxy 代理
func (p *ProxyServer) Proxy() {
	if !p.preCheckRequisite() {
		return
	}

	// 创建Session
	sw, err := CreateSession(p)
	if err != nil {
		logger.Errorf("Request %s: Create session failed: %s", p.UserConn.ID(), err.Error())
		return
	}
	defer RemoveSession(sw)
	srvConn, err := p.getServerConn()
	// 连接后端服务器失败
	if err != nil {
		p.sendConnectErrorMsg(err)
		return
	}

	logger.Infof("Session %s bridge start", sw.ID)
	_ = sw.Bridge(p.UserConn, srvConn)
	logger.Infof("Session %s bridge end", sw.ID)
}
