package proxy

import (
	"fmt"
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
		logger.Debugf("Database %s login mode is: %s", p.Database.Name, model.LoginModeManual)
	}
	if p.SystemUser.Password == "" {
		needManualSet = true
		logger.Debugf("Database  %s neither has password", p.Database.Name)
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

func (p *DBProxyServer) getUsernameIfNeed() (err error) {
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

func (p *DBProxyServer) checkProtocolMatch() bool {
	return strings.EqualFold(p.Database.DBType, p.SystemUser.Protocol)
}

func (p *DBProxyServer) checkProtocolClientInstalled() bool {
	switch strings.ToLower(p.Database.DBType) {
	case "mysql":
		return utils.IsInstalledMysqlClient()
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
	msg := fmt.Sprintf(i18n.T("Database connecting to %s %.1f"), p.Database, delay)
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
		return
	}
	if !p.checkProtocolClientInstalled() {
		msg := utils.WrapperWarn(i18n.T("Database %s protocol client not installed."))
		msg = fmt.Sprintf(msg, p.Database.DBType)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		return
	}
	if !p.validatePermission() {
		msg := fmt.Sprintf("You don't have permission login %s", p.Database.Name)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		return
	}
	if err := p.checkRequiredAuth(); err != nil {
		msg := fmt.Sprintf("You get database %s auth info err: %s", p.Database.Name, err)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		return
	}
	return true
}

func (p *DBProxyServer) checkRequiredAuth() error {
	info := service.GetSystemUserDatabaseAuthInfo(p.SystemUser.ID)
	p.SystemUser.Password = info.Password
	if err := p.getUsernameIfNeed(); err != nil {
		logger.Errorf("Get database %s auth username err: %s", p.Database.Name, err)
		return err
	}

	if err := p.getAuthOrManualSet(); err != nil {
		logger.Errorf("Get database %s auth password err: %s", p.Database.Name, err)
		return err
	}
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
		logger.Error("Check requisite failed")
		return
	}
	srvConn, err := p.getServerConn()
	// 连接后端服务器失败
	if err != nil {
		logger.Errorf("Create database server conn failed: %s", err)
		p.sendConnectErrorMsg(err)
		return
	}
	defer srvConn.Close()
	// 创建Session
	sw, err := CreateDBSession(p)
	if err != nil {
		logger.Error("Create database Session failed")
		return
	}
	defer RemoveDBSession(sw)

	if err = sw.Bridge(p.UserConn, srvConn); err != nil {
		logger.Errorf("DB Session %s bridge end: %s", sw.ID, err)
	}

}
