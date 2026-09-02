package sessiontools

import (
	"regexp"
	"sort"
	"strings"

	"github.com/jumpserver-dev/sdk-go/model"
)

type ValueSanitizer interface {
	Sanitize(column, value string) string
}

type maskingRule struct {
	rule     model.DataMaskingRule
	patterns []*regexp.Regexp
}

type MySQLSanitizer struct {
	rules []maskingRule
}

func NewMySQLSanitizer(rules []model.DataMaskingRule) *MySQLSanitizer {
	active := make([]model.DataMaskingRule, 0, len(rules))
	for _, rule := range rules {
		if rule.IsActive {
			active = append(active, rule)
		}
	}
	sort.SliceStable(active, func(i, j int) bool {
		return active[i].Priority < active[j].Priority
	})
	result := &MySQLSanitizer{rules: make([]maskingRule, 0, len(active))}
	for _, rule := range active {
		compiled := maskingRule{rule: rule}
		for _, raw := range strings.Split(rule.FieldsPattern, ",") {
			pattern := strings.TrimSpace(raw)
			if pattern == "" {
				continue
			}
			expression := regexp.QuoteMeta(pattern)
			expression = strings.ReplaceAll(expression, `\*`, ".*")
			if value, err := regexp.Compile("(?i)^" + expression + "$"); err == nil {
				compiled.patterns = append(compiled.patterns, value)
			}
		}
		if len(compiled.patterns) > 0 {
			result.rules = append(result.rules, compiled)
		}
	}
	return result
}

func (s *MySQLSanitizer) Sanitize(column, value string) string {
	if s == nil {
		return value
	}
	result := value
	for _, item := range s.rules {
		if item.matches(column) {
			result = applyMask(item.rule, value)
		}
	}
	return result
}

func (r maskingRule) matches(column string) bool {
	for _, pattern := range r.patterns {
		if pattern.MatchString(column) {
			return true
		}
	}
	return false
}

func applyMask(rule model.DataMaskingRule, value string) string {
	chars := []rune(value)
	switch rule.MaskingMethod {
	case "fixed_char":
		return rule.MaskPattern
	case "hide_middle":
		if len(chars) < 3 {
			return rule.MaskPattern
		}
		return string(chars[:1]) + strings.Repeat("*", len(chars)-2) +
			string(chars[len(chars)-1:])
	case "keep_prefix":
		if len(chars) <= 2 {
			return "####"
		}
		return string(chars[:2]) + strings.Repeat("*", len(chars)-2)
	case "keep_suffix":
		if len(chars) <= 2 {
			return "####"
		}
		return strings.Repeat("*", len(chars)-2) + string(chars[len(chars)-2:])
	default:
		return rule.MaskPattern
	}
}
