package proxy

import (
	"sync"
)

var sessManager = newSessionManager()

func GetSessionById(id string)(s *SwitchSession, ok bool){
	s, ok = sessManager.Get(id)
	return
}

func GetAliveSessions() []string {
	return sessManager.Range()
}

func AddCommonSwitch(s *SwitchSession) {
	sessManager.Add(s.ID, s)
}

func RemoveCommonSwitch(s *SwitchSession) {
	sessManager.Delete(s.ID)
}

func newSessionManager() *sessionManager {
	return &sessionManager{
		data: make(map[string]*SwitchSession),
	}
}

type sessionManager struct {
	data map[string]*SwitchSession
	sync.Mutex
}

func (s *sessionManager) Add(id string, sess *SwitchSession) {
	s.Lock()
	defer s.Unlock()
	s.data[id] = sess
}
func (s *sessionManager) Get(id string) (sess *SwitchSession, ok bool) {
	s.Lock()
	defer s.Unlock()
	sess, ok = s.data[id]
	return
}

func (s *sessionManager) Delete(id string) {
	s.Lock()
	defer s.Unlock()
	delete(s.data, id)
}

func (s *sessionManager) Range() []string {
	sids := make([]string, 0, len(s.data))
	s.Lock()
	defer s.Unlock()
	for sid := range s.data {
		sids = append(sids, sid)
	}
	return sids
}
