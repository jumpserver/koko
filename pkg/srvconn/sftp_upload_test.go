package srvconn

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/pkg/sftp"
)

func TestUploadTempPermissionsAndAudit(t *testing.T) {
	clientPipe, serverPipe := net.Pipe()
	_ = clientPipe.SetDeadline(time.Now().Add(10 * time.Second))
	t.Cleanup(func() { _ = clientPipe.Close(); _ = serverPipe.Close() })
	server, err := sftp.NewServer(serverPipe)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve() }()
	client, err := sftp.NewClientPipe(clientPipe, clientPipe)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	logs := make(chan model.FTPLog, 4)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var entry model.FTPLog
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			t.Error(err)
		}
		logs <- entry
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer core.Close()
	api, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
	if err != nil {
		t.Fatal(err)
	}
	account := &model.PermAccount{Username: "upload", Actions: model.Actions{{Value: model.ActionUpload}}}
	connection := &SftpConn{client: client, rootDirPath: t.TempDir()}
	asset := &AssetDir{
		suMaps: map[string]*model.PermAccount{"upload": account},
		user:   &model.User{}, detailAsset: &model.PermAsset{}, jmsService: api,
	}
	asset.sftpSessions.Store(account.String(), &SftpSession{SftpConn: connection, sess: &model.Session{ID: "session"}})
	u := &UserSftpConn{assetDir: asset}
	for _, operation := range []func() error{
		func() error { _, err := u.Open("/stage"); return err },
		func() error { return u.Remove("/stage") },
	} {
		if err := operation(); err != sftp.ErrSshFxPermissionDenied {
			t.Fatalf("ordinary download/delete should be denied: %v", err)
		}
	}
	file, err := u.OpenUploadTemp("/stage", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := u.OpenUploadTemp("/stage", true); err == nil {
		t.Fatal("a repeated prepare must not truncate an existing upload")
	}
	file, err = u.OpenUploadTemp("/stage", false)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4)
	if _, err := file.ReadAt(data, 0); err != nil || string(data) != "data" {
		t.Fatalf("upload-only checksum read failed: %q, %v", data, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	entry, err := u.CommitUploadTemp("/stage", "/target", false)
	if err != nil {
		t.Fatal(err)
	}
	file, err = u.OpenUploadTemp("/target", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.ReadAt(data, 0); err != nil || string(data) != "data" {
		t.Fatalf("upload-only recording read failed: %q, %v", data, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || entry.Operate != model.OperateUpload || !entry.IsSuccess || entry.Path != filepath.Join(connection.rootDirPath, "target") {
		t.Fatalf("expected one upload audit for final path, got %+v (%d logs)", entry, len(logs))
	}
	file, err = u.OpenUploadTemp("/discard", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := u.DiscardUploadTemp("/discard"); err != nil {
		t.Fatalf("upload-only cancellation failed: %v", err)
	}
	if len(logs) != 1 || connection.Ref() != 0 {
		t.Fatalf("temporary operations leaked logs or handles: logs=%d refs=%d", len(logs), connection.Ref())
	}
	account.Actions = nil
	if _, err := u.OpenUploadTemp("/target", false); err != sftp.ErrSshFxPermissionDenied {
		t.Fatalf("temporary access without upload permission: %v", err)
	}
	if err := u.DiscardUploadTemp("/target"); err != sftp.ErrSshFxPermissionDenied {
		t.Fatalf("temporary deletion without upload permission: %v", err)
	}
}
