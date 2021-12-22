package model

import (
	"regexp"
	"sort"
)

type RuleAction int

const (
	ActionDeny    RuleAction = 0
	ActionAllow   RuleAction = 9
	ActionConfirm RuleAction = 2
	ActionUnknown RuleAction = 3

	TypeRegex = "regex"
	TypeCmd   = "command"
)

type SystemUserFilterRule struct {
	ID        string     `json:"id"`
	Priority  int        `json:"priority"`
	Type      string     `json:"type"`
	Content   string     `json:"content"`
	Action    RuleAction `json:"action"`
	OrgId     string     `json:"org_id"`
	RePattern string     `json:"pattern"` // 已经处理过的正则字符

	pattern  *regexp.Regexp
	compiled bool
}

func (sf *SystemUserFilterRule) Pattern() *regexp.Regexp {
	if sf.compiled {
		return sf.pattern
	}

	pattern, err := regexp.Compile(sf.RePattern)
	if err == nil {
		sf.pattern = pattern
		sf.compiled = true
	}
	return pattern
}

func (sf *SystemUserFilterRule) Match(cmd string) (RuleAction, string) {
	pattern := sf.Pattern()
	if pattern == nil {
		return ActionUnknown, ""
	}
	found := pattern.FindString(cmd)
	if found == "" {
		return ActionUnknown, ""
	}
	return sf.Action, found
}

var _ sort.Interface = FilterRules{}

type FilterRules []SystemUserFilterRule

func (f FilterRules) Swap(i, j int) {
	f[i], f[j] = f[j], f[i]
}

func (f FilterRules) Len() int {
	return len(f)
}

/*
	core 优先级的值越小，优先级越高，因此按此排序，第一个是优先级最高的
	优先级相同则 Action Deny 的优先级更高
*/

func (f FilterRules) Less(i, j int) bool {
	switch {
	case f[i].Priority == f[j].Priority:
		return actionPriorityMap[f[i].Action] < actionPriorityMap[f[j].Action]
	default:
		return f[i].Priority < f[j].Priority
	}
}

var (
	actionPriorityMap = map[RuleAction]int{
		ActionDeny:    0,
		ActionConfirm: 1,
		ActionAllow:   2,
		ActionUnknown: 3,
	}
)
