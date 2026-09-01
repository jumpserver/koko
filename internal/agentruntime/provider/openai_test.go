package provider

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go/v3/responses"
)

func TestResponseResultExtractsNativeToolCall(t *testing.T) {
	var response responses.Response
	if err := json.Unmarshal([]byte(`{
		"id":"resp-1","status":"completed","model":"test-model",
		"output":[{"type":"function_call","id":"fc-1","call_id":"call-1",
			"name":"inspect","arguments":"{\"path\":\"/\"}","status":"completed"}],
		"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2,
			"input_tokens_details":{"cached_tokens":0},
			"output_tokens_details":{"reasoning_tokens":0}}
	}`), &response); err != nil {
		t.Fatal(err)
	}
	result := responseResult(&response, "request-1")
	if result.ToolCall == nil || result.ToolCall.ID != "call-1" ||
		result.ToolCall.Name != "inspect" || string(result.ToolCall.Arguments) != `{"path":"/"}` {
		t.Fatalf("tool call = %#v", result.ToolCall)
	}
}

func TestResponseInputIncludesNativeToolOutput(t *testing.T) {
	items := responseInput(nil, "continue", []ToolOutput{{
		CallID: "call-1", Output: `{"ok":true}`,
	}}, true)
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	var values []map[string]any
	if err = json.Unmarshal(encoded, &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0]["type"] != "function_call_output" ||
		values[0]["call_id"] != "call-1" || values[1]["role"] != "user" {
		t.Fatalf("response input = %s", encoded)
	}
}
