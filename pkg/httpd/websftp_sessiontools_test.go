package httpd

import (
	"context"
	"encoding/json"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jumpserver/koko/internal/sessiontools"
)

func TestWebSftpVersionDeletePrecondition(t *testing.T) {
	for _, change := range []string{"unchanged", "size", "mtime", "mode"} {
		t.Run(change, func(t *testing.T) {
			file := &fstest.MapFile{Data: []byte("abc"), Mode: 0644, ModTime: time.Unix(1787816033, 0)}
			fs := &volumeTestFS{files: fstest.MapFS{"root/b.txt": file}}
			executor := &webSFTPAgentToolExecutor{volume: newSFTPVolume(fs, nil), canDelete: true}
			handlers, err := sessiontools.NewFileToolHandlers(executor, sessiontools.FileToolCapabilities{Delete: true})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			stat, err := executor.Stat(ctx, "/b.txt")
			if err != nil {
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
			if change == "unchanged" {
				if err != nil || fs.mutation != "remove /root/b.txt" {
					t.Fatalf("unchanged file was not deleted: %v", err)
				}
			} else if err == nil || fs.mutation != "" {
				t.Fatalf("changed file was not protected: %v", err)
			}
		})
	}
}
