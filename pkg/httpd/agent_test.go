package httpd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/internal/agentexec"
	"github.com/jumpserver/koko/pkg/config"
)

func TestAgentConfigRefreshAndRouting(t *testing.T) {
	type reply struct {
		status int
		body   string
	}
	var response atomic.Value
	response.Store(reply{200, `{}`})
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/terminal/terminals/config/" || req.Header.Get("Authorization") == "" {
			t.Error("configuration did not use the authenticated component endpoint")
		}
		value := response.Load().(reply)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(value.status)
		_, _ = w.Write([]byte(value.body))
	}))
	defer core.Close()
	api, err := service.NewAuthJMService(service.JMSCoreHost(core.URL), service.JMSAccessKey("component", "secret"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{apiClient: api, agentRuntime: &agentexec.Runtime{}, agentRequests: make(chan struct{}, 32)}
	router := gin.New()
	router.GET("/koko/health/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.GET("/koko/api/session/:id/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	s.registerAgentRoutes(router)
	request := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	if request("/koko/mcp").Code != 503 || request("/koko/health/").Code != 204 {
		t.Fatal("unconfigured MCP interrupted ordinary Koko routes")
	}
	configJSON := func(resource, issuer string) string {
		data, _ := json.Marshal(map[string]any{"MCP_ENABLED": true, "MCP_PUBLIC_URL": resource, "MCP_AUTHORIZATION_SERVER": issuer})
		return string(data)
	}
	resource, issuer := "http://127.0.0.1:5000/koko/mcp", "http://core.example"
	for _, test := range []struct {
		name, body, want, issuer string
		status                   int
		fail                     bool
	}{
		{"missing at startup", `{}`, "", "", 200, true},
		{"disabled at startup", `{"MCP_ENABLED":false}`, "", "", 200, false},
		{"first address", configJSON(resource, issuer), resource, issuer, 200, false},
		{"unchanged", configJSON(resource, issuer), resource, issuer, 200, false},
		{"missing", `{}`, resource, issuer, 200, true},
		{"null switch", `{"MCP_ENABLED":null}`, resource, issuer, 200, true},
		{"missing addresses", `{"MCP_ENABLED":true}`, resource, issuer, 200, true},
		{"null addresses", `{"MCP_ENABLED":true,"MCP_PUBLIC_URL":null,"MCP_AUTHORIZATION_SERVER":null}`, resource, issuer, 200, true},
		{"invalid", configJSON("invalid", issuer), resource, issuer, 200, true},
		{"invalid issuer", configJSON(resource, "invalid"), resource, issuer, 200, true},
		{"empty enabled address", configJSON("", issuer), resource, issuer, 200, true},
		{"static route conflict", configJSON("http://mcp.example/koko/health", issuer), resource, issuer, 200, true},
		{"parameter route conflict", configJSON("http://mcp.example/koko/api/session/id", issuer), resource, issuer, 200, true},
		{"fetch failure", `{"detail":"sensitive-storage-key"}`, resource, issuer, 503, true},
		{"issuer update", configJSON(resource, "https://new-core.example"), resource, "https://new-core.example", 200, false},
		{"new address and path", configJSON("https://new.example/new/mcp", issuer), "https://new.example/new/mcp", issuer, 200, false},
		{"disable with invalid addresses", `{"MCP_ENABLED":false,"MCP_PUBLIC_URL":"invalid"}`, "", "", 200, false},
		{"reenable", configJSON(resource, issuer), resource, issuer, 200, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := s.agentHTTP.Load()
			runtime := s.agentRuntime
			response.Store(reply{test.status, test.body})
			err := s.refreshAgentConfig(context.Background())
			if (err != nil) != test.fail || (err != nil && strings.Contains(err.Error(), "sensitive-storage-key")) {
				t.Fatalf("unexpected refresh error: %v", err)
			}
			current := s.agentHTTP.Load()
			if s.agentRuntime != runtime {
				t.Fatal("configuration update replaced the execution runtime")
			}
			if test.fail && current != before {
				t.Fatal("failed refresh replaced the last valid configuration")
			}
			if test.name == "unchanged" && current != before {
				t.Fatal("unchanged configuration rebuilt the handler")
			}
			if test.want == "" {
				if current != nil || request("/koko/mcp").Code != 503 {
					t.Fatal("empty configuration did not leave MCP waiting")
				}
				return
			}
			paths := agentPaths(current.Path)
			for _, path := range paths[3:] {
				w := request(path)
				var metadata struct {
					Resource string
					Issuers  []string `json:"authorization_servers"`
				}
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &metadata) != nil || metadata.Resource != test.want || len(metadata.Issuers) != 1 || metadata.Issuers[0] != test.issuer {
					t.Fatalf("metadata did not follow the configured resource: %s", w.Body.String())
				}
			}
			if request(current.Path).Code != 401 || request("/koko/health/").Code != 204 {
				t.Fatal("dynamic route lost authentication or replaced a Koko route")
			}
			if before != nil && before.Path != current.Path && request(before.Path).Code != 404 {
				t.Fatal("retired MCP route still accepts new requests")
			}
		})
	}
}

func TestAgentRefreshStopsAfterCancelledFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"MCP_ENABLED":true,"MCP_PUBLIC_URL":"http://mcp.example/koko/mcp","MCP_AUTHORIZATION_SERVER":"http://core.example"}`))
	}))
	defer core.Close()
	api, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{apiClient: api, agentDone: make(chan struct{})}
	go func() { s.runAgentRefresh(ctx, make(chan time.Time)); close(s.agentDone) }()
	defer func() {
		close(release)
		select {
		case <-s.agentDone:
			if s.agentHTTP.Load() != nil {
				t.Error("configuration was applied after shutdown")
			}
		case <-time.After(time.Second):
			t.Error("configuration refresh did not stop after the fetch completed")
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("configuration fetch did not start")
	}
	cancel()
	deadline, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	waited := make(chan struct{})
	go func() { s.waitAgentRefresh(deadline); close(waited) }()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("blocked component fetch extended the shutdown deadline")
	}
}

func TestAgentDisabledStartupAndAuditRecovery(t *testing.T) {
	configRequests, events := make(chan struct{}, 1), make(chan struct{}, 1)
	var unavailable atomic.Bool
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/api/v1/terminal/terminals/config/" {
			select {
			case configRequests <- struct{}{}:
			default:
			}
			if unavailable.Load() {
				w.WriteHeader(503)
				return
			}
			_, _ = w.Write([]byte(`{"MCP_ENABLED":false}`))
			return
		}
		if req.URL.Path != "/api/v1/terminal/agent/actions/recovered/events/" {
			t.Errorf("unexpected request %s", req.URL.Path)
		}
		_, _ = w.Write([]byte(`{}`))
		select {
		case events <- struct{}{}:
		default:
		}
	}))
	defer core.Close()
	api, err := service.NewAuthJMService(service.JMSCoreHost(core.URL), service.JMSAccessKey("component", "secret"))
	if err != nil {
		t.Fatal(err)
	}
	previous := config.GlobalConfig
	conf := config.GetConf()
	conf.DataFolderPath, conf.CoreHost = t.TempDir(), core.URL
	conf.AccessKeyFilePath = filepath.Join(conf.DataFolderPath, "access_key")
	config.GlobalConfig = &conf
	defer func() { config.GlobalConfig = previous }()
	if err := os.WriteFile(conf.AccessKeyFilePath, []byte("component:secret"), 0600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(conf.DataFolderPath, "agent")
	t.Run("disabled without history", func(t *testing.T) {
		s := &Server{apiClient: api, agentRequests: make(chan struct{}, 1)}
		defer s.Stop()
		router := gin.New()
		s.registerAgentRoutes(router)
		s.startAgentRefresh()
		select {
		case <-configRequests:
		case <-time.After(3 * time.Second):
			t.Fatal("disabled MCP did not fetch Core configuration")
		}
		if _, err := os.Stat(directory); !os.IsNotExist(err) {
			t.Fatalf("disabled MCP created storage: %v", err)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("POST", "/koko/mcp", nil))
		if response.Code != 503 {
			t.Fatalf("disabled MCP: HTTP %d", response.Code)
		}
	})
	t.Run("enable retries initialization without interrupting Koko", func(t *testing.T) {
		s := &Server{apiClient: api, agentRequests: make(chan struct{}, 1)}
		defer s.Stop()
		router := gin.New()
		router.GET("/koko/health/", func(c *gin.Context) { c.Status(204) })
		s.registerAgentRoutes(router)
		keyPath := conf.AccessKeyFilePath
		conf.AccessKeyFilePath = filepath.Join(conf.DataFolderPath, "missing-key")
		err := s.applyAgentConfig(context.Background(), true, "http://127.0.0.1:5000/koko/mcp", core.URL)
		conf.AccessKeyFilePath = keyPath
		if err == nil || s.agentHTTP.Load() != nil {
			t.Fatal("failed initialization enabled MCP")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/koko/health/", nil))
		if response.Code != 204 {
			t.Fatal("MCP initialization failure interrupted Koko")
		}
		if err := s.applyAgentConfig(context.Background(), true, "http://127.0.0.1:5000/koko/mcp", core.URL); err != nil {
			t.Fatal(err)
		}
		if s.agentRuntime == nil || s.agentHTTP.Load() == nil {
			t.Fatal("retry did not initialize MCP")
		}
	})
	t.Run("recover while Core configuration is unavailable", func(t *testing.T) {
		journal, err := agentexec.OpenJournal(directory, 256<<20)
		if err != nil {
			t.Fatal(err)
		}
		if err = journal.PutAction(agentexec.Action{ID: "recovered", Status: "running"}, "capability"); err == nil {
			err = journal.Append("recovered", "succeeded", nil)
		}
		closeErr := journal.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("seed journal: %v, %v", err, closeErr)
		}
		unavailable.Store(true)
		s := &Server{apiClient: api, agentRequests: make(chan struct{}, 1)}
		defer s.Stop()
		s.startAgentRefresh()
		select {
		case <-events:
		case <-time.After(3 * time.Second):
			t.Fatal("pending audit was not delivered without Core configuration")
		}
		if s.agentHTTP.Load() != nil {
			t.Fatal("recovery enabled MCP ingress")
		}
	})
}

func TestAgentReloadSharesRequestCapacity(t *testing.T) {
	s := &Server{agentRuntime: &agentexec.Runtime{}, agentRequests: make(chan struct{}, 1)}
	router := gin.New()
	s.registerAgentRoutes(router)
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() {
		close(release)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("in-flight request did not finish after reload")
		}
	}()
	s.agentHTTP.Store(&agentexec.HTTP{Path: "/koko/mcp", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})})
	go func() {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/koko/mcp", nil))
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first request did not start")
	}
	if err := s.applyAgentConfig(context.Background(), true, "http://mcp.example/new/mcp", "http://core.example"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/new/mcp", nil))
	if w.Code != 429 {
		t.Fatalf("reload bypassed shared capacity: HTTP %d", w.Code)
	}
}

func TestAgentRoutesClientIP(t *testing.T) {
	for _, test := range []struct {
		name, peer, forwarded, want string
	}{
		{"direct IPv4", "127.0.0.1:1234", "", "127.0.0.1"},
		{"direct IPv6", "[::1]:1234", "", "::1"},
		{"docker-web", "172.18.0.4:1234", "198.51.100.23, 127.0.0.1", "198.51.100.23"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &Server{agentRuntime: &agentexec.Runtime{}, agentRequests: make(chan struct{}, 1)}
			s.agentHTTP.Store(&agentexec.HTTP{Path: "/koko/mcp", Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				_, _ = w.Write([]byte(req.RemoteAddr))
			})})
			router := gin.New()
			s.registerAgentRoutes(router)
			paths := agentPaths("/koko/mcp")
			for _, path := range paths[:3] {
				req := httptest.NewRequest("POST", "http://127.0.0.1:5000"+path, nil)
				req.RemoteAddr = test.peer
				req.Header.Set("X-Forwarded-For", test.forwarded)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				if response.Code != 200 || response.Body.String() != test.want {
					t.Fatalf("%s source IP = %q, expected %q (HTTP %d)", path, response.Body.String(), test.want, response.Code)
				}
			}
		})
	}
}

func TestAgentEncodedResourceDiscovery(t *testing.T) {
	for _, path := range []string{"/koko/mcp%23a", "/koko/mcp%3Fa", "/koko/mcp%20a", "/koko/%E8%B5%84%E4%BA%A7", "/koko%2Fmcp"} {
		t.Run(path, func(t *testing.T) {
			s := &Server{agentRuntime: &agentexec.Runtime{}, agentRequests: make(chan struct{}, 1)}
			router := gin.New()
			s.registerAgentRoutes(router)
			resource := "https://mcp.example" + path
			if err := s.applyAgentConfig(context.Background(), true, resource, "http://core.example"); err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", resource, nil))
			_, challenge, ok := strings.Cut(response.Header().Get("WWW-Authenticate"), `resource_metadata="`)
			metadataURL, _, closed := strings.Cut(challenge, `"`)
			address, err := url.Parse(metadataURL)
			if response.Code != 401 || !ok || !closed || err != nil || address.Fragment != "" || address.RawQuery != "" || address.Path != s.agentHTTP.Load().MetadataPath {
				t.Fatalf("invalid encoded resource challenge: %q", response.Header().Get("WWW-Authenticate"))
			}
			response = httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("GET", metadataURL, nil))
			var metadata struct{ Resource string }
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &metadata) != nil || metadata.Resource != resource {
				t.Fatalf("discovery lost exact resource identity: %s", response.Body.String())
			}
		})
	}
}
