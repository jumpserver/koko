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
	for _, required := range []string{"draft-only", "latest revision", "never claim"} {
		if !strings.Contains(profile.instructions, required) {
			t.Fatalf("script runtime policy is missing %q", required)
		}
	}
}

func TestSQLRuntimeProfileUsesChenTools(t *testing.T) {
	profile, ok := runtimeProfilePolicyFor("sql")
	if !ok {
		t.Fatal("SQL runtime profile is missing")
	}
	for _, required := range []string{"draft-only", "verified editor context", "proposal tool", "never claim"} {
		if !strings.Contains(profile.instructions, required) {
			t.Fatalf("SQL runtime profile does not contain %q", required)
		}
	}
	if !profile.requiresApproval("inspect_schema") || profile.requiresApproval("validate_sql") {
		t.Fatal("SQL runtime approval policy is invalid")
	}
}
