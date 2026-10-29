package agentexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jumpserver/koko/internal/assetexec"
)

func fileTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "files"), 0700); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(dir, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	return &Runtime{journal: journal, opts: Options{Directory: dir, MaxFileBytes: 100 << 20}}
}

func TestFileReservationsIncludeUnwrittenBytes(t *testing.T) {
	r := fileTestRuntime(t)
	seed, err := os.Create(filepath.Join(r.opts.Directory, "files", "reserved.data"))
	if err != nil {
		t.Fatal(err)
	}
	if err = seed.Truncate(950 << 20); err != nil {
		t.Fatal(err)
	}
	_ = seed.Close()
	f, err := r.reserveFile("first", 60<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() != 60<<20 {
		t.Fatal("unfinished transfer did not reserve its requested size")
	}
	if second, err := r.reserveFile("second", 60<<20); err == nil {
		_ = second.Close()
		t.Fatal("concurrent transfer reused reserved capacity")
	}
}

func TestFilePublicationCleansFailedCommitAndNeverReplaces(t *testing.T) {
	r := fileTestRuntime(t)
	f, err := r.reserveFile("missing", 3)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("new")
	if err = r.persistArtifact("missing", f, 3, "digest"); err == nil {
		t.Fatal("missing journal operation was committed")
	}
	for _, suffix := range []string{".part", ".data"} {
		if _, err = os.Stat(filepath.Join(r.opts.Directory, "files", "missing"+suffix)); !os.IsNotExist(err) {
			t.Fatal("failed journal commit leaked staged data")
		}
	}
	f, err = r.reserveFile("collision", 3)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("new")
	final, _ := r.artifactPath("collision", ".data")
	if err = os.WriteFile(final, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = r.persistArtifact("collision", f, 3, "digest"); err == nil {
		t.Fatal("overwrote an existing artifact")
	}
	data, err := os.ReadFile(final)
	if err != nil || string(data) != "old" {
		t.Fatal("existing artifact changed")
	}
	if err = r.journal.PutAction(Action{ID: "download", Kind: "file_download", Status: "running"}, "capability"); err != nil {
		t.Fatal(err)
	}
	f, err = r.reserveFile("download", 100<<20)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("abc")
	if err = r.persistArtifact("download", f, 3, "digest"); err != nil {
		t.Fatal(err)
	}
	final, _ = r.artifactPath("download", ".data")
	info, err := os.Stat(final)
	if err != nil || info.Size() != 3 {
		t.Fatal("download retained its reservation instead of actual length")
	}
}

type fileCore struct{ action Action }

func (c fileCore) Call(_ context.Context, _ string, _ any, out any) error {
	action, ok := out.(*Action)
	if !ok {
		return errors.New("unexpected Core request")
	}
	*action = c.action
	return nil
}

type unreadUpload struct{ read bool }

func (r *unreadUpload) Read([]byte) (int, error) { r.read = true; return 0, io.EOF }

func TestUploadRetryPreservesStagedAndDispatchedArtifact(t *testing.T) {
	r := fileTestRuntime(t)
	hash := sha256.Sum256([]byte("old"))
	digest := hex.EncodeToString(hash[:])
	action := Action{ID: "upload", Kind: "file_upload", SessionID: "session", Status: "queued", Request: map[string]any{"path": "/file", "size": float64(3), "sha256": digest}}
	if err := r.journal.PutAction(action, "capability"); err != nil {
		t.Fatal(err)
	}
	f, err := r.reserveFile(action.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("old")
	if err = r.persistArtifact(action.ID, f, 3, digest); err != nil {
		t.Fatal(err)
	}
	r.core = fileCore{action}
	p := Principal{UserID: "user"}
	r.sessions = map[string]*liveSession{"session": {owner: owner(p), connection: &assetexec.Connection{}, info: Session{Status: "active"}}}
	r.jobs = map[string]context.CancelFunc{action.ID: func() {}}
	for _, dispatched := range []bool{false, true} {
		if dispatched {
			if err = r.journal.Intent(action.ID); err != nil {
				t.Fatal(err)
			}
		}
		body := &unreadUpload{}
		if _, err = r.Upload(context.Background(), p, action.ID, body, 3, digest); err != nil {
			t.Fatal(err)
		}
		if body.read {
			t.Fatal("retry consumed a replacement upload")
		}
		final, _ := r.artifactPath(action.ID, ".data")
		data, err := os.ReadFile(final)
		if err != nil || string(data) != "old" {
			t.Fatal("retry changed durable upload bytes")
		}
	}
}

func TestFileResultsFitEncodedAuditEnvelope(t *testing.T) {
	r := fileTestRuntime(t)
	action := Action{ID: "list", Kind: "file_list", Status: "queued"}
	if err := r.journal.PutAction(action, strings.Repeat("c", 4096)); err != nil {
		t.Fatal(err)
	}
	limit, err := r.filePayloadLimit(action)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = textFileResult(assetexec.FileResult{}, []byte{255}, limit); err == nil {
		t.Fatal("binary file accepted as UTF-8")
	}
	if _, err = textFileResult(assetexec.FileResult{}, []byte(strings.Repeat("\x00", 64<<10)), limit); err == nil {
		t.Fatal("JSON escaping bypassed text event limit")
	}
	values := make([]assetexec.FileInfo, 200)
	for i := range values {
		values[i] = assetexec.FileInfo{Name: "name", Path: strings.Repeat("\x00", 1000)}
	}
	result := listedFileResult(values, false, limit)
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > limit || result["truncated"] != true {
		t.Fatal("directory result did not fit the encoded payload budget")
	}
	if err = r.journal.Append(action.ID, "succeeded", result); err != nil {
		t.Fatalf("bounded payload did not fit real audit event: %v", err)
	}
}
