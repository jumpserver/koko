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
	p := Principal{ContextToken: "context", UserID: "user", ClientID: "desktop", OrgID: "org", Resource: resource, Scopes: []string{"agent:access"}, ExpiresAt: time.Now().Add(time.Hour)}
	r.sessions = map[string]*liveSession{"session": {owner: owner(p), key: "connect-key", connection: &assetexec.Connection{}, info: Session{ID: "session", ActionID: "connect", Status: "active", SessionToken: "capability", ApprovalURL: "/core/approve/"}}}
	var introspections atomic.Int32
	remote := make(chan string, 1)
	r.core = coreFunc(func(_ context.Context, path string, body, out any) error {
		values := body.(map[string]any)
		switch path {
		case "introspect/":
			introspections.Add(1)
			if (values["org_id"] != "" && values["org_id"] != "org") || values["access_token"] != "valid" {
				return &CoreError{Status: http.StatusForbidden, Operation: path}
			}
			return respond(out, p)
		case "assets/":
			if values["context_token"] != p.ContextToken {
				return errors.New("missing principal")
			}
			return respond(out, map[string]any{"results": []any{map[string]any{"id": "asset"}}})
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
	_, endpoint, _, remote := httpTestEndpoint(t, scheme)
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
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
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
	if !names["search_assets"] || !names["connect_asset"] || !names["get_action"] || !names["cancel_action"] || !names["upload_file"] {
		t.Fatal("native tools/list lost required tools")
	}
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("native tools/call %s failed: %v", name, err)
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
	value := call("get_action", map[string]any{"action_id": "approval"})
	for key, expected := range map[string]string{"approval_url": issuer + "/core/approve/", "handoff_url": issuer + "/core/handoff/", "audit_url": issuer + "/core/audit/"} {
		if value[key] != expected {
			t.Fatalf("relative %s escaped public normalization: %v", key, value[key])
		}
	}
	if value["result"].(map[string]any)["handoff_url"] != issuer+"/core/handoff/" {
		t.Fatal("action result handoff remained relative")
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
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{"list_files": "limit", "write_file": "content overwrite", "rename_file": "destination", "stat_file": "", "read_file": "", "make_directory": "", "remove_file": ""}
	for _, tool := range listed.Tools {
		extra, ok := fields[tool.Name]
		if !ok {
			continue
		}
		var schema struct{ Properties map[string]json.RawMessage }
		encoded, _ := json.Marshal(tool.InputSchema)
		if err = json.Unmarshal(encoded, &schema); err != nil {
			t.Fatal(err)
		}
		expected := strings.Fields("session_id path idempotency_key " + extra)
		if len(schema.Properties) != len(expected) {
			t.Fatalf("%s exposes unrelated fields: %s", tool.Name, encoded)
		}
		for _, name := range expected {
			if schema.Properties[name] == nil {
				t.Fatalf("%s omitted %s", tool.Name, name)
			}
		}
		delete(fields, tool.Name)
	}
	if len(fields) != 0 {
		t.Fatal("file tools missing from tools/list")
	}
	for _, test := range []struct {
		name  string
		extra map[string]any
	}{
		{"write_file", nil}, {"write_file", map[string]any{"content": nil}},
		{"rename_file", nil}, {"read_file", map[string]any{"content": "ignored"}},
		{"list_files", map[string]any{"destination": "/other"}},
		{"write_file", map[string]any{"content": "", "destination": "/other"}},
	} {
		args := map[string]any{"session_id": "session", "path": "/file", "idempotency_key": "file-key"}
		for key, value := range test.extra {
			args[key] = value
		}
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: test.name, Arguments: args})
		if err != nil || result == nil || !result.IsError || actions.Load() != 0 {
			t.Fatalf("invalid %s arguments reached an action: result=%v err=%v actions=%d", test.name, result, err, actions.Load())
		}
	}
	if _, err = r.journal.Action("file-action"); !errors.Is(err, os.ErrNotExist) || len(r.jobs) != 0 {
		t.Fatal("invalid file arguments created local execution work")
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "write_file", Arguments: map[string]any{"session_id": "session", "path": "/file", "content": "", "idempotency_key": "file-key"}})
	if err != nil || result == nil || result.IsError || actions.Load() != 1 {
		t.Fatalf("explicit empty content rejected: result=%v err=%v", result, err)
	}
	if request := <-submitted; request["content"] != "" {
		t.Fatal("explicit empty content was not preserved")
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
		return respond(out, Principal{ContextToken: "context", UserID: "user", ClientID: "client", OrgID: "org", Resource: resource, Scopes: []string{"agent:access"}, ExpiresAt: time.Now().Add(time.Hour)})
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
		{"missing token", "", "org", "", 401}, {"wrong token", "wrong", "org", "", 401}, {"wrong organization", "valid", "other", "", 401}, {"wrong origin", "valid", "org", "https://untrusted.example", 403},
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
	oversized := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"padding":"` + strings.Repeat("x", 128<<10) + `"}}`
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
