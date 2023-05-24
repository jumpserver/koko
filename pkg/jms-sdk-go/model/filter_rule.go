package model

import (
	"regexp"
	"sort"

	"github.com/dlclark/regexp2"
)

var _ sort.Interface = CommandACLs{}

type CommandACLs []CommandACL

func (f CommandACLs) Swap(i, j int) {
	f[i], f[j] = f[j], f[i]
}

func (f CommandACLs) Len() int {
	return len(f)
}

/*
	core 优先级的值越小，优先级越高，因此按此排序，第一个是优先级最高的
	优先级相同则 Action Deny 的优先级更高
*/

func (f CommandACLs) Less(i, j int) bool {
	switch {
	case f[i].Priority == f[j].Priority:
		return actionPriorityMap[f[i].Action] < actionPriorityMap[f[j].Action]
	default:
		return f[i].Priority < f[j].Priority
	}
}

type CommandACL struct {
	ID            string              `json:"id"`
	Action        CommandAction       `json:"action"`
	CommandGroups []CommandFilterItem `json:"command_groups"`
	IsActive      bool                `json:"is_active"`
	Name          string              `json:"name"`
	Priority      int                 `json:"priority"`
	Reviewers     []interface{}       `json:"reviewers"`
}

func (cf *CommandFilterItem) Pattern() FindStringer {
	if cf.compiled {
		return cf.pattern
	}
	regexp2Opt := regexp2.None
	if cf.IgnoreCase {
		regexp2Opt = regexp2.IgnoreCase
	}
	regexp2Pattern, err := regexp2.Compile(cf.RePattern, regexp2Opt)
	if err == nil {
		cf.pattern = &Regexp2{regexp2Pattern}
		cf.compiled = true
	}
	return cf.pattern
}

func (sf *CommandACL) Match(cmd string) (CommandFilterItem, CommandAction, string) {
	for i := range sf.CommandGroups {
		item := sf.CommandGroups[i]
		pattern := item.Pattern()
		if pattern == nil {
			continue
		}
		found := pattern.FindString(cmd)
		if found == "" {
			continue
		}
		return item, sf.Action, found
	}
	return CommandFilterItem{}, ActionUnknown, ""
}

type CommandFilterItem struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	IgnoreCase bool   `json:"ignore_case"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	RePattern  string `json:"pattern"`

	pattern  FindStringer
	compiled bool
}

type CommandAction string

const (
	ActionReject  = "reject"
	ActionAccept  = "accept"
	ActionReview  = "review"
	ActionUnknown = "Unknown"
)

var (
	actionPriorityMap = map[CommandAction]int{
		ActionReject:  0,
		ActionReview:  1,
		ActionAccept:  2,
		ActionUnknown: 2,
	}
)

type FindStringer interface {
	FindString(s string) string
}

type StdRegexp struct {
	pattern *regexp.Regexp
}

func (r *StdRegexp) FindString(s string) string {
	return r.pattern.FindString(s)
}

type Regexp2 struct {
	pattern *regexp2.Regexp
}

func (r *Regexp2) FindString(s string) string {
	match, err := r.pattern.FindStringMatch(s)
	if err != nil || match == nil {
		return ""
	}
	return match.String()
}
