package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver/koko/internal/agentapi"
	"github.com/jumpserver/koko/internal/agentruntime/provider"
)

type scriptedProvider struct {
	requests []provider.CompletionRequest
	results  []provider.CompletionResult
}

func (p *scriptedProvider) Info() provider.ProviderInfo {
	return provider.ProviderInfo{Name: "test", Model: "test"}
}

func (p *scriptedProvider) Complete(
	_ context.Context,
	request provider.CompletionRequest,
) (provider.CompletionResult, error) {
	p.requests = append(p.requests, request)
	return p.results[len(p.requests)-1], nil
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
	model := &scriptedProvider{results: []provider.CompletionResult{
		{ToolCall: &provider.ToolCall{
			ID: "provider-call-1", Name: "inspect", Arguments: json.RawMessage(`{}`),
		}},
		{Content: "I completed the inspection but need another turn to continue."},
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
	if len(model.requests[0].Tools) != 1 || model.requests[0].Tools[0].Name != "inspect" {
		t.Fatalf("registered tools = %#v", model.requests[0].Tools)
	}
	if strings.Contains(model.requests[0].User, "inputSchema") ||
		strings.Contains(model.requests[0].User, "agent_next") {
		t.Fatal("session tool schema leaked into the prompt")
	}
	if len(finalRequest.Tools) != 0 {
		t.Fatalf("final request tools = %#v", finalRequest.Tools)
	}
	if len(finalRequest.ToolOutputs) != 1 ||
		finalRequest.ToolOutputs[0].CallID != "provider-call-1" {
		t.Fatalf("final request tool outputs = %#v", finalRequest.ToolOutputs)
	}
}

func TestProviderToolNameAliasesMCPNames(t *testing.T) {
	name := providerToolName("namespace.inspect")
	if name == "namespace.inspect" || len(name) > maxProviderToolNameBytes {
		t.Fatalf("provider tool name = %q", name)
	}
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '_' || char == '-' {
			continue
		}
		t.Fatalf("provider tool name contains invalid character %q", char)
	}
}

func TestReadOnlyToolAllowsOneConsecutiveRefresh(t *testing.T) {
	readOnly := true
	if got := maxConsecutiveToolAttempts(agentapi.ToolDefinition{
		Annotations: agentapi.ToolAnnotations{ReadOnlyHint: &readOnly},
	}); got != 2 {
		t.Fatalf("read-only attempts = %d, want 2", got)
	}
	if got := maxConsecutiveToolAttempts(agentapi.ToolDefinition{}); got != 1 {
		t.Fatalf("mutating attempts = %d, want 1", got)
	}
}

func TestFinalResultToolEndsToolLoop(t *testing.T) {
	model := &scriptedProvider{results: []provider.CompletionResult{
		{ToolCall: &provider.ToolCall{
			ID: "proposal-call", Name: "propose_script", Arguments: json.RawMessage(`{}`),
		}},
		{Content: "The applied proposal is ready to save."},
	}}
	completed := make(chan Completion, 1)
	runtime, err := New(
		Config{
			Profile: "script",
			Tools: []agentapi.ToolDefinition{{
				Name: "propose_script", InputSchema: json.RawMessage(`{"type":"object"}`),
				Meta: map[string]any{finalResultMetaKey: true},
			}},
			MaxRounds: 3, MaxModelRequests: 3, RunTimeout: time.Second,
		},
		func() (provider.Provider, error) { return model, nil },
		Callbacks{
			Started:        func(string, string) error { return nil },
			History:        func(string) []Message { return nil },
			EmitModelEvent: func(string, string, string, any) error { return nil },
			CallTool: func(_ context.Context, request ToolRequest) (ToolObservation, error) {
				return ToolObservation{
					ToolCallID: request.ToolName, ToolName: request.ToolName,
					Status: "success", Result: json.RawMessage(`{"structuredContent":{"status":"applied"}}`),
				}, nil
			},
			Complete: func(_, _ string, completion Completion) error {
				completed <- completion
				return nil
			},
			Fail: func(_, _ string, err error) error { return err },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if err = runtime.Start("run-1", "message-1", "Update the script", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not complete")
	}
	if len(model.requests) != 2 || len(model.requests[1].Tools) != 0 ||
		!strings.Contains(model.requests[1].System, "final user decision") {
		t.Fatalf("final proposal request = %#v", model.requests)
	}
}

func TestIdenticalFailedReadOnlyToolStopsWithoutRetry(t *testing.T) {
	readOnly := true
	model := &scriptedProvider{results: []provider.CompletionResult{
		{ToolCall: &provider.ToolCall{
			ID: "provider-call-1", Name: "inspect_schema", Arguments: json.RawMessage(`{"query":"*"}`),
		}},
		{ToolCall: &provider.ToolCall{
			ID: "provider-call-2", Name: "inspect_schema", Arguments: json.RawMessage(`{"query":"*"}`),
		}},
	}}
	completed := make(chan Completion, 1)
	toolCalls := 0
	runtime, err := New(
		Config{
			Profile: "sql",
			Tools: []agentapi.ToolDefinition{{
				Name: "inspect_schema",
				InputSchema: json.RawMessage(
					`{"type":"object","required":["query"],"properties":{"query":{"type":"string"}}}`,
				),
				Annotations: agentapi.ToolAnnotations{ReadOnlyHint: &readOnly},
			}},
			MaxRounds: 3, MaxModelRequests: 3, RunTimeout: time.Second,
		},
		func() (provider.Provider, error) { return model, nil },
		Callbacks{
			Started:        func(string, string) error { return nil },
			History:        func(string) []Message { return nil },
			EmitModelEvent: func(string, string, string, any) error { return nil },
			CallTool: func(_ context.Context, request ToolRequest) (ToolObservation, error) {
				toolCalls++
				return ToolObservation{
					ToolCallID: request.ToolName, ToolName: request.ToolName, Status: "error",
					Error: &agentapi.JSONRPCError{
						Code: -32602, Message: "The active schema is not available to the SQL assistant",
					},
				}, nil
			},
			Complete: func(_, _ string, completion Completion) error {
				completed <- completion
				return nil
			},
			Fail: func(_, _ string, err error) error { return err },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if err = runtime.Start("run-1", "message-1", "List tables", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case completion := <-completed:
		if completion.Answer != "The active schema is not available to the SQL assistant" {
			t.Fatalf("answer = %q", completion.Answer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not complete")
	}
	if toolCalls != 1 || len(model.requests) != 2 {
		t.Fatalf("tool calls = %d, model requests = %d", toolCalls, len(model.requests))
	}
}

func TestDuplicateToolCallDisablesToolsAndCompletes(t *testing.T) {
	readOnly := true
	model := &scriptedProvider{results: []provider.CompletionResult{
		{ToolCall: &provider.ToolCall{
			ID: "provider-call-1", Name: "inspect", Arguments: json.RawMessage(`{}`),
		}},
		{ToolCall: &provider.ToolCall{
			ID: "provider-call-2", Name: "inspect", Arguments: json.RawMessage(`{}`),
		}},
		{ToolCall: &provider.ToolCall{
			ID: "provider-call-3", Name: "inspect", Arguments: json.RawMessage(`{}`),
		}},
		{Content: "The existing inspection result is sufficient."},
	}}
	completed := make(chan Completion, 1)
	failed := make(chan error, 1)
	toolCalls := 0
	runtime, err := New(
		Config{
			Profile: "terminal",
			Tools: []agentapi.ToolDefinition{{
				Name: "inspect", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
				Annotations: agentapi.ToolAnnotations{ReadOnlyHint: &readOnly},
			}},
			MaxRounds: 5, MaxModelRequests: 5, RunTimeout: time.Second,
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
	if err = runtime.Start("run-1", "message-1", "Inspect once", nil); err != nil {
		t.Fatal(err)
	}

	select {
	case completion := <-completed:
		if completion.Answer == "" {
			t.Fatal("completion answer is empty")
		}
	case err = <-failed:
		t.Fatalf("run failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("run did not complete")
	}
	if toolCalls != 2 || len(model.requests) != 4 {
		t.Fatalf("tool calls = %d, model requests = %d", toolCalls, len(model.requests))
	}
	last := model.requests[3]
	if len(last.Tools) != 0 || !strings.Contains(last.System, "identical tool call") {
		t.Fatalf("duplicate guard request = %#v", last)
	}
	if len(last.ToolOutputs) != 1 || last.ToolOutputs[0].CallID != "provider-call-3" {
		t.Fatalf("duplicate tool output = %#v", last.ToolOutputs)
	}
}

func TestArgumentRepairKeepsLatestToolObservation(t *testing.T) {
	inputSchema := json.RawMessage(`{"type":"object","required":["expected_revision","content"],"properties":{"expected_revision":{"type":"integer"},"content":{"type":"string"}}}`)
	compiled, err := compileSchema(inputSchema)
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptedProvider{results: []provider.CompletionResult{{
		ToolCall: &provider.ToolCall{
			ID: "repaired-proposal", Name: "propose_script",
			Arguments: json.RawMessage(`{"expected_revision":55,"content":"updated"}`),
		},
	}}}
	runtime := Runtime{
		provider:  model,
		callbacks: Callbacks{EmitModelEvent: func(string, string, string, any) error { return nil }},
	}
	attempted := json.RawMessage(`{"expected_revision":"55","content":"updated"}`)
	observations := []ToolObservation{{
		ToolName: "read_script", Status: "success",
		Result: json.RawMessage(`{"structuredContent":{"revision":55}}`),
	}}
	arguments, _, err := runtime.repairToolArguments(
		context.Background(), "run-1", "message-1", "Update the script", 2,
		toolContract{definition: agentapi.ToolDefinition{
			Name: "propose_script", InputSchema: inputSchema,
		}, providerName: "propose_script", input: compiled},
		attempted, observations, errors.New("expected integer"), nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(arguments) != `{"expected_revision":55,"content":"updated"}` {
		t.Fatalf("repaired arguments = %s", arguments)
	}
	repair := model.requests[0]
	if repair.RequiredTool != "propose_script" ||
		!strings.Contains(repair.User, `"expected_revision":"55"`) ||
		!strings.Contains(repair.User, `"revision":55`) {
		t.Fatalf("repair context = %q", repair.User)
	}
}
