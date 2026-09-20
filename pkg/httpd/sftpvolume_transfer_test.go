package httpd

import (
	"bytes"
	"errors"
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
	"github.com/pkg/sftp"
)

type transferErrorFS struct {
	sftpFileSystem
	statErr   error
	discarded string
}

func (f *transferErrorFS) Stat(string) (os.FileInfo, error) { return nil, f.statErr }
func (f *transferErrorFS) DiscardUploadTemp(path string) error {
	f.discarded = path
	return nil
}

func TestTransferPreservesStatErrors(t *testing.T) {
	for _, statErr := range []error{os.ErrPermission, sftp.ErrSSHFxConnectionLost} {
		volume := newSFTPVolume(&transferErrorFS{statErr: statErr}, nil)
		if _, err := volume.prepareTransfer("id", "/target", 1, "overwrite"); !errors.Is(err, statErr) {
			t.Fatalf("prepare hid a remote failure: %v", err)
		}
		if _, err := volume.transferStatus("id", "/target", 1); !errors.Is(err, statErr) {
			t.Fatalf("status reported a remote failure as a missing upload: %v", err)
		}
	}
	volume := newSFTPVolume(&transferErrorFS{statErr: &sftp.StatusError{Code: 2}}, nil)
	if result, err := volume.transferStatus("id", "/target", 1); err != nil || result.State != "missing" {
		t.Fatalf("missing upload was not identified: %+v, %v", result, err)
	}
}

func TestTransferCancelUsesUploadCleanup(t *testing.T) {
	fs := &transferErrorFS{}
	volume := newSFTPVolume(fs, nil)
	if _, err := volume.cancelTransfer("id", "/target", true); err != nil {
		t.Fatal(err)
	}
	if fs.discarded != "/.target.jms-transfer-id.part" {
		t.Fatalf("cancel did not discard the staging file: %q", fs.discarded)
	}
}

type uploadRecordingStorage struct{ uploaded chan []byte }

func (s uploadRecordingStorage) TypeName() string { return "test" }
func (s uploadRecordingStorage) Upload(path, _ string) error {
	data, err := os.ReadFile(path)
	if err == nil {
		s.uploaded <- data
	}
	return err
}

func TestUploadRecordingMatchesCommittedBytes(t *testing.T) {
	previous := config.GlobalConfig
	cfg := config.GetConf()
	cfg.FTPFileFolderPath = t.TempDir()
	config.GlobalConfig = &cfg
	t.Cleanup(func() { config.GlobalConfig = previous })
	finished := make(chan struct{}, 6)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finished <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer core.Close()
	api, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
	if err != nil {
		t.Fatal(err)
	}
	storage := uploadRecordingStorage{uploaded: make(chan []byte, 6)}
	want := bytes.Repeat([]byte("a"), 64*1024+1)
	for _, tc := range []struct {
		name    string
		data    []byte
		want    []byte
		wantErr bool
		maxSize int64
	}{
		{name: "matching", data: want, want: want},
		{name: "empty"},
		{name: "replaced", data: bytes.Repeat([]byte("b"), len(want)), want: want, wantErr: true},
		{name: "short", data: want[:len(want)-1], want: want, wantErr: true},
		{name: "long", data: append(append([]byte{}, want...), 'a'), want: want, wantErr: true},
		{name: "limit", data: want, want: want, maxSize: int64(len(want))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limit := tc.maxSize
			if limit == 0 {
				limit = 128 * 1024
			}
			recorder := proxy.NewFTPFileRecord(api, storage, limit)
			volume := newSFTPVolume(nil, recorder)
			log := &model.FTPLog{ID: tc.name, DateStart: common.NewNowUTCTime()}
			reader := bytes.NewReader(tc.data)
			err := volume.recordUploadContents(log, reader, int64(len(tc.want)), sha256Hex(tc.want))
			if (err != nil) != tc.wantErr {
				t.Fatalf("recordUploadContents error = %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr || tc.maxSize != 0 {
				files, err := filepath.Glob(filepath.Join(cfg.FTPFileFolderPath, "*", tc.name))
				if err != nil || len(files) != 0 {
					t.Fatalf("invalid recording was retained: %v, %v", files, err)
				}
				if tc.maxSize != 0 && reader.Len() != len(tc.data) {
					t.Fatal("oversized recording consumed remote contents")
				}
				return
			}
			select {
			case data := <-storage.uploaded:
				if !bytes.Equal(data, tc.want) {
					t.Fatal("recorded bytes differ from verified upload contents")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("verified recording was not uploaded")
			}
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				t.Fatal("verified recording was not finalized")
			}
		})
	}
}
