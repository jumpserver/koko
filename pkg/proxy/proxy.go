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

var _ proxyEngine = (*ProxyServer)(nil)

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
		logger.Debugf("Conn[%s] system user %s login mode is: %s",
			p.UserConn.ID(), p.SystemUser.Name, model.LoginModeManual)
	}
	if p.SystemUser.Password == "" && p.SystemUser.PrivateKey == "" {
		needManualSet = true
		logger.Debugf("Conn[%s] system user %s neither has password nor private key",
			p.UserConn.ID(), p.SystemUser.Name)
	}
	if needManualSet {
		term := utils.NewTerminal(p.UserConn, "password: ")
		line, err := term.ReadPassword(fmt.Sprintf("%s's password: ", p.SystemUser.Username))
		if err != nil {
			logger.Errorf("Conn[%s] get password from user err: %s", p.UserConn.ID(), err.Error())
			return err
		}
		p.SystemUser.Password = line
		logger.Debugf("Conn[%s] get password from user input: %s", p.UserConn.ID(), line)
	}
	return nil
}

// getSystemUserUsernameIfNeed 获取系统用户用户名，或手动设置
func (p *ProxyServer) getSystemUserUsernameIfNeed() (err error) {
	if p.SystemUser.Username == "" {
		logger.Infof("Conn[%s] need manuel input systemuser username", p.UserConn.ID())
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
		logger.Infof("Conn[%s] get username from user input: %s", p.UserConn.ID(), username)
	}
	return
}

func (p *ProxyServer) getSystemUserBasicInfo() {
	logger.Infof("Conn[%s] start to get systemUser auth info from core server", p.UserConn.ID())
	var info model.SystemUserAuthInfo
	if p.SystemUser.UsernameSameWithUser {
		p.SystemUser.Username = p.User.Username
		logger.Infof("Conn[%s] SystemUser username same with user: %s", p.UserConn.ID(), p.User.Username)
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
		logger.Errorf("Conn[%s] create ssh client (%s@%s) err: %s",
			p.UserConn.ID(), p.SystemUser.Name, p.Asset.Hostname, err)
		return nil, err
	}
	sess, err := newClient.NewSession()
	if err != nil {
		logger.Errorf("Conn[%s] ssh client (%s@%s) create session closed err: %s",
			p.UserConn.ID(), p.SystemUser.Name, p.Asset.Hostname, err)
		return nil, err
	}
	pty := p.UserConn.Pty()
	srvConn = srvconn.NewServerSSHConnection(sess,
		srvconn.OptionCharset(p.getAssetCharset()))
	err = srvConn.Connect(pty.Window.Height, pty.Window.Width, pty.Term)
	go func() {
		_ = sess.Wait()
		logger.Infof("Conn[%s] ssh client(%s@%s) session closed.",
			p.UserConn.ID(), p.SystemUser.Name, p.Asset.Hostname)
		_ = newClient.Close()
		logger.Infof("Conn[%s] ssh ssh client(%s@%s) recycled and current ref: %d",
			p.UserConn.ID(), p.SystemUser.Name, p.Asset.Hostname, newClient.RefCount())
	}()
	if err != nil {
		_ = sess.Close()
		logger.Errorf("Conn[%s] ssh client(%s@%s) start shell err: %s",
			p.UserConn.ID(), p.SystemUser.Name, p.Asset.Hostname, err)
		return nil, err
	}
	logger.Infof("User %s ssh client(%s@%s) start shell success.",
		p.User.Name, p.SystemUser.Name, p.Asset.Hostname)
	return
}

func (p *ProxyServer) getCacheSSHConn() (srvConn *srvconn.ServerSSHConnection, ok bool) {
	key := srvconn.MakeReuseSSHClientKey(p.User, p.Asset, p.SystemUser)
	if cacheSSHClient, ok := srvconn.GetClientFromCache(key); ok {
		logger.Infof("Conn[%s] get cache ssh client(%s@%s)",
			p.UserConn.ID(), p.SystemUser.Name, p.Asset.Hostname)
		sess, err1 := cacheSSHClient.NewSession()
		if err1 != nil {
			logger.Errorf("Conn[%s] cache ssh client(%s@%s) create session err: %s",
				p.UserConn.ID(), p.SystemUser.Name, p.Asset.Hostname, err1)
			return nil, false
		}
		pty := p.UserConn.Pty()
		srvConn := srvconn.NewServerSSHConnection(sess,
			srvconn.OptionCharset(p.getAssetCharset()))
		err2 := srvConn.Connect(pty.Window.Height, pty.Window.Width, pty.Term)
		go func() {
			_ = sess.Wait()
			logger.Infof("Conn[%s] reused ssh client(%s@%s) session closed.",
				p.UserConn.ID(), p.SystemUser.Name, p.Asset.Hostname)
			_ = cacheSSHClient.Close()
			logger.Infof("Conn[%s] reused ssh client(%s@%s) recycled and current ref: %d",
				p.UserConn.ID(), p.SystemUser.Name, p.Asset.Hostname, cacheSSHClient.RefCount())
		}()
		if err2 != nil {
			_ = sess.Close()
			logger.Errorf("Conn[%s] reuse ssh client(%s@%s) start shell err: %s",
				p.UserConn.ID(), p.SystemUser.Name, p.Asset.Hostname, err2)
			return nil, false
		}
		logger.Infof("Conn[%s] reuse ssh client(%s@%s) start shell success",
			p.UserConn.ID(), p.SystemUser.Name, p.Asset.Hostname)

		reuseMsg := fmt.Sprintf(i18n.T("Reuse SSH connections (%s@%s) [Number of connections: %d]"),
			p.SystemUser.Username, p.Asset.Hostname, cacheSSHClient.RefCount())
		utils.IgnoreErrWriteString(p.UserConn, reuseMsg+"\r\n")
		return srvConn, true
	}
	logger.Errorf("Conn[%s] did not found cache ssh client(%s@%s)",
		p.UserConn.ID(), p.SystemUser.Name, p.Asset.Hostname)
	return nil, false
}

// getTelnetConn 获取telnet连接
func (p *ProxyServer) getTelnetConn() (srvConn *srvconn.ServerTelnetConnection, err error) {
	conf := config.GetConf()
	cusString := conf.TelnetRegex
	pattern, err := regexp.Compile(cusString)
	if err != nil {
		logger.Errorf("Conn[%s] telnet custom regex %s compile err: %s",
			p.UserConn.ID(), cusString, err)
	}
	srvConn = &srvconn.ServerTelnetConnection{
		User:                 p.User,
		Asset:                p.Asset,
		SystemUser:           p.SystemUser,
		CustomString:         cusString,
		CustomSuccessPattern: pattern,
		Overtime:             time.Duration(conf.SSHTimeout) * time.Second,
		Charset:              p.getAssetCharset(),
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
		logger.Errorf("Conn[%s] checking protocol matched failed: %s", p.UserConn.ID(), msg)
		return
	}
	logger.Infof("Conn[%s] System user and asset protocol matched", p.UserConn.ID())
	if p.checkProtocolIsGraph() {
		msg := i18n.T("Terminal only support protocol ssh/telnet, please use web terminal to access")
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Errorf("Conn[%s] checking requisite failed: %s", p.UserConn.ID(), msg)
		return
	}
	logger.Infof("Conn[%s] System user protocol %s supported", p.UserConn.ID(), p.SystemUser.Protocol)
	if !p.validatePermission() {
		msg := fmt.Sprintf("You don't have permission login %s@%s", p.SystemUser.Username, p.Asset.Hostname)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Errorf("Conn[%s] checking permission failed.", p.UserConn.ID())
		return
	}
	logger.Infof("Conn[%s] has permission to access hostname %s", p.UserConn.ID(), p.Asset.Hostname)
	if err := p.checkRequiredSystemUserInfo(); err != nil {
		msg := fmt.Sprintf("You get asset %s systemuser info err: %s", p.Asset.Hostname, err)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Errorf("Conn[%s] get system user info failed: %s", p.UserConn.ID(), err)
		return
	}
	return true
}

func (p *ProxyServer) checkRequiredSystemUserInfo() error {
	p.getSystemUserBasicInfo()
	logger.Infof("Conn[%s] get systemUser auth info from core server success", p.UserConn.ID())
	if err := p.getSystemUserUsernameIfNeed(); err != nil {
		logger.Errorf("Conn[%s] get asset %s systemUser's username err: %s",
			p.UserConn.ID(), p.Asset.Hostname, err)
		return err
	}
	logger.Infof("Conn[%s] get systemUser's username %s success", p.UserConn.ID(), p.SystemUser.Username)
	if p.checkRequireReuseClient() {
		if cacheSSHConnection, ok := p.getCacheSSHConn(); ok {
			p.cacheSSHConnection = cacheSSHConnection
			logger.Infof("Conn[%s] will use cache SSH conn", p.UserConn.ID())
			return nil
		}
	}
	if err := p.getSystemUserAuthOrManualSet(); err != nil {
		logger.Errorf("Conn[%s] Get asset %s systemuser password/PrivateKey err: %s",
			p.UserConn.ID(), p.Asset.Hostname, err)
		return err
	}
	logger.Infof("Conn[%s] get systemUser password/PrivateKey success", p.UserConn.ID())
	return nil
}

func (p *ProxyServer) checkRequireReuseClient() bool {
	if config.GetConf().ReuseConnection {
		if strings.EqualFold(p.Asset.Platform, "linux") &&
			strings.EqualFold(p.SystemUser.Protocol, "ssh") {
			return true
		}
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

func (p *ProxyServer) getAssetCharset() string {
	platform := service.GetAssetPlatform(p.Asset.ID)
	charset := strings.ToLower(platform.Charset)
	logger.Infof("Conn[%s] asset %s charset use: %s",
		p.UserConn.ID(), p.Asset.Hostname, charset)
	return charset
}

// Proxy 代理
func (p *ProxyServer) Proxy() {
	if !p.preCheckRequisite() {
		return
	}
	logger.Infof("Conn[%s] checking pre requisite success", p.UserConn.ID())
	// 创建Session
	sw, ok := CreateCommonSwitch(p)
	if !ok {
		msg := i18n.T("Connect with api server failed")
		if p.cacheSSHConnection != nil {
			_ = p.cacheSSHConnection.Close()
		}
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Errorf("Conn[%s] submit session %s to core server err: %s", p.UserConn.ID(), sw.ID, msg)
		return
	}
	logger.Infof("Conn[%s] create session %s success", p.UserConn.ID(), sw.ID)
	defer RemoveCommonSwitch(sw)
	srvConn, err := p.getServerConn()
	// 连接后端服务器失败
	if err != nil {
		logger.Errorf("Conn[%s] getting srv conn failed: %s", p.UserConn.ID(), err)
		p.sendConnectErrorMsg(err)
		return
	}
	logger.Infof("Conn[%s] getting srv conn success", p.UserConn.ID())
	_ = sw.Bridge(p.UserConn, srvConn)
	logger.Infof("Conn[%s] end session %s bridge", p.UserConn.ID(), sw.ID)
}

func (p *ProxyServer) MapData(s *commonSwitch) map[string]interface{} {
	var dataEnd interface{}
	if s.DateEnd != "" {
		dataEnd = s.DateEnd
	}
	return map[string]interface{}{
		"id":             s.ID,
		"user":           fmt.Sprintf("%s (%s)", p.User.Name, p.User.Username),
		"asset":          p.Asset.Hostname,
		"org_id":         p.Asset.OrgID,
		"login_from":     p.UserConn.LoginFrom(),
		"system_user":    p.SystemUser.Username,
		"protocol":       p.SystemUser.Protocol,
		"remote_addr":    p.UserConn.RemoteAddr(),
		"is_finished":    s.finished,
		"date_start":     s.DateStart,
		"date_end":       dataEnd,
		"user_id":        p.User.ID,
		"asset_id":       p.Asset.ID,
		"system_user_id": p.SystemUser.ID,
		"is_success":     s.isConnected,
	}
}

func (p *ProxyServer) NewParser(s *commonSwitch) ParseEngine {
	shellParser := newParser(s.ID)
	msg := i18n.T("Create session failed")
	if cmdRules, err := service.GetSystemUserFilterRules(p.SystemUser.ID); err == nil {
		logger.Infof("Conn[%s] get command filter rules success", p.UserConn.ID())
		shellParser.SetCMDFilterRules(cmdRules)
	} else {
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Error(msg + err.Error())
	}
	return &shellParser
}

func (p *ProxyServer) GenerateRecordCommand(s *commonSwitch, input, output string,
	riskLevel int64) *model.Command {
	return &model.Command{
		SessionID:  s.ID,
		OrgID:      p.Asset.OrgID,
		Input:      input,
		Output:     output,
		User:       fmt.Sprintf("%s (%s)", p.User.Name, p.User.Username),
		Server:     p.Asset.Hostname,
		SystemUser: p.SystemUser.Username,
		Timestamp:  time.Now().Unix(),
		RiskLevel:  riskLevel,
	}
}
