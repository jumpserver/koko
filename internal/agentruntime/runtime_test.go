package agentruntime

import (
	"encoding/json"
	"testing"
)

func TestApplyExecutionMode(t *testing.T) {
	arguments, err := applyExecutionMode(
		json.RawMessage(`{"command":"free -h","execution":"background"}`),
		"execute_command",
		map[string]any{"execution_mode": "pty"},
	)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(arguments, &result); err != nil {
		t.Fatal(err)
	}
	if result["execution"] != "pty" {
		t.Fatalf("execution = %v, want pty", result["execution"])
	}
}
