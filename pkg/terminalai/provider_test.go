package terminalai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
)

func TestNewProviderSelection(t *testing.T) {
	config := ProviderConfig{
		APIKey: "test-key",
		Model:  "test-model",
	}
	provider, err := NewProvider(config)
	if err != nil {
		t.Fatalf("create default provider: %v", err)
	}
	if got := provider.Info().Name; got != ProviderOpenAI {
		t.Fatalf("default provider = %q, want %q", got, ProviderOpenAI)
	}

	config.Name = "unknown"
	if _, err = NewProvider(config); err == nil {
		t.Fatal("expected unknown provider to fail")
	}
}

func TestModelClientUsesProviderEnvironment(t *testing.T) {
	t.Setenv(ProviderEnvName, ProviderDeepSeek)
	client, err := NewModelClient(model.TerminalConfig{
		GptApiKey: "test-key",
		GptModel:  "test-model",
	})
	if err != nil {
		t.Fatalf("create model client: %v", err)
	}
	if got := client.ProviderInfo().Name; got != ProviderDeepSeek {
		t.Fatalf("provider = %q, want %q", got, ProviderDeepSeek)
	}
}

func TestRuntimeCapabilityIncludesProviderInfo(t *testing.T) {
	provider, err := NewProvider(ProviderConfig{
		Name:   ProviderOpenAI,
		APIKey: "test-key",
		Model:  "test-model",
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	var message ChatMessage
	runtime := NewRuntime(
		1,
		&ModelClient{provider: provider},
		nil,
		nil,
		func(value ChatMessage) {
			message = value
		},
	)
	defer runtime.Close()
	runtime.AnnounceCapability()

	if len(message.Parts) != 1 || message.Parts[0].Type != "data-capability" {
		t.Fatalf("unexpected capability message: %#v", message)
	}
	capability, ok := message.Parts[0].Data.(map[string]any)
	if !ok {
		t.Fatalf("capability data = %#v", message.Parts[0].Data)
	}
	if capability["provider"] != ProviderOpenAI ||
		capability["model"] != "test-model" {
		t.Fatalf("capability provider info = %#v", capability)
	}
}

func TestRegisterProviderRejectsDuplicate(t *testing.T) {
	const name = "provider-registry-test"
	factory := func(ProviderConfig) (Provider, error) {
		return nil, nil
	}
	if err := RegisterProvider(name, factory); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	if err := RegisterProvider(name, factory); err == nil {
		t.Fatal("expected duplicate provider registration to fail")
	}
}

func TestOpenAICompatibleProviders(t *testing.T) {
	for _, providerName := range []string{ProviderOpenAI, ProviderDeepSeek} {
		t.Run(providerName, func(t *testing.T) {
			var requestCount int
			server := httptest.NewServer(http.HandlerFunc(
				func(writer http.ResponseWriter, request *http.Request) {
					requestCount++
					if request.URL.Path != "/v1/chat/completions" {
						t.Errorf("request path = %q", request.URL.Path)
					}
					if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
						t.Errorf("authorization header = %q", got)
					}
					var body map[string]any
					if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
						t.Errorf("decode request: %v", err)
						writer.WriteHeader(http.StatusBadRequest)
						return
					}
					if got := body["model"]; got != "test-model" {
						t.Errorf("request model = %#v", got)
					}
					if _, exists := body["tools"]; exists {
						t.Error("request unexpectedly contains tools")
					}
					if _, exists := body["tool_choice"]; exists {
						t.Error("request unexpectedly contains tool_choice")
					}

					content := "plain response"
					if format, exists := body["response_format"]; exists {
						value, ok := format.(map[string]any)
						if !ok || value["type"] != "json_object" {
							t.Errorf("response format = %#v", format)
						}
						content = `{"kind":"answer","answer":"ok"}`
					}
					writer.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprintf(writer, `{
						"id":"chatcmpl-test",
						"object":"chat.completion",
						"created":1,
						"model":"test-model",
						"choices":[{
							"index":0,
							"message":{
								"role":"assistant",
								"reasoning_content":"private reasoning",
								"content":%q
							},
							"finish_reason":"stop"
						}]
					}`, content)
				},
			))
			defer server.Close()

			provider, err := NewProvider(ProviderConfig{
				Name:    providerName,
				APIKey:  "test-key",
				BaseURL: server.URL + "/v1",
				Model:   "test-model",
			})
			if err != nil {
				t.Fatalf("create provider: %v", err)
			}
			if info := provider.Info(); info.Name != providerName ||
				info.Model != "test-model" ||
				!info.Capabilities.StructuredOutput ||
				!info.Capabilities.ToolCall ||
				info.Capabilities.Streaming {
				t.Fatalf("unexpected provider info: %#v", info)
			}

			jsonContent, err := provider.CompleteJSON(
				context.Background(), "return JSON", "request",
			)
			if err != nil {
				t.Fatalf("complete JSON: %v", err)
			}
			if jsonContent != `{"kind":"answer","answer":"ok"}` {
				t.Fatalf("JSON content = %q", jsonContent)
			}
			textContent, err := provider.CompleteText(
				context.Background(), "return text", "request",
			)
			if err != nil {
				t.Fatalf("complete text: %v", err)
			}
			if textContent != "plain response" {
				t.Fatalf("text content = %q", textContent)
			}
			if requestCount != 2 {
				t.Fatalf("request count = %d, want 2", requestCount)
			}
		})
	}
}

func TestProviderUsesSingleNativeToolCall(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
				t.Errorf("decode request: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(writer, `{
				"id":"chatcmpl-tool",
				"object":"chat.completion",
				"created":1,
				"model":"test-model",
				"choices":[{
					"index":0,
					"message":{
						"role":"assistant",
						"content":"",
						"tool_calls":[{
							"id":"call-1",
							"type":"function",
							"function":{
								"name":"react_next",
								"arguments":"{\"kind\":\"finish\"}"
							}
						}]
					},
					"finish_reason":"tool_calls"
				}]
			}`)
		},
	))
	defer server.Close()
	provider, err := NewProvider(ProviderConfig{
		Name: ProviderOpenAI, APIKey: "test-key",
		BaseURL: server.URL + "/v1", Model: "test-model",
		ToolCallMode: ToolCallEnabled,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	content, err := provider.CompleteAction(
		context.Background(), "system", "user",
		ActionTool{
			Name: "react_next", Description: "next",
			Parameters: map[string]any{"type": "object"},
		},
	)
	if err != nil {
		t.Fatalf("complete action: %v", err)
	}
	if content != `{"kind":"finish"}` {
		t.Fatalf("tool arguments = %q", content)
	}
	if requestBody["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls = %#v", requestBody["parallel_tool_calls"])
	}
	tools, ok := requestBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", requestBody["tools"])
	}
	choice, ok := requestBody["tool_choice"].(map[string]any)
	if !ok || choice["type"] != "function" {
		t.Fatalf("tool_choice = %#v", requestBody["tool_choice"])
	}
}

func TestProviderAutoFallsBackWhenToolsAreUnsupported(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			requestCount++
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode request: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			writer.Header().Set("Content-Type", "application/json")
			if _, hasTools := body["tools"]; hasTools {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(writer, `{
					"error":{
						"message":"tools are not supported",
						"type":"invalid_request_error",
						"param":"tools",
						"code":"unsupported"
					}
				}`)
				return
			}
			_, _ = fmt.Fprint(writer, `{
				"id":"chatcmpl-json",
				"object":"chat.completion",
				"created":1,
				"model":"test-model",
				"choices":[{
					"index":0,
					"message":{
						"role":"assistant",
						"content":"{\"kind\":\"finish\"}"
					},
					"finish_reason":"stop"
				}]
			}`)
		},
	))
	defer server.Close()
	provider, err := NewProvider(ProviderConfig{
		Name: ProviderOpenAI, APIKey: "test-key",
		BaseURL: server.URL + "/v1", Model: "test-model",
		ToolCallMode: ToolCallAuto,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	content, err := provider.CompleteAction(
		context.Background(), "system", "user",
		ActionTool{
			Name: "react_next", Description: "next",
			Parameters: map[string]any{"type": "object"},
		},
	)
	if err != nil {
		t.Fatalf("complete action: %v", err)
	}
	if content != `{"kind":"finish"}` {
		t.Fatalf("fallback content = %q", content)
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2", requestCount)
	}
	if provider.Info().Capabilities.ToolCall {
		t.Fatal("tool call capability remained enabled after fallback")
	}
}

func TestModelRequestBudgetCountsProviderRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(writer, `{
				"id":"chatcmpl-json",
				"object":"chat.completion",
				"created":1,
				"model":"test-model",
				"choices":[{
					"index":0,
					"message":{"role":"assistant","content":"{}"},
					"finish_reason":"stop"
				}]
			}`)
		},
	))
	defer server.Close()
	provider, err := NewProvider(ProviderConfig{
		Name: ProviderOpenAI, APIKey: "test-key",
		BaseURL: server.URL + "/v1", Model: "test-model",
		ToolCallMode: ToolCallDisabled,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	ctx := withModelRequestBudget(context.Background(), 1)
	if _, err = provider.CompleteJSON(ctx, "system", "user"); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if _, err = provider.CompleteJSON(ctx, "system", "user"); err == nil ||
		!strings.Contains(err.Error(), errModelRequestLimit) {
		t.Fatalf("second request error = %v", err)
	}
}

func TestProviderLive(t *testing.T) {
	if os.Getenv("TERMINAL_AI_LIVE_TEST") != "1" {
		t.Skip("live provider test is disabled")
	}
	providerName := strings.TrimSpace(os.Getenv(ProviderEnvName))
	if providerName == "" {
		t.Fatal("TERMINAL_AI_PROVIDER is required for the live test")
	}
	provider, err := NewProvider(ProviderConfig{
		Name:         providerName,
		APIKey:       os.Getenv("KOKO_DEV_AI_API_KEY"),
		BaseURL:      os.Getenv("KOKO_DEV_AI_BASE_URL"),
		Model:        os.Getenv("KOKO_DEV_AI_MODEL"),
		Proxy:        os.Getenv("KOKO_DEV_AI_PROXY"),
		ToolCallMode: os.Getenv(ToolCallEnvName),
	})
	if err != nil {
		t.Fatalf("create live provider: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	content, err := provider.CompleteJSON(
		ctx,
		`Return a JSON object only. Do not include markdown.`,
		`Return {"ok":true}.`,
	)
	if err != nil {
		t.Fatalf("live provider request: %v", err)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err = json.Unmarshal([]byte(content), &result); err != nil {
		t.Fatalf("decode live provider response: %v", err)
	}
	if !result.OK {
		t.Fatal("live provider returned an unexpected result")
	}

	client := &ModelClient{provider: provider}
	request := ReActRequest{
		Question:    "This is a provider protocol health check. Do not execute a command; finish immediately.",
		PlanSummary: "Validate the ReAct action transport",
		Steps: []Step{{
			ID: "health-step", Title: "Validate transport",
			Objective: "Return a finish action without terminal execution",
			Status:    StepPending, rootStepID: "health-step",
		}},
		Profile:  "protocol: health-check",
		Snapshot: "No terminal execution is required.",
		Mode:     ModePTYOnly, Round: 1, MaxRounds: maxReActRounds,
	}
	plan := newReActPlan("health-plan", "Validate transport", []Step{{
		ID: "health-step", Title: "Validate transport",
		Objective: "Return a finish action without terminal execution",
		Status:    StepPending, rootStepID: "health-step",
	}})
	var action ReActDecision
	var validationErr error
	for attempt := 0; attempt < 2; attempt++ {
		action, err = client.Next(ctx, request)
		if err != nil {
			t.Fatalf("live ReAct action: %v", err)
		}
		if action.Kind != ReActFinish {
			validationErr = fmt.Errorf(
				"health check requires a finish action, got %q", action.Kind,
			)
		} else {
			_, validationErr = plan.preview(action)
		}
		if validationErr == nil {
			break
		}
		request.Correction = validationErr.Error()
	}
	if validationErr != nil {
		t.Fatalf(
			"validate live ReAct action after correction: %v (nextStep=%t proposal=%t summaryLength=%d)",
			validationErr, action.NextStepID != "", action.Proposal != nil,
			len(action.Summary),
		)
	}
	t.Logf(
		"live ReAct transport completed with toolCall=%t",
		provider.Info().Capabilities.ToolCall,
	)
}
