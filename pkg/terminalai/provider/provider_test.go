package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderRoutingAndModelLimits(t *testing.T) {
	tests := []struct {
		name      string
		transport string
	}{
		{NameGPT, "chat-completions"},
		{NameOpenAI, "responses"},
		{NameDeepSeek, "deepseek-chat-completions"},
		{"custom", "chat-completions"},
	}
	for _, test := range tests {
		modelProvider, err := New(Config{
			Name: test.name, APIKey: "key", Model: "model",
		})
		if err != nil {
			t.Fatalf("create provider %q: %v", test.name, err)
		}
		if got := modelProvider.Info().EffectiveTransport; got != test.transport {
			t.Fatalf("provider %q transport = %q, want %q", test.name, got, test.transport)
		}
	}
	if contextTokens, outputTokens := ModelLimits(NameDeepSeek, "deepseek-v4-pro"); contextTokens != 1_000_000 || outputTokens != 384_000 {
		t.Fatalf("DeepSeek V4 limits = %d/%d", contextTokens, outputTokens)
	}
	if contextTokens, outputTokens := ModelLimits(NameDeepSeek, "deepseek-reasoner"); contextTokens != 65_536 || outputTokens != 32_768 {
		t.Fatalf("DeepSeek reasoner limits = %d/%d", contextTokens, outputTokens)
	}
	if contextTokens, outputTokens := ModelLimits(NameOpenAI, "gpt-5.6"); contextTokens != 1_050_000 || outputTokens != 128_000 {
		t.Fatalf("GPT-5.6 limits = %d/%d", contextTokens, outputTokens)
	}
}

func TestDeepSeekURLUsesDeepSeekProvider(t *testing.T) {
	modelProvider, err := New(Config{
		Name: NameOpenAI, APIKey: "key", BaseURL: "https://api.deepseek.com/v1",
		Model: "deepseek-chat",
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	info := modelProvider.Info()
	if info.Name != NameDeepSeek || info.EffectiveTransport != "deepseek-chat-completions" {
		t.Fatalf("provider info = %#v", info)
	}
}

func TestOpenAIReplaysPreviousEncryptedReasoning(t *testing.T) {
	var requests [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, body)
		turn := len(requests)
		writer.Header().Set("Content-Type", "application/json")
		if turn == 2 {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"error":{"message":"encrypted reasoning item is invalid","type":"invalid_request_error","param":"encrypted_content","code":"invalid_state"}}`)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"id": "response", "object": "response", "created_at": 1,
			"status": "completed", "model": "gpt-5.6",
			"output": []any{
				map[string]any{
					"type": "reasoning", "id": "reasoning",
					"summary": []any{}, "encrypted_content": "encrypted-" + string(rune('0'+turn)),
				},
				map[string]any{
					"type": "message", "id": "message", "status": "completed",
					"role": "assistant", "content": []any{map[string]any{
						"type": "output_text", "text": `{"value":"ok"}`, "annotations": []any{},
					}},
				},
			},
			"usage": map[string]any{
				"input_tokens": 1, "output_tokens": 2, "total_tokens": 3,
				"input_tokens_details":  map[string]any{"cached_tokens": 0},
				"output_tokens_details": map[string]any{"reasoning_tokens": 1},
			},
		})
	}))
	defer server.Close()

	modelProvider, err := New(Config{
		Name: NameOpenAI, APIKey: "key", BaseURL: server.URL,
		Model: "gpt-5.6", ReasoningMode: ReasoningAuto,
	})
	if err != nil {
		t.Fatalf("create OpenAI provider: %v", err)
	}
	tool := &ActionTool{
		Name: "action", Parameters: map[string]any{
			"type": "object", "properties": map[string]any{
				"value": map[string]any{"type": "string"},
			},
		},
	}
	for _, input := range []string{"first", "second"} {
		_, err = modelProvider.Complete(context.Background(), CompletionRequest{
			Operation: OperationAction, System: "system", User: input, Tool: tool,
		})
		if err != nil {
			t.Fatalf("complete %q: %v", input, err)
		}
	}
	if len(requests) != 3 {
		t.Fatalf("request count = %d, want 3", len(requests))
	}
	second := string(requests[1])
	for _, value := range []string{`"encrypted_content":"encrypted-1"`, `"first"`, `"second"`} {
		if !strings.Contains(second, value) {
			t.Fatalf("second request does not contain %s: %s", value, second)
		}
	}
	if strings.Contains(second, "previous_response_id") {
		t.Fatalf("store=false request used server state: %s", second)
	}
	third := string(requests[2])
	if strings.Contains(third, `"encrypted_content":"encrypted-1"`) ||
		strings.Contains(third, `"first"`) || !strings.Contains(third, `"second"`) {
		t.Fatalf("invalid reasoning state was not cleared: %s", third)
	}
}

func TestOpenAIStoreUsesPreviousResponseID(t *testing.T) {
	var requests [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"id":"response-1","object":"response","created_at":1,"status":"completed","model":"gpt-5.6","output":[{"type":"message","id":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}`)
	}))
	defer server.Close()

	modelProvider, err := New(Config{
		Name: NameOpenAI, APIKey: "key", BaseURL: server.URL,
		Model: "gpt-5.6", Store: true, ReasoningMode: ReasoningOff,
	})
	if err != nil {
		t.Fatalf("create OpenAI provider: %v", err)
	}
	for _, input := range []string{"first", "second"} {
		_, err = modelProvider.Complete(context.Background(), CompletionRequest{
			Operation: OperationText, System: "system", User: input,
		})
		if err != nil {
			t.Fatalf("complete %q: %v", input, err)
		}
	}
	second := string(requests[1])
	if !strings.Contains(second, `"previous_response_id":"response-1"`) ||
		strings.Contains(second, `"first"`) {
		t.Fatalf("stored second request did not use server state: %s", second)
	}
}

func TestOpenAIResponsesEndpointFallback(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("x-request-id", "request-id")
		if len(paths) == 1 {
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(writer, `{"error":{"message":"endpoint not found","type":"invalid_request_error","code":"not_found"}}`)
			return
		}
		_, _ = io.WriteString(writer, `{"id":"completion","model":"gpt-4o","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer server.Close()

	modelProvider, err := New(Config{
		Name: NameOpenAI, APIKey: "key", BaseURL: server.URL,
		Model: "gpt-4o", ReasoningMode: ReasoningOff,
	})
	if err != nil {
		t.Fatalf("create OpenAI provider: %v", err)
	}
	result, err := modelProvider.Complete(context.Background(), CompletionRequest{
		Operation: OperationText, System: "system", User: "request",
	})
	if err != nil || result.Content != "ok" || result.RequestID != "request-id" {
		t.Fatalf("fallback completion = %q, %v", result.Content, err)
	}
	if len(paths) != 2 || !strings.HasSuffix(paths[0], "/responses") ||
		!strings.HasSuffix(paths[1], "/chat/completions") {
		t.Fatalf("fallback paths = %v", paths)
	}
	if modelProvider.Info().EffectiveTransport != "chat-completions" {
		t.Fatalf("fallback transport = %q", modelProvider.Info().EffectiveTransport)
	}
}

func TestOpenAIOrdinaryClientErrorDoesNotFallback(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, `{"error":{"message":"invalid request","type":"invalid_request_error","param":"input","code":"invalid_parameter"}}`)
	}))
	defer server.Close()

	modelProvider, err := New(Config{
		Name: NameOpenAI, APIKey: "key", BaseURL: server.URL,
		Model: "gpt-4o", ReasoningMode: ReasoningOff,
	})
	if err != nil {
		t.Fatalf("create OpenAI provider: %v", err)
	}
	_, err = modelProvider.Complete(context.Background(), CompletionRequest{
		Operation: OperationText, System: "system", User: "request",
	})
	if err == nil || calls != 1 ||
		modelProvider.Info().EffectiveTransport != "responses" {
		t.Fatalf("ordinary 400 triggered fallback: calls=%d, error=%v", calls, err)
	}
}

func TestIncompleteOutputIsNotAccepted(t *testing.T) {
	if err := validateChatFinish(CompletionResult{FinishReason: "length"}); !IsKind(err, ErrorOutputLimit) {
		t.Fatalf("chat length error = %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"id":"response","object":"response","created_at":1,"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"model":"gpt-5.6","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}`)
	}))
	defer server.Close()

	modelProvider, err := New(Config{
		Name: NameOpenAI, APIKey: "key", BaseURL: server.URL,
		Model: "gpt-5.6", ReasoningMode: ReasoningOff,
	})
	if err != nil {
		t.Fatalf("create OpenAI provider: %v", err)
	}
	result, err := modelProvider.Complete(context.Background(), CompletionRequest{
		Operation: OperationText, System: "system", User: "request",
	})
	if !IsKind(err, ErrorOutputLimit) || !result.OutputTruncated {
		t.Fatalf("incomplete response = %#v, %v", result, err)
	}
}

func TestDeepSeekThinkingFallback(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		requests = append(requests, body)
		writer.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(writer, `{"error":{"message":"thinking is not supported","type":"invalid_request_error","param":"thinking","code":"invalid_parameter"}}`)
			return
		}
		_, _ = io.WriteString(writer, `{"id":"completion","model":"deepseek-v4-pro","choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"","tool_calls":[{"id":"call","type":"function","function":{"name":"action","arguments":"{\"value\":\"ok\"}"}}]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer server.Close()

	modelProvider, err := New(Config{
		Name: NameDeepSeek, APIKey: "key", BaseURL: server.URL,
		Model: "deepseek-v4-pro", ReasoningMode: ReasoningAuto,
	})
	if err != nil {
		t.Fatalf("create DeepSeek provider: %v", err)
	}
	result, err := modelProvider.Complete(context.Background(), CompletionRequest{
		Operation: OperationAction, System: "system", User: "request",
		Tool: &ActionTool{Name: "action", Parameters: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("complete action: %v", err)
	}
	if result.Content != `{"value":"ok"}` || len(requests) != 2 {
		t.Fatalf("fallback result = %q, requests = %d", result.Content, len(requests))
	}
	if thinkingType(requests[0]) != "enabled" || requests[0]["reasoning_effort"] != "high" {
		t.Fatalf("first request did not enable thinking: %#v", requests[0])
	}
	if _, exists := requests[0]["temperature"]; exists {
		t.Fatalf("thinking request sent temperature: %#v", requests[0])
	}
	if thinkingType(requests[1]) != "disabled" {
		t.Fatalf("fallback request did not disable thinking: %#v", requests[1])
	}
	if got := requests[0]["max_tokens"]; got != float64(384_000) {
		t.Fatalf("DeepSeek max_tokens = %#v", got)
	}
}

func thinkingType(request map[string]any) string {
	thinking, _ := request["thinking"].(map[string]any)
	value, _ := thinking["type"].(string)
	return value
}
