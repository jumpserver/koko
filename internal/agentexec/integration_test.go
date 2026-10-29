package agentexec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type authorizedTransport struct {
	base       http.RoundTripper
	token, org string
}

func (a authorizedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header = r.Header.Clone()
	r.Header.Set("Authorization", "Bearer "+a.token)
	r.Header.Set("X-JMS-ORG", a.org)
	return a.base.RoundTrip(r)
}

// The opt-in fixture belongs to an isolated Core/MySQL deployment. It supplies
// registered component credentials and a resource-bound test-user OAuth token.
func TestMCPThroughCoreToMySQL(t *testing.T) {
	path := os.Getenv("KOKO_AGENT_CORE_TEST_CONFIG")
	if path == "" {
		t.Skip("isolated Core fixture is not configured")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		CoreURL   string `json:"core_url"`
		Resource  string `json:"resource_url"`
		Token     string `json:"access_token"`
		KeyID     string `json:"component_key_id"`
		KeySecret string `json:"component_key_secret"`
		AssetID   string `json:"asset_id"`
		Account   string `json:"account"`
		OrgID     string `json:"org_id"`
		Protocol  string `json:"protocol"`
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	clear(data)
	if f.Protocol != "mysql" || !strings.HasPrefix(f.CoreURL, "http://127.0.0.1:") {
		t.Fatal("fixture must use the isolated local Core and MySQL")
	}
	core, err := NewCoreClient(f.CoreURL, model.AccessKey{ID: f.KeyID, Secret: f.KeySecret})
	if err != nil {
		t.Fatal(err)
	}
	jms, err := service.NewAuthJMService(service.JMSCoreHost(f.CoreURL), service.JMSAccessKey(f.KeyID, f.KeySecret))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(core, jms, Options{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	endpoint, err := NewHTTP(runtime, f.Resource, "https://agent-test.example")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(endpoint.Handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	httpClient := server.Client()
	httpClient.Transport = authorizedTransport{base: httpClient.Transport, token: f.Token, org: f.OrgID}
	client := mcp.NewClient(&mcp.Implementation{Name: "isolated-agent-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + endpoint.Path, HTTPClient: httpClient}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result, callErr := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if callErr != nil {
			t.Fatalf("%s: %v", name, callErr)
		}
		if result.IsError {
			t.Fatalf("%s returned a tool error: %v", name, result.Content)
		}
		raw, marshalErr := json.Marshal(result.StructuredContent)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		var out map[string]any
		if json.Unmarshal(raw, &out) != nil || out == nil {
			t.Fatalf("%s has no structured result", name)
		}
		return out
	}
	wait := func(id string) map[string]any {
		t.Helper()
		for ctx.Err() == nil {
			a := call("get_action", map[string]any{"action_id": id, "limit": 32})
			state, _ := a["status"].(string)
			if terminalState(state) {
				if state != "succeeded" {
					t.Fatalf("asset operation %s: %v", state, a["result"])
				}
				return a
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("operation timeout")
		return nil
	}
	assets := call("search_assets", map[string]any{"limit": 20})
	if len(assets["results"].([]any)) == 0 {
		t.Fatal("authorized asset discovery was empty")
	}
	connected := call("connect_asset", map[string]any{"asset_id": f.AssetID, "account": f.Account, "protocol": f.Protocol, "idempotency_key": uuid.NewString()})
	wait(connected["action_id"].(string))
	sid := connected["session_id"].(string)
	execute := func(command, key string) map[string]any {
		return call("execute_command", map[string]any{"session_id": sid, "command": command, "idempotency_key": key, "timeout_seconds": 5})
	}
	created := execute("CREATE TEMPORARY TABLE jms_agent_e2e (value INT)", uuid.NewString())
	wait(created["action_id"].(string))
	key := uuid.NewString()
	inserted := execute("INSERT INTO jms_agent_e2e VALUES (7)", key)
	wait(inserted["action_id"].(string))
	retry := execute("INSERT INTO jms_agent_e2e VALUES (7)", key)
	if retry["action_id"] != inserted["action_id"] {
		t.Fatal("retry created a second operation")
	}
	queried := execute("SELECT COUNT(*) AS rows_seen FROM jms_agent_e2e", uuid.NewString())
	qid := queried["action_id"].(string)
	wait(qid)
	// Wait for durable evidence delivery, then verify the actual DB result.
	var output strings.Builder
	for ctx.Err() == nil {
		runtime.flush(ctx)
		a := call("get_action", map[string]any{"action_id": qid, "limit": 32})
		output.Reset()
		if events, ok := a["events"].([]any); ok {
			for _, item := range events {
				payload, _ := item.(map[string]any)["payload"].(map[string]any)
				if payload["stream"] == "stdout" {
					if text, ok := payload["data"].(string); ok {
						output.WriteString(text)
					}
				}
			}
		}
		if strings.Contains(output.String(), "rows_seen\n1\n") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(output.String(), "rows_seen\n1\n") {
		t.Fatal("actual row count did not prove at-most-once dispatch")
	}
	closed := call("disconnect_asset", map[string]any{"session_id": sid})
	if closed["status"] != "closed" {
		t.Fatal("session did not close after operations drained")
	}
}
