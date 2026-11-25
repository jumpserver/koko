package chen

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver/koko/pkg/chen/engine"
)

func TestManagedWorkspaceRoutes(t *testing.T) {
	app := engine.New(nil, engine.Options{StateDir: t.TempDir()})
	t.Cleanup(app.Close)
	router := gin.New()
	(&Runtime{app: app}).RegisterRoutes(router)

	for path, status := range map[string]int{
		"/koko/chen/api/v1/capabilities":      http.StatusOK,
		"/koko/chen/api/v1/sessions/s/events": http.StatusUnauthorized,
		"/chen/api/v1/capabilities":           http.StatusNotFound,
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != status {
			t.Errorf("GET %s returned %d, want %d", path, response.Code, status)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/koko/chen/api/v1/sessions", strings.NewReader(`{"selector":"local-db","request_id":"standalone-attempt"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Errorf("selector authentication returned %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestStandaloneWorkspaceUsesKokoRoute(t *testing.T) {
	app := engine.New(nil, engine.Options{SingleMode: true, StateDir: t.TempDir()})
	t.Cleanup(app.Close)
	handler := app.Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/koko/chen/api/v1/capabilities", nil))
	var capabilities struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &capabilities); err != nil || capabilities.Mode != "single" {
		t.Fatalf("unexpected capabilities: %s", response.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/koko/chen/api/v1/sessions", strings.NewReader(`{"connect_token":"managed","request_id":"managed-mode-attempt"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("Core token on standalone returned %d", response.Code)
	}
}
