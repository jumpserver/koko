package feature

import (
	"strings"
	"unicode"

	"github.com/jumpserver/koko/pkg/dbkit/masking"
)

type DataMaskingRule struct {
	IsActive      *bool  `json:"is_active,omitempty"`
	Name          string `json:"name"`
	FieldsPattern string `json:"fields_pattern"`
	MaskPattern   string `json:"mask_pattern"`
	MaskingMethod string `json:"masking_method"`
}

// Older callers send only active rules and omit is_active.
func (r DataMaskingRule) Enabled() bool {
	return (r.IsActive == nil || *r.IsActive) && strings.ContainsFunc(r.FieldsPattern, func(c rune) bool {
		return c != ',' && !unicode.IsSpace(c)
	})
}

const DataMaskingKey = "data-masking-rules"

const (
	MaskingMethodFixedChar  = masking.FixedChar
	MaskingMethodHideMiddle = masking.HideMiddle
	MaskingMethodKeepPrefix = masking.KeepPrefix
	MaskingMethodKeepSuffix = masking.KeepSuffix
)

// CompilePolicy maps the terminal's wire format into an ordered session policy.
// Legacy callers can omit is_active; inactive and empty rules remain ignored.
func CompilePolicy(rules []DataMaskingRule) (masking.Policy, error) {
	var input []masking.Rule
	for _, r := range rules {
		if r.Enabled() {
			input = append(input, masking.Rule{Fields: strings.ToLower(r.FieldsPattern), Transform: masking.Transform{Method: r.MaskingMethod, Pattern: r.MaskPattern}})
		}
	}
	return masking.Compile(input)
}
