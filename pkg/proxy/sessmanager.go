package proxy

import (
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
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


func AddSession(sw Session) {
	lock.Lock()
	defer lock.Unlock()
	sessionMap[sw.SessionID()] = sw
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

func CreateCommonSwitch(p proxyEngine) (s *commonSwitch, ok bool) {
	s = NewCommonSwitch(p)
	ok = postSession(s.MapData())
	if ok {
		AddSession(s)
	}
	return s, ok
}

func RemoveCommonSwitch(s *commonSwitch) {
	lock.Lock()
	defer lock.Unlock()
	delete(sessionMap, s.ID)
	finishSession(s.MapData())
	logger.Infof("Session %s has finished", s.ID)
}
