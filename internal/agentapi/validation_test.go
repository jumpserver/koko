package agentapi

import "testing"

func TestValidIdentifier(t *testing.T) {
	for value, expected := range map[string]bool{
		"session-1":  true,
		"asset:user": true,
		"":           false,
		"session/1":  false,
	} {
		if actual := ValidIdentifier(value); actual != expected {
			t.Errorf("ValidIdentifier(%q) = %v, want %v", value, actual, expected)
		}
	}
}
