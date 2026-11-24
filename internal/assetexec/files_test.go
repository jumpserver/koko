package assetexec

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/proxy"
)

type retainedFileStorage struct {
	kind string
	data chan []byte
}

func (s retainedFileStorage) TypeName() string { return s.kind }
func (s retainedFileStorage) Upload(name, _ string) error {
	data, err := os.ReadFile(name)
	if err == nil {
		s.data <- data
	}
	return err
}

type failedFileSource struct{}

func (failedFileSource) ReadAt([]byte, int64) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestFileRetentionUsesConfiguredStorageAndLimit(t *testing.T) {
	previous := config.GlobalConfig
	cfg := config.GetConf()
	cfg.FTPFileFolderPath = t.TempDir()
	config.GlobalConfig = &cfg
	t.Cleanup(func() { config.GlobalConfig = previous })
	finished := make(chan string, 1)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected retention request: %s", r.Method)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{}")
		finished <- r.URL.Path
	}))
	t.Cleanup(core.Close)
	jms, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0, 255, 29, 128}, 16385)
	for _, tc := range []struct {
		name, storage string
		limit         int64
		broken, kept  bool
	}{
		{"disabled", "test", 0, false, false},
		{"null", "null", 1 << 20, false, false},
		{"retained", "test", 1 << 20, false, true},
		{"at_limit", "test", int64(len(payload)), false, false},
		{"over_limit", "test", int64(len(payload) - 1), false, false},
		{"read_failure", "test", 1 << 20, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := make(chan []byte, 1)
			conn := &Connection{}
			if tc.limit > 0 {
				conn.recorder = proxy.NewFTPFileRecord(jms, retainedFileStorage{tc.storage, stored}, tc.limit)
			}
			log := &model.FTPLog{ID: tc.name, DateStart: common.NewNowUTCTime()}
			var source io.ReaderAt = bytes.NewReader(payload)
			if tc.broken {
				source = failedFileSource{}
			}
			conn.RecordFile(FileResult{Size: int64(len(payload)), fileLog: log}, source)
			if tc.kept {
				select {
				case path := <-finished:
					if path != "/api/v1/audits/ftp-logs/"+log.ID+"/" || !bytes.Equal(<-stored, payload) {
						t.Fatal("retention lost bytes or completed a different FTP log")
					}
				case <-time.After(3 * time.Second):
					t.Fatal("file retention did not complete")
				}
			} else {
				select {
				case <-stored:
					t.Fatal("disabled, oversized or incomplete file was retained")
				default:
				}
			}
			if _, err := os.Stat(filepath.Join(cfg.FTPFileFolderPath, log.DateStart.UTC().Format("2006-01-02"), log.ID)); !os.IsNotExist(err) {
				t.Fatal("file retention left a local copy", err)
			}
		})
	}
}
