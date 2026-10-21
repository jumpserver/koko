package httpd

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/srvconn"
)

func TestWebSftpFilePathRoundTrip(t *testing.T) {
	for _, path := range []string{
		"/root/中文%20.txt",
		"/root/�.txt",
		"/root/\xff.txt",
		"/root/\xfe.txt",
		"/root/\xd6\xd0\xce\xc4.txt",
		"/root/\xff/child.txt",
	} {
		t.Run(path, func(t *testing.T) {
			entry := newWebSftpFileInfo(filepath.Dir(path), srvconn.NewFakeFile(filepath.Base(path), false))
			data, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			// The browser uses the display name and forwards the optional raw path.
			var browserEntry struct {
				Name       string `json:"name"`
				PathBase64 string `json:"path_base64,omitempty"`
			}
			if err := json.Unmarshal(data, &browserEntry); err != nil {
				t.Fatal(err)
			}
			if (browserEntry.PathBase64 == "") != utf8.ValidString(path) {
				t.Fatal("raw path must be sent only when JSON would lose bytes")
			}
			requestData, err := json.Marshal(struct {
				Path       string `json:"path"`
				PathBase64 string `json:"path_base64,omitempty"`
			}{filepath.Join(filepath.Dir(path), browserEntry.Name), browserEntry.PathBase64})
			if err != nil {
				t.Fatal(err)
			}
			var request webSftpRequest
			if err := json.Unmarshal(requestData, &request); err != nil {
				t.Fatal(err)
			}
			if request.Path != path {
				t.Fatalf("operation path = %q, want %q", request.Path, path)
			}
		})
	}
}

func TestWebSftpRequestRejectsInvalidBase64(t *testing.T) {
	var request webSftpRequest
	if err := json.Unmarshal([]byte(`{"path":"/root/�.txt","path_base64":"!"}`), &request); err == nil {
		t.Fatal("invalid Base64 must not fall back to the display path")
	}
}
