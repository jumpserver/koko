//go:build linux

package proxy

import (
	"bytes"
	"os"
	"testing"
)

func TestReplayCacheWriter(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "replay-*.cast")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	writer := newReplayWriter(file, "test")
	writer.releaseEvery = int64(os.Getpagesize() * 2)
	data := bytes.Repeat([]byte("x"), os.Getpagesize()*3)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if writer.released != int64(len(data)) {
		t.Fatalf("released %d bytes, want %d", writer.released, len(data))
	}
	got, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("replay changed after cache release")
	}
}
