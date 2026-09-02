package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jumpserver/koko/internal/agentapi"
)

func TestToolExchangeIsRetainedInSessionHistory(t *testing.T) {
	session := &agentSession{}
	call := agentapi.ToolCall{
		RunID: "run-1", ToolCallID: "call-1", ToolName: "terminal_snapshot",
		Arguments: json.RawMessage(`{}`),
	}
	session.appendToolCallHistory(1, call)
	session.appendToolResultHistory(1, call, safeToolResult{
		RunID: "run-1", ToolCallID: "call-1", Done: true, Status: "success",
		Result: json.RawMessage(`{"structuredContent":{"content":"screen"},"_meta":{"private":"ignored"}}`),
	})
	if len(session.history) != 2 || session.history[0].message.Role != "tool_call" ||
		session.history[1].message.Role != "tool_result" {
		t.Fatal("tool exchange was not retained in order")
	}
	if strings.Contains(session.history[1].message.Text, `"_meta"`) {
		t.Fatal("transport metadata leaked into model history")
	}
}
