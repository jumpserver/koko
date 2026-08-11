package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/jumpserver/koko/pkg/config"
)

func TestOriginAllowed(t *testing.T) {
	previousConfig := config.GlobalConfig
	config.GlobalConfig = &config.Config{DOMAINS: "client.example.com"}
	t.Cleanup(func() { config.GlobalConfig = previousConfig })

	tests := []struct {
		name    string
		origin  string
		host    string
		allowed bool
	}{
		{name: "same host", origin: "https://jump.example.com", host: "jump.example.com", allowed: true},
		{name: "tauri localhost", origin: "http://tauri.localhost", host: "jump.example.com", allowed: true},
		{name: "configured domain", origin: "https://client.example.com", host: "jump.example.com", allowed: true},
		{name: "untrusted domain", origin: "https://evil.example.com", host: "jump.example.com", allowed: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "http://"+test.host+"/lion/health/", nil)
			request.Host = test.host
			request.Header.Set("Origin", test.origin)
			if actual := OriginAllowed(request); actual != test.allowed {
				t.Fatalf("OriginAllowed() = %v, want %v", actual, test.allowed)
			}
		})
	}
}
