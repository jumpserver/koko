package agent

import (
	"strings"
	"testing"
)

func TestScriptRuntimeProfileIsProposalOnly(t *testing.T) {
	profile, ok := runtimeProfilePolicyFor("script")
	if !ok {
		t.Fatal("script runtime profile is not registered")
	}
	for _, required := range []string{
		"read_script", "propose_script", "do not call another tool", "Never execute", "Never claim", "approval_required to false",
	} {
		if !strings.Contains(profile.instructions, required) {
			t.Fatalf("script runtime policy is missing %q", required)
		}
	}
}
