package proxy

import (
	"context"
	"sync"

	"github.com/jumpserver/koko/pkg/model"
)

const (
	StatusQuery = "query"
	StatusStart = "start"
	StatusNone  = "none"
)

type commandConfirmStatus struct {
	Status string
	data   string
	Rule   model.SystemUserFilterRule
	Cmd    string
	sync.Mutex
	wg sync.WaitGroup

	ctx        context.Context
	cancelFunc context.CancelFunc

	action    model.RuleAction
	Processor string
}

func (c *commandConfirmStatus) SetStatus(status string) {
	c.Lock()
	defer c.Unlock()
	c.Status = status
}

func (c *commandConfirmStatus) SetAction(action model.RuleAction) {
	c.Lock()
	defer c.Unlock()
	c.action = action
}

func (c *commandConfirmStatus) GetAction() model.RuleAction {
	c.Lock()
	defer c.Unlock()
	return c.action
}

func (c *commandConfirmStatus) SetProcessor(processor string) {
	c.Lock()
	defer c.Unlock()
	c.Processor = processor
}

func (c *commandConfirmStatus) SetRule(rule model.SystemUserFilterRule) {
	c.Lock()
	defer c.Unlock()
	c.Rule = rule
}

func (c *commandConfirmStatus) SetData(data string) {
	c.Lock()
	defer c.Unlock()
	c.data = data
}

func (c *commandConfirmStatus) SetCmd(cmd string) {
	c.Lock()
	defer c.Unlock()
	c.Cmd = cmd
}

func (c *commandConfirmStatus) ResetCtx() {
	c.Lock()
	defer c.Unlock()
	c.ctx, c.cancelFunc = context.WithCancel(context.Background())
}

func (c *commandConfirmStatus) InRunning() bool {
	c.Lock()
	defer c.Unlock()
	switch c.Status {
	case StatusStart:
		return true
	}
	return false
}

func (c *commandConfirmStatus) InQuery() bool {
	c.Lock()
	defer c.Unlock()
	switch c.Status {
	case StatusQuery:
		return true
	}
	return false
}

func (c *commandConfirmStatus) IsNeedCancel(b []byte) bool {
	if len(b) > 0 {
		switch b[0] {
		case CtrlC, CtrlD:
			c.cancelFunc()
			c.wg.Wait()
			return true
		}
	}
	return false
}

const (
	CtrlC = 3
	CtrlD = 4
)
