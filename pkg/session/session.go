package session

import "github.com/jumpserver/koko/pkg/jms-sdk-go/model"

func NewSession(s *model.Session, terminalFunc func(task *model.TerminalTask)) *Session {
	return &Session{Session: s, terminalFunc: terminalFunc}
}

type Session struct {
	*model.Session
	terminalFunc func(task *model.TerminalTask)
}

func (s *Session) Terminal(task *model.TerminalTask) {
	if s.terminalFunc != nil {
		s.terminalFunc(task)
	}
}
