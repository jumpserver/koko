package sessiontools

import "testing"

func TestAgentCommandPolicyKeepsReadOnlyMetadataAndDocumentFields(t *testing.T) {
	for _, tc := range []struct{ protocol, command string }{
		{"redis", "CLIENT ID"},
		{"redis", "CLIENT LIST"},
		{"mongodb", `db.runCommand({"find":"items","filter":{"lsid":"field","autocommit":false}})`},
	} {
		if eligible, err := AgentDatabaseCommandEligible(tc.protocol, tc.command); err != nil || !eligible {
			t.Fatalf("ordinary command rejected: %s: %v", tc.command, err)
		}
	}
	for _, tc := range []struct{ protocol, command string }{
		{"redis", "CLIENT TRACKING ON"},
		{"mongodb", `db.runCommand({"find":"items","lsid":{"id":"session"}})`},
	} {
		constraints, err := ProtocolCommandValidator(tc.protocol)(tc.command)
		if err != nil || !constraints.BackgroundEligible {
			t.Fatalf("MCP policy changed the existing terminal validator: %v", err)
		}
	}
}
