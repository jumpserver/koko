package agentexec

import (
	"context"
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
}
