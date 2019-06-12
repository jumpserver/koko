package proxy

import (
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/utils"
)

var sessionMap = make(map[string]*SwitchSession)
var lock = new(sync.RWMutex)

func HandleSessionTask(task model.TerminalTask) {
	switch task.Name {
	case "kill_session":
		KillSession(task.Args)
		service.FinishTask(task.ID)
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
	delete(sessionMap, sw.ID)
	finishSession(sw)
}

func AddSession(sw *SwitchSession) {
	lock.Lock()
	defer lock.Unlock()
	sessionMap[sw.ID] = sw
}

func CreateSession(p *ProxyServer) (sw *SwitchSession, err error) {
	// 创建Session
	sw = NewSwitchSession(p)
	// Post到Api端
	ok := postSession(sw)
	msg := i18n.T("Connect with api server failed")
	if !ok {
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Error(msg)
		return
	}
	// 获取系统用户的过滤规则，并设置
	cmdRules, err := service.GetSystemUserFilterRules(p.SystemUser.ID)
	if err != nil {
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
	service.FinishReply(s.ID)
	logger.Debugf("Finish session: %s", s.ID)
}
