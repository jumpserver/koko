package srvconn

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadK8sReverseProxyCertificateWithoutFiles(t *testing.T) {
	dir := t.TempDir()
	certificate, err := loadK8sReverseProxyCertificate(
		filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key"),
	)
	if err != nil {
		t.Fatalf("generate ephemeral certificate: %v", err)
	}
	if len(certificate.Certificate) == 0 {
		t.Fatal("generated certificate is empty")
	}
}

func TestK8sReverseProxyRejectsAuthorizationWithoutEchoingIt(t *testing.T) {
	proxy := NewK8sReverseProxy(0)
	for _, authorization := range []string{
		"Basic bearer-secret", "Bearer missing-token-secret",
	} {
		request := httptest.NewRequest(http.MethodGet, "https://localhost/api", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		proxy.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized ||
			strings.Contains(response.Body.String(), authorization) ||
			strings.Contains(response.Body.String(), "secret") {
			t.Fatalf("authorization=%q status=%d body=%q", authorization, response.Code, response.Body.String())
		}
	}
}
