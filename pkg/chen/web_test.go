package chen

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStandaloneWebRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("workspace"), 0600); err != nil {
		t.Fatal(err)
	}
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler, closeWeb, err := webHandler(api, "127.0.0.1:8082", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer closeWeb()
	for _, tc := range []struct {
		url    string
		status int
	}{
		{"http://localhost/?selector=dev", http.StatusTemporaryRedirect},
		{"http://localhost/luna/chen/?selector=dev", http.StatusOK},
		{"http://localhost/koko/chen/api/v1/capabilities", http.StatusNoContent},
		{"http://localhost/luna/missing.js", http.StatusNotFound},
		{"http://untrusted.invalid/koko/chen/api/v1/sessions", http.StatusForbidden},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.url, nil))
		if w.Code != tc.status {
			t.Errorf("%s: got %d, want %d", tc.url, w.Code, tc.status)
		}
		if tc.status == http.StatusTemporaryRedirect && w.Header().Get("Location") != "/luna/chen/?selector=dev" {
			t.Errorf("redirect lost selector: %s", w.Header().Get("Location"))
		}
	}
}
