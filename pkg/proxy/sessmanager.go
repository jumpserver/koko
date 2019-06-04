package proxy

import (
	"cocogo/pkg/i18n"
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
	"cocogo/pkg/service"
	"cocogo/pkg/utils"
	"sync"
	"time"
)

var sessionMap = make(map[string]*SwitchSession)
var lock = new(sync.RWMutex)

func HandleSessionTask(task model.TerminalTask) {
	switch task.Name {
	case "kill_session":
		KillSession(task.Args)
		service.FinishTask(task.Id)
	default:

	}
}

func KillSession(sessionID string) {
	lock.RLock()
	defer lock.RUnlock()
	if sw, ok := sessionMap[sessionID]; ok {
		sw.Terminate()
	}
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
	delete(sessionMap, sw.Id)
	finishSession(sw)
}

func AddSession(sw *SwitchSession) {
	lock.Lock()
	defer lock.Unlock()
	sessionMap[sw.Id] = sw
}

func CreateSession(p *ProxyServer) (sw *SwitchSession, err error) {
	// 创建Session
	sw = NewSwitchSession(p)
	// Post到Api端
	ok := postSession(sw)
	if !ok {
		msg := i18n.T("Connect with api server failed")
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Error(msg)
		return
	}
	// 获取系统用户的过滤规则，并设置
	cmdRules, err := service.GetSystemUserFilterRules(p.SystemUser.Id)
	if err != nil {
		msg := i18n.T("Connect with api server failed")
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Error(msg + err.Error())
	}
	sw.SetFilterRules(cmdRules)
	AddSession(sw)
	return
}

func postSession(s *SwitchSession) bool {
	data := s.MapData()
	for i := 0; i < 5; i++ {
		if service.CreateSession(data) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func finishSession(s *SwitchSession) {
	data := s.MapData()
	service.FinishSession(data)
	service.FinishReply(s.Id)
	logger.Debugf("Finish session: %s", s.Id)
}
