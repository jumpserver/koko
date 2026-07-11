package monitoring

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSnapshot(t *testing.T) {
	snapshot := Snapshot()
	if snapshot.Timestamp.IsZero() {
		t.Fatal("snapshot timestamp is zero")
	}
	if snapshot.Go.HeapSysBytes == 0 || snapshot.Go.SysBytes == 0 {
		t.Fatalf("Go memory statistics are empty: %+v", snapshot.Go)
	}
	if snapshot.Process.RSSBytes == 0 {
		t.Fatalf("process RSS is empty: %+v", snapshot.Process)
	}
	if snapshot.Terminal.OutputBufferLimitBytes == 0 {
		t.Fatalf("terminal parser metrics are empty: %+v", snapshot.Terminal)
	}
	if _, err := json.Marshal(snapshot); err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
}

func TestReadLimitFile(t *testing.T) {
	path := t.TempDir() + "/memory.max"
	if err := os.WriteFile(path, []byte("max\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	limit, unlimited, err := readLimitFile(path)
	if err != nil || limit != 0 || !unlimited {
		t.Fatalf("read unlimited value = %d, %t, %v", limit, unlimited, err)
	}
	if err := os.WriteFile(path, []byte("1048576\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	limit, unlimited, err = readLimitFile(path)
	if err != nil || limit != 1048576 || unlimited {
		t.Fatalf("read numeric value = %d, %t, %v", limit, unlimited, err)
	}
}
