package webproxy

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVideoChunksAreOrderedAndIdempotent(t *testing.T) {
	r := &webRecording{dir: t.TempDir(), format: "mp4", startedAt: time.Now()}
	header := []byte{0, 0, 0, 12, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	for _, sequence := range []int{0, 0} {
		if count, err := r.addVideoChunk(sequence, header, false); err != nil || count != 1 {
			t.Fatalf("chunk count=%d error=%v", count, err)
		}
	}
	if _, err := r.addVideoChunk(2, []byte("out of order"), false); err == nil {
		t.Fatal("accepted an out-of-order chunk")
	}
	if _, err := r.addVideoChunk(0, []byte("changed retry"), false); err == nil {
		t.Fatal("accepted a retry with changed data")
	}
	if _, err := r.addVideoChunk(1, []byte("payload"), true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(r.dir, "video.mp4"))
	if err != nil || !bytes.Equal(data, append(header, []byte("payload")...)) || !r.resized {
		t.Fatalf("invalid video data or resize metadata: %v", err)
	}
}

func TestNativeVideoFinalization(t *testing.T) {
	input, ffmpeg := os.Getenv("KOKO_TEST_VIDEO"), os.Getenv("KOKO_TEST_FFMPEG")
	if input == "" || ffmpeg == "" {
		t.Skip("set KOKO_TEST_VIDEO and KOKO_TEST_FFMPEG for native video finalization")
	}
	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, resized := range []bool{false, true} {
		m, err := newRecordingManager(t.TempDir(), ffmpeg)
		if err != nil {
			t.Fatal(err)
		}
		r, err := m.start("01234567-89ab-4cde-8fab-0123456789ab", "https://asset.test/", 640, 360)
		if err != nil {
			t.Fatal(err)
		}
		r.format = filepath.Ext(input)[1:]
		if _, err := r.addVideoChunk(0, data, resized); err != nil {
			t.Fatal(err)
		}
		result, err := m.finish(context.Background(), r.id, 4000)
		if err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(result.Path); err != nil || info.Size() == 0 || result.ChunkCount != 1 {
			t.Fatalf("missing MP4: %v", err)
		}
	}
}
