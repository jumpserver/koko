package proxy

import (
	"sync"
)

var (
	sessManager = newSessionManager()
)

func GetSessionById(id string) (s *SwitchSession, ok bool) {
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

		cmdTunnelData: make(map[string]func()),
	}
}

func AddCommandSession(id string, cancelFunc func()) {
	sessManager.AddCommandSession(id, cancelFunc)
}

func RemoveCommandSession(id string) {
	sessManager.RemoveCommandSession(id)
}

func GetCommandSession(id string) (cancelFunc func(), ok bool) {
	return sessManager.GetCommandSession(id)
}

type sessionManager struct {
	data map[string]*SwitchSession
	sync.Mutex

	cmdTunnelData map[string]func()
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

func (s *sessionManager) AddCommandSession(id string, cancelFunc func()) {
	s.Lock()
	defer s.Unlock()
	s.cmdTunnelData[id] = cancelFunc
}

func (s *sessionManager) RemoveCommandSession(id string) {
	s.Lock()
	defer s.Unlock()
	delete(s.cmdTunnelData, id)
}

func (s *sessionManager) GetCommandSession(id string) (cancelFunc func(), ok bool) {
	s.Lock()
	defer s.Unlock()
	cancelFunc, ok = s.cmdTunnelData[id]
	return
}

func (s *sessionManager) Range() []string {
	s.Lock()
	defer s.Unlock()
	sids := make([]string, 0, len(s.data)+len(s.cmdTunnelData))
	for sid := range s.data {
		sids = append(sids, sid)
	}
	for sid := range s.cmdTunnelData {
		sids = append(sids, sid)
	}
	return sids
}
