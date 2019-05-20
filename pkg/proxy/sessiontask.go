package proxy

import (
	"cocogo/pkg/model"
	"cocogo/pkg/service"
	"sync"
)

var sessionMap = make(map[string]*SwitchSession)
var lock = new(sync.RWMutex)

func HandlerSessionTask(task model.TerminalTask) {
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
}

func AddSession(sw *SwitchSession) {
	lock.Lock()
	defer lock.Unlock()
	sessionMap[sw.Id] = sw
}
