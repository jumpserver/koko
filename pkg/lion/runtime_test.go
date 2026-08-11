package lion

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/lion/tunnel"
)

func TestRegisterRoutesExcludesStaticUI(t *testing.T) {
	previousConfig := config.GlobalConfig
	config.GlobalConfig = &config.Config{ShareRoomType: config.ShareTypeLocal}
	t.Cleanup(func() { config.GlobalConfig = previousConfig })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	runtime := NewRuntime(nil)
	runtime.RegisterRoutes(engine)

	routes := make(map[string]struct{})
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}
	for _, expected := range []string{
		"GET /lion/health/",
		"GET /lion/ws/connect/",
		"GET /lion/ws/monitor/",
		"GET /lion/ws/share/",
		"GET /lion/ws/token/",
		"GET /lion/api/tunnels/:tid/streams/:index/:filename",
		"POST /lion/api/tunnels/:tid/streams/:index/:filename",
		"POST /lion/api/share/",
		"POST /lion/api/share/remove/",
		"POST /lion/api/share/:id/",
		"GET /lion/token/tunnels/:tid/streams/:index/:filename",
		"POST /lion/token/tunnels/:tid/streams/:index/:filename",
	} {
		if _, ok := routes[expected]; !ok {
			t.Errorf("missing route %s", expected)
		}
	}
	for route := range routes {
		if route == "GET /lion/connect/" || route == "GET /lion/assets/*filepath" {
			t.Errorf("Koko must not serve Lion UI route %s", route)
		}
	}

	request := httptest.NewRequest(http.MethodOptions, "/lion/api/share/", nil)
	request.Header.Set("Origin", "http://tauri.localhost")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("Lion CORS preflight status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "http://tauri.localhost" {
		t.Fatalf("Lion CORS response missing allowed origin")
	}
}

func TestRedisConfigFailureFallsBackToLocalCache(t *testing.T) {
	previousConfig := config.GlobalConfig
	config.GlobalConfig = &config.Config{
		ShareRoomType:      config.ShareTypeRedis,
		RedisSentinelHosts: "invalid",
	}
	t.Cleanup(func() { config.GlobalConfig = previousConfig })

	runtime := NewRuntime(nil)
	if _, ok := runtime.tunnelService.Cache.GuaTunnelCache.(*tunnel.GuaTunnelLocalCache); !ok {
		t.Fatalf("expected local cache fallback, got %T", runtime.tunnelService.Cache.GuaTunnelCache)
	}
}

func TestDisabledPandaDoesNotCreateClient(t *testing.T) {
	if client := newPandaClient(config.Config{EnablePanda: false}); client != nil {
		t.Fatalf("disabled Panda client = %T, want nil", client)
	}
}
