package httpd

import (
	"bytes"
	"errors"
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
	"github.com/jumpserver/koko/pkg/httpd/ws"
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

type missingStatFS struct{ sftpFileSystem }

func (f *missingStatFS) Stat(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
func (f *missingStatFS) DiscardUploadTemp(string) error   { return nil }

type bufferTransferFile struct {
	data   []byte
	closed bool
	fail   error
}

func (f *bufferTransferFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *bufferTransferFile) WriteAt(p []byte, off int64) (int, error) {
	if f.fail != nil {
		return 0, f.fail
	}
	end := int(off) + len(p)
	if end > len(f.data) {
		next := make([]byte, end)
		copy(next, f.data)
		f.data = next
	}
	copy(f.data[off:], p)
	return len(p), nil
}

func (f *bufferTransferFile) Stat() (os.FileInfo, error) {
	return bufferInfo{size: int64(len(f.data))}, nil
}
func (f *bufferTransferFile) Close() error { f.closed = true; return nil }

type bufferInfo struct{ size int64 }

func (bufferInfo) Name() string       { return "part" }
func (i bufferInfo) Size() int64      { return i.size }
func (bufferInfo) Mode() os.FileMode  { return 0o600 }
func (bufferInfo) ModTime() time.Time { return time.Time{} }
func (bufferInfo) IsDir() bool        { return false }
func (bufferInfo) Sys() any           { return nil }

func TestTransferReusesWriteHandle(t *testing.T) {
	opens := 0
	file := &bufferTransferFile{}
	volume := newSFTPVolume(&missingStatFS{}, nil)
	volume.openWrite = func(string, bool) (transferIO, error) {
		opens++
		file.closed = false
		return file, nil
	}
	chunk := []byte("hello")
	sum := sha256Hex(chunk)
	if _, err := volume.prepareTransfer("tid", "/target.bin", 10, "overwrite"); err != nil {
		t.Fatal(err)
	}
	if _, err := volume.writeTransferChunk("tid", "/target.bin", 10, 0, sum, chunk); err != nil {
		t.Fatal(err)
	}
	if _, err := volume.writeTransferChunk("tid", "/target.bin", 10, 5, sum, chunk); err != nil {
		t.Fatal(err)
	}
	if opens != 1 {
		t.Fatalf("opens = %d", opens)
	}
	if file.closed {
		t.Fatal("handle closed between chunks")
	}
	if _, err := volume.cancelTransfer("tid", "/target.bin", true); err != nil {
		t.Fatal(err)
	}
	if !file.closed {
		t.Fatal("handle not closed on cancel")
	}
}

func TestTransferReusesReadHandle(t *testing.T) {
	opens := 0
	file := &bufferTransferFile{data: bytes.Repeat([]byte("a"), 8)}
	volume := newSFTPVolume(&missingStatFS{}, nil)
	volume.openRead = func(string) (transferIO, error) {
		opens++
		file.closed = false
		return file, nil
	}
	if _, _, err := volume.readTransferChunk("tid", "/source.bin", 0, 4); err != nil {
		t.Fatal(err)
	}
	if _, meta, err := volume.readTransferChunk("tid", "/source.bin", 4, 4); err != nil {
		t.Fatal(err)
	} else if !meta.EOF {
		t.Fatal("expected eof")
	}
	if opens != 1 {
		t.Fatalf("opens = %d", opens)
	}
	if !file.closed {
		t.Fatal("handle not closed on eof")
	}
}

func TestTransferWriteErrorReopensHandle(t *testing.T) {
	opens := 0
	file := &bufferTransferFile{fail: io.ErrClosedPipe}
	volume := newSFTPVolume(&missingStatFS{}, nil)
	volume.openWrite = func(string, bool) (transferIO, error) {
		opens++
		file.closed = false
		return file, nil
	}
	chunk := []byte("hello")
	sum := sha256Hex(chunk)
	if _, err := volume.prepareTransfer("tid", "/target.bin", 5, "overwrite"); err != nil {
		t.Fatal(err)
	}
	if _, err := volume.writeTransferChunk("tid", "/target.bin", 5, 0, sum, chunk); err == nil {
		t.Fatal("expected write error")
	}
	if !file.closed {
		t.Fatal("failed write left the handle open")
	}
	file.fail = nil
	if _, err := volume.writeTransferChunk("tid", "/target.bin", 5, 0, sum, chunk); err != nil {
		t.Fatal(err)
	}
	if opens != 2 {
		t.Fatalf("opens = %d", opens)
	}
}

func TestTransferReadHonorsBinaryFlag(t *testing.T) {
	user := &UserWebsocket{
		conn:           ws.NewSocket(nil, httptest.NewRequest("GET", "/", nil)),
		messageChannel: make(chan *Message, 2),
	}
	handler := newWebSFTP(user)
	file := &bufferTransferFile{data: []byte("abc")}
	volume := newSFTPVolume(&missingStatFS{}, nil)
	volume.openRead = func(string) (transferIO, error) { return file, nil }
	handler.volume = volume
	handler.handleTransferRead(&webSftpRequest{TransferID: "tid", Path: "/f", OffSet: 0, Length: 3, Binary: true}, &Message{Id: "bin"})
	handler.handleTransferRead(&webSftpRequest{TransferID: "tid", Path: "/f", OffSet: 0, Length: 3}, &Message{Id: "json"})
	first := <-user.messageChannel
	second := <-user.messageChannel
	if first.Type != SFTPTransferBinary || !bytes.Equal(first.Raw, []byte("abc")) {
		t.Fatalf("binary response = %+v", first)
	}
	if second.Type != SFTPBinary {
		t.Fatalf("json response type = %s", second.Type)
	}
}

func TestHandleMessageBinaryTransferWrite(t *testing.T) {
	user := &UserWebsocket{
		conn:           ws.NewSocket(nil, httptest.NewRequest("GET", "/", nil)),
		messageChannel: make(chan *Message, 2),
	}
	handler := newWebSFTP(user)
	file := &bufferTransferFile{}
	volume := newSFTPVolume(&missingStatFS{}, nil)
	volume.openWrite = func(string, bool) (transferIO, error) {
		file.closed = false
		return file, nil
	}
	handler.volume = volume
	close(handler.ready)
	chunk := []byte("hello")
	if _, err := volume.prepareTransfer("tid", "/target.bin", 5, "overwrite"); err != nil {
		t.Fatal(err)
	}
	frame, err := encodeSftpBinaryFrame(&Message{
		Id:   "req-write",
		Type: SFTPData,
		Cmd:  "transfer_write",
		Data: `{"transfer_id":"tid","path":"/target.bin","size":5,"offset":0,"sha256":"` + sha256Hex(chunk) + `"}`,
		Raw:  chunk,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler.HandleMessage(&Message{Type: TerminalBinary, Raw: frame})
	select {
	case msg := <-user.messageChannel:
		if msg.Err != "" {
			t.Fatalf("write failed: %s", msg.Err)
		}
		if msg.Type != SFTPData || msg.Cmd != "transfer_write" {
			t.Fatalf("ack = %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("binary transfer_write produced no acknowledgement")
	}
	if !bytes.Equal(file.data, chunk) {
		t.Fatalf("volume wrote %q", file.data)
	}
}

func TestWebsocketCapabilitiesAdvertiseTransferBinary(t *testing.T) {
	caps, ok := newWebSFTP(&UserWebsocket{}).WebsocketCapabilities()["web_sftp"].(webSftpCapabilities)
	if !ok || !caps.TransferBinary {
		t.Fatalf("transfer_binary missing: %+v", caps)
	}
}
