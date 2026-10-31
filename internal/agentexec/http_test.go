package agentexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jumpserver/koko/internal/assetexec"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func httpTestEndpoint(t *testing.T, scheme string) (*Runtime, *HTTP, *atomic.Int32, chan string) {
	t.Helper()
	r := fileTestRuntime(t)
	resource, issuer := scheme+"://mcp.example/mcp", scheme+"://jumpserver.example"
	r.opts.MaxSessions = 128
	p := Principal{ContextToken: "context", UserID: "user", ClientID: "desktop", OrgID: "org", Resource: resource, AllOrganizations: true, Scopes: []string{"agent:access"}, ExpiresAt: time.Now().Add(time.Hour)}
	r.sessions = map[string]*liveSession{"session": {owner: owner(p), key: "connect-key", connection: &assetexec.Connection{}, info: Session{ID: "session", ActionID: "connect", Status: "active", SessionToken: "capability", ApprovalURL: "/core/approve/"}}}
	var introspections atomic.Int32
	remote := make(chan string, 1)
	r.core = coreFunc(func(_ context.Context, path string, body, out any) error {
		values := body.(map[string]any)
		switch path {
		case "introspect/":
			introspections.Add(1)
			if _, supplied := values["org_id"]; supplied {
				t.Fatal("client-selected organization reached Core")
			}
			if values["access_token"] != "valid" {
				return &CoreError{Status: http.StatusForbidden, Operation: path}
			}
			return respond(out, p)
		case "assets/":
			if values["context_token"] != p.ContextToken {
				return errors.New("missing principal")
			}
			return respond(out, map[string]any{"results": []any{map[string]any{"id": "asset"}}})
		case "sessions/get/":
			return respond(out, map[string]any{"session_id": "session", "session_token": "private", "approval_url": "/core/approve/"})
		case "sessions/list/", "actions/list/":
			return respond(out, map[string]any{"results": []any{map[string]any{"session_id": "session", "action_id": "approval", "audit_url": "/core/audit/"}}})
		case "actions/lookup/":
			return respond(out, Action{ID: "approval", ActionID: "approval"})
		case "sessions/":
			remote <- values["remote_addr"].(string)
			return respond(out, Session{ID: "session", ActionID: "connect", SessionToken: "capability"})
		case "actions/approval/get/":
			return respond(out, Action{ID: "approval", Status: "awaiting_approval", ApprovalURL: "/core/approve/", HandoffURL: "/core/handoff/", AuditURL: "/core/audit/", Result: map[string]any{"handoff_url": "/core/handoff/"}})
		case "actions/upload/get/", "actions/upload/authorize/":
			hash := sha256.Sum256([]byte("12345678"))
			return respond(out, Action{ID: "upload", Kind: "file_upload", Status: "queued", SessionID: "session", Request: map[string]any{"path": "/file", "size": 8, "sha256": hex.EncodeToString(hash[:])}})
		case "actions/download/get/":
			return respond(out, Action{ID: "download", Kind: "file_download", Status: "succeeded"})
		default:
			return errors.New("unexpected test Core request")
		}
	})
	endpoint, err := NewHTTP(r, resource, issuer)
	if err != nil {
		t.Fatal(err)
	}
	return r, endpoint, &introspections, remote
}

func TestHTTPRejectsInvalidPublicResource(t *testing.T) {
	for _, address := range []string{
		"https://host/mcp%", "https://host/mcp%2F", "https://host/mcp%00", "https://host/mcp%7F", "https://host/mcp%ff",
		"https://host/mcp%5Cpath", "https://host/mcp?", "https://host/mcp#", "https://host/mcp\n", "https://host:65536/mcp",
	} {
		if _, err := NewHTTP(&Runtime{}, address, "https://core.example"); err == nil {
			t.Fatalf("invalid MCP resource accepted: %q", address)
		}
	}
}

func httpTestRequest(t *testing.T, endpoint *HTTP, method, path, body, token, org, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "192.0.2.1:12345"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-JMS-ORG", org)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	endpoint.Handler.ServeHTTP(w, req)
	return w
}

func TestHTTPMCPNativeSDKAndAbsolutePublicLinks(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) { testHTTPMCPNativeSDKAndAbsolutePublicLinks(t, scheme) })
	}
}

func testHTTPMCPNativeSDKAndAbsolutePublicLinks(t *testing.T, scheme string) {
	t.Helper()
	r, endpoint, _, remote := httpTestEndpoint(t, scheme)
	var coreCalls atomic.Int32
	originalCore := r.core
	r.core = coreFunc(func(ctx context.Context, path string, body, out any) error {
		if path != "introspect/" {
			coreCalls.Add(1)
		}
		return originalCore.Call(ctx, path, body, out)
	})
	issuer := scheme + "://jumpserver.example"
	server := httptest.NewUnstartedServer(endpoint.Handler)
	if scheme == "https" {
		server.StartTLS()
	} else {
		server.Start()
	}
	defer server.Close()
	clientHTTP := server.Client()
	clientHTTP.Transport = authorizedTransport{base: clientHTTP.Transport, token: "valid"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "http-boundary-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + endpoint.Path, HTTPClient: clientHTTP}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if init := session.InitializeResult(); init == nil || init.ServerInfo.Name != "jumpserver-assets" {
		t.Fatal("native initialize failed")
	}
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	expectedTools := map[string]bool{
		"search_assets": true, "connect_asset": false, "query_sessions": true,
		"disconnect_asset": false, "execute_command": false, "query_actions": true,
		"cancel_action": false, "file_read": true, "file_write": false, "open_terminal": false,
	}
	if len(listed.Tools) != len(expectedTools) {
		t.Fatalf("expected 10 tools, got %d", len(listed.Tools))
	}
	for _, tool := range listed.Tools {
		if tool.OutputSchema == nil || tool.Annotations == nil {
			t.Fatalf("%s omitted its result schema or annotations", tool.Name)
		}
		readOnly, ok := expectedTools[tool.Name]
		if !ok || tool.Annotations.ReadOnlyHint != readOnly || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint == readOnly {
			t.Fatalf("unexpected tool or unsafe annotation: %s", tool.Name)
		}
		if tool.Name == "cancel_action" {
			var schema struct{ Properties map[string]json.RawMessage }
			encoded, err := json.Marshal(tool.InputSchema)
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(encoded, &schema); err != nil || len(schema.Properties) != 1 || schema.Properties["action_id"] == nil {
				t.Fatalf("cancel_action must expose only action_id: %s (%v)", encoded, err)
			}
		}
	}
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result == nil || result.IsError {
			t.Fatalf("native tools/call %s failed: %v %v", name, result, err)
		}
		encoded, _ := json.Marshal(result.StructuredContent)
		var value map[string]any
		if err = json.Unmarshal(encoded, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	if value := call("search_assets", map[string]any{"limit": 1}); len(value["results"].([]any)) != 1 {
		t.Fatal("search did not preserve authenticated result")
	}
	value := call("query_actions", map[string]any{"operation": "get", "action_id": "approval"})
	for key, expected := range map[string]string{"approval_url": issuer + "/core/approve/", "handoff_url": issuer + "/core/handoff/", "audit_url": issuer + "/core/audit/"} {
		if value[key] != expected {
			t.Fatalf("relative %s escaped public normalization: %v", key, value[key])
		}
	}
	if value["result"].(map[string]any)["handoff_url"] != issuer+"/core/handoff/" {
		t.Fatal("action result handoff remained relative")
	}
	if value := call("query_actions", map[string]any{"operation": "get", "idempotency_key": "original-key"}); value["action_id"] != "approval" {
		t.Fatal("action recovery lost its ID", value)
	}
	if value := call("query_sessions", map[string]any{"operation": "get", "idempotency_key": "connect-key"}); value["session_token"] != nil || value["connection_status"] != "ready" {
		t.Fatal("unsafe or inconsistent session recovery", value)
	}
	for _, name := range []string{"query_sessions", "query_actions"} {
		value := call(name, map[string]any{"operation": "list"})
		if value["results"].([]any)[0].(map[string]any)["audit_url"] != issuer+"/core/audit/" {
			t.Fatal("recovery audit link remained relative", name)
		}
	}
	if value := call("query_sessions", map[string]any{"operation": "get", "session_id": "session"}); value["session_token"] != nil || value["session_id"] != "session" {
		t.Fatal("session query exposed credentials or lost its ID")
	}
	for _, name := range []string{"get_action", "upload_file"} {
		before := coreCalls.Load()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if (err == nil && (result == nil || !result.IsError)) || coreCalls.Load() != before {
			t.Fatalf("unsupported tool reached Core: %s", name)
		}
	}
	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{"query_sessions", map[string]any{}},
		{"query_sessions", map[string]any{"operation": "connect"}},
		{"query_sessions", map[string]any{"operation": "get", "session_id": "session", "idempotency_key": "connect-key"}},
		{"query_sessions", map[string]any{"operation": "list", "limit": 101}},
		{"query_actions", map[string]any{"operation": "cancel", "action_id": "approval"}},
		{"query_actions", map[string]any{"operation": "get"}},
		{"query_actions", map[string]any{"operation": "get", "action_id": "approval", "idempotency_key": "original-key"}},
		{"query_actions", map[string]any{"operation": "get", "action_id": "approval", "limit": 33}},
		{"query_actions", map[string]any{"operation": "list", "action_id": "approval"}},
	} {
		before := coreCalls.Load()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: test.name, Arguments: test.args})
		if err != nil || result == nil || !result.IsError || result.StructuredContent == nil || coreCalls.Load() != before {
			t.Fatalf("invalid query reached Core or lost its error contract: %s %v %v", test.name, result, err)
		}
	}
	value = call("connect_asset", map[string]any{"asset_id": "asset", "account": "root", "protocol": "ssh", "idempotency_key": "connect-key"})
	if value["approval_url"] != issuer+"/core/approve/" {
		t.Fatal("public session approval remained relative")
	}
	if value["session_token"] != nil {
		t.Fatal("private capability exposed")
	}
	if host := <-remote; host != "127.0.0.1" {
		t.Fatalf("connection remote address included port: %q", host)
	}
}

func TestHTTPFileToolSchemasRequireExplicitContent(t *testing.T) {
	r, endpoint, _, _ := httpTestEndpoint(t, "https")
	originalCore := r.core
	var actions atomic.Int32
	submitted := make(chan map[string]any, 1)
	r.core = coreFunc(func(ctx context.Context, path string, body, out any) error {
		if path != "actions/" {
			return originalCore.Call(ctx, path, body, out)
		}
		actions.Add(1)
		values := body.(map[string]any)
		request := values["request"].(map[string]any)
		submitted <- request
		return respond(out, Action{ID: "file-action", Kind: values["kind"].(string), Status: "succeeded", Request: request})
	})
	server := httptest.NewTLSServer(endpoint.Handler)
	defer server.Close()
	clientHTTP := server.Client()
	clientHTTP.Transport = authorizedTransport{base: clientHTTP.Transport, token: "valid", org: "org"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "file-schema-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + endpoint.Path, HTTPClient: clientHTTP}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, test := range []struct {
		name, operation string
		extra           map[string]any
	}{
		{"file_write", "write", nil},
		{"file_write", "write", map[string]any{"content": nil}},
		{"file_write", "rename", nil},
		{"file_read", "read", map[string]any{"content": "ignored"}},
		{"file_read", "list", map[string]any{"destination": "/other"}},
		{"file_read", "list", map[string]any{"limit": 201}},
		{"file_write", "write", map[string]any{"content": "", "destination": "/other"}},
		{"file_write", "upload", map[string]any{"remote_path": "/file", "size": 1, "sha256": "invalid"}},
		{"file_read", "remove", nil},
	} {
		args := map[string]any{"operation": test.operation, "session_id": "session", "path": "/file", "idempotency_key": "file-key"}
		for key, value := range test.extra {
			args[key] = value
		}
		if test.operation == "upload" {
			delete(args, "path")
		}
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: test.name, Arguments: args})
		if err != nil || result == nil || !result.IsError || actions.Load() != 0 {
			t.Fatalf("invalid %s arguments reached an action: result=%v err=%v actions=%d", test.name, result, err, actions.Load())
		}
		if result.StructuredContent == nil {
			t.Fatal("argument error omitted its structured contract")
		}
	}
	if _, err = r.journal.Action("file-action"); !errors.Is(err, os.ErrNotExist) || len(r.jobs) != 0 {
		t.Fatal("invalid file arguments created local execution work")
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "file_write", Arguments: map[string]any{"operation": "write", "session_id": "session", "path": "/file", "content": "", "idempotency_key": "file-key"}})
	if err != nil || result == nil || result.IsError || actions.Load() != 1 {
		t.Fatalf("explicit empty content rejected: result=%v err=%v", result, err)
	}
	if request := <-submitted; request["content"] != "" {
		t.Fatal("explicit empty content was not preserved")
	}
	for _, test := range []struct {
		name, operation, kind string
		extra                 map[string]any
	}{
		{"file_read", "list", "file_list", nil},
		{"file_read", "stat", "file_stat", nil},
		{"file_read", "read", "file_read", nil},
		{"file_read", "download", "file_download", nil},
		{"file_write", "write", "file_write", map[string]any{"content": "text"}},
		{"file_write", "upload", "file_upload", map[string]any{"size": 1, "sha256": strings.Repeat("a", 64)}},
		{"file_write", "mkdir", "file_mkdir", nil},
		{"file_write", "rename", "file_rename", map[string]any{"destination": "/other"}},
		{"file_write", "remove", "file_remove", nil},
	} {
		args := map[string]any{"operation": test.operation, "session_id": "session", "path": "/file", "idempotency_key": "file-key"}
		if test.operation == "upload" || test.operation == "download" {
			delete(args, "path")
			args["remote_path"] = "/file"
		}
		for key, value := range test.extra {
			args[key] = value
		}
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: test.name, Arguments: args})
		if err != nil || result == nil || result.IsError {
			t.Fatalf("%s operation=%s failed: %v %v", test.name, test.operation, result, err)
		}
		var action Action
		encoded, _ := json.Marshal(result.StructuredContent)
		if json.Unmarshal(encoded, &action) != nil || action.Kind != test.kind || action.Request["path"] != "/file" {
			t.Fatalf("wrong operation submitted to Core: %s", encoded)
		}
		<-submitted
	}
	for i, content := range []string{strings.Repeat("a", 65536), strings.Repeat("\x00", 65536), strings.Repeat("中", 21846)} {
		before := actions.Load()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "file_write", Arguments: map[string]any{
			"operation": "write", "session_id": "session", "path": "/file", "content": content, "idempotency_key": "boundary-" + strconv.Itoa(i),
		}})
		if err != nil || result == nil || result.IsError != (i == 2) {
			t.Fatalf("incorrect UTF-8 content boundary: %v %v", result, err)
		}
		if i == 2 {
			if actions.Load() != before {
				t.Fatal("oversized UTF-8 content reached Core")
			}
		} else if request := <-submitted; request["content"] != content {
			t.Fatal("boundary content changed")
		}
	}
}

func TestHTTPAuthenticationMetadataOriginAndMCPBodyLimit(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) { testHTTPAuthenticationMetadataOriginAndMCPBodyLimit(t, scheme) })
	}
}

func TestHTTPAuthenticationDistinguishesCoreFailures(t *testing.T) {
	r, endpoint, _, _ := httpTestEndpoint(t, "https")
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"unauthorized", &CoreError{Status: 401}, 401},
		{"forbidden", &CoreError{Status: 403}, 401},
		{"bad request", &CoreError{Status: 400}, 400},
		{"server error", &CoreError{Status: 500}, 500},
		{"unavailable", &CoreError{Status: 503}, 500},
		{"timeout", context.DeadlineExceeded, 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			r.core = coreFunc(func(context.Context, string, any, any) error { return test.err })
			response := httpTestRequest(t, endpoint, "POST", endpoint.Path, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "valid", "", "")
			if response.Code != test.status {
				t.Fatalf("got HTTP %d, want %d", response.Code, test.status)
			}
			if test.status != 401 && response.Header().Get("WWW-Authenticate") != "" {
				t.Fatal("service or request error triggered OAuth login")
			}
		})
	}
}

func TestHTTPIssuerMatchesCoreOrigin(t *testing.T) {
	for _, test := range []struct{ issuer, want string }{
		{"https://jumpserver.example", "https://jumpserver.example"},
		{"https://jumpserver.example/", "https://jumpserver.example"},
		{"http://jumpserver.example:8080/", "http://jumpserver.example:8080"},
		{"https://jumpserver.example/core", ""},
		{"https://jumpserver.example//", ""},
		{"https://jumpserver.example/?", ""},
		{"https://jumpserver.example/#fragment", ""},
	} {
		t.Run(test.issuer, func(t *testing.T) {
			endpoint, err := NewHTTP(&Runtime{}, "https://jumpserver.example/koko/mcp", test.issuer)
			if test.want == "" {
				if err == nil {
					t.Fatal("unsupported issuer accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			endpoint.Metadata.ServeHTTP(response, httptest.NewRequest("GET", endpoint.MetadataPath, nil))
			var metadata struct {
				Issuers []string `json:"authorization_servers"`
			}
			if json.Unmarshal(response.Body.Bytes(), &metadata) != nil || len(metadata.Issuers) != 1 || metadata.Issuers[0] != test.want {
				t.Fatal("metadata and public links do not use Core's canonical origin")
			}
		})
	}
}

func TestHTTPSnapshotsKeepResourceBinding(t *testing.T) {
	resources := map[string]string{
		"old": "http://old.example/mcp",
		"new": "https://new.example/agent/mcp",
	}
	r := &Runtime{core: coreFunc(func(_ context.Context, _ string, body, out any) error {
		values := body.(map[string]any)
		resource := resources[values["access_token"].(string)]
		if resource == "" || values["resource"] != resource {
			return &CoreError{Status: http.StatusForbidden}
		}
		return respond(out, Principal{ContextToken: "context", UserID: "user", ClientID: "client", OrgID: "org", Resource: resource, AllOrganizations: true, Scopes: []string{"agent:access"}, ExpiresAt: time.Now().Add(time.Hour)})
	})}
	endpoints := make(map[string]*HTTP)
	for _, name := range []string{"old", "new"} {
		endpoint, err := NewHTTP(r, resources[name], "https://jumpserver.example/")
		if err != nil {
			t.Fatal(err)
		}
		endpoints[name] = endpoint
	}
	for _, name := range []string{"old", "new"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			endpoint := endpoints[name]
			metadata := httptest.NewRecorder()
			endpoint.Metadata.ServeHTTP(metadata, httptest.NewRequest("GET", endpoint.MetadataPath, nil))
			var value struct {
				Resource string `json:"resource"`
			}
			if json.Unmarshal(metadata.Body.Bytes(), &value) != nil || value.Resource != resources[name] {
				t.Fatal("metadata changed with another HTTP snapshot")
			}
			for token := range resources {
				want := http.StatusUnauthorized
				if token == name {
					want = http.StatusOK
				}
				response := httpTestRequest(t, endpoint, "POST", endpoint.Path, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, token, "", "")
				if response.Code != want {
					t.Fatalf("token %s returned %d, want %d", token, response.Code, want)
				}
			}
		})
	}
}

func testHTTPAuthenticationMetadataOriginAndMCPBodyLimit(t *testing.T, scheme string) {
	t.Helper()
	_, endpoint, introspections, _ := httpTestEndpoint(t, scheme)
	issuer := scheme + "://jumpserver.example"
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	for _, test := range []struct {
		name, token, org, origin string
		status                   int
	}{
		{"missing token", "", "org", "", 401}, {"wrong token", "wrong", "org", "", 401}, {"organization header cannot select scope", "valid", "other", "", 200}, {"wrong origin", "valid", "org", "https://untrusted.example", 403},
		{"same origin", "valid", "org", scheme + "://mcp.example", 200},
		{"organization from authorization", "valid", "", "", 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := introspections.Load()
			response := httpTestRequest(t, endpoint, "POST", endpoint.Path, body, test.token, test.org, test.origin)
			if response.Code != test.status {
				t.Fatalf("got HTTP %d", response.Code)
			}
			if test.status == 401 && !strings.Contains(response.Header().Get("WWW-Authenticate"), `resource_metadata="`+endpoint.Resource+`/.well-known/oauth-protected-resource"`) {
				t.Fatal("401 omitted protected resource metadata")
			}
			if test.status == 403 && before != introspections.Load() {
				t.Fatal("untrusted Origin reached Core")
			}
		})
	}
	metadata := httptest.NewRecorder()
	endpoint.Metadata.ServeHTTP(metadata, httptest.NewRequest("GET", endpoint.MetadataPath, nil))
	var result map[string]any
	if metadata.Code != 200 || json.Unmarshal(metadata.Body.Bytes(), &result) != nil || result["resource"] != endpoint.Resource || result["authorization_servers"].([]any)[0] != issuer {
		t.Fatal("public protected resource metadata is invalid")
	}
	oversized := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"padding":"` + strings.Repeat("x", 512<<10) + `"}}`
	if response := httpTestRequest(t, endpoint, "POST", endpoint.Path, oversized, "valid", "org", ""); response.Code != 413 {
		t.Fatalf("MCP body limit returned HTTP %d", response.Code)
	}
}

func TestHTTPFileTransferLengthAndReadLimits(t *testing.T) {
	r, endpoint, _, _ := httpTestEndpoint(t, "https")
	r.opts.MaxFileBytes = 8
	for _, test := range []struct {
		length int64
		status int
	}{{9, 413}, {-1, 411}} {
		req := httptest.NewRequest("POST", endpoint.Path+"/files/upload", strings.NewReader("123456789"))
		req.ContentLength = test.length
		req.Header.Set("Authorization", "Bearer valid")
		req.Header.Set("X-JMS-ORG", "org")
		response := httptest.NewRecorder()
		endpoint.Handler.ServeHTTP(response, req)
		if response.Code != test.status {
			t.Fatalf("invalid upload length returned %d", response.Code)
		}
	}
	hash := sha256.Sum256([]byte("12345678"))
	digest := hex.EncodeToString(hash[:])
	action := Action{ID: "upload", Kind: "file_upload", Status: "queued", SessionID: "session", Request: map[string]any{"path": "/file", "size": 8, "sha256": digest}}
	if err := r.journal.PutAction(action, "capability"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", endpoint.Path+"/files/upload", strings.NewReader("123456789"))
	req.ContentLength = 8 // Force the reader limit, independently of header checking.
	req.Header.Set("Authorization", "Bearer valid")
	req.Header.Set("X-JMS-ORG", "org")
	req.Header.Set("X-JumpServer-Action-ID", "upload")
	req.Header.Set("X-Content-SHA256", digest)
	response := httptest.NewRecorder()
	endpoint.Handler.ServeHTTP(response, req)
	if response.Code != 413 {
		t.Fatalf("upload reader exceeded declared limit: HTTP %d", response.Code)
	}
	part, _ := r.artifactPath("upload", ".part")
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal("oversized HTTP upload leaked staging file")
	}
	if err := r.journal.PutAction(Action{ID: "download", Kind: "file_download", Status: "succeeded"}, "capability"); err != nil {
		t.Fatal(err)
	}
	final, _ := r.artifactPath("download", ".data")
	if err := os.WriteFile(final, []byte("12345678"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.journal.update("download", func(rec *record) error { rec.Artifact, rec.Size, rec.SHA256 = final, 8, digest; return nil }); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest("GET", endpoint.Path+"/files/download", nil)
	req.Header.Set("Authorization", "Bearer valid")
	req.Header.Set("X-JMS-ORG", "org")
	req.Header.Set("X-JumpServer-Action-ID", "download")
	response = httptest.NewRecorder()
	endpoint.Handler.ServeHTTP(response, req)
	if response.Code != 200 || response.Body.String() != "12345678" || response.Header().Get("Content-Length") != "8" || response.Header().Get("X-Content-SHA256") != digest {
		t.Fatal("download did not preserve verified bytes and metadata")
	}
}

func TestHTTPRemoteIPAndLinkNormalizationPreserveApprovedRequest(t *testing.T) {
	for input, want := range map[string]string{"192.0.2.3:22": "192.0.2.3", "192.0.2.3": "192.0.2.3", "[2001:db8::1]:22": "2001:db8::1", "forged.example:22": ""} {
		if got := remoteIP(input); got != want {
			t.Fatalf("remote IP %q: %q", input, got)
		}
	}
	request := map[string]any{"handoff_url": "/literal-command-input"}
	action := Action{ApprovalURL: "/approve", Request: request}
	result := normalizeToolResult(action, "https://jumpserver.example").(Action)
	if result.ApprovalURL != "https://jumpserver.example/approve" || result.Request["handoff_url"] != "/literal-command-input" || action.ApprovalURL != "/approve" {
		t.Fatal("presentation normalization mutated approved data")
	}
}
