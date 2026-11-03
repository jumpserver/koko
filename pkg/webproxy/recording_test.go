package webproxy

import (
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordingWithSlimFFmpeg(t *testing.T) {
	ffmpeg := os.Getenv("KOKO_TEST_FFMPEG")
	if ffmpeg == "" {
		t.Skip("set KOKO_TEST_FFMPEG to the built slim FFmpeg executable")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "frames"), 0700); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"00000001.jpg", "00000002.jpg"} {
		frame := image.NewRGBA(image.Rect(0, 0, 65, 49))
		for y := 0; y < 49; y++ {
			for x := 0; x < 65; x++ {
				frame.Set(x, y, color.RGBA{R: uint8(x*3 + i*20), G: uint8(y * 5), B: 100, A: 255})
			}
		}
		f, err := os.Create(filepath.Join(dir, "frames", name))
		if err != nil {
			t.Fatal(err)
		}
		err = jpeg.Encode(f, frame, nil)
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	manager := &recordingManager{ffmpegPath: ffmpeg}
	recording := &webRecording{dir: dir, lastTimestamp: 1000, frames: []recordedFrame{{Name: "00000001.jpg", TimestampMS: 0}, {Name: "00000002.jpg", TimestampMS: 400}}}
	if err := manager.encodeMP4(context.Background(), recording); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "recording.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	moov, mdat := -1, -1
	for offset := 0; offset+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		kind := string(data[offset+4 : offset+8])
		if size < 8 || size > len(data)-offset {
			t.Fatal("invalid MP4 box")
		}
		if kind == "moov" {
			moov = offset
		}
		if kind == "mdat" {
			mdat = offset
		}
		offset += size
	}
	if moov < 0 || mdat < 0 || moov > mdat {
		t.Fatal("recording is missing MP4 faststart metadata or video data")
	}
}
