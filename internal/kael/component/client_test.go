package component

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestComponentRegistrationModelConfigAndHeartbeat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case registerPath:
			if request.Header.Get("Authorization") != "BootstrapToken bootstrap-secret" {
				t.Errorf("unexpected registration authorization: %s", request.Header.Get("Authorization"))
			}
			var payload map[string]string
			_ = json.NewDecoder(request.Body).Decode(&payload)
			if payload["type"] != componentType || payload["name"] != "kael-test" {
				t.Errorf("unexpected registration payload: %#v", payload)
			}
			_, _ = response.Write([]byte(`{"service_account":{"access_key":{"id":"access-id","secret":"access-secret"}}}`))
		case terminalConfigPath:
			assertSigned(t, request)
			_, _ = response.Write([]byte(`{"CHAT_AI_ENABLED":true,"CHAT_AI_PROVIDER":"openai_compatible","CHAT_AI_BASE_URL":"https://model.example.test/v1","CHAT_AI_API_KEY":"model-secret","CHAT_AI_MODEL":"model-1"}`))
		case heartbeatPath:
			assertSigned(t, request)
			_, _ = response.Write([]byte(`[]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	keyPath := filepath.Join(t.TempDir(), "keys", ".access_key")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := Connect(Options{CoreURL: server.URL, TLSVerify: true, Timeout: 30 * time.Second, Name: "kael-test", BootstrapToken: "bootstrap-secret", AccessKeyFile: keyPath})
	if err != nil {
		t.Fatal(err)
	}
	config, err := client.ModelConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if config.Provider != "openai_compatible" || config.BaseURL != "https://model.example.test/v1" || config.Model != "model-1" || config.APIKey != "model-secret" {
		t.Fatalf("unexpected model config: %#v", config)
	}
	if err = client.Heartbeat(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("access key was not stored privately: %#o", info.Mode().Perm())
	}
}

func TestComponentConnectRetryLimit(t *testing.T) {
	for _, path := range []string{registerPath, profilePath} {
		t.Run(path, func(t *testing.T) {
			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.URL.Path != path {
					t.Errorf("unexpected request path: %s", request.URL.Path)
				}
				attempts++
				response.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()

			keyPath := filepath.Join(t.TempDir(), ".access_key")
			if path == profilePath {
				if err := os.WriteFile(keyPath, []byte("access-id:access-secret"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			options := Options{CoreURL: server.URL, Name: "kael-test", BootstrapToken: "bootstrap-secret", AccessKeyFile: keyPath}
			_, err := connect(options, func(time.Duration) {})
			if err == nil || attempts != 10 {
				t.Fatalf("expected failure after 10 attempts: attempts=%d err=%v", attempts, err)
			}
		})
	}
}

func TestComponentReregistersUnauthorizedAccessKey(t *testing.T) {
	registrations := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case profilePath:
			assertSigned(t, request)
			if !strings.Contains(request.Header.Get("Authorization"), `keyId="access-id"`) {
				response.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = response.Write([]byte(`{"id":"service-account-id"}`))
		case registerPath:
			registrations++
			if registrations == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = response.Write([]byte(`{"service_account":{"access_key":{"id":"access-id","secret":"access-secret"}}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	keyPath := filepath.Join(t.TempDir(), ".access_key")
	if err := os.WriteFile(keyPath, []byte("expired-id:expired-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	options := Options{CoreURL: server.URL, Name: "kael-test", BootstrapToken: "bootstrap-secret", AccessKeyFile: keyPath}
	if _, err := connect(options, func(time.Duration) {}); err != nil {
		t.Fatal(err)
	}
	options.BootstrapToken = ""
	if _, err := Connect(options); err != nil {
		t.Fatalf("restart did not reuse the new access key: %v", err)
	}
	if registrations != 2 {
		t.Fatalf("expected registration to succeed on retry: registrations=%d", registrations)
	}
}

func assertSigned(t *testing.T, request *http.Request) {
	t.Helper()
	if !strings.HasPrefix(request.Header.Get("Authorization"), "Signature ") || request.Header.Get("X-JMS-ORG") != "ROOT" {
		t.Errorf("request was not signed as a component: authorization=%q org=%q", request.Header.Get("Authorization"), request.Header.Get("X-JMS-ORG"))
	}
}

func TestRuntimeStoreAppendTransportFailure(t *testing.T) {
	for _, refused := range []bool{true, false} {
		name, want := "response_lost", ErrRuntimeStoreCommitUncertain
		if refused {
			name, want = "connection_refused", ErrRuntimeStoreUnavailable
		}
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
			}))
			defer server.Close()
			if refused {
				server.Close()
			}
			client := connectedClient(Options{CoreURL: server.URL, Timeout: time.Second}, nil, accessKey{ID: "id", Secret: "secret"})
			defer client.openAPIClient.CloseIdleConnections()
			if _, err := client.AppendRuntimeStore(uuid.NewString(), 0, false, "record"); !errors.Is(err, want) {
				t.Fatalf("expected %v, got %v", want, err)
			}
		})
	}
}
