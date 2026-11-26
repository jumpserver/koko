package policy

import (
	"testing"

	"github.com/jumpserver/koko/internal/kael/domain"
)

func TestWorkspaceProfileIsAvailable(t *testing.T) {
	profile, ok := Get("workspace")
	if !ok || profile.Kind != "capability" || profile.MaxRisk != "dangerous" || Namespace(profile) != "luna.workspace" {
		t.Fatalf("unexpected workspace profile: profile=%+v available=%t", profile, ok)
	}

	available := Available(domain.Principal{})
	if len(available) < 2 || available[0].ID != "general" || available[1].ID != "workspace" {
		t.Fatalf("workspace profile is not available after general: %+v", available)
	}
}

func TestRegistrationPolicy(t *testing.T) {
	for _, tc := range []struct {
		name         string
		annotations  domain.ToolAnnotations
		risk         string
		confirmation bool
	}{
		{"default", domain.ToolAnnotations{}, "write", true},
		{"read", domain.ToolAnnotations{ReadOnly: true, Idempotent: true}, "read", false},
		{"external read", domain.ToolAnnotations{ReadOnly: true, OpenWorld: true}, "read", true},
		{"destructive read", domain.ToolAnnotations{ReadOnly: true, Destructive: true}, "dangerous", true},
		{"external write", domain.ToolAnnotations{OpenWorld: true}, "dangerous", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			risk, confirmation := RegistrationPolicy(tc.annotations)
			if risk != tc.risk || confirmation != tc.confirmation {
				t.Fatalf("policy = %s, %t", risk, confirmation)
			}
		})
	}
}

func TestPanelApprovalModes(t *testing.T) {
	for _, registered := range []bool{false, true} {
		for _, mode := range []string{"auto", "always", "never"} {
			want := mode == "always" || mode == "auto" && registered
			if ConfirmationRequired(registered, mode) != want {
				t.Fatalf("unexpected confirmation: registered=%t mode=%s", registered, mode)
			}
		}
	}
}
