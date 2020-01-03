package proxy

import (
	"errors"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/utils"
)

var sessionMap = make(map[string]Session)
var lock = new(sync.RWMutex)

type Session interface {
	SessionID() string
	Terminate()
}

func HandleSessionTask(task model.TerminalTask) {
	switch task.Name {
	case "kill_session":
		if ok := KillSession(task.Args); ok {
			service.FinishTask(task.ID)
		}
	default:

	}
}

func KillSession(sessionID string) bool {
	lock.RLock()
	defer lock.RUnlock()
	if sw, ok := sessionMap[sessionID]; ok {
		sw.Terminate()
		return true
	}
	return false
}

func GetAliveSessions() []string {
	lock.RLock()
	defer lock.RUnlock()
	sids := make([]string, 0, len(sessionMap))
	for sid := range sessionMap {
		sids = append(sids, sid)
	}
	return sids
}

func RemoveSession(sw *SwitchSession) {
	lock.Lock()
	defer lock.Unlock()
	delete(sessionMap, sw.ID)
	data := sw.MapData()
	finishSession(data)
	logger.Infof("Session %s has finished", sw.ID)
}

func AddSession(sw Session) {
	lock.Lock()
	defer lock.Unlock()
	sessionMap[sw.SessionID()] = sw
}

func CreateSession(p *ProxyServer) (sw *SwitchSession, err error) {
	// 创建Session
	sw = NewSwitchSession(p)
	// Post到Api端
	data := sw.MapData()
	ok := postSession(data)
	msg := i18n.T("Connect with api server failed")
	if !ok {
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Error(msg)
		return sw, errors.New("connect api server failed")
	}
	// 获取系统用户的过滤规则，并设置
	cmdRules, err := service.GetSystemUserFilterRules(p.SystemUser.ID)
	if err != nil {
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Error(msg + err.Error())
		return sw, errors.New("connect api server failed")
	}
	sw.SetFilterRules(cmdRules)
	AddSession(sw)
	return
}

func postSession(data map[string]interface{}) bool {
	for i := 0; i < 5; i++ {
		if service.CreateSession(data) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func finishSession(data map[string]interface{}) {
	service.FinishSession(data)
}

func CreateDBSession(p *DBProxyServer) (sw *DBSwitchSession, err error) {
	// 创建Session
	sw = &DBSwitchSession{
		p: p,
	}
	sw.Initial()
	data := sw.MapData()
	ok := postSession(data)
	msg := i18n.T("Create database session failed")
	if !ok {
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Error(msg)
		return sw, errors.New("create database session failed")
	}
	AddSession(sw)
	return
}

func RemoveDBSession(sw *DBSwitchSession) {
	lock.Lock()
	defer lock.Unlock()
	delete(sessionMap, sw.ID)
	finishSession(sw.MapData())
	logger.Infof("DB Session %s has finished", sw.ID)
}
