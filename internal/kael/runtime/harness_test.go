package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/model"
)

func TestCompatibleCodexVersion(t *testing.T) {
	tests := map[string]bool{
		"codex-cli 0.144.4":   false,
		"codex-cli 0.153.1":   false,
		"codex-cli 0.153.2":   true,
		"codex-cli 0.153.999": true,
		"codex-cli 0.154.0":   true,
		"codex-cli 1.0.0":     true,
		"codex-cli 0.153":     false,
		"codex-cli 0.153.dev": false,
		"codex 0.153.2":       false,
	}
	for output, want := range tests {
		t.Run(output, func(t *testing.T) {
			if got := compatibleCodexVersion(output); got != want {
				t.Fatalf("compatibleCodexVersion(%q) = %t, want %t", output, got, want)
			}
		})
	}
}

// Runs an actual compatible Codex executable against a local Responses fixture.
// No real model request or external API credential is used.
func TestCodexAppServerIntegration(t *testing.T) {
	binary := os.Getenv("KAEL_TEST_CODEX")
	if binary == "" {
		t.Skip("set KAEL_TEST_CODEX to a compatible Codex binary for the protocol integration test")
	}
	var mu sync.Mutex
	requests := 0
	var firstInput []any
	var firstInstructions any
	releaseCancelledRequest := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/responses") {
			t.Errorf("unexpected provider route: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("missing configured provider authentication")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid model request")
			w.WriteHeader(400)
			return
		}
		for _, raw := range body["tools"].([]any) {
			tool := raw.(map[string]any)
			name, _ := tool["name"].(string)
			if name != "kael_inspect" && name != "update_plan" && name != "request_user_input" && name != "skills" {
				t.Errorf("unexpected environment tool: %v", tool["name"])
			}
		}
		encodedInput, _ := json.Marshal(body["input"])
		if strings.Contains(string(encodedInput), "cancel fixture") {
			select {
			case <-r.Context().Done():
			case <-releaseCancelledRequest:
			}
			return
		}
		mu.Lock()
		requests++
		n := requests
		currentInput := body["input"].([]any)
		if n == 1 {
			firstInput, firstInstructions = currentInput, body["instructions"]
		}
		if n == 3 && (len(currentInput) < len(firstInput) || !reflect.DeepEqual(firstInput, currentInput[:len(firstInput)]) || !reflect.DeepEqual(firstInstructions, body["instructions"])) {
			t.Error("switching response language changed the existing model request prefix")
		}
		mu.Unlock()
		expectedLanguage := "Simplified Chinese"
		if n > 2 {
			expectedLanguage = "Traditional Chinese"
		}
		preference := strings.LastIndex(string(encodedInput), "Luna response language: ")
		if preference < 0 || !strings.HasPrefix(string(encodedInput)[preference:], "Luna response language: "+expectedLanguage) {
			t.Errorf("model request is missing the current response language: %s", expectedLanguage)
		}
		if n%2 == 0 && !strings.Contains(string(encodedInput), "healthy") {
			t.Error("tool receipt was not returned to the model")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(kind string, value map[string]any) {
			value["type"] = kind
			raw, _ := json.Marshal(value)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, raw)
			w.(http.Flusher).Flush()
		}
		responseID := fmt.Sprintf("resp_%d", n)
		base := map[string]any{"id": responseID, "object": "response", "created_at": 1, "status": "in_progress", "model": "gpt-5.4", "output": []any{}}
		emit("response.created", map[string]any{"response": base})
		var item map[string]any
		if n%2 == 1 {
			item = map[string]any{"id": fmt.Sprintf("fc_%d", n), "type": "function_call", "name": "kael_inspect", "call_id": fmt.Sprintf("call_%d", n), "arguments": "{}", "status": "completed"}
			emit("response.output_item.added", map[string]any{"output_index": 0, "item": item})
			emit("response.output_item.done", map[string]any{"output_index": 0, "item": item})
		} else {
			item = map[string]any{"id": fmt.Sprintf("msg_%d", n), "type": "message", "role": "assistant", "phase": "final_answer", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "Verified healthy.", "annotations": []any{}}}}
			emit("response.output_item.added", map[string]any{"output_index": 0, "item": map[string]any{"id": item["id"], "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}})
			emit("response.output_text.delta", map[string]any{"item_id": item["id"], "output_index": 0, "content_index": 0, "delta": "Verified healthy."})
			emit("response.output_item.done", map[string]any{"output_index": 0, "item": item})
		}
		base["status"] = "completed"
		base["output"] = []any{item}
		base["usage"] = map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12, "input_tokens_details": map[string]any{"cached_tokens": 0}, "output_tokens_details": map[string]any{"reasoning_tokens": 0}}
		emit("response.completed", map[string]any{"response": base})
	}))
	defer server.Close()
	defer close(releaseCancelledRequest)
	config := model.Config{Provider: "openai", Model: "gpt-5.4", APIKey: "fixture-key", BaseURL: server.URL + "/v1"}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	harness, err := NewHarness(ctx, binary, t.TempDir(), func(context.Context) (model.Config, error) { return config, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	input := Input{Run: domain.Run{SubjectID: "user", OrganizationID: "org", ConversationID: "conversation", PanelSessionID: "panel"}, OutputMessageID: "out-1", Messages: []domain.Message{{ID: "in-1", Role: "user", Content: "Inspect the service and verify health."}}, Registrations: []domain.Registration{{ID: "registration", Name: "inspect", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`), AnnotationsJSON: json.RawMessage(`{"readOnlyHint":true}`)}}}
	calls := 0
	input.Context = &domain.ContextSnapshot{Data: json.RawMessage(`{"response_language":"zh"}`)}
	output := ""
	callbacks := Callbacks{ModelStarted: func(int, model.Info) error { return nil }, ModelCompleted: func(int, model.Result, time.Duration) error { return nil }, MessageDelta: func(text string) error { output += text; return nil }, CallTool: func(_ context.Context, reg domain.Registration, args json.RawMessage, _ int64) (ToolObservation, error) {
		calls++
		if reg.ID != "registration" || string(args) != "{}" {
			t.Error("lost tool binding")
		}
		return ToolObservation{Status: "success", Result: json.RawMessage(`{"healthy":true}`)}, nil
	}}
	result, err := harness.Execute(ctx, input, callbacks)
	if err != nil {
		t.Fatalf("first turn: %v (model requests=%d)", err, requests)
	}
	if result.Answer != "Verified healthy." || output != result.Answer || calls != 1 || result.Usage.InputTokens != 20 {
		t.Fatalf("unexpected turn: %+v, output=%q calls=%d", result, output, calls)
	}
	key := digest([]string{"user", "org", "conversation", "panel"})
	firstThread := harness.sessions[key].thread
	input.Messages = append(input.Messages, domain.Message{ID: "out-1", Role: "assistant", Content: result.Answer}, domain.Message{ID: "in-2", Role: "user", Content: "Verify again."})
	input.OutputMessageID = "out-2"
	input.Context = &domain.ContextSnapshot{Data: json.RawMessage(`{"response_language":"zh_hant"}`)}
	output = ""
	result, err = harness.Execute(ctx, input, callbacks)
	if err != nil {
		t.Fatalf("second turn: %v", err)
	}
	if calls != 2 || harness.sessions[key].thread != firstThread || result.Usage.InputTokens != 20 {
		t.Fatalf("continuation or incremental usage failed: calls=%d usage=%+v", calls, result.Usage)
	}
	input.Messages = append(input.Messages, domain.Message{ID: "out-2", Role: "assistant", Content: result.Answer}, domain.Message{ID: "in-3", Role: "user", Content: "Inspect using the refreshed capability."})
	input.OutputMessageID = "out-3"
	input.Registrations[0].DefinitionVersion = "2"
	result, err = harness.Execute(ctx, input, callbacks)
	if err != nil {
		t.Fatalf("registry refresh: %v", err)
	}
	if harness.sessions[key].thread == firstThread {
		t.Fatal("changed registration reused an old tool snapshot")
	}
	input.Messages = append(input.Messages, domain.Message{ID: "out-3", Role: "assistant", Content: result.Answer}, domain.Message{ID: "in-4", Role: "user", Content: "cancel fixture"})
	input.OutputMessageID = "out-4"
	cancelCtx, cancelTurn := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancelTurn()
	_, err = harness.Execute(cancelCtx, input, callbacks)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("actual process cancellation: %v", err)
	}
	if len(harness.sessions) != 0 {
		t.Fatal("cancelled Codex session retained for reuse")
	}

}
