package agentexec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
)

func TestCoreClientSignsRequestsAndHidesErrorBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Signature ") || r.Header.Get("X-JMS-ORG") != "ROOT" {
			t.Error("missing registered component authentication")
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"secret":"asset-password"}`))
	}))
	defer server.Close()
	c, err := NewCoreClient(server.URL, model.AccessKey{ID: "component", Secret: "component-secret"})
	if err != nil {
		t.Fatal(err)
	}
	err = c.Call(context.Background(), "introspect/", map[string]any{"access_token": "external-token"}, &map[string]any{})
	if err == nil || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "external-token") {
		t.Fatal("unsafe Core error", err)
	}
}

func TestCoreClientHonorsCancellation(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	c, err := NewCoreClient(server.URL, model.AccessKey{ID: "component", Secret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = c.Call(ctx, "assets/", nil, &map[string]any{})
	if err == nil || time.Since(start) > time.Second {
		t.Fatal("Core cancellation did not bound the request")
	}
}

func TestCoreClientExplainsRejectedOperationsWithoutReflectingSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":["File operations require an SFTP session"],"password":"never-expose-this"}`))
	}))
	defer server.Close()
	c, err := NewCoreClient(server.URL, model.AccessKey{ID: "component", Secret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	err = c.Call(context.Background(), "actions/", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "protocol=sftp") || strings.Contains(err.Error(), "never-expose-this") {
		t.Fatalf("missing safe SFTP guidance: %v", err)
	}
	for status, expected := range map[int]string{403: "accounts[].id", 404: "Refresh search_assets"} {
		if message := (&CoreError{Status: status, Operation: "sessions/"}).Error(); !strings.Contains(message, expected) {
			t.Fatalf("missing correction for HTTP %d: %s", status, message)
		}
	}
}

func TestStableErrorsAndSafeRetryGuidance(t *testing.T) {
	var value any
	_ = json.Unmarshal([]byte(`{"code":"sftp_session_required","detail":"本地化说明","password":"never-expose-this"}`), &value)
	err := &CoreError{Status: 400, Operation: "actions/", Code: publicCoreError(value, 0)}
	result := toolError(err)
	if result.Code != "sftp_session_required" || result.NextStep != "choose_compatible_session" || strings.Contains(result.Message, "never-expose-this") {
		t.Fatal("stable error contract lost or exposed private details", result)
	}
	for _, path := range []string{"actions/", "actions/action/get/", "sessions/list/"} {
		result = toolError(&CoreError{Status: 503, Operation: path, RetryAfterMS: 3000})
		if result.Retryable != (path != "actions/") || result.RetryAfterMS != 3000 {
			t.Fatal("unsafe retry guidance", path, result)
		}
		if path == "actions/" && result.NextStep != "lookup_by_idempotency_key" {
			t.Fatal("unknown submission invited a replay")
		}
	}
}

func TestMalformedCoreSuccessDoesNotInviteMutationReplay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("invalid JSON")) }))
	defer server.Close()
	client, err := NewCoreClient(server.URL, model.AccessKey{ID: "component", Secret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Call(context.Background(), "actions/", nil, &Action{})
	if err == nil {
		t.Fatal("malformed success response accepted")
	}
	result := toolError(err)
	if result.Retryable || result.NextStep != "lookup_by_idempotency_key" {
		t.Fatal("ambiguous success invited a mutation replay", result)
	}
}

func TestTransferDescriptorPreservesExpiryWithoutCredentials(t *testing.T) {
	expires := time.Now().Add(time.Minute)
	action := Action{ID: "download", Kind: "file_download", Status: "succeeded", Transfer: &Transfer{ExpiresAt: &expires}}
	describeAction(&action, Principal{Resource: "https://host/mcp", ContextToken: "private-context"})
	if !action.Transfer.Ready || action.Transfer.ExpiresAt != &expires || !action.Transfer.RequiresClientTransfer || action.NextStep != "download_bytes" {
		t.Fatal("incomplete file transfer descriptor", action.Transfer)
	}
	encoded, _ := json.Marshal(action.Transfer)
	if strings.Contains(string(encoded), "private-context") || action.Transfer.Headers["Authorization"] != "" {
		t.Fatal("transfer descriptor exposed credentials")
	}
}

func TestIntrospectionRequiresCoreOrganization(t *testing.T) {
	resource := "https://jumpserver.example/koko/mcp"
	r := &Runtime{}
	r.core = coreFunc(func(_ context.Context, _ string, body, out any) error {
		if body.(map[string]any)["org_id"] != "" {
			t.Fatal("client must not invent an organization")
		}
		return respond(out, Principal{ContextToken: "context", UserID: "user", ClientID: "client", Resource: resource})
	})
	if _, err := r.Introspect(context.Background(), "token", "", resource); err == nil {
		t.Fatal("identity without Core-authorized organization was accepted")
	}
	r.core = coreFunc(func(_ context.Context, _ string, body, out any) error {
		if body.(map[string]any)["org_id"] != "" {
			t.Fatal("client must not select an organization for discovery")
		}
		return respond(out, Principal{ContextToken: "context", UserID: "user", ClientID: "client", Resource: resource,
			OrgID: "00000000-0000-0000-0000-000000000000", AllOrganizations: true})
	})
	if p, err := r.Introspect(context.Background(), "token", "", resource); err != nil || !p.AllOrganizations {
		t.Fatalf("Core-issued multi-organization scope was lost: %v", err)
	}
}
