package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveStorePrunesOnlyExpiredClosedLogs(t *testing.T) {
	directory := t.TempDir()
	now := time.Now()
	writeLog := func(name string, modified time.Time) {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte("event\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	writeLog("expired.closed.jsonl", now.Add(-25*time.Hour))
	writeLog("recent.closed.jsonl", now.Add(-time.Hour))
	writeLog("idle.active.jsonl", now.Add(-25*time.Hour))
	writeLog("broken.quarantined-1.jsonl", now.Add(-25*time.Hour))

	store, err := openArchiveStore(directory, 10, maxJSONLFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := store.pruneClosedBefore(now.Add(-24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	for _, name := range []string{"recent.closed.jsonl", "idle.active.jsonl", "broken.quarantined-1.jsonl"} {
		if _, err = os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("expected %s to remain: %v", name, err)
		}
	}
	if _, err = os.Stat(filepath.Join(directory, "expired.closed.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("expired closed log still exists: %v", err)
	}
}
