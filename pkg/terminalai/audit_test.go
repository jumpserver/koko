package terminalai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditWriterIsolatesUserAndRetainsTenSessions(t *testing.T) {
	root := t.TempDir()
	active := newAuditWriter("user-1", 1, root, 10)
	if active == nil {
		t.Fatal("create active memory writer")
	}
	active.SetSessionID("active")
	active.Write("provider_response", map[string]any{"content": "complete"})

	for index := 0; index < 11; index++ {
		writer := newAuditWriter("user-1", 1, root, 10)
		if writer == nil {
			t.Fatalf("create memory writer %d", index)
		}
		writer.SetSessionID("session-" + string(rune('a'+index)))
		writer.Write("event", map[string]any{"index": index})
		writer.Close()
	}
	if _, err := os.Stat(filepath.Join(root, "user-1", "active.jsonl")); err != nil {
		t.Fatalf("active session was pruned: %v", err)
	}
	active.Close()
	entries, err := os.ReadDir(filepath.Join(root, "user-1"))
	if err != nil {
		t.Fatalf("read user memory: %v", err)
	}
	if len(entries) != 10 {
		t.Fatalf("retained sessions = %d, want 10", len(entries))
	}
	info, err := os.Stat(filepath.Join(root, "user-1"))
	if err != nil {
		t.Fatalf("stat user memory: %v", err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("user memory permissions = %v", info.Mode().Perm())
	}
	fileInfo, err := entries[0].Info()
	if err != nil {
		t.Fatalf("stat session memory: %v", err)
	}
	if fileInfo.Mode().Perm() != 0600 {
		t.Fatalf("session memory permissions = %v", fileInfo.Mode().Perm())
	}
	content, err := os.ReadFile(filepath.Join(root, "user-1", entries[0].Name()))
	if err != nil || !strings.Contains(string(content), `"event":"session_metrics"`) {
		t.Fatalf("session metrics were not written: %v", err)
	}
}
