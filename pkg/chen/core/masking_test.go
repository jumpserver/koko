package core

import (
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
)

func TestMaskKeepsWebGUIOriginAndValuePolicy(t *testing.T) {
	rules, err := compileMaskers([]model.DataMaskingRule{{FieldsPattern: "email", MaskingMethod: "hide_middle"}})
	if err != nil {
		t.Fatal(err)
	}
	g := &Grant{maskers: rules}
	provider := new(CoreProvider)
	for _, tc := range []struct {
		origin, label string
		value, want   any
		masked        bool
	}{
		{"email", "alias", 12345, "1***5", true},
		{"other", "EMAIL", []byte("管🙂员"), "管*员", true},
		{"email", "alias", nil, "####", true},
		{"", "alias", "raw", "####", true},
		{"unrelated", "alias", 12345, 12345, false},
	} {
		if got, masked := provider.Mask(g, tc.origin, tc.label, tc.value); got != tc.want || masked != tc.masked {
			t.Errorf("%s/%s: got %v,%v want %v,%v", tc.origin, tc.label, got, masked, tc.want, tc.masked)
		}
	}
}
