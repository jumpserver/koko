package session

import (
	"fmt"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

type TaskFunc func(task *model.TerminalTask) error

func NewSession(s *model.Session, taskFunc TaskFunc) *Session {
	return &Session{Session: s,
		handleTaskFunc: taskFunc,
	}
}

type Session struct {
	*model.Session
	handleTaskFunc func(task *model.TerminalTask) error
}

func (s *Session) HandleTask(task *model.TerminalTask) error {
	if s.handleTaskFunc != nil {
		return s.handleTaskFunc(task)
	}
	return fmt.Errorf("no handle task func")
}
