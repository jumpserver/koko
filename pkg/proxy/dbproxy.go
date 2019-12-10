package proxy

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

type DBProxyServer struct {
	UserConn UserConnection
	User     *model.User
	Database *model.Database
}

func (p *DBProxyServer) getAuthOrManualSet() error {
	needManualSet := false
	if p.Database.LoginMode == model.LoginModeManual {
		needManualSet = true
		logger.Debugf("Database %s login mode is: %s", p.Database.Name, model.LoginModeManual)
	}
	if p.Database.Password == "" {
		needManualSet = true
		logger.Debugf("Database  %s neither has password", p.Database.Name)
	}
	if needManualSet {
		term := utils.NewTerminal(p.UserConn, "password: ")
		line, err := term.ReadPassword(fmt.Sprintf("%s's password: ", p.Database.Username))
		if err != nil {
			logger.Errorf("Get password from user err %s", err.Error())
			return err
		}
		p.Database.Password = line
		logger.Debug("Get password from user input: ", line)
	}
	return nil
}

func (p *DBProxyServer) getUsernameIfNeed() (err error) {
	if p.Database.Username == "" {
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
		p.Database.Username = username
		logger.Debug("Get username from user input: ", username)
	}
	return
}

// checkProtocolMatch 检查协议是否匹配
func (p *DBProxyServer) checkProtocolMatch() bool {
	return p.Database.DBType == "mysql"
}

// validatePermission 检查是否有权限连接
func (p *DBProxyServer) validatePermission() bool {
	return true
}

// getSSHConn 获取ssh连接
func (p *DBProxyServer) getMysqlConn() (srvConn *srvconn.ServerMysqlConnection, err error) {
	port, err := strconv.Atoi(p.Database.Port)
	if err != nil {
		return
	}
	srvConn = srvconn.NewMysqlServer(
		srvconn.SqlHost(p.Database.Host),
		srvconn.SqlPort(port),
		srvconn.SqlUsername(p.Database.Username),
		srvconn.SqlPassword(p.Database.Password),
		srvconn.SqlDBName(p.Database.DBName),
	)
	err = srvConn.Connect()
	return
}

// getServerConn 获取获取server连接
func (p *DBProxyServer) getServerConn() (srvConn srvconn.ServerConnection, err error) {

	return p.getMysqlConn()
}

// sendConnectingMsg 发送连接信息
func (p *DBProxyServer) sendConnectingMsg(done chan struct{}, delayDuration time.Duration) {
	delay := 0.0
	msg := fmt.Sprintf(i18n.T("Connecting to %s@%s  %.1f"), p.Database.Username, p.Database.Host, delay)
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
		msg := utils.WrapperWarn(i18n.T("Database %s protocol %s are inconsistent."))
		msg = fmt.Sprintf(msg, p.Database.Username, p.Database.Host)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		return
	}
	if !p.validatePermission() {
		msg := fmt.Sprintf("You don't have permission login %s@%s", p.Database.Username, p.Database.Host)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		return
	}
	if err := p.checkRequiredSystemUserInfo(); err != nil {
		msg := fmt.Sprintf("You get asset %s systemuser info err: %s", p.Database.Host, err)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		return
	}
	return true
}

func (p *DBProxyServer) checkRequiredSystemUserInfo() error {
	if err := p.getUsernameIfNeed(); err != nil {
		logger.Errorf("Get asset %s systemuser username err: %s", p.Database.Username, err)
		return err
	}

	if err := p.getAuthOrManualSet(); err != nil {
		logger.Errorf("Get asset %s systemuser password/PrivateKey err: %s", p.Database.Host, err)
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
		return
	}
	// 创建Session
	sw := DBSwitchSession{
		p: p,
	}
	sw.Initial()
	srvConn, err := p.getServerConn()
	// 连接后端服务器失败
	if err != nil {
		p.sendConnectErrorMsg(err)
		return
	}
	_ = sw.Bridge(p.UserConn, srvConn)
}
