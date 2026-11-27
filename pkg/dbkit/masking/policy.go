// Package masking matches column names and transforms text. The caller owns
// authorization, rule ordering, unknown-origin policy and wire/value conversion.
package masking

import (
	"errors"
	"regexp"
	"strings"
)

const (
	FixedChar  = "fixed_char"
	HideMiddle = "hide_middle"
	KeepPrefix = "keep_prefix"
	KeepSuffix = "keep_suffix"
)

type Transform struct{ Method, Pattern string }
type Rule struct {
	Fields    string
	Transform Transform
}

type compiled struct {
	patterns  []*regexp.Regexp
	transform Transform
}

// Policy is immutable after compilation. Its zero value matches nothing.
type Policy struct{ rules []compiled }

// Compile takes active rules in their existing application order; the last
// matching rule wins. '*' is the only wildcard, not general regular expressions.
func Compile(rules []Rule) (Policy, error) {
	var policy Policy
	for _, r := range rules {
		if strings.TrimSpace(r.Fields) == "" {
			return Policy{}, errors.New("unsupported_empty_data_masking_rule")
		}
		c := compiled{transform: r.Transform}
		for _, field := range strings.Split(r.Fields, ",") {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			re, err := regexp.Compile("(?i)^" + strings.ReplaceAll(regexp.QuoteMeta(field), `\*`, ".*") + "$")
			if err != nil {
				return Policy{}, errors.New("unsupported_data_masking_pattern")
			}
			c.patterns = append(c.patterns, re)
		}
		if len(c.patterns) == 0 {
			return Policy{}, errors.New("unsupported_empty_data_masking_pattern")
		}
		policy.rules = append(policy.rules, c)
	}
	return policy, nil
}

func (p Policy) Enabled() bool { return len(p.rules) > 0 }

// Match considers only the evidence provided by the caller; no match is not
// proof that an unknown source column is safe.
func (p Policy) Match(names ...string) (Transform, bool) {
	for i := len(p.rules) - 1; i >= 0; i-- {
		for _, re := range p.rules[i].patterns {
			for _, name := range names {
				if re.MatchString(name) {
					return p.rules[i].transform, true
				}
			}
		}
	}
	return Transform{}, false
}

// Text preserves Unicode code points. Pattern is a literal, including when
// empty: adapters with a default replacement must supply it explicitly.
func (m Transform) Text(s string) string {
	switch m.Method {
	case HideMiddle, KeepPrefix, KeepSuffix:
		r := []rune(s)
		if len(r) <= 2 {
			if m.Method == HideMiddle {
				return m.Pattern
			}
			return "####"
		}
		stars := strings.Repeat("*", len(r)-2)
		switch m.Method {
		case HideMiddle:
			return string(r[:1]) + stars + string(r[len(r)-1:])
		case KeepPrefix:
			return string(r[:2]) + stars
		default:
			return stars + string(r[len(r)-2:])
		}
	default:
		return m.Pattern
	}
}
