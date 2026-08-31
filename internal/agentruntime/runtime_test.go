package agentruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver/koko/internal/agentapi"
	"github.com/jumpserver/koko/internal/agentruntime/provider"
)

type scriptedProvider struct {
	requests []provider.CompletionRequest
	results  []string
}

func (p *scriptedProvider) Info() provider.ProviderInfo {
	return provider.ProviderInfo{Name: "test", Model: "test"}
}

func (p *scriptedProvider) Complete(
	_ context.Context,
	request provider.CompletionRequest,
) (provider.CompletionResult, error) {
	p.requests = append(p.requests, request)
	return provider.CompletionResult{Content: p.results[len(p.requests)-1]}, nil
}

func (p *scriptedProvider) CompactState(provider.ContextTier) {}

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

func TestFinalRoundCompletesWithPartialAnswer(t *testing.T) {
	model := &scriptedProvider{results: []string{
		`{"kind":"tool_call","answer":"","summary":"Inspect","tool_name":"inspect","arguments":{},"approval_required":false,"approval_summary":""}`,
		`{"kind":"answer","answer":"I completed the inspection but need another turn to continue.","summary":"","tool_name":"","arguments":{},"approval_required":false,"approval_summary":""}`,
	}}
	completed := make(chan Completion, 1)
	failed := make(chan error, 1)
	toolCalls := 0
	runtime, err := New(
		Config{
			Profile: "terminal",
			Tools: []agentapi.ToolDefinition{{
				Name: "inspect", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
			}},
			MaxRounds: 2, MaxModelRequests: 2, RunTimeout: time.Second,
		},
		func() (provider.Provider, error) { return model, nil },
		Callbacks{
			Started:        func(string, string) error { return nil },
			History:        func(string) []Message { return nil },
			EmitModelEvent: func(string, string, string, any) error { return nil },
			CallTool: func(_ context.Context, request ToolRequest) (ToolObservation, error) {
				toolCalls++
				return ToolObservation{
					ToolCallID: "call-1", ToolName: request.ToolName,
					Status: "success", Result: json.RawMessage(`{}`),
				}, nil
			},
			Complete: func(_, _ string, completion Completion) error {
				completed <- completion
				return nil
			},
			Fail: func(_, _ string, err error) error {
				failed <- err
				return nil
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if err = runtime.Start("run-1", "message-1", "Inspect and continue", nil); err != nil {
		t.Fatal(err)
	}

	select {
	case completion := <-completed:
		if !completion.Partial || completion.FinishReason != FinishReasonRoundLimit {
			t.Fatalf("completion = %#v", completion)
		}
	case err = <-failed:
		t.Fatalf("run failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("run did not complete")
	}
	if toolCalls != 1 || len(model.requests) != 2 {
		t.Fatalf("tool calls = %d, model requests = %d", toolCalls, len(model.requests))
	}
	finalRequest := model.requests[1]
	if !strings.Contains(finalRequest.System, "final allowed round") {
		t.Fatal("final request is missing the round-limit instruction")
	}
	properties := finalRequest.Tool.Parameters["properties"].(map[string]any)
	kinds := properties["kind"].(map[string]any)["enum"].([]string)
	if len(kinds) != 1 || kinds[0] != "answer" {
		t.Fatalf("final action kinds = %v", kinds)
	}
}
