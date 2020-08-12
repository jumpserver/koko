package proxy

import (
	"bytes"
	"fmt"
	"os/exec"
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

var _ proxyEngine = (*DBProxyServer)(nil)

type DBProxyServer struct {
	UserConn   UserConnection
	User       *model.User
	Database   *model.Database
	SystemUser *model.SystemUser
}

func (p *DBProxyServer) getAuthOrManualSet() error {
	needManualSet := false
	if p.SystemUser.LoginMode == model.LoginModeManual {
		needManualSet = true
		logger.Debugf("Conn[%s] Database %s login mode is: %s",
			p.UserConn.ID(), p.Database.Name, model.LoginModeManual)
	}
	if p.SystemUser.Password == "" {
		needManualSet = true
		logger.Debugf("Conn[%s] Database %s neither has password",
			p.UserConn.ID(), p.Database.Name)
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

func (p *DBProxyServer) getUsernameIfNeed() (err error) {
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

func (p *DBProxyServer) checkProtocolMatch() bool {
	return strings.EqualFold(p.Database.DBType, p.SystemUser.Protocol)
}

func (p *DBProxyServer) checkProtocolClientInstalled() bool {
	switch strings.ToLower(p.Database.DBType) {
	case "mysql":
		return IsInstalledMysqlClient()
	}

	return false
}

// validatePermission 检查是否有权限连接
func (p *DBProxyServer) validatePermission() bool {
	return service.ValidateUserDatabasePermission(p.User.ID, p.Database.ID, p.SystemUser.ID)
}

// getSSHConn 获取ssh连接
func (p *DBProxyServer) getMysqlConn() (srvConn *srvconn.ServerMysqlConnection, err error) {
	srvConn = srvconn.NewMysqlServer(
		srvconn.SqlHost(p.Database.Host),
		srvconn.SqlPort(p.Database.Port),
		srvconn.SqlUsername(p.SystemUser.Username),
		srvconn.SqlPassword(p.SystemUser.Password),
		srvconn.SqlDBName(p.Database.DBName),
	)
	err = srvConn.Connect()
	return
}

// getServerConn 获取获取server连接
func (p *DBProxyServer) getServerConn() (srvConn srvconn.ServerConnection, err error) {
	done := make(chan struct{})
	defer func() {
		utils.IgnoreErrWriteString(p.UserConn, "\r\n")
		close(done)
	}()
	go p.sendConnectingMsg(done, config.GetConf().SSHTimeout*time.Second)
	return p.getMysqlConn()
}

// sendConnectingMsg 发送连接信息
func (p *DBProxyServer) sendConnectingMsg(done chan struct{}, delayDuration time.Duration) {
	delay := 0.0
	msg := fmt.Sprintf(i18n.T("Connecting to Database %s %.1f"), p.Database, delay)
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
func (p *DBProxyServer) preCheckRequisite() (ok bool) {
	if !p.checkProtocolMatch() {
		msg := utils.WrapperWarn(i18n.T("System user <%s> and database <%s> protocol are inconsistent."))
		msg = fmt.Sprintf(msg, p.SystemUser.Username, p.Database.DBType)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Errorf("Conn[%s] checking protocol matched failed: %s", p.UserConn.ID(), msg)
		return
	}
	logger.Infof("Conn[%s] System user and asset protocol matched", p.UserConn.ID())
	if !p.checkProtocolClientInstalled() {
		msg := utils.WrapperWarn(i18n.T("Database %s protocol client not installed."))
		msg = fmt.Sprintf(msg, p.Database.DBType)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Errorf("Conn[%s] checking permission failed.", p.UserConn.ID())
		return
	}
	logger.Infof("Conn[%s] System user protocol %s supported", p.UserConn.ID(), p.SystemUser.Protocol)
	if !p.validatePermission() {
		msg := fmt.Sprintf("You don't have permission login %s", p.Database.Name)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		return
	}
	logger.Infof("Conn[%s] has permission to access database %s", p.UserConn.ID(), p.Database.Name)
	if err := p.checkRequiredAuth(); err != nil {
		msg := fmt.Sprintf("You get database %s auth info err: %s", p.Database.Name, err)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Errorf("Conn[%s] get system user info failed: %s", p.UserConn.ID(), err)
		return
	}
	return true
}

func (p *DBProxyServer) checkRequiredAuth() error {
	info := service.GetSystemUserDatabaseAuthInfo(p.SystemUser.ID)
	p.SystemUser.Password = info.Password
	logger.Infof("Conn[%s] get database %s auth info from core server success",
		p.UserConn.ID(), p.Database.Name)
	if err := p.getUsernameIfNeed(); err != nil {
		logger.Errorf("Conn[%s] get database %s auth username err: %s",
			p.UserConn.ID(), p.Database.Name, err)
		return err
	}

	if err := p.getAuthOrManualSet(); err != nil {
		logger.Errorf("Conn[%s] get database %s auth password err: %s",
			p.UserConn.ID(), p.Database.Name, err)
		return err
	}
	logger.Infof("Conn[%s] get systemUser auth success", p.UserConn.ID())
	return nil
}

// sendConnectErrorMsg 发送连接错误消息
func (p *DBProxyServer) sendConnectErrorMsg(err error) {
	msg := fmt.Sprintf("Connect database %s error: %s\r\n", p.Database.Host, err)
	utils.IgnoreErrWriteString(p.UserConn, msg)
	logger.Error(msg)
}

// Proxy 代理
func (p *DBProxyServer) Proxy() {
	if !p.preCheckRequisite() {
		logger.Errorf("Conn[%s] Check requisite failed", p.UserConn.ID())
		return
	}
	logger.Infof("Conn[%s] checking pre requisite success", p.UserConn.ID())
	// 创建Session
	sw, ok := CreateCommonSwitch(p)
	if !ok {
		msg := i18n.T("Create database session failed")
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Error(msg)
		return
	}
	logger.Infof("Conn[%s] create database session %s success", p.UserConn.ID(), sw.ID)
	defer RemoveCommonSwitch(sw)
	srvConn, err := p.getServerConn()
	// 连接后端服务器失败
	if err != nil {
		logger.Errorf("Conn[%s] create database conn failed: %s", p.UserConn.ID(), err)
		p.sendConnectErrorMsg(err)
		return
	}
	logger.Infof("Conn[%s] get database conn success", p.UserConn.ID())
	_ = sw.Bridge(p.UserConn, srvConn)
	logger.Infof("Conn[%s] end database session %s bridge", p.UserConn.ID(), sw.ID)

}

func (p *DBProxyServer) GenerateRecordCommand(s *commonSwitch, input, output string,
	riskLevel int64) *model.Command {
	return &model.Command{
		SessionID:  s.ID,
		OrgID:      p.Database.OrgID,
		Input:      input,
		Output:     output,
		User:       fmt.Sprintf("%s (%s)", p.User.Name, p.User.Username),
		Server:     p.Database.Name,
		SystemUser: p.SystemUser.Username,
		Timestamp:  time.Now().Unix(),
		RiskLevel:  riskLevel,
	}
}

func (p *DBProxyServer) NewParser(s *commonSwitch) ParseEngine {
	dbParser := newDBParser(s.ID)
	msg := i18n.T("Create database session failed")
	if cmdRules, err := service.GetSystemUserFilterRules(p.SystemUser.ID); err == nil {
		dbParser.SetCMDFilterRules(cmdRules)
	} else {
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Error(msg + err.Error())
	}
	return &dbParser
}

func (p *DBProxyServer) MapData(s *commonSwitch) map[string]interface{} {
	var dataEnd interface{}
	if s.DateEnd != "" {
		dataEnd = s.DateEnd
	}
	return map[string]interface{}{
		"id":             s.ID,
		"user":           fmt.Sprintf("%s (%s)", p.User.Name, p.User.Username),
		"asset":          p.Database.Name,
		"org_id":         p.Database.OrgID,
		"login_from":     p.UserConn.LoginFrom(),
		"system_user":    p.SystemUser.Username,
		"protocol":       p.SystemUser.Protocol,
		"remote_addr":    p.UserConn.RemoteAddr(),
		"is_finished":    s.finished,
		"date_start":     s.DateStart,
		"date_end":       dataEnd,
		"user_id":        p.User.ID,
		"asset_id":       p.Database.ID,
		"system_user_id": p.SystemUser.ID,
		"is_success":     s.isConnected,
	}
}

func IsInstalledMysqlClient() bool {
	checkLine := "mysql -V"
	cmd := exec.Command("bash", "-c", checkLine)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		logger.Errorf("check mysql client installed failed: %s", err, out)
		return false
	}
	if bytes.HasPrefix(out, []byte("mysql")) {
		return true
	}
	logger.Errorf("check mysql client installed failed: %s", out)
	return false
}
