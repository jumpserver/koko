package session

import (
	"sync"
)

var (
	sessManager = newSessionManager()
)

func GetSessionById(id string) (s *Session, ok bool) {
	s, ok = sessManager.Get(id)
	return
}

func GetAliveSessions() []string {
	return sessManager.Range()
}

func AddSession(s *Session) {
	sessManager.Add(s.ID, s)
}

func RemoveSession(s *Session) {
	sessManager.Delete(s.ID)
}

func RemoveSessionById(id string) {
	sessManager.Delete(id)
}

func newSessionManager() *sessionManager {
	return &sessionManager{
		data: make(map[string]*Session),
	}
}

type sessionManager struct {
	data map[string]*Session
	sync.Mutex
}

func (s *sessionManager) Add(id string, sess *Session) {
	s.Lock()
	defer s.Unlock()
	s.data[id] = sess
}
func (s *sessionManager) Get(id string) (sess *Session, ok bool) {
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
	s.Lock()
	defer s.Unlock()
	sids := make([]string, 0, len(s.data))
	for sid := range s.data {
		sids = append(sids, sid)
	}

	return sids
}
