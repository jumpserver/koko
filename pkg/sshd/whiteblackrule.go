package sshd

import (
	"regexp"
)

const (
	actionDeny  = true
	actionAllow = false
)

type RuleFilter interface {

	// 判断是否是匹配当前规则
	Match(string) bool

	// 是否阻断命令
	BlockCommand() bool
}

type Rule struct {
	priority int

	ruleType string

	contents []string

	action bool
}

func (w *Rule) Match(s string) bool {
	switch w.ruleType {
	case "command":

		for _, content := range w.contents {
			if content == s {
				return true
			}
		}
		return false
	default:
		for _, content := range w.contents {
			if matched, _ := regexp.MatchString(content, s); matched {
				return true
			}
		}
		return false
	}

}

func (w *Rule) BlockCommand() bool {
	return w.action
}
