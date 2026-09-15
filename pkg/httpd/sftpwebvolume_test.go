package httpd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jumpserver/koko/internal/sessiontools"
)

type versionTestExecutor struct {
	sessiontools.FileExecutor
	files fstest.MapFS
}

func (e *versionTestExecutor) Stat(_ context.Context, path string) (sessiontools.FileEntry, error) {
	info, err := e.files.Stat(strings.TrimPrefix(path, "/"))
	if err != nil {
		return sessiontools.FileEntry{}, err
	}
	return agentToolFileEntry(path, info), nil
}

func (e *versionTestExecutor) Delete(_ context.Context, path, _ string, _ bool) error {
	delete(e.files, strings.TrimPrefix(path, "/"))
	return nil
}

func TestWebSftpVersionDeletePrecondition(t *testing.T) {
	for _, change := range []string{"unchanged", "size", "mtime", "mode"} {
		t.Run(change, func(t *testing.T) {
			file := &fstest.MapFile{Data: []byte("abc"), Mode: 0644, ModTime: time.Unix(1787816033, 0)}
			executor := &versionTestExecutor{files: fstest.MapFS{"b.txt": file}}
			handlers, err := sessiontools.NewFileToolHandlers(executor, sessiontools.FileToolCapabilities{Delete: true})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			entry, err := handlers[1].Call(ctx, json.RawMessage(`{"path":"/b.txt"}`))
			if err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			var stat sessiontools.FileEntry
			if err = json.Unmarshal(payload, &stat); err != nil {
				t.Fatal(err)
			}
			if stat.Version != "stat:3:1787816033:-rw-r--r--" {
				t.Fatalf("expected printable metadata version, got %q", stat.Version)
			}
			arguments, err := json.Marshal(map[string]string{"path": stat.Path, "expected_version": stat.Version})
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "size":
				file.Data = append(file.Data, 'd')
			case "mtime":
				file.ModTime = file.ModTime.Add(time.Second)
			case "mode":
				file.Mode = 0600
			}
			_, err = handlers[2].Call(ctx, arguments)
			_, exists := executor.files["b.txt"]
			if change == "unchanged" {
				if err != nil || exists {
					t.Fatalf("unchanged file was not deleted: %v", err)
				}
			} else if err == nil || !exists {
				t.Fatalf("changed file was not protected: %v", err)
			}
		})
	}
}
