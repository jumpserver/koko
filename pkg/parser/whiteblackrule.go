package parser

import (
	"regexp"
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

func (r *Rule) Match(s string) bool {
	switch r.ruleType {
	case "command":
		for _, content := range r.contents {
			if content == s {
				return true
			}
		}
		return false
	default:
		for _, content := range r.contents {
			if matched, _ := regexp.MatchString(content, s); matched {
				return true
			}
		}
		return false
	}

}

func (r *Rule) BlockCommand() bool {
	return r.action
}
