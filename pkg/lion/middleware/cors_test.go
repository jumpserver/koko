package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jumpserver/koko/pkg/config"
)

func TestCORS(t *testing.T) {
	previousConfig := config.GlobalConfig
	config.GlobalConfig = &config.Config{DOMAINS: "portal.example"}
	t.Cleanup(func() { config.GlobalConfig = previousConfig })

	router := gin.New()
	router.Use(CORS())
	const uploadPath = "/koko/lion/api/tunnels/tunnel/streams/1/file.txt"
	router.Any(uploadPath, func(ctx *gin.Context) { ctx.Status(http.StatusCreated) })

	for _, tc := range []struct {
		name    string
		origin  string
		allowed bool
	}{
		{"desktop", "jms-app://app", true},
		{"same origin web", "https://jumpserver.example", true},
		{"configured web", "https://portal.example", true},
		{"development", "http://localhost:3000", true},
		{"other desktop host", "jms-app://other", false},
		{"desktop lookalike", "jms-app://app.example", false},
		{"web app host", "https://app", false},
		{"opaque origin", "null", false},
	} {
		for _, method := range []string{http.MethodOptions, http.MethodPost} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				req := httptest.NewRequest(method, "https://jumpserver.example"+uploadPath, nil)
				req.Header.Set("Origin", tc.origin)
				if method == http.MethodOptions {
					req.Header.Set("Access-Control-Request-Method", http.MethodPost)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)

				wantStatus, wantOrigin, wantCredentials := http.StatusForbidden, "", ""
				if tc.allowed {
					wantStatus, wantOrigin, wantCredentials = http.StatusCreated, tc.origin, "true"
					if method == http.MethodOptions {
						wantStatus = http.StatusNoContent
						if !strings.Contains(response.Header().Get("Access-Control-Allow-Methods"), http.MethodPost) {
							t.Fatal("preflight does not allow POST")
						}
					}
				}
				if response.Code != wantStatus {
					t.Fatalf("status = %d, want %d", response.Code, wantStatus)
				}
				if response.Header().Get("Access-Control-Allow-Origin") != wantOrigin ||
					response.Header().Get("Access-Control-Allow-Credentials") != wantCredentials {
					t.Fatalf("unexpected CORS headers: %v", response.Header())
				}
			})
		}
	}
}
