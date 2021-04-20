package model

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type RuleAction int

const (
	ActionDeny    RuleAction = 0
	ActionAllow   RuleAction = 1
	ActionConfirm RuleAction = 2
	ActionUnknown RuleAction = 3

	TypeRegex = "regex"
	TypeCmd   = "command"
)

type SystemUserFilterRule struct {
	ID       string     `json:"id"`
	Priority int        `json:"priority"`
	Type     string     `json:"type"`
	Content  string     `json:"content"`
	Action   RuleAction `json:"action"`
	OrgId    string     `json:"org_id"`

	pattern  *regexp.Regexp
	compiled bool
}

func (sf *SystemUserFilterRule) Pattern() *regexp.Regexp {
	if sf.compiled {
		return sf.pattern
	}
	var regexs string
	if sf.Type == TypeCmd {
		var regex []string
		content := strings.ReplaceAll(sf.Content, "\r\n", "\n")
		content = strings.ReplaceAll(content, "\r", "\n")
		for _, cmd := range strings.Split(content, "\n") {
			cmd = regexp.QuoteMeta(cmd)
			cmd = strings.Replace(cmd, " ", "\\s+", 1)
			regexItem := fmt.Sprintf(`\b%s\b`, cmd)
			lastRune, _ := utf8.DecodeLastRuneInString(cmd)
			if lastRune != utf8.RuneError && !unicode.IsLetter(lastRune) {
				regexItem = fmt.Sprintf(`\b%s`, cmd)
			}
			regex = append(regex, regexItem)
		}
		regexs = strings.Join(regex, "|")
	} else {
		regexs = sf.Content
	}
	pattern, err := regexp.Compile(regexs)
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

// core 优先级的值越大，优先级越高，因此按此排序，第一个是优先级最高的

func (f FilterRules) Less(i, j int) bool {
	return f[i].Priority < f[j].Priority
}
