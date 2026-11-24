package core

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
	"github.com/jumpserver-dev/sdk-go/model"
)

type commandMatcher struct {
	pattern     *regexp.Regexp
	advanced    *regexp2.Regexp
	matchFailed bool
	action      model.CommandAction
	aclID       string
	groupID     string
}

// Preserve the Core-provided order (first matching ACL/group wins). Keep the
// linear-time engine for ordinary patterns and use the existing backtracking
// engine for Java lookarounds/backreferences, with a bounded match deadline.
func compileMatchers(rules []model.CommandACL) ([]commandMatcher, error) {
	var result []commandMatcher
	for _, rule := range rules {
		if len(rule.CommandGroups) == 0 {
			return nil, errors.New("unsupported_empty_command_acl")
		}
		switch rule.Action {
		case model.ActionAccept, model.ActionReject, model.ActionReview, model.ActionWarning, model.ActionNotifyAndWarn:
		default:
			return nil, errors.New("unsupported_command_acl_action")
		}
		for _, group := range rule.CommandGroups {
			if strings.TrimSpace(group.RePattern) == "" {
				return nil, errors.New("unsupported_empty_command_acl_pattern")
			}
			pattern := group.RePattern
			if group.IgnoreCase {
				pattern = "(?i)" + pattern
			}
			compiled, err := regexp.Compile(pattern)
			matcher := commandMatcher{pattern: compiled, action: rule.Action, aclID: rule.ID, groupID: group.ID}
			if err != nil {
				matcher.advanced, err = regexp2.Compile(javaACLQuotedPattern(pattern), regexp2.RE2)
				if err != nil {
					return nil, errors.New("unsupported_command_acl_regex")
				}
				matcher.advanced.MatchTimeout = 100 * time.Millisecond
			}
			result = append(result, matcher)
		}
	}
	return result, nil
}

func matchPolicy(g *Grant, sql string) *commandMatcher {
	for i := range g.matchers {
		m := &g.matchers[i]
		matched := false
		if m.pattern != nil {
			matched = m.pattern.MatchString(sql)
		} else if m.advanced != nil {
			var err error
			matched, err = m.advanced.MatchString(sql)
			if err != nil {
				// A timeout is a policy failure, never permission to try a later
				// accepting rule. Do not mutate the shared compiled matcher.
				failed := *m
				failed.action, failed.matchFailed = model.ActionReject, true
				return &failed
			}
		}
		if matched {
			return &g.matchers[i]
		}
	}
	return nil
}

func (p *CoreProvider) Authorize(ctx context.Context, g *Grant, sql string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := g.Check(); err != nil {
		return err
	}
	if strings.TrimSpace(sql) == "" {
		return errors.New("empty_sql")
	}
	matched := matchPolicy(g, sql)
	if matched == nil {
		return nil
	}
	e := &PolicyError{ACLID: matched.aclID, GroupID: matched.groupID}
	if matched.matchFailed {
		e.Code, e.RiskLevel = "command_acl_match_failed", int32(model.RejectLevel)
		return e
	}
	switch matched.action {
	case model.ActionAccept, model.ActionWarning:
		// Ordinary warnings are audited without an interactive confirmation.
		return nil
	case model.ActionNotifyAndWarn:
		e.Code, e.RiskLevel = "command_warning_confirmation_required", int32(model.WarningLevel)
	case model.ActionReject:
		e.Code, e.RiskLevel = "command_rejected_by_acl", int32(model.RejectLevel)
	case model.ActionReview:
		e.Code, e.RiskLevel = "command_review_required", int32(model.ReviewReject)
	default:
		e.Code, e.RiskLevel = "unsupported_command_acl_action", int32(model.RejectLevel)
	}
	return e
}

// Java's literal quotation can be combined with lookarounds. regexp2 supports
// the latter but not \Q...\E, so translate only the quoted text, preserving all
// other escapes and regex operators verbatim.
func javaACLQuotedPattern(pattern string) string {
	var out strings.Builder
	for i := 0; i < len(pattern); {
		if i+1 < len(pattern) && pattern[i] == '\\' {
			if pattern[i+1] == 'Q' {
				i += 2
				end := strings.Index(pattern[i:], `\E`)
				if end < 0 {
					end = len(pattern) - i
				}
				out.WriteString(regexp.QuoteMeta(pattern[i : i+end]))
				i += end
				if i < len(pattern) {
					i += 2
				}
				continue
			}
			out.WriteString(pattern[i : i+2])
			i += 2
			continue
		}
		out.WriteByte(pattern[i])
		i++
	}
	return out.String()
}

type maskRule struct {
	fields  []*regexp.Regexp
	method  string
	pattern string
}

func compileMaskers(rules []model.DataMaskingRule) ([]maskRule, error) {
	result := make([]maskRule, 0, len(rules))
	for _, rule := range rules {
		if strings.TrimSpace(rule.FieldsPattern) == "" {
			return nil, errors.New("unsupported_empty_data_masking_rule")
		}
		r := maskRule{method: rule.MaskingMethod, pattern: rule.MaskPattern}
		for _, field := range strings.Split(rule.FieldsPattern, ",") {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			// Only '*' is special; a Core field pattern is not a regex.
			pattern := "(?i)^" + strings.ReplaceAll(regexp.QuoteMeta(field), `\*`, ".*") + "$"
			r.fields = append(r.fields, regexp.MustCompile(pattern))
		}
		if len(r.fields) == 0 {
			return nil, errors.New("unsupported_empty_data_masking_pattern")
		}
		result = append(result, r)
	}
	return result, nil
}

// Mask must run before any result value is exposed or persisted as a browser
// result. Matching both the source column and label preserves alias protection
// when source-column metadata is available. Last matching rule wins, as in Java.
func (p *CoreProvider) Mask(g *Grant, column, label string, value any) (any, bool) {
	if g == nil {
		return "####", true
	}
	var match *maskRule
	for i := range g.maskers {
		for _, field := range g.maskers[i].fields {
			if field.MatchString(column) || field.MatchString(label) {
				match = &g.maskers[i]
				break
			}
		}
	}
	if match == nil {
		if len(g.maskers) > 0 && column == "" {
			// Without origin metadata an alias/expression can hide a protected
			// source field. Never claim label-only matching enforces that policy.
			return "####", true
		}
		return value, false
	}
	pattern := match.pattern
	if pattern == "" {
		pattern = "####"
	}
	if value == nil || match.method == "fixed_char" {
		return pattern, true
	}
	var str string
	switch v := value.(type) {
	case string:
		str = v
	case []byte:
		str = string(v)
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		str = fmt.Sprint(v)
	default:
		return pattern, true
	}
	runes := []rune(str)
	switch match.method {
	case "hide_middle":
		if len(runes) < 3 {
			return pattern, true
		}
		return string(runes[:1]) + strings.Repeat("*", len(runes)-2) + string(runes[len(runes)-1:]), true
	case "keep_prefix":
		if len(runes) <= 2 {
			return "####", true
		}
		return string(runes[:2]) + strings.Repeat("*", len(runes)-2), true
	case "keep_suffix":
		if len(runes) <= 2 {
			return "####", true
		}
		return strings.Repeat("*", len(runes)-2) + string(runes[len(runes)-2:]), true
	default:
		return pattern, true
	}
}
